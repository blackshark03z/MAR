package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"mar/internal/domain"
)

var (
	ErrEffectIntentConflict = errors.New("operation id already belongs to a different effect intent")
	ErrEffectStateConflict  = errors.New("effect state transition precondition failed")
	ErrEffectReconcile      = errors.New("effect was dispatched but not durably observed; reconciliation is required before retry")
)

func (s *SQLite) PrepareEffect(ctx context.Context, record domain.EffectRecord) (domain.EffectRecord, bool, error) {
	payload, err := record.Intent.CanonicalJSON()
	if err != nil {
		return domain.EffectRecord{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.EffectRecord{}, false, err
	}
	defer tx.Rollback()

	existing, err := getEffectWithQueryer(ctx, tx, record.Intent.OperationID)
	if err == nil {
		if existing.IntentHash != record.IntentHash {
			return domain.EffectRecord{}, false, ErrEffectIntentConflict
		}
		// Existing uncertain/observed effects must remain readable after the
		// originating attempt is fenced so recovery can reconcile them. A
		// PREPARED effect is still dispatch-capable, so it continues to require
		// current attempt authority.
		if existing.State == domain.EffectPrepared {
			if err := validateAttemptAuthorityTx(ctx, tx, record.Intent.TaskID, record.Intent.AttemptID, record.Intent.RunEpoch); err != nil {
				return domain.EffectRecord{}, false, err
			}
		}
		if err := tx.Commit(); err != nil {
			return domain.EffectRecord{}, false, err
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return domain.EffectRecord{}, false, err
	}

	if err := validateAttemptAuthorityTx(ctx, tx, record.Intent.TaskID, record.Intent.AttemptID, record.Intent.RunEpoch); err != nil {
		return domain.EffectRecord{}, false, err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO effect_intents(
    operation_id, task_id, attempt_id, run_epoch, effect_type, intent_hash, intent_json,
    state, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Intent.OperationID, record.Intent.TaskID, record.Intent.AttemptID, record.Intent.RunEpoch,
		string(record.Intent.Type), record.IntentHash, payload, string(domain.EffectPrepared),
		record.CreatedAt.UTC().Format(time.RFC3339Nano), record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return domain.EffectRecord{}, false, fmt.Errorf("insert effect intent: %w", err)
	}
	record.State = domain.EffectPrepared
	if err := tx.Commit(); err != nil {
		return domain.EffectRecord{}, false, err
	}
	return record, true, nil
}

func (s *SQLite) GetEffect(ctx context.Context, operationID string) (domain.EffectRecord, error) {
	return getEffectWithQueryer(ctx, s.db, operationID)
}

// MarkEffectDispatched must be committed before the caller starts the physical
// side effect. After this point a crash creates a reconciliation obligation.
func (s *SQLite) MarkEffectDispatched(ctx context.Context, operationID, taskID, attemptID string, runEpoch int64, now time.Time) (domain.EffectRecord, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.EffectRecord{}, err
	}
	defer tx.Rollback()
	if err := validateAttemptAuthorityTx(ctx, tx, taskID, attemptID, runEpoch); err != nil {
		return domain.EffectRecord{}, err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `
UPDATE effect_intents
SET state = ?, dispatched_at = ?, updated_at = ?
WHERE operation_id = ? AND task_id = ? AND attempt_id = ? AND run_epoch = ? AND state = ?`,
		string(domain.EffectDispatched), stamp, stamp,
		operationID, taskID, attemptID, runEpoch, string(domain.EffectPrepared),
	)
	if err != nil {
		return domain.EffectRecord{}, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.EffectRecord{}, ErrEffectStateConflict
	}
	record, err := getEffectWithQueryer(ctx, tx, operationID)
	if err != nil {
		return domain.EffectRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.EffectRecord{}, err
	}
	return record, nil
}

// ObserveEffect is a recovery/orchestrator operation and intentionally does not
// require the original attempt to remain ACTIVE. A stale attempt's already-
// dispatched effect must still be reconcilable after it is fenced.
func (s *SQLite) ObserveEffect(ctx context.Context, operationID string, outcome domain.ObservationOutcome, result json.RawMessage, now time.Time) (domain.EffectRecord, error) {
	if outcome != domain.OutcomeApplied && outcome != domain.OutcomeNotApplied {
		return domain.EffectRecord{}, errors.New("valid observation outcome is required")
	}
	if len(result) > 0 && !json.Valid(result) {
		return domain.EffectRecord{}, errors.New("observed result must be valid JSON")
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
UPDATE effect_intents
SET state = ?, observation_outcome = ?, observed_result = ?, observed_at = ?, updated_at = ?
WHERE operation_id = ? AND state = ?`,
		string(domain.EffectObserved), string(outcome), nullableBytes(result), stamp, stamp,
		operationID, string(domain.EffectDispatched),
	)
	if err != nil {
		return domain.EffectRecord{}, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.EffectRecord{}, ErrEffectStateConflict
	}
	return s.GetEffect(ctx, operationID)
}

// RearmNotApplied is the only path back to PREPARED. It is explicit proof that
// reconciliation found the physical side effect did not occur, so retry is safe.
func (s *SQLite) RearmNotApplied(ctx context.Context, operationID string, now time.Time) (domain.EffectRecord, error) {
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
UPDATE effect_intents
SET state = ?, observation_outcome = '', observed_result = NULL,
    dispatched_at = NULL, observed_at = NULL,
    reconciliation_count = reconciliation_count + 1, updated_at = ?
WHERE operation_id = ? AND state = ? AND observation_outcome = ?`,
		string(domain.EffectPrepared), stamp, operationID, string(domain.EffectObserved), string(domain.OutcomeNotApplied),
	)
	if err != nil {
		return domain.EffectRecord{}, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.EffectRecord{}, ErrEffectStateConflict
	}
	return s.GetEffect(ctx, operationID)
}

func validateAttemptAuthorityTx(ctx context.Context, tx *sql.Tx, taskID, attemptID string, epoch int64) error {
	var currentEpoch int64
	var authority string
	err := tx.QueryRowContext(ctx, `
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

func getEffectWithQueryer(ctx context.Context, q queryer, operationID string) (domain.EffectRecord, error) {
	var record domain.EffectRecord
	var intentJSON []byte
	var state, outcome, created, updated string
	var result []byte
	var dispatched, observed sql.NullString
	err := q.QueryRowContext(ctx, `
SELECT operation_id, task_id, attempt_id, run_epoch, effect_type, intent_hash, intent_json,
       state, observation_outcome, observed_result, reconciliation_count,
       created_at, updated_at, dispatched_at, observed_at
FROM effect_intents WHERE operation_id = ?`, operationID).Scan(
		&record.Intent.OperationID, &record.Intent.TaskID, &record.Intent.AttemptID, &record.Intent.RunEpoch,
		&record.Intent.Type, &record.IntentHash, &intentJSON,
		&state, &outcome, &result, &record.ReconciliationCount,
		&created, &updated, &dispatched, &observed,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.EffectRecord{}, ErrNotFound
	}
	if err != nil {
		return domain.EffectRecord{}, err
	}
	if err := json.Unmarshal(intentJSON, &record.Intent); err != nil {
		return domain.EffectRecord{}, err
	}
	record.State = domain.EffectState(state)
	record.ObservationOutcome = domain.ObservationOutcome(outcome)
	if len(result) > 0 {
		record.ObservedResult = append(json.RawMessage(nil), result...)
	}
	var parseErr error
	if record.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created); parseErr != nil {
		return domain.EffectRecord{}, parseErr
	}
	if record.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated); parseErr != nil {
		return domain.EffectRecord{}, parseErr
	}
	if dispatched.Valid {
		tm, err := time.Parse(time.RFC3339Nano, dispatched.String)
		if err != nil {
			return domain.EffectRecord{}, err
		}
		record.DispatchedAt = &tm
	}
	if observed.Valid {
		tm, err := time.Parse(time.RFC3339Nano, observed.String)
		if err != nil {
			return domain.EffectRecord{}, err
		}
		record.ObservedAt = &tm
	}
	return record, nil
}

func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
