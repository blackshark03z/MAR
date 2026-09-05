package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestPersistVerificationOutcomeRejectsProfileAndAcceptanceIdentityMismatch(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	project := domain.Project{ID: "project-verify-store", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "verify store authority",
		Acceptance:          []string{"required acceptance"},
		ProjectID:           project.ID,
		BaseRevision:        "base-revision",
		VerificationProfile: "required-profile",
		Priority:            "P2",
	}
	contractHash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{
		ID:             "task-verify-store",
		IdempotencyKey: "verify-store-key",
		Contract:       contract,
		ContractHash:   contractHash,
		State:          domain.TaskSubmitted,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if _, _, err := s.SubmitTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := s.OrchestratorTransition(ctx, task.ID, domain.TaskSubmitted, domain.TaskPreflight, now); err != nil {
		t.Fatal(err)
	}
	if err := s.OrchestratorTransition(ctx, task.ID, domain.TaskPreflight, domain.TaskWaitingResource, now); err != nil {
		t.Fatal(err)
	}
	workspace := domain.Workspace{
		ID: "workspace-verify-store", TaskID: task.ID, ProjectID: project.ID, Path: t.TempDir(), BaseRevision: contract.BaseRevision,
		CreatedAt: now, UpdatedAt: now,
	}
	workspace, _, err = s.BeginWorkspace(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWorkspaceReady(ctx, workspace.ID, task.ID, contract.BaseRevision, now); err != nil {
		t.Fatal(err)
	}
	attempt, err := s.BeginAttempt(ctx, task.ID, "attempt-verify-store", "worker", "supervisor", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TransitionTaskForAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, domain.TaskRunning, domain.TaskVerifying, now); err != nil {
		t.Fatal(err)
	}

	makeOutcome := func(profileID, criterion string) (domain.VerificationEvidence, domain.TaskResult) {
		environment := json.RawMessage(`{"schema":1,"tool":"go"}`)
		environmentDigest := sha256.Sum256(environment)
		outputDigest := sha256.Sum256([]byte("ok"))
		evidence := domain.VerificationEvidence{
			ID:                "evidence-" + profileID + "-" + criterion,
			TaskID:            task.ID,
			AttemptID:         attempt.ID,
			RunEpoch:          attempt.RunEpoch,
			GoalHash:          contractHash,
			BaseRevision:      contract.BaseRevision,
			CandidateRevision: contract.BaseRevision,
			ProfileID:         profileID,
			ProfileHash:       "profile-hash",
			EnvironmentJSON:   environment,
			EnvironmentHash:   hex.EncodeToString(environmentDigest[:]),
			Commands: []domain.VerificationCommandEvidence{{
				Name: "go", Args: []string{"test", "./..."}, ExitCode: 0, Passed: true,
				OutputSHA256: hex.EncodeToString(outputDigest[:]),
			}},
			Acceptance: []domain.AcceptanceEvidence{{Criterion: criterion, Passed: true, EvidenceRefs: []string{"command:1"}}},
			Verdict:    domain.VerificationPass,
			CreatedAt:  now,
		}
		evidence.IntegrityHash, _ = evidence.IntegrityDigest()
		result := domain.TaskResult{
			ID: "result-" + profileID + "-" + criterion, TaskID: task.ID, GoalHash: contractHash,
			BaseRevision: contract.BaseRevision, FinalRevision: contract.BaseRevision, ChangedAreas: []string{}, EvidenceID: evidence.ID,
			VerificationExecuted: []string{"go test ./..."}, PassFailEvidence: []string{"command:1:PASS"}, UnresolvedRisks: []string{},
			IntegrationStatus: "NOT_INTEGRATED", WorkspaceDisposition: "RETAINED", Verdict: domain.ResultVerified, CreatedAt: now,
		}
		return evidence, result
	}

	wrongProfileEvidence, wrongProfileResult := makeOutcome("wrong-profile", contract.Acceptance[0])
	if _, err := s.PersistVerificationOutcome(ctx, wrongProfileEvidence, wrongProfileResult, now); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("profile identity mismatch was not rejected: %v", err)
	}
	wrongAcceptanceEvidence, wrongAcceptanceResult := makeOutcome(contract.VerificationProfile, "different acceptance")
	if _, err := s.PersistVerificationOutcome(ctx, wrongAcceptanceEvidence, wrongAcceptanceResult, now); err == nil {
		t.Fatal("acceptance identity mismatch was not rejected")
	}
	persisted, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != domain.TaskVerifying {
		t.Fatalf("rejected evidence changed authoritative task state: %s", persisted.State)
	}
	if _, ok, err := s.LatestTaskResult(ctx, task.ID); err != nil || ok {
		t.Fatalf("rejected evidence persisted a task result: ok=%v err=%v", ok, err)
	}
}
