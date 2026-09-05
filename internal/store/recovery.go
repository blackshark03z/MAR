package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"mar/internal/domain"
)

// RequirePhysicalRecovery is the fail-closed daemon-restart path when MAR has
// durable evidence of a mutation-capable attempt but no live supervisor handle
// from which it can obtain a fresh OS termination proof. Logical authority is
// revoked and the task is BLOCKED; the attempt intentionally remains not
// physically terminated, so replacement admission is still impossible.
func (s *SQLite) RequirePhysicalRecovery(ctx context.Context, taskID, attemptID string, epoch int64, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var state string
	var currentEpoch int64
	if err := tx.QueryRowContext(ctx, `SELECT state, run_epoch FROM tasks WHERE id = ?`, taskID).Scan(&state, &currentEpoch); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if currentEpoch != epoch {
		return ErrStaleAttempt
	}
	switch domain.TaskState(state) {
	case domain.TaskComplete, domain.TaskFailed, domain.TaskCancelled:
		return ErrStateConflict
	}

	var authority string
	if err := tx.QueryRowContext(ctx, `
SELECT authority_state FROM execution_attempts
WHERE attempt_id = ? AND task_id = ? AND run_epoch = ?`, attemptID, taskID, epoch).Scan(&authority); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if domain.AttemptAuthorityState(authority) == domain.AttemptPhysicallyTerminated {
		return ErrStateConflict
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	if domain.AttemptAuthorityState(authority) == domain.AttemptActive {
		res, err := tx.ExecContext(ctx, `
UPDATE execution_attempts
SET authority_state = ?, heartbeat_at = ?
WHERE attempt_id = ? AND task_id = ? AND run_epoch = ? AND authority_state = ?`,
			string(domain.AttemptLogicallyFenced), stamp, attemptID, taskID, epoch, string(domain.AttemptActive))
		if err != nil {
			return err
		}
		rows, _ := res.RowsAffected()
		if rows != 1 {
			return ErrStaleAttempt
		}
	}
	res, err := tx.ExecContext(ctx, `
UPDATE tasks SET state = ?, updated_at = ?
WHERE id = ? AND run_epoch = ?`, string(domain.TaskBlocked), stamp, taskID, epoch)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	return tx.Commit()
}
