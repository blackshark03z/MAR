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

var ErrIntegrationIntegrity = errors.New("integration attempt integrity is invalid")

func (s *SQLite) PrepareIntegrationAttempt(ctx context.Context, seed domain.IntegrationAttempt, now time.Time) (domain.IntegrationAttempt, error) {
	latest, ok, err := s.LatestTaskResult(ctx, seed.TaskID)
	if err != nil {
		return domain.IntegrationAttempt{}, err
	}
	if !ok || latest.ID != seed.TaskResultID || latest.Version != seed.TaskResultVersion || latest.FinalRevision != seed.TaskResultRevision || latest.FinalRevision != seed.CandidateRevision || latest.EvidenceID != seed.EvidenceID || latest.Verdict != domain.ResultVerified {
		return domain.IntegrationAttempt{}, ErrStateConflict
	}
	if _, err := s.GetVerificationEvidence(ctx, seed.EvidenceID); err != nil {
		return domain.IntegrationAttempt{}, err
	}
	if existing, ok, err := s.integrationAttemptByResult(ctx, seed.TaskID, seed.TaskResultID); err != nil || ok {
		return existing, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.IntegrationAttempt{}, err
	}
	defer tx.Rollback()

	var state, projectID string
	if err := tx.QueryRowContext(ctx, `SELECT state, project_id FROM tasks WHERE id = ?`, seed.TaskID).Scan(&state, &projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.IntegrationAttempt{}, ErrNotFound
		}
		return domain.IntegrationAttempt{}, err
	}
	if domain.TaskState(state) != domain.TaskVerified || projectID != seed.ProjectID {
		return domain.IntegrationAttempt{}, ErrStateConflict
	}
	var unsafeAttempts int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_attempts WHERE task_id = ? AND authority_state != ?`, seed.TaskID, string(domain.AttemptPhysicallyTerminated)).Scan(&unsafeAttempts); err != nil {
		return domain.IntegrationAttempt{}, err
	}
	if unsafeAttempts != 0 {
		return domain.IntegrationAttempt{}, ErrPhysicalFenceRequired
	}
	var workspaceBase, workspaceHead string
	if err := tx.QueryRowContext(ctx, `SELECT base_revision, head_revision FROM workspaces WHERE task_id = ? AND state = ?`, seed.TaskID, string(domain.WorkspaceReady)).Scan(&workspaceBase, &workspaceHead); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.IntegrationAttempt{}, ErrNotFound
		}
		return domain.IntegrationAttempt{}, err
	}
	if workspaceBase != seed.ExpectedHead || workspaceHead != seed.CandidateRevision {
		return domain.IntegrationAttempt{}, ErrStateConflict
	}
	var latestID string
	var latestVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT result_id, version FROM task_results WHERE task_id = ? ORDER BY version DESC LIMIT 1`, seed.TaskID).Scan(&latestID, &latestVersion); err != nil {
		return domain.IntegrationAttempt{}, err
	}
	if latestID != seed.TaskResultID || latestVersion != seed.TaskResultVersion {
		return domain.IntegrationAttempt{}, ErrStateConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM integration_attempts WHERE task_id = ?`, seed.TaskID).Scan(&seed.Version); err != nil {
		return domain.IntegrationAttempt{}, err
	}
	seed.Status = domain.IntegrationPrepared
	seed.ObservedHead = ""
	seed.Failure = ""
	seed.CreatedAt = now.UTC()
	seed.UpdatedAt = now.UTC()
	seed.IntegrityHash, err = seed.IntegrityDigest()
	if err != nil {
		return domain.IntegrationAttempt{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO integration_attempts(
    integration_attempt_id, task_id, version, project_id, expected_ref, expected_head,
    task_result_id, task_result_version, task_result_revision, candidate_revision, evidence_id,
    status, observed_head, failure, integrity_hash, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seed.ID, seed.TaskID, seed.Version, seed.ProjectID, seed.ExpectedRef, seed.ExpectedHead,
		seed.TaskResultID, seed.TaskResultVersion, seed.TaskResultRevision, seed.CandidateRevision, seed.EvidenceID,
		string(seed.Status), seed.ObservedHead, seed.Failure, seed.IntegrityHash,
		seed.CreatedAt.Format(time.RFC3339Nano), seed.UpdatedAt.Format(time.RFC3339Nano)); err != nil {
		return domain.IntegrationAttempt{}, fmt.Errorf("insert integration attempt: %w", err)
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`, string(domain.TaskReadyToIntegrate), stamp, seed.TaskID, string(domain.TaskVerified))
	if err != nil {
		return domain.IntegrationAttempt{}, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.IntegrationAttempt{}, ErrStateConflict
	}
	if err := tx.Commit(); err != nil {
		return domain.IntegrationAttempt{}, err
	}
	return seed, nil
}

func (s *SQLite) MarkIntegrationDispatched(ctx context.Context, attemptID string, now time.Time) (domain.IntegrationAttempt, error) {
	attempt, err := s.GetIntegrationAttempt(ctx, attemptID)
	if err != nil {
		return domain.IntegrationAttempt{}, err
	}
	if attempt.Status == domain.IntegrationDispatched || attempt.Status == domain.IntegrationComplete {
		return attempt, nil
	}
	if attempt.Status != domain.IntegrationPrepared {
		return domain.IntegrationAttempt{}, ErrStateConflict
	}
	attempt.Status = domain.IntegrationDispatched
	attempt.UpdatedAt = now.UTC()
	attempt.IntegrityHash, err = attempt.IntegrityDigest()
	if err != nil {
		return domain.IntegrationAttempt{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.IntegrationAttempt{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE integration_attempts SET status = ?, integrity_hash = ?, updated_at = ? WHERE integration_attempt_id = ? AND status = ?`,
		string(attempt.Status), attempt.IntegrityHash, attempt.UpdatedAt.Format(time.RFC3339Nano), attempt.ID, string(domain.IntegrationPrepared))
	if err != nil {
		return domain.IntegrationAttempt{}, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.IntegrationAttempt{}, ErrStateConflict
	}
	res, err = tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		string(domain.TaskIntegrating), attempt.UpdatedAt.Format(time.RFC3339Nano), attempt.TaskID, string(domain.TaskReadyToIntegrate))
	if err != nil {
		return domain.IntegrationAttempt{}, err
	}
	rows, _ = res.RowsAffected()
	if rows != 1 {
		return domain.IntegrationAttempt{}, ErrStateConflict
	}
	if err := tx.Commit(); err != nil {
		return domain.IntegrationAttempt{}, err
	}
	return attempt, nil
}

func (s *SQLite) FinalizeIntegrationApplied(ctx context.Context, attemptID, observedHead, newResultID string, now time.Time) (domain.TaskResult, error) {
	attempt, err := s.GetIntegrationAttempt(ctx, attemptID)
	if err != nil {
		return domain.TaskResult{}, err
	}
	latest, ok, err := s.LatestTaskResult(ctx, attempt.TaskID)
	if err != nil || !ok {
		if err == nil {
			err = ErrNotFound
		}
		return domain.TaskResult{}, err
	}
	if attempt.Status == domain.IntegrationComplete {
		return latest, nil
	}
	if attempt.Status != domain.IntegrationDispatched || observedHead != attempt.CandidateRevision || latest.ID != attempt.TaskResultID || latest.Version != attempt.TaskResultVersion {
		return domain.TaskResult{}, ErrStateConflict
	}
	if _, err := s.GetVerificationEvidence(ctx, attempt.EvidenceID); err != nil {
		return domain.TaskResult{}, err
	}

	integrated := latest
	integrated.ID = newResultID
	integrated.Version = latest.Version + 1
	integrated.IntegrationStatus = "INTEGRATED"
	integrated.PassFailEvidence = append(append([]string{}, latest.PassFailEvidence...), "integration:"+attempt.ID+":APPLIED:"+observedHead)
	integrated.UnresolvedRisks = append([]string{}, latest.UnresolvedRisks...)
	integrated.ChangedAreas = append([]string{}, latest.ChangedAreas...)
	integrated.VerificationExecuted = append([]string{}, latest.VerificationExecuted...)
	integrated.CreatedAt = now.UTC()
	integrated.IntegrityHash, err = integrated.IntegrityDigest()
	if err != nil {
		return domain.TaskResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.TaskResult{}, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM integration_attempts WHERE integration_attempt_id = ?`, attempt.ID).Scan(&status); err != nil {
		return domain.TaskResult{}, err
	}
	if domain.IntegrationStatus(status) != domain.IntegrationDispatched {
		return domain.TaskResult{}, ErrStateConflict
	}
	var taskState string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, attempt.TaskID).Scan(&taskState); err != nil {
		return domain.TaskResult{}, err
	}
	if domain.TaskState(taskState) != domain.TaskIntegrating {
		return domain.TaskResult{}, ErrStateConflict
	}
	var currentResultID string
	var currentResultVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT result_id, version FROM task_results WHERE task_id = ? ORDER BY version DESC LIMIT 1`, attempt.TaskID).Scan(&currentResultID, &currentResultVersion); err != nil {
		return domain.TaskResult{}, err
	}
	if currentResultID != latest.ID || currentResultVersion != latest.Version {
		return domain.TaskResult{}, ErrStateConflict
	}
	if err := insertIntegrationResultTx(ctx, tx, integrated); err != nil {
		return domain.TaskResult{}, err
	}
	attempt.Status = domain.IntegrationComplete
	attempt.ObservedHead = observedHead
	attempt.Failure = ""
	attempt.UpdatedAt = now.UTC()
	attempt.IntegrityHash, err = attempt.IntegrityDigest()
	if err != nil {
		return domain.TaskResult{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE integration_attempts SET status = ?, observed_head = ?, failure = '', integrity_hash = ?, updated_at = ? WHERE integration_attempt_id = ? AND status = ?`,
		string(attempt.Status), attempt.ObservedHead, attempt.IntegrityHash, attempt.UpdatedAt.Format(time.RFC3339Nano), attempt.ID, string(domain.IntegrationDispatched))
	if err != nil {
		return domain.TaskResult{}, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.TaskResult{}, ErrStateConflict
	}
	res, err = tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`, string(domain.TaskComplete), attempt.UpdatedAt.Format(time.RFC3339Nano), attempt.TaskID, string(domain.TaskIntegrating))
	if err != nil {
		return domain.TaskResult{}, err
	}
	rows, _ = res.RowsAffected()
	if rows != 1 {
		return domain.TaskResult{}, ErrStateConflict
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskResult{}, err
	}
	return integrated, nil
}

func (s *SQLite) BlockVerifiedIntegration(ctx context.Context, taskID, reason, newResultID string, now time.Time) (domain.TaskResult, error) {
	latest, ok, err := s.LatestTaskResult(ctx, taskID)
	if err != nil || !ok {
		if err == nil {
			err = ErrNotFound
		}
		return domain.TaskResult{}, err
	}
	if latest.IntegrationStatus == "BLOCKED" {
		return latest, nil
	}
	if latest.Verdict != domain.ResultVerified {
		return domain.TaskResult{}, ErrStateConflict
	}
	blocked := latest
	blocked.ID = newResultID
	blocked.Version = latest.Version + 1
	blocked.IntegrationStatus = "BLOCKED"
	blocked.PassFailEvidence = append(append([]string{}, latest.PassFailEvidence...), "integration:BLOCKED")
	blocked.UnresolvedRisks = append(append([]string{}, latest.UnresolvedRisks...), reason)
	blocked.ChangedAreas = append([]string{}, latest.ChangedAreas...)
	blocked.VerificationExecuted = append([]string{}, latest.VerificationExecuted...)
	blocked.CreatedAt = now.UTC()
	blocked.IntegrityHash, err = blocked.IntegrityDigest()
	if err != nil {
		return domain.TaskResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.TaskResult{}, err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, taskID).Scan(&state); err != nil {
		return domain.TaskResult{}, err
	}
	if domain.TaskState(state) != domain.TaskVerified {
		return domain.TaskResult{}, ErrStateConflict
	}
	var currentID string
	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT result_id, version FROM task_results WHERE task_id = ? ORDER BY version DESC LIMIT 1`, taskID).Scan(&currentID, &currentVersion); err != nil {
		return domain.TaskResult{}, err
	}
	if currentID != latest.ID || currentVersion != latest.Version {
		return domain.TaskResult{}, ErrStateConflict
	}
	if err := insertIntegrationResultTx(ctx, tx, blocked); err != nil {
		return domain.TaskResult{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`, string(domain.TaskBlocked), now.UTC().Format(time.RFC3339Nano), taskID, string(domain.TaskVerified))
	if err != nil {
		return domain.TaskResult{}, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.TaskResult{}, ErrStateConflict
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskResult{}, err
	}
	return blocked, nil
}

func (s *SQLite) BlockIntegrationAttempt(ctx context.Context, attemptID, observedHead, reason, newResultID string, now time.Time) (domain.TaskResult, error) {
	attempt, err := s.GetIntegrationAttempt(ctx, attemptID)
	if err != nil {
		return domain.TaskResult{}, err
	}
	if attempt.Status == domain.IntegrationBlocked {
		latest, ok, err := s.LatestTaskResult(ctx, attempt.TaskID)
		if err != nil || !ok {
			if err == nil {
				err = ErrNotFound
			}
			return domain.TaskResult{}, err
		}
		return latest, nil
	}
	if attempt.Status != domain.IntegrationPrepared && attempt.Status != domain.IntegrationDispatched {
		return domain.TaskResult{}, ErrStateConflict
	}
	latest, ok, err := s.LatestTaskResult(ctx, attempt.TaskID)
	if err != nil || !ok {
		if err == nil {
			err = ErrNotFound
		}
		return domain.TaskResult{}, err
	}
	if latest.ID != attempt.TaskResultID || latest.Version != attempt.TaskResultVersion || latest.Verdict != domain.ResultVerified {
		return domain.TaskResult{}, ErrStateConflict
	}
	blocked := latest
	blocked.ID = newResultID
	blocked.Version = latest.Version + 1
	blocked.IntegrationStatus = "BLOCKED"
	blocked.PassFailEvidence = append(append([]string{}, latest.PassFailEvidence...), "integration:"+attempt.ID+":BLOCKED")
	blocked.UnresolvedRisks = append(append([]string{}, latest.UnresolvedRisks...), reason)
	blocked.ChangedAreas = append([]string{}, latest.ChangedAreas...)
	blocked.VerificationExecuted = append([]string{}, latest.VerificationExecuted...)
	blocked.CreatedAt = now.UTC()
	blocked.IntegrityHash, err = blocked.IntegrityDigest()
	if err != nil {
		return domain.TaskResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.TaskResult{}, err
	}
	defer tx.Rollback()
	var currentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM integration_attempts WHERE integration_attempt_id = ?`, attempt.ID).Scan(&currentStatus); err != nil {
		return domain.TaskResult{}, err
	}
	if domain.IntegrationStatus(currentStatus) != attempt.Status {
		return domain.TaskResult{}, ErrStateConflict
	}
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, attempt.TaskID).Scan(&state); err != nil {
		return domain.TaskResult{}, err
	}
	wantState := domain.TaskReadyToIntegrate
	if attempt.Status == domain.IntegrationDispatched {
		wantState = domain.TaskIntegrating
	}
	if domain.TaskState(state) != wantState {
		return domain.TaskResult{}, ErrStateConflict
	}
	var currentResultID string
	var currentResultVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT result_id, version FROM task_results WHERE task_id = ? ORDER BY version DESC LIMIT 1`, attempt.TaskID).Scan(&currentResultID, &currentResultVersion); err != nil {
		return domain.TaskResult{}, err
	}
	if currentResultID != latest.ID || currentResultVersion != latest.Version {
		return domain.TaskResult{}, ErrStateConflict
	}
	if err := insertIntegrationResultTx(ctx, tx, blocked); err != nil {
		return domain.TaskResult{}, err
	}
	attempt.Status = domain.IntegrationBlocked
	attempt.ObservedHead = observedHead
	attempt.Failure = reason
	attempt.UpdatedAt = now.UTC()
	attempt.IntegrityHash, err = attempt.IntegrityDigest()
	if err != nil {
		return domain.TaskResult{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE integration_attempts SET status = ?, observed_head = ?, failure = ?, integrity_hash = ?, updated_at = ? WHERE integration_attempt_id = ? AND status = ?`,
		string(attempt.Status), attempt.ObservedHead, attempt.Failure, attempt.IntegrityHash, attempt.UpdatedAt.Format(time.RFC3339Nano), attempt.ID, currentStatus)
	if err != nil {
		return domain.TaskResult{}, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.TaskResult{}, ErrStateConflict
	}
	res, err = tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`, string(domain.TaskBlocked), attempt.UpdatedAt.Format(time.RFC3339Nano), attempt.TaskID, string(wantState))
	if err != nil {
		return domain.TaskResult{}, err
	}
	rows, _ = res.RowsAffected()
	if rows != 1 {
		return domain.TaskResult{}, ErrStateConflict
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskResult{}, err
	}
	return blocked, nil
}

func (s *SQLite) GetIntegrationAttempt(ctx context.Context, attemptID string) (domain.IntegrationAttempt, error) {
	return integrationAttemptQuery(ctx, s.db, `
SELECT integration_attempt_id, task_id, version, project_id, expected_ref, expected_head,
       task_result_id, task_result_version, task_result_revision, candidate_revision, evidence_id,
       status, observed_head, failure, integrity_hash, created_at, updated_at
FROM integration_attempts WHERE integration_attempt_id = ?`, attemptID)
}

func (s *SQLite) LatestIntegrationAttempt(ctx context.Context, taskID string) (domain.IntegrationAttempt, bool, error) {
	attempt, err := integrationAttemptQuery(ctx, s.db, `
SELECT integration_attempt_id, task_id, version, project_id, expected_ref, expected_head,
       task_result_id, task_result_version, task_result_revision, candidate_revision, evidence_id,
       status, observed_head, failure, integrity_hash, created_at, updated_at
FROM integration_attempts WHERE task_id = ? ORDER BY version DESC LIMIT 1`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IntegrationAttempt{}, false, nil
	}
	if err != nil {
		return domain.IntegrationAttempt{}, false, err
	}
	return attempt, true, nil
}

func (s *SQLite) ListPendingIntegrationAttempts(ctx context.Context) ([]domain.IntegrationAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT integration_attempt_id, task_id, version, project_id, expected_ref, expected_head,
       task_result_id, task_result_version, task_result_revision, candidate_revision, evidence_id,
       status, observed_head, failure, integrity_hash, created_at, updated_at
FROM integration_attempts WHERE status IN (?, ?) ORDER BY updated_at ASC, integration_attempt_id ASC`, string(domain.IntegrationPrepared), string(domain.IntegrationDispatched))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.IntegrationAttempt, 0)
	for rows.Next() {
		attempt, err := scanIntegrationAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, rows.Err()
}

func (s *SQLite) integrationAttemptByResult(ctx context.Context, taskID, resultID string) (domain.IntegrationAttempt, bool, error) {
	attempt, err := integrationAttemptQuery(ctx, s.db, `
SELECT integration_attempt_id, task_id, version, project_id, expected_ref, expected_head,
       task_result_id, task_result_version, task_result_revision, candidate_revision, evidence_id,
       status, observed_head, failure, integrity_hash, created_at, updated_at
FROM integration_attempts WHERE task_id = ? AND task_result_id = ?`, taskID, resultID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IntegrationAttempt{}, false, nil
	}
	if err != nil {
		return domain.IntegrationAttempt{}, false, err
	}
	return attempt, true, nil
}

func integrationAttemptQuery(ctx context.Context, q queryer, query string, args ...any) (domain.IntegrationAttempt, error) {
	row := q.QueryRowContext(ctx, query, args...)
	return scanIntegrationAttempt(row)
}

func scanIntegrationAttempt(row rowScanner) (domain.IntegrationAttempt, error) {
	var attempt domain.IntegrationAttempt
	var status, created, updated string
	if err := row.Scan(&attempt.ID, &attempt.TaskID, &attempt.Version, &attempt.ProjectID, &attempt.ExpectedRef, &attempt.ExpectedHead,
		&attempt.TaskResultID, &attempt.TaskResultVersion, &attempt.TaskResultRevision, &attempt.CandidateRevision, &attempt.EvidenceID,
		&status, &attempt.ObservedHead, &attempt.Failure, &attempt.IntegrityHash, &created, &updated); err != nil {
		return domain.IntegrationAttempt{}, err
	}
	attempt.Status = domain.IntegrationStatus(status)
	var err error
	attempt.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.IntegrationAttempt{}, err
	}
	attempt.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return domain.IntegrationAttempt{}, err
	}
	if !attempt.IntegrityValid() {
		return domain.IntegrationAttempt{}, ErrIntegrationIntegrity
	}
	return attempt, nil
}

func insertIntegrationResultTx(ctx context.Context, tx *sql.Tx, result domain.TaskResult) error {
	if !result.IntegrityValid() {
		return errors.New("integration task result integrity is invalid")
	}
	changedJSON, _ := json.Marshal(result.ChangedAreas)
	verificationJSON, _ := json.Marshal(result.VerificationExecuted)
	passFailJSON, _ := json.Marshal(result.PassFailEvidence)
	risksJSON, _ := json.Marshal(result.UnresolvedRisks)
	resourceJSON, _ := json.Marshal(result.ResourceSummary)
	_, err := tx.ExecContext(ctx, `
INSERT INTO task_results(
    result_id, task_id, version, goal_hash, base_revision, final_revision, changed_areas_json,
    evidence_id, verification_executed_json, pass_fail_evidence_json, unresolved_risks_json,
    integration_status, workspace_disposition, resource_summary_json, verdict, integrity_hash, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		result.ID, result.TaskID, result.Version, result.GoalHash, result.BaseRevision, result.FinalRevision,
		changedJSON, result.EvidenceID, verificationJSON, passFailJSON, risksJSON,
		result.IntegrationStatus, result.WorkspaceDisposition, resourceJSON, string(result.Verdict), result.IntegrityHash,
		result.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}
