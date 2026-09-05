package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"mar/internal/domain"
)

var (
	ErrControlIdempotencyConflict = errors.New("control idempotency key already belongs to different control content")
	ErrControlIntegrity           = errors.New("task control integrity is invalid")
)

func (s *SQLite) PublishTaskControl(ctx context.Context, controlID, taskID, idempotencyKey string, kind domain.ControlKind, payload json.RawMessage, allowedStates []domain.TaskState, now time.Time) (domain.TaskControl, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	defer tx.Rollback()

	if existing, ok, err := existingTaskControl(ctx, tx, taskID, idempotencyKey, kind, payload); err != nil || ok {
		return existing, false, err
	}
	state, _, err := taskControlState(ctx, tx, taskID)
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	if !containsTaskState(allowedStates, state) {
		return domain.TaskControl{}, false, ErrStateConflict
	}
	control, err := insertTaskControlTx(ctx, tx, controlID, taskID, idempotencyKey, kind, payload, now)
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskControl{}, false, err
	}
	return control, true, nil
}

func (s *SQLite) PublishTaskInput(ctx context.Context, controlID, taskID, idempotencyKey string, payload json.RawMessage, now time.Time) (domain.TaskControl, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	defer tx.Rollback()

	if existing, ok, err := existingTaskControl(ctx, tx, taskID, idempotencyKey, domain.ControlInput, payload); err != nil || ok {
		return existing, false, err
	}
	state, epoch, err := taskControlState(ctx, tx, taskID)
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	if state != domain.TaskInputRequired || epoch <= 0 {
		return domain.TaskControl{}, false, ErrStateConflict
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_attempts WHERE task_id = ? AND run_epoch = ? AND authority_state = ?`, taskID, epoch, string(domain.AttemptActive)).Scan(&active); err != nil {
		return domain.TaskControl{}, false, err
	}
	if active != 1 {
		return domain.TaskControl{}, false, ErrStaleAttempt
	}
	control, err := insertTaskControlTx(ctx, tx, controlID, taskID, idempotencyKey, domain.ControlInput, payload, now)
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ? AND run_epoch = ?`,
		string(domain.TaskRunning), stamp, taskID, string(domain.TaskInputRequired), epoch)
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.TaskControl{}, false, ErrStateConflict
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskControl{}, false, err
	}
	return control, true, nil
}

// RequestTaskCancellation records the request durably before changing authority.
// Pre-attempt tasks can become CANCELLED immediately. A running task is only
// logically fenced here; physical process termination remains supervisor-owned.
func (s *SQLite) RequestTaskCancellation(ctx context.Context, controlID, taskID, idempotencyKey string, payload json.RawMessage, now time.Time) (domain.TaskControl, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	defer tx.Rollback()

	if existing, ok, err := existingTaskControl(ctx, tx, taskID, idempotencyKey, domain.ControlCancel, payload); err != nil || ok {
		return existing, false, err
	}
	state, epoch, err := taskControlState(ctx, tx, taskID)
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	if state == domain.TaskComplete || state == domain.TaskFailed || state == domain.TaskCancelled {
		return domain.TaskControl{}, false, ErrStateConflict
	}
	control, err := insertTaskControlTx(ctx, tx, controlID, taskID, idempotencyKey, domain.ControlCancel, payload, now)
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	switch state {
	case domain.TaskSubmitted, domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady:
		res, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`, string(domain.TaskCancelled), stamp, taskID, string(state))
		if err != nil {
			return domain.TaskControl{}, false, err
		}
		rows, _ := res.RowsAffected()
		if rows != 1 {
			return domain.TaskControl{}, false, ErrStateConflict
		}
	default:
		res, err := tx.ExecContext(ctx, `
UPDATE execution_attempts SET authority_state = ?, heartbeat_at = ?
WHERE task_id = ? AND run_epoch = ? AND authority_state = ?`,
			string(domain.AttemptLogicallyFenced), stamp, taskID, epoch, string(domain.AttemptActive))
		if err != nil {
			return domain.TaskControl{}, false, err
		}
		rows, _ := res.RowsAffected()
		if rows == 0 {
			var unsafe int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_attempts WHERE task_id = ? AND authority_state != ?`, taskID, string(domain.AttemptPhysicallyTerminated)).Scan(&unsafe); err != nil {
				return domain.TaskControl{}, false, err
			}
			if unsafe == 0 {
				res, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`, string(domain.TaskCancelled), stamp, taskID, string(state))
				if err != nil {
					return domain.TaskControl{}, false, err
				}
				updated, _ := res.RowsAffected()
				if updated != 1 {
					return domain.TaskControl{}, false, ErrStateConflict
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskControl{}, false, err
	}
	return control, true, nil
}

func (s *SQLite) FinalizeTaskCancellation(ctx context.Context, taskID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, taskID).Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if domain.TaskState(state) == domain.TaskCancelled {
		return tx.Commit()
	}
	var cancelCount, unsafe int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_controls WHERE task_id = ? AND kind = ?`, taskID, string(domain.ControlCancel)).Scan(&cancelCount); err != nil {
		return err
	}
	if cancelCount == 0 {
		return ErrStateConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_attempts WHERE task_id = ? AND authority_state != ?`, taskID, string(domain.AttemptPhysicallyTerminated)).Scan(&unsafe); err != nil {
		return err
	}
	if unsafe != 0 {
		return ErrPhysicalFenceRequired
	}
	res, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`, string(domain.TaskCancelled), now.UTC().Format(time.RFC3339Nano), taskID, state)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	return tx.Commit()
}

func (s *SQLite) ControlsSince(ctx context.Context, taskID string, afterVersion int64, limit int) ([]domain.TaskControl, error) {
	if limit <= 0 || limit > 64 {
		limit = 32
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT control_id, task_id, version, idempotency_key, kind, payload_json, integrity_hash, created_at
FROM task_controls WHERE task_id = ? AND version > ? ORDER BY version ASC LIMIT ?`, taskID, afterVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	controls := make([]domain.TaskControl, 0)
	for rows.Next() {
		control, err := scanTaskControl(rows)
		if err != nil {
			return nil, err
		}
		controls = append(controls, control)
	}
	return controls, rows.Err()
}

func (s *SQLite) LatestTaskControl(ctx context.Context, taskID string) (domain.TaskControl, bool, error) {
	return latestTaskControlQuery(ctx, s.db, `
SELECT control_id, task_id, version, idempotency_key, kind, payload_json, integrity_hash, created_at
FROM task_controls WHERE task_id = ? ORDER BY version DESC LIMIT 1`, taskID)
}

func (s *SQLite) LatestTaskControlByKind(ctx context.Context, taskID string, kind domain.ControlKind) (domain.TaskControl, bool, error) {
	return latestTaskControlQuery(ctx, s.db, `
SELECT control_id, task_id, version, idempotency_key, kind, payload_json, integrity_hash, created_at
FROM task_controls WHERE task_id = ? AND kind = ? ORDER BY version DESC LIMIT 1`, taskID, string(kind))
}

func latestTaskControlQuery(ctx context.Context, q queryer, query string, args ...any) (domain.TaskControl, bool, error) {
	row := q.QueryRowContext(ctx, query, args...)
	control, err := scanTaskControl(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskControl{}, false, nil
	}
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	return control, true, nil
}

func (s *SQLite) CurrentAttemptByTask(ctx context.Context, taskID string) (domain.ExecutionAttempt, bool, error) {
	var attemptID string
	err := s.db.QueryRowContext(ctx, `SELECT attempt_id FROM execution_attempts WHERE task_id = ? ORDER BY run_epoch DESC LIMIT 1`, taskID).Scan(&attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ExecutionAttempt{}, false, nil
	}
	if err != nil {
		return domain.ExecutionAttempt{}, false, err
	}
	attempt, err := s.GetAttempt(ctx, attemptID)
	if err != nil {
		return domain.ExecutionAttempt{}, false, err
	}
	return attempt, true, nil
}

func existingTaskControl(ctx context.Context, q queryer, taskID, idempotencyKey string, kind domain.ControlKind, payload json.RawMessage) (domain.TaskControl, bool, error) {
	row := q.QueryRowContext(ctx, `
SELECT control_id, task_id, version, idempotency_key, kind, payload_json, integrity_hash, created_at
FROM task_controls WHERE task_id = ? AND idempotency_key = ?`, taskID, idempotencyKey)
	control, err := scanTaskControl(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskControl{}, false, nil
	}
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	if control.Kind != kind || !bytes.Equal(control.Payload, payload) {
		return domain.TaskControl{}, false, ErrControlIdempotencyConflict
	}
	return control, true, nil
}

func taskControlState(ctx context.Context, q queryer, taskID string) (domain.TaskState, int64, error) {
	var state string
	var epoch int64
	if err := q.QueryRowContext(ctx, `SELECT state, run_epoch FROM tasks WHERE id = ?`, taskID).Scan(&state, &epoch); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, ErrNotFound
		}
		return "", 0, err
	}
	return domain.TaskState(state), epoch, nil
}

func insertTaskControlTx(ctx context.Context, tx *sql.Tx, controlID, taskID, idempotencyKey string, kind domain.ControlKind, payload json.RawMessage, now time.Time) (domain.TaskControl, error) {
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM task_controls WHERE task_id = ?`, taskID).Scan(&version); err != nil {
		return domain.TaskControl{}, err
	}
	control := domain.TaskControl{ID: controlID, TaskID: taskID, Version: version, IdempotencyKey: idempotencyKey, Kind: kind, Payload: append(json.RawMessage(nil), payload...), CreatedAt: now.UTC()}
	var err error
	control.IntegrityHash, err = control.IntegrityDigest()
	if err != nil {
		return domain.TaskControl{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO task_controls(control_id, task_id, version, idempotency_key, kind, payload_json, integrity_hash, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, control.ID, control.TaskID, control.Version, control.IdempotencyKey, string(control.Kind), []byte(control.Payload), control.IntegrityHash, control.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return domain.TaskControl{}, fmt.Errorf("insert task control: %w", err)
	}
	return control, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanTaskControl(row rowScanner) (domain.TaskControl, error) {
	var control domain.TaskControl
	var kind, created string
	if err := row.Scan(&control.ID, &control.TaskID, &control.Version, &control.IdempotencyKey, &kind, &control.Payload, &control.IntegrityHash, &created); err != nil {
		return domain.TaskControl{}, err
	}
	control.Kind = domain.ControlKind(kind)
	var err error
	control.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.TaskControl{}, err
	}
	if !control.IntegrityValid() {
		return domain.TaskControl{}, ErrControlIntegrity
	}
	return control, nil
}

func containsTaskState(states []domain.TaskState, target domain.TaskState) bool {
	for _, state := range states {
		if state == target {
			return true
		}
	}
	return false
}
