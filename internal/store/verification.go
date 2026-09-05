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

func (s *SQLite) PersistVerificationOutcome(ctx context.Context, evidence domain.VerificationEvidence, result domain.TaskResult, now time.Time) (domain.TaskResult, error) {
	if err := evidence.ValidateIdentity(); err != nil || !evidence.IntegrityValid() {
		if err != nil {
			return domain.TaskResult{}, err
		}
		return domain.TaskResult{}, errors.New("verification evidence integrity is invalid")
	}
	if result.TaskID != evidence.TaskID || result.EvidenceID != evidence.ID || result.FinalRevision != evidence.CandidateRevision || result.GoalHash != evidence.GoalHash || result.BaseRevision != evidence.BaseRevision {
		return domain.TaskResult{}, errors.New("verification evidence and task result identity mismatch")
	}
	target := domain.TaskBlocked
	if evidence.Verdict == domain.VerificationPass && result.Verdict == domain.ResultVerified {
		target = domain.TaskVerified
	} else if evidence.Verdict != domain.VerificationFail || result.Verdict != domain.ResultVerificationFailed {
		return domain.TaskResult{}, errors.New("verification/result verdict combination is invalid")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.TaskResult{}, err
	}
	defer tx.Rollback()
	if err := validateAttemptAuthorityTx(ctx, tx, evidence.TaskID, evidence.AttemptID, evidence.RunEpoch); err != nil {
		return domain.TaskResult{}, err
	}
	var taskState, contractHash string
	var contractJSON []byte
	if err := tx.QueryRowContext(ctx, `
SELECT state, contract_hash, contract_json
FROM tasks WHERE id = ?`, evidence.TaskID).Scan(&taskState, &contractHash, &contractJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.TaskResult{}, ErrNotFound
		}
		return domain.TaskResult{}, err
	}
	var contract domain.GoalContract
	if err := json.Unmarshal(contractJSON, &contract); err != nil {
		return domain.TaskResult{}, fmt.Errorf("decode durable Goal Contract for verification: %w", err)
	}
	if domain.TaskState(taskState) != domain.TaskVerifying || contractHash != evidence.GoalHash || contract.BaseRevision != evidence.BaseRevision || contract.VerificationProfile != evidence.ProfileID {
		return domain.TaskResult{}, ErrStateConflict
	}
	if len(contract.Acceptance) != len(evidence.Acceptance) {
		return domain.TaskResult{}, errors.New("verification acceptance evidence does not match Goal Contract")
	}
	for i, criterion := range contract.Acceptance {
		if strings.TrimSpace(criterion) != evidence.Acceptance[i].Criterion {
			return domain.TaskResult{}, errors.New("verification acceptance criterion identity mismatch")
		}
	}
	var workspaceHead string
	if err := tx.QueryRowContext(ctx, `SELECT head_revision FROM workspaces WHERE task_id = ? AND state = ?`, evidence.TaskID, string(domain.WorkspaceReady)).Scan(&workspaceHead); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.TaskResult{}, ErrNotFound
		}
		return domain.TaskResult{}, err
	}
	if workspaceHead != evidence.CandidateRevision {
		return domain.TaskResult{}, ErrStateConflict
	}

	var nextVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM task_results WHERE task_id = ?`, result.TaskID).Scan(&nextVersion); err != nil {
		return domain.TaskResult{}, err
	}
	result.Version = nextVersion
	result.IntegrityHash, err = result.IntegrityDigest()
	if err != nil {
		return domain.TaskResult{}, err
	}
	if err := result.ValidateIdentity(); err != nil || !result.IntegrityValid() {
		if err != nil {
			return domain.TaskResult{}, err
		}
		return domain.TaskResult{}, errors.New("task result integrity is invalid")
	}

	commandsJSON, _ := json.Marshal(evidence.Commands)
	acceptanceJSON, _ := json.Marshal(evidence.Acceptance)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO verification_evidence(
    evidence_id, task_id, attempt_id, run_epoch, goal_hash, base_revision, candidate_revision,
    profile_id, profile_hash, environment_json, environment_hash, commands_json, acceptance_json,
    verdict, integrity_hash, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		evidence.ID, evidence.TaskID, evidence.AttemptID, evidence.RunEpoch, evidence.GoalHash,
		evidence.BaseRevision, evidence.CandidateRevision, evidence.ProfileID, evidence.ProfileHash,
		[]byte(evidence.EnvironmentJSON), evidence.EnvironmentHash, commandsJSON, acceptanceJSON,
		string(evidence.Verdict), evidence.IntegrityHash, evidence.CreatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return domain.TaskResult{}, fmt.Errorf("insert verification evidence: %w", err)
	}

	changedJSON, _ := json.Marshal(result.ChangedAreas)
	verificationJSON, _ := json.Marshal(result.VerificationExecuted)
	passFailJSON, _ := json.Marshal(result.PassFailEvidence)
	risksJSON, _ := json.Marshal(result.UnresolvedRisks)
	resourceJSON, _ := json.Marshal(result.ResourceSummary)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO task_results(
    result_id, task_id, version, goal_hash, base_revision, final_revision, changed_areas_json,
    evidence_id, verification_executed_json, pass_fail_evidence_json, unresolved_risks_json,
    integration_status, workspace_disposition, resource_summary_json, verdict, integrity_hash, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		result.ID, result.TaskID, result.Version, result.GoalHash, result.BaseRevision, result.FinalRevision,
		changedJSON, result.EvidenceID, verificationJSON, passFailJSON, risksJSON,
		result.IntegrationStatus, result.WorkspaceDisposition, resourceJSON, string(result.Verdict),
		result.IntegrityHash, result.CreatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return domain.TaskResult{}, fmt.Errorf("insert task result: %w", err)
	}

	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `
UPDATE tasks SET state = ?, updated_at = ?
WHERE id = ? AND state = ? AND run_epoch = ?`, target, stamp, evidence.TaskID, string(domain.TaskVerifying), evidence.RunEpoch)
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
	return result, nil
}

func (s *SQLite) LatestTaskResult(ctx context.Context, taskID string) (domain.TaskResult, bool, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT result_id, task_id, version, goal_hash, base_revision, final_revision, changed_areas_json,
       evidence_id, verification_executed_json, pass_fail_evidence_json, unresolved_risks_json,
       integration_status, workspace_disposition, resource_summary_json, verdict, integrity_hash, created_at
FROM task_results WHERE task_id = ? ORDER BY version DESC LIMIT 1`, taskID)
	var result domain.TaskResult
	var changedJSON, verificationJSON, passFailJSON, risksJSON, resourceJSON []byte
	var verdict, created string
	if err := row.Scan(&result.ID, &result.TaskID, &result.Version, &result.GoalHash, &result.BaseRevision, &result.FinalRevision,
		&changedJSON, &result.EvidenceID, &verificationJSON, &passFailJSON, &risksJSON,
		&result.IntegrationStatus, &result.WorkspaceDisposition, &resourceJSON, &verdict, &result.IntegrityHash, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.TaskResult{}, false, nil
		}
		return domain.TaskResult{}, false, err
	}
	if err := json.Unmarshal(changedJSON, &result.ChangedAreas); err != nil {
		return domain.TaskResult{}, false, err
	}
	if err := json.Unmarshal(verificationJSON, &result.VerificationExecuted); err != nil {
		return domain.TaskResult{}, false, err
	}
	if err := json.Unmarshal(passFailJSON, &result.PassFailEvidence); err != nil {
		return domain.TaskResult{}, false, err
	}
	if err := json.Unmarshal(risksJSON, &result.UnresolvedRisks); err != nil {
		return domain.TaskResult{}, false, err
	}
	if err := json.Unmarshal(resourceJSON, &result.ResourceSummary); err != nil {
		return domain.TaskResult{}, false, err
	}
	result.Verdict = domain.ResultVerdict(verdict)
	var err error
	result.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.TaskResult{}, false, err
	}
	if !result.IntegrityValid() {
		return domain.TaskResult{}, false, errors.New("latest task result integrity is invalid")
	}
	return result, true, nil
}

func (s *SQLite) GetVerificationEvidence(ctx context.Context, evidenceID string) (domain.VerificationEvidence, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT evidence_id, task_id, attempt_id, run_epoch, goal_hash, base_revision, candidate_revision,
       profile_id, profile_hash, environment_json, environment_hash, commands_json, acceptance_json,
       verdict, integrity_hash, created_at
FROM verification_evidence WHERE evidence_id = ?`, evidenceID)
	var evidence domain.VerificationEvidence
	var commandsJSON, acceptanceJSON []byte
	var verdict, created string
	if err := row.Scan(&evidence.ID, &evidence.TaskID, &evidence.AttemptID, &evidence.RunEpoch,
		&evidence.GoalHash, &evidence.BaseRevision, &evidence.CandidateRevision, &evidence.ProfileID,
		&evidence.ProfileHash, &evidence.EnvironmentJSON, &evidence.EnvironmentHash,
		&commandsJSON, &acceptanceJSON, &verdict, &evidence.IntegrityHash, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.VerificationEvidence{}, ErrNotFound
		}
		return domain.VerificationEvidence{}, err
	}
	if err := json.Unmarshal(commandsJSON, &evidence.Commands); err != nil {
		return domain.VerificationEvidence{}, err
	}
	if err := json.Unmarshal(acceptanceJSON, &evidence.Acceptance); err != nil {
		return domain.VerificationEvidence{}, err
	}
	evidence.Verdict = domain.VerificationVerdict(verdict)
	var err error
	evidence.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.VerificationEvidence{}, err
	}
	if !evidence.IntegrityValid() {
		return domain.VerificationEvidence{}, errors.New("verification evidence integrity is invalid")
	}
	return evidence, nil
}
