package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestCompleteWorkspaceRemovalPersistsRemovedDispositionAtomically(t *testing.T) {
	ctx := context.Background()
	s, taskID, workspaceID := completeWorkspaceRetentionFixture(t, domain.AttemptPhysicallyTerminated, "INTEGRATED", "RETAINED")
	defer s.Close()

	candidates, err := s.ListTerminalWorkspaceRemovalCandidates(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0] != taskID {
		t.Fatalf("completed retained workspace was not reclaimable: %v", candidates)
	}
	workspace, err := s.BeginWorkspaceRemoval(ctx, taskID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if workspace.State != domain.WorkspaceRemoving {
		t.Fatalf("workspace did not enter REMOVING: %+v", workspace)
	}
	if err := s.FinishWorkspaceRemoval(ctx, workspaceID, "result-workspace-removed", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetWorkspaceByTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != domain.WorkspaceRemoved || stored.RemovedAt == nil {
		t.Fatalf("workspace removal was not durable: %+v", stored)
	}
	result, ok, err := s.LatestTaskResult(ctx, taskID)
	if err != nil || !ok {
		t.Fatalf("latest result missing after removal: ok=%v err=%v", ok, err)
	}
	if result.Version != 2 || result.WorkspaceDisposition != "REMOVED" || !result.IntegrityValid() {
		t.Fatalf("workspace disposition did not advance atomically: %+v", result)
	}
	if got := result.PassFailEvidence[len(result.PassFailEvidence)-1]; got != "workspace:"+workspaceID+":REMOVED" {
		t.Fatalf("workspace removal evidence missing from result: %q", got)
	}
	candidates, err = s.ListTerminalWorkspaceRemovalCandidates(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("removed workspace remained a reclaim candidate: %v", candidates)
	}
}

func TestCompleteWorkspaceRemovalRequiresPhysicalTermination(t *testing.T) {
	ctx := context.Background()
	s, taskID, _ := completeWorkspaceRetentionFixture(t, domain.AttemptActive, "INTEGRATED", "RETAINED")
	defer s.Close()
	if _, err := s.BeginWorkspaceRemoval(ctx, taskID, time.Now().UTC()); !errors.Is(err, ErrPhysicalFenceRequired) {
		t.Fatalf("complete workspace removal without physical fence must fail closed, got %v", err)
	}
}

func TestCompleteWorkspaceRemovalRequiresVerifiedIntegratedRetainedResult(t *testing.T) {
	ctx := context.Background()
	s, taskID, _ := completeWorkspaceRetentionFixture(t, domain.AttemptPhysicallyTerminated, "BLOCKED", "RETAINED")
	defer s.Close()
	if _, err := s.BeginWorkspaceRemoval(ctx, taskID, time.Now().UTC()); !errors.Is(err, ErrWorkspaceRemovalUnsafe) {
		t.Fatalf("non-integrated complete workspace must remain retained, got %v", err)
	}
}

func completeWorkspaceRetentionFixture(t *testing.T, attemptState domain.AttemptAuthorityState, integrationStatus, disposition string) (*SQLite, string, string) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	project := domain.Project{ID: "project-retention", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		s.Close()
		t.Fatal(err)
	}
	contract := domain.GoalContract{Goal: "retention fixture", Acceptance: []string{"durable"}, ProjectID: project.ID, BaseRevision: "base", VerificationProfile: "go-standard", Priority: "P2"}
	taskID := "task-retention"
	task := domain.Task{ID: taskID, IdempotencyKey: "retention-key", Contract: contract, ContractHash: "goal-hash", State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
	if _, _, err := s.SubmitTask(ctx, task); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET state = ?, run_epoch = 1, updated_at = ? WHERE id = ?`, string(domain.TaskComplete), now.Format(time.RFC3339Nano), taskID); err != nil {
		s.Close()
		t.Fatal(err)
	}
	workspaceID := "workspace-retention"
	if _, err := s.db.ExecContext(ctx, `INSERT INTO workspaces(id,task_id,project_id,path,base_revision,head_revision,state,failure,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		workspaceID, taskID, project.ID, filepath.Join(t.TempDir(), "workspace"), "base", "candidate", string(domain.WorkspaceReady), "", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO execution_attempts(attempt_id,task_id,run_epoch,worker_id,supervisor_id,authority_state,started_at,heartbeat_at,lease_deadline,terminated_at,terminal_status) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		"attempt-retention", taskID, 1, "worker", "supervisor", string(attemptState), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano), nullableTimeForRetention(attemptState, now), "fixture"); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO verification_evidence(evidence_id,task_id,attempt_id,run_epoch,goal_hash,base_revision,candidate_revision,profile_id,profile_hash,environment_json,environment_hash,commands_json,acceptance_json,verdict,integrity_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"evidence-retention", taskID, "attempt-retention", 1, "goal-hash", "base", "candidate", "go-standard", "profile-hash", "{}", "environment-hash", "[]", "[]", "PASS", "fixture-integrity", now.Format(time.RFC3339Nano)); err != nil {
		s.Close()
		t.Fatal(err)
	}
	result := domain.TaskResult{
		ID: "result-retained", TaskID: taskID, Version: 1, GoalHash: "goal-hash", BaseRevision: "base", FinalRevision: "candidate",
		ChangedAreas: []string{}, EvidenceID: "evidence-retention", VerificationExecuted: []string{"go test"}, PassFailEvidence: []string{"verification:PASS"},
		UnresolvedRisks: []string{}, IntegrationStatus: integrationStatus, WorkspaceDisposition: disposition, Verdict: domain.ResultVerified, CreatedAt: now,
	}
	result.IntegrityHash, err = result.IntegrityDigest()
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err := insertIntegrationResultTx(ctx, tx, result); err != nil {
		_ = tx.Rollback()
		s.Close()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		s.Close()
		t.Fatal(err)
	}
	return s, taskID, workspaceID
}

func nullableTimeForRetention(state domain.AttemptAuthorityState, now time.Time) any {
	if state == domain.AttemptPhysicallyTerminated {
		return now.Format(time.RFC3339Nano)
	}
	return nil
}

func TestCompleteWorkspaceRemovalFinalizationIsIdempotentAfterRemovingCrashWindow(t *testing.T) {
	ctx := context.Background()
	s, taskID, workspaceID := completeWorkspaceRetentionFixture(t, domain.AttemptPhysicallyTerminated, "INTEGRATED", "RETAINED")
	defer s.Close()

	workspace, err := s.BeginWorkspaceRemoval(ctx, taskID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if workspace.State != domain.WorkspaceRemoving {
		t.Fatalf("workspace did not enter REMOVING before simulated crash: %+v", workspace)
	}

	// Simulate a daemon/process crash after the physical side effect but before
	// durable finalization. Re-entering Begin must preserve the recoverable
	// REMOVING state instead of inventing a new lifecycle transition.
	replayed, err := s.BeginWorkspaceRemoval(ctx, taskID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State != domain.WorkspaceRemoving || replayed.ID != workspaceID {
		t.Fatalf("REMOVING replay lost workspace identity: %+v", replayed)
	}

	const resultID = "result-workspace-removed-idempotent"
	if err := s.FinishWorkspaceRemoval(ctx, workspaceID, resultID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	first, ok, err := s.LatestTaskResult(ctx, taskID)
	if err != nil || !ok {
		t.Fatalf("first finalization result missing: ok=%v err=%v", ok, err)
	}
	if first.Version != 2 || first.ID != resultID || first.WorkspaceDisposition != "REMOVED" {
		t.Fatalf("unexpected first finalization: %+v", first)
	}

	// A repeated recovery pass after commit must be a no-op: no third result
	// version and no duplicate removal evidence.
	if err := s.FinishWorkspaceRemoval(ctx, workspaceID, resultID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	second, ok, err := s.LatestTaskResult(ctx, taskID)
	if err != nil || !ok {
		t.Fatalf("idempotent finalization result missing: ok=%v err=%v", ok, err)
	}
	if second.Version != first.Version || second.ID != first.ID || second.IntegrityHash != first.IntegrityHash {
		t.Fatalf("replayed finalization created divergent result: first=%+v second=%+v", first, second)
	}
}

func TestTerminalWorkspaceRemovalCandidatesIncludeOnlySafeTerminalTaskClasses(t *testing.T) {
	ctx := context.Background()
	s, taskID, _ := completeWorkspaceRetentionFixture(t, domain.AttemptPhysicallyTerminated, "INTEGRATED", "RETAINED")
	defer s.Close()

	candidates, err := s.ListTerminalWorkspaceRemovalCandidates(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !containsRetentionCandidate(candidates, taskID) {
		t.Fatalf("verified integrated COMPLETE task missing from terminal candidates: %v", candidates)
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM task_results WHERE task_id = ?`, taskID); err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskCancelled, domain.TaskFailed} {
		if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET state = ? WHERE id = ?`, string(state), taskID); err != nil {
			t.Fatal(err)
		}
		candidates, err = s.ListTerminalWorkspaceRemovalCandidates(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		if !containsRetentionCandidate(candidates, taskID) {
			t.Fatalf("%s task without result missing from terminal candidates: %v", state, candidates)
		}
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET state = ? WHERE id = ?`, string(domain.TaskBlocked), taskID); err != nil {
		t.Fatal(err)
	}
	candidates, err = s.ListTerminalWorkspaceRemovalCandidates(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if containsRetentionCandidate(candidates, taskID) {
		t.Fatalf("BLOCKED task must never be auto-reclaimed: %v", candidates)
	}
}

func containsRetentionCandidate(taskIDs []string, want string) bool {
	for _, taskID := range taskIDs {
		if taskID == want {
			return true
		}
	}
	return false
}

func TestTerminalWorkspaceRemovalCandidatesRequirePhysicalFence(t *testing.T) {
	ctx := context.Background()
	s, taskID, _ := completeWorkspaceRetentionFixture(t, domain.AttemptActive, "INTEGRATED", "RETAINED")
	defer s.Close()
	candidates, err := s.ListTerminalWorkspaceRemovalCandidates(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if containsRetentionCandidate(candidates, taskID) {
		t.Fatalf("terminal candidate must remain hidden until physical termination is proven: %v", candidates)
	}
}
