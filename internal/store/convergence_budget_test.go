package store

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestTaskConvergenceUsageTracksSemanticProgressBeforeDeclaringNoProgress(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	project := domain.Project{ID: "convergence-project", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "converge without false no-progress",
		Acceptance:          []string{"semantic progress is durable"},
		ProjectID:           project.ID,
		BaseRevision:        "base-convergence",
		VerificationProfile: "test",
		Priority:            "P1",
	}
	hash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: "convergence-task", IdempotencyKey: "convergence-key", Contract: contract, ContractHash: hash, State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
	if _, _, err := s.SubmitTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	seedAttempt := func(epoch int64, payload domain.SemanticCheckpointPayload) {
		t.Helper()
		attemptID := fmt.Sprintf("attempt-%d", epoch)
		started := now.Add(time.Duration(epoch) * time.Minute)
		terminated := started.Add(5 * time.Second)
		if _, err := s.db.ExecContext(ctx, `
INSERT INTO execution_attempts(attempt_id, task_id, run_epoch, worker_id, supervisor_id, authority_state, started_at, heartbeat_at, lease_deadline, terminated_at, terminal_status)
VALUES (?, ?, ?, 'worker', 'supervisor', ?, ?, ?, ?, ?, 'test-terminated')`,
			attemptID, task.ID, epoch, string(domain.AttemptPhysicallyTerminated),
			started.Format(time.RFC3339Nano), terminated.Format(time.RFC3339Nano), terminated.Format(time.RFC3339Nano), terminated.Format(time.RFC3339Nano),
		); err != nil {
			t.Fatal(err)
		}
		cp := domain.SemanticCheckpoint{
			ID:              fmt.Sprintf("checkpoint-%d", epoch),
			TaskID:          task.ID,
			AttemptID:       attemptID,
			RunEpoch:        epoch,
			Version:         epoch,
			GoalHash:        hash,
			BaseRevision:    contract.BaseRevision,
			CurrentRevision: "same-revision",
			Payload:         payload,
			CreatedAt:       terminated.Add(-time.Second),
		}
		cp.IntegrityHash, err = cp.IntegrityDigest()
		if err != nil {
			t.Fatal(err)
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, `
INSERT INTO semantic_checkpoints(checkpoint_id, task_id, attempt_id, run_epoch, version, goal_hash, base_revision, current_revision, payload_json, integrity_hash, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			cp.ID, cp.TaskID, cp.AttemptID, cp.RunEpoch, cp.Version, cp.GoalHash, cp.BaseRevision, cp.CurrentRevision, payloadJSON, cp.IntegrityHash, cp.CreatedAt.Format(time.RFC3339Nano),
		); err != nil {
			t.Fatal(err)
		}
	}

	first := domain.SemanticCheckpointPayload{CompletedWork: []string{"inspected root cause"}, CurrentHypothesis: "hypothesis one", VerificationStatus: "pending", NextAction: "implement", CriticalEvidenceRefs: []string{"evidence-a"}}
	second := domain.SemanticCheckpointPayload{CompletedWork: []string{"inspected root cause", "implemented fix"}, CurrentHypothesis: "hypothesis two", ChangedAreas: []string{"internal/store"}, VerificationStatus: "focused tests pending", NextAction: "test", CriticalEvidenceRefs: []string{"evidence-a", "evidence-b"}}
	seedAttempt(1, first)
	seedAttempt(2, second)

	usage, err := s.TaskConvergenceUsage(ctx, task.ID, now.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if usage.NoProgressStreak != 0 {
		t.Fatalf("same revision with semantic progress counted as no progress: %+v", usage)
	}

	third := second
	third.CurrentHypothesis = "wording-only hypothesis change"
	third.NextAction = "wording-only next action"
	third.CompletedWork = []string{"implemented fix", "inspected root cause"}
	third.CriticalEvidenceRefs = []string{"evidence-b", "evidence-a"}
	seedAttempt(3, third)
	usage, err = s.TaskConvergenceUsage(ctx, task.ID, now.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if usage.NoProgressStreak != 1 {
		t.Fatalf("one repeated semantic signature should produce streak 1, got %+v", usage)
	}

	fourth := third
	fourth.CurrentHypothesis = "another planning-only change"
	fourth.NextAction = "another planning-only action"
	seedAttempt(4, fourth)
	usage, err = s.TaskConvergenceUsage(ctx, task.ID, now.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if usage.NoProgressStreak != 2 {
		t.Fatalf("two consecutive repeated semantic signatures should produce streak 2, got %+v", usage)
	}
}

func TestSemanticProgressSignatureRejectsCorruptCheckpointAndNormalizesOrder(t *testing.T) {
	payloadA := domain.SemanticCheckpointPayload{CompletedWork: []string{"b", "a"}, CurrentHypothesis: "ignored", ChangedAreas: []string{"z", "x"}, VerificationStatus: " pass ", NextAction: "ignored", CriticalEvidenceRefs: []string{"two", "one"}}
	payloadB := domain.SemanticCheckpointPayload{CompletedWork: []string{"a", "b"}, CurrentHypothesis: "different", ChangedAreas: []string{"x", "z"}, VerificationStatus: "pass", NextAction: "different", CriticalEvidenceRefs: []string{"one", "two"}}
	cpA := domain.SemanticCheckpoint{CurrentRevision: " rev ", Payload: payloadA}
	cpB := domain.SemanticCheckpoint{CurrentRevision: "rev", Payload: payloadB}
	a, err := semanticProgressSignature(cpA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := semanticProgressSignature(cpB)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("stable semantic evidence ordering/planning text changed signature: %q != %q", a, b)
	}
}
