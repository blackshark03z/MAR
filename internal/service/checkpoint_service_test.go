package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/store"
)

func semanticPayload(label string) domain.SemanticCheckpointPayload {
	return domain.SemanticCheckpointPayload{
		CompletedWork:        []string{"completed " + label},
		CurrentHypothesis:    "hypothesis " + label,
		ChangedAreas:         []string{"internal/agent"},
		VerificationStatus:   "not-yet-verified",
		Blockers:             nil,
		RemainingWork:        []string{"remaining " + label},
		NextAction:           "next " + label,
		CriticalEvidenceRefs: []string{"evidence:" + label},
	}
}

func TestSemanticCheckpointIsVersionedDurableAndLatestValid(t *testing.T) {
	s, svc, dbPath := newHarness(t)
	ctx := context.Background()
	task := readyTask(t, svc, "checkpoint-versioned")
	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-checkpoint", "supervisor-checkpoint", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.PublishCheckpoint(ctx, task.ID, attempt.ID, attempt.RunEpoch, "abc123", semanticPayload("one"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.PublishCheckpoint(ctx, task.ID, attempt.ID, attempt.RunEpoch, "abc123", semanticPayload("two"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 2 || first.ID == second.ID {
		t.Fatalf("checkpoint versions are not immutable/monotonic: first=%+v second=%+v", first, second)
	}
	if !first.IntegrityValid() || !second.IntegrityValid() {
		t.Fatal("checkpoint integrity digest did not validate")
	}
	latest, ok, err := svc.LatestValidCheckpoint(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || latest.ID != second.ID || latest.Payload.NextAction != "next two" {
		t.Fatalf("unexpected latest checkpoint: ok=%v checkpoint=%+v", ok, latest)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	afterRestart, ok, err := reopened.LatestValidCheckpoint(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || afterRestart.ID != second.ID || !afterRestart.IntegrityValid() {
		t.Fatalf("checkpoint did not survive SQLite reopen: ok=%v checkpoint=%+v", ok, afterRestart)
	}
}

func TestSemanticCheckpointRejectsStaleAttemptAndOversizedPayload(t *testing.T) {
	_, svc, _ := newHarness(t)
	ctx := context.Background()
	task := readyTask(t, svc, "checkpoint-fence")
	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-checkpoint", "supervisor-checkpoint", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.LogicalFenceAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishCheckpoint(ctx, task.ID, attempt.ID, attempt.RunEpoch, "abc123", semanticPayload("stale")); !errors.Is(err, store.ErrStaleAttempt) {
		t.Fatalf("stale attempt published checkpoint: %v", err)
	}

	bigTask := readyTask(t, svc, "checkpoint-size")
	bigAttempt, err := svc.BeginAttempt(ctx, bigTask.ID, "worker-big", "supervisor-big", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	payload := semanticPayload("big")
	payload.CurrentHypothesis = strings.Repeat("x", 70<<10)
	if _, err := svc.PublishCheckpoint(ctx, bigTask.ID, bigAttempt.ID, bigAttempt.RunEpoch, "abc123", payload); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized checkpoint was not rejected: %v", err)
	}
}
