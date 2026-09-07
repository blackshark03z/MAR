package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"mar/internal/domain"
)

func (s *SQLite) ListRecentTasks(ctx context.Context, limit int) ([]domain.Task, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, idempotency_key, contract_json, contract_hash, state, run_epoch, created_at, updated_at
FROM tasks ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent tasks: %w", err)
	}
	defer rows.Close()
	result := make([]domain.Task, 0, limit)
	for rows.Next() {
		var task domain.Task
		var payload []byte
		var state, created, updated string
		if err := rows.Scan(&task.ID, &task.IdempotencyKey, &payload, &task.ContractHash, &state, &task.RunEpoch, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan recent task: %w", err)
		}
		if err := json.Unmarshal(payload, &task.Contract); err != nil {
			return nil, fmt.Errorf("decode recent task contract: %w", err)
		}
		task.State = domain.TaskState(state)
		if task.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		if task.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}
