package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestRequirePhysicalRecoveryPreservesBlockedTaskRecencyOnRetry(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	t0 := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	project := domain.Project{ID: "recovery-recency", Root: t.TempDir(), CreatedAt: t0}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{Goal: "recovery recency", Acceptance: []string{"blocked retry preserves task recency"}, ProjectID: project.ID, BaseRevision: "base", VerificationProfile: "test", Priority: "P2"}
	hash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: "task-recovery-recency", IdempotencyKey: "recovery-recency", Contract: contract, ContractHash: hash, State: domain.TaskWorkspaceReady, CreatedAt: t0, UpdatedAt: t0}
	if _, _, err := s.SubmitTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	attempt, err := s.BeginAttempt(ctx, task.ID, "attempt-recovery-recency", "worker", "supervisor", t0.Add(time.Minute), t0.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	firstRecovery := t0.Add(3 * time.Minute)
	if err := s.RequirePhysicalRecovery(ctx, task.ID, attempt.ID, attempt.RunEpoch, firstRecovery); err != nil {
		t.Fatal(err)
	}
	blocked, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.State != domain.TaskBlocked || !blocked.UpdatedAt.Equal(firstRecovery) {
		t.Fatalf("first recovery did not record the real BLOCKED transition: state=%s updated=%s", blocked.State, blocked.UpdatedAt)
	}
	fenced, err := s.GetAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fenced.AuthorityState != domain.AttemptLogicallyFenced {
		t.Fatalf("first recovery did not fence attempt: %s", fenced.AuthorityState)
	}

	maintenanceRetry := t0.Add(30 * time.Minute)
	if err := s.RequirePhysicalRecovery(ctx, task.ID, attempt.ID, attempt.RunEpoch, maintenanceRetry); err != nil {
		t.Fatal(err)
	}
	afterRetry, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !afterRetry.UpdatedAt.Equal(firstRecovery) {
		t.Fatalf("maintenance recovery rewrote user-facing task recency: got=%s want=%s", afterRetry.UpdatedAt, firstRecovery)
	}
	fencedAfterRetry, err := s.GetAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fencedAfterRetry.AuthorityState != domain.AttemptLogicallyFenced {
		t.Fatalf("maintenance retry weakened fencing: %s", fencedAfterRetry.AuthorityState)
	}
}
