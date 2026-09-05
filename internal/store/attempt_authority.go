package store

import (
	"context"
	"database/sql"
	"errors"

	"mar/internal/domain"
)

// ValidateAttemptAuthority is a read-only fencing check for MAR-mediated worker
// actions. It succeeds only while the referenced attempt is ACTIVE and its
// run_epoch is still the task's current epoch.
func (s *SQLite) ValidateAttemptAuthority(ctx context.Context, taskID, attemptID string, epoch int64) error {
	if s == nil || s.db == nil || taskID == "" || attemptID == "" || epoch <= 0 {
		return ErrStaleAttempt
	}
	var currentEpoch int64
	var authority string
	err := s.db.QueryRowContext(ctx, `
SELECT t.run_epoch, a.authority_state
FROM tasks t
JOIN execution_attempts a ON a.task_id = t.id
WHERE t.id = ? AND a.attempt_id = ? AND a.run_epoch = ?`, taskID, attemptID, epoch).Scan(&currentEpoch, &authority)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrStaleAttempt
	}
	if err != nil {
		return err
	}
	if currentEpoch != epoch || domain.AttemptAuthorityState(authority) != domain.AttemptActive {
		return ErrStaleAttempt
	}
	return nil
}
