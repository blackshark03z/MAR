package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"mar/internal/domain"
)

type ProjectScheduleState struct {
	ProjectID        string
	LastDispatchedAt *time.Time
	DispatchCount    int64
}

func (s *SQLite) ListWaitingTasks(ctx context.Context) ([]domain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, idempotency_key, contract_json, contract_hash, state, run_epoch, created_at, updated_at
FROM tasks WHERE state = ? ORDER BY updated_at ASC, id ASC`, string(domain.TaskWaitingResource))
	if err != nil {
		return nil, fmt.Errorf("list waiting tasks: %w", err)
	}
	defer rows.Close()
	var tasks []domain.Task
	for rows.Next() {
		var task domain.Task
		var payload []byte
		var state, created, updated string
		if err := rows.Scan(&task.ID, &task.IdempotencyKey, &payload, &task.ContractHash, &state, &task.RunEpoch, &created, &updated); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &task.Contract); err != nil {
			return nil, fmt.Errorf("decode waiting goal contract: %w", err)
		}
		task.State = domain.TaskState(state)
		if task.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		if task.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *SQLite) ListProjectScheduleStates(ctx context.Context) (map[string]ProjectScheduleState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT project_id, last_dispatched_at, dispatch_count FROM project_scheduler_state`)
	if err != nil {
		return nil, fmt.Errorf("list project scheduler state: %w", err)
	}
	defer rows.Close()
	result := make(map[string]ProjectScheduleState)
	for rows.Next() {
		var state ProjectScheduleState
		var last sql.NullString
		if err := rows.Scan(&state.ProjectID, &last, &state.DispatchCount); err != nil {
			return nil, err
		}
		if last.Valid {
			tm, err := time.Parse(time.RFC3339Nano, last.String)
			if err != nil {
				return nil, err
			}
			state.LastDispatchedAt = &tm
		}
		result[state.ProjectID] = state
	}
	return result, rows.Err()
}

func (s *SQLite) RecordProjectDispatch(ctx context.Context, projectID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO project_scheduler_state(project_id, last_dispatched_at, dispatch_count)
VALUES (?, ?, 1)
ON CONFLICT(project_id) DO UPDATE SET
    last_dispatched_at = excluded.last_dispatched_at,
    dispatch_count = project_scheduler_state.dispatch_count + 1`,
		projectID, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record project dispatch: %w", err)
	}
	return nil
}
