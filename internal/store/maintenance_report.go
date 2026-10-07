package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type MaintenanceCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type MaintenanceWebTurnBytes struct {
	TaskState     string `json:"task_state"`
	Turns         int64  `json:"turns"`
	RequestBytes  int64  `json:"request_bytes"`
	ResponseBytes int64  `json:"response_bytes"`
}

type MaintenanceDBStats struct {
	TaskStates                   []MaintenanceCount        `json:"task_states"`
	TerminalStatuses14d          []MaintenanceCount        `json:"terminal_statuses"`
	WebTurnsByTaskState          []MaintenanceWebTurnBytes `json:"web_turns_by_task_state"`
	WebTurnRequestBytes          int64                     `json:"web_turn_request_bytes"`
	WebTurnResponseBytes         int64                     `json:"web_turn_response_bytes"`
	CompactableTerminalTurnBytes int64                     `json:"compactable_terminal_turn_bytes"`
}

func (s *SQLite) MaintenanceDBStats(ctx context.Context, since time.Time) (MaintenanceDBStats, error) {
	result := MaintenanceDBStats{}

	rows, err := s.db.QueryContext(ctx, `
SELECT state, COUNT(*)
FROM tasks
GROUP BY state
ORDER BY COUNT(*) DESC, state
`)
	if err != nil {
		return result, fmt.Errorf("query maintenance task states: %w", err)
	}
	for rows.Next() {
		var item MaintenanceCount
		if err := rows.Scan(&item.Name, &item.Count); err != nil {
			rows.Close()
			return result, fmt.Errorf("scan maintenance task state: %w", err)
		}
		result.TaskStates = append(result.TaskStates, item)
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT terminal_status, COUNT(*)
FROM execution_attempts
WHERE started_at >= ? AND TRIM(terminal_status) <> ''
GROUP BY terminal_status
ORDER BY COUNT(*) DESC, terminal_status
`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return result, fmt.Errorf("query maintenance terminal statuses: %w", err)
	}
	for rows.Next() {
		var item MaintenanceCount
		if err := rows.Scan(&item.Name, &item.Count); err != nil {
			rows.Close()
			return result, fmt.Errorf("scan maintenance terminal status: %w", err)
		}
		result.TerminalStatuses14d = append(result.TerminalStatuses14d, item)
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT t.state,
       COUNT(w.turn_id),
       COALESCE(SUM(LENGTH(w.request_json)), 0),
       COALESCE(SUM(LENGTH(w.response_json)), 0)
FROM tasks t
LEFT JOIN web_turns w ON w.task_id = t.id
GROUP BY t.state
ORDER BY COALESCE(SUM(LENGTH(w.request_json)), 0) DESC, t.state
`)
	if err != nil {
		return result, fmt.Errorf("query maintenance web turn bytes: %w", err)
	}
	for rows.Next() {
		var item MaintenanceWebTurnBytes
		if err := rows.Scan(&item.TaskState, &item.Turns, &item.RequestBytes, &item.ResponseBytes); err != nil {
			rows.Close()
			return result, fmt.Errorf("scan maintenance web turn bytes: %w", err)
		}
		result.WebTurnsByTaskState = append(result.WebTurnsByTaskState, item)
		result.WebTurnRequestBytes += item.RequestBytes
		result.WebTurnResponseBytes += item.ResponseBytes
		switch strings.ToUpper(strings.TrimSpace(item.TaskState)) {
		case "COMPLETE", "CANCELLED":
			result.CompactableTerminalTurnBytes += item.RequestBytes + item.ResponseBytes
		}
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	return result, nil
}
