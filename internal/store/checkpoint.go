package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mar/internal/domain"
)

func (s *SQLite) PublishCheckpoint(ctx context.Context, checkpointID, taskID, attemptID string, runEpoch int64, currentRevision string, payload domain.SemanticCheckpointPayload, now time.Time) (domain.SemanticCheckpoint, error) {
	if strings.TrimSpace(checkpointID) == "" || strings.TrimSpace(taskID) == "" || strings.TrimSpace(attemptID) == "" {
		return domain.SemanticCheckpoint{}, errors.New("checkpoint id, task id and attempt id are required")
	}
	if runEpoch <= 0 || strings.TrimSpace(currentRevision) == "" {
		return domain.SemanticCheckpoint{}, errors.New("positive run epoch and current revision are required")
	}
	if err := payload.Validate(); err != nil {
		return domain.SemanticCheckpoint{}, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.SemanticCheckpoint{}, fmt.Errorf("begin checkpoint transaction: %w", err)
	}
	defer tx.Rollback()
	if err := validateAttemptAuthorityTx(ctx, tx, taskID, attemptID, runEpoch); err != nil {
		return domain.SemanticCheckpoint{}, err
	}

	var contractHash string
	var contractJSON []byte
	if err := tx.QueryRowContext(ctx, `SELECT contract_hash, contract_json FROM tasks WHERE id = ?`, taskID).Scan(&contractHash, &contractJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SemanticCheckpoint{}, ErrNotFound
		}
		return domain.SemanticCheckpoint{}, fmt.Errorf("read task for checkpoint: %w", err)
	}
	var contract domain.GoalContract
	if err := json.Unmarshal(contractJSON, &contract); err != nil {
		return domain.SemanticCheckpoint{}, fmt.Errorf("decode checkpoint goal contract: %w", err)
	}

	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM semantic_checkpoints WHERE task_id = ?`, taskID).Scan(&version); err != nil {
		return domain.SemanticCheckpoint{}, fmt.Errorf("allocate checkpoint version: %w", err)
	}
	checkpoint := domain.SemanticCheckpoint{
		ID:              checkpointID,
		TaskID:          taskID,
		AttemptID:       attemptID,
		RunEpoch:        runEpoch,
		Version:         version,
		GoalHash:        contractHash,
		BaseRevision:    contract.BaseRevision,
		CurrentRevision: strings.TrimSpace(currentRevision),
		Payload:         payload,
		CreatedAt:       now.UTC(),
	}
	if err := checkpoint.ValidateIdentity(); err != nil {
		return domain.SemanticCheckpoint{}, err
	}
	checkpoint.IntegrityHash, err = checkpoint.IntegrityDigest()
	if err != nil {
		return domain.SemanticCheckpoint{}, fmt.Errorf("hash checkpoint: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return domain.SemanticCheckpoint{}, fmt.Errorf("encode checkpoint payload: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO semantic_checkpoints(
    checkpoint_id, task_id, attempt_id, run_epoch, version, goal_hash,
    base_revision, current_revision, payload_json, integrity_hash, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		checkpoint.ID, checkpoint.TaskID, checkpoint.AttemptID, checkpoint.RunEpoch, checkpoint.Version,
		checkpoint.GoalHash, checkpoint.BaseRevision, checkpoint.CurrentRevision, payloadJSON,
		checkpoint.IntegrityHash, checkpoint.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return domain.SemanticCheckpoint{}, fmt.Errorf("insert semantic checkpoint: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.SemanticCheckpoint{}, fmt.Errorf("commit semantic checkpoint: %w", err)
	}
	return checkpoint, nil
}

func (s *SQLite) LatestValidCheckpoint(ctx context.Context, taskID string) (domain.SemanticCheckpoint, bool, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return domain.SemanticCheckpoint{}, false, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT c.checkpoint_id, c.task_id, c.attempt_id, c.run_epoch, c.version, c.goal_hash,
       c.base_revision, c.current_revision, c.payload_json, c.integrity_hash, c.created_at,
       a.task_id, a.run_epoch
FROM semantic_checkpoints c
JOIN execution_attempts a ON a.attempt_id = c.attempt_id
WHERE c.task_id = ?
ORDER BY c.version DESC`, taskID)
	if err != nil {
		return domain.SemanticCheckpoint{}, false, fmt.Errorf("list semantic checkpoints: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var checkpoint domain.SemanticCheckpoint
		var payloadJSON []byte
		var created string
		var attemptTaskID string
		var attemptEpoch int64
		if err := rows.Scan(
			&checkpoint.ID, &checkpoint.TaskID, &checkpoint.AttemptID, &checkpoint.RunEpoch, &checkpoint.Version,
			&checkpoint.GoalHash, &checkpoint.BaseRevision, &checkpoint.CurrentRevision, &payloadJSON,
			&checkpoint.IntegrityHash, &created, &attemptTaskID, &attemptEpoch,
		); err != nil {
			return domain.SemanticCheckpoint{}, false, fmt.Errorf("scan semantic checkpoint: %w", err)
		}
		if attemptTaskID != checkpoint.TaskID || attemptEpoch != checkpoint.RunEpoch || checkpoint.RunEpoch > task.RunEpoch {
			continue
		}
		if checkpoint.GoalHash != task.ContractHash || checkpoint.BaseRevision != task.Contract.BaseRevision {
			continue
		}
		if err := json.Unmarshal(payloadJSON, &checkpoint.Payload); err != nil {
			continue
		}
		checkpoint.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil || !checkpoint.IntegrityValid() {
			continue
		}
		return checkpoint, true, nil
	}
	if err := rows.Err(); err != nil {
		return domain.SemanticCheckpoint{}, false, fmt.Errorf("iterate semantic checkpoints: %w", err)
	}
	return domain.SemanticCheckpoint{}, false, nil
}
