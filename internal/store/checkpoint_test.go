package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestLatestValidCheckpointSkipsCorruptNewestSnapshot(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	project := domain.Project{ID: "checkpoint-project", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "checkpoint recovery",
		Acceptance:          []string{"latest valid checkpoint loads"},
		ProjectID:           project.ID,
		BaseRevision:        "base-checkpoint",
		VerificationProfile: "test",
		Priority:            "P2",
	}
	hash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: "checkpoint-task", IdempotencyKey: "checkpoint-key", Contract: contract, ContractHash: hash, State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
	if _, _, err := s.SubmitTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	for _, transition := range []struct{ from, to domain.TaskState }{
		{domain.TaskSubmitted, domain.TaskPreflight},
		{domain.TaskPreflight, domain.TaskWaitingResource},
		{domain.TaskWaitingResource, domain.TaskWorkspaceReady},
	} {
		if err := s.OrchestratorTransition(ctx, task.ID, transition.from, transition.to, now); err != nil {
			t.Fatal(err)
		}
	}
	attempt, err := s.BeginAttempt(ctx, task.ID, "checkpoint-attempt", "worker", "supervisor", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	payload1 := domain.SemanticCheckpointPayload{CurrentHypothesis: "first", VerificationStatus: "pending", NextAction: "continue first"}
	first, err := s.PublishCheckpoint(ctx, "checkpoint-1", task.ID, attempt.ID, attempt.RunEpoch, "base-checkpoint", payload1, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	payload2 := domain.SemanticCheckpointPayload{CurrentHypothesis: "second", VerificationStatus: "pending", NextAction: "continue second"}
	second, err := s.PublishCheckpoint(ctx, "checkpoint-2", task.ID, attempt.ID, attempt.RunEpoch, "base-checkpoint", payload2, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != 2 {
		t.Fatalf("unexpected second checkpoint version: %d", second.Version)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE semantic_checkpoints SET integrity_hash = 'corrupt' WHERE checkpoint_id = ?`, second.ID); err != nil {
		t.Fatal(err)
	}
	latest, ok, err := s.LatestValidCheckpoint(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || latest.ID != first.ID || !latest.IntegrityValid() {
		t.Fatalf("corrupt newest checkpoint overrode valid recovery state: ok=%v checkpoint=%+v", ok, latest)
	}
}
