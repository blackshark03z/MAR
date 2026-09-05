package effects_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/effects"
	"mar/internal/service"
	"mar/internal/store"
)

func effectHarness(t *testing.T) (*store.SQLite, *service.TaskService, *effects.Manager, domain.Task, domain.ExecutionAttempt) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewTaskService(s)
	ctx := context.Background()
	if _, _, err := svc.RegisterProject(ctx, "effects-project", filepath.Join(t.TempDir(), "repo")); err != nil {
		s.Close()
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "effects",
		Acceptance:          []string{"safe retry"},
		ProjectID:           "effects-project",
		BaseRevision:        "abc",
		VerificationProfile: "test",
		Priority:            "P2",
	}
	task, _, err := svc.Submit(ctx, "effects-task", contract)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			s.Close()
			t.Fatal(err)
		}
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-a", "supervisor-a", time.Minute)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	return s, svc, effects.New(s), task, attempt
}

func effectIntent(task domain.Task, attempt domain.ExecutionAttempt, operationID string, payload string) domain.EffectIntent {
	return domain.EffectIntent{
		OperationID:          operationID,
		TaskID:               task.ID,
		AttemptID:            attempt.ID,
		RunEpoch:             attempt.RunEpoch,
		Type:                 domain.EffectLocalObservable,
		ExpectedPrecondition: "workspace-head=abc",
		Payload:              json.RawMessage(payload),
	}
}

func TestDispatchedEffectRequiresReconciliationAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mar.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewTaskService(s)
	ctx := context.Background()
	if _, _, err := svc.RegisterProject(ctx, "p", filepath.Join(t.TempDir(), "repo")); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{Goal: "crash", Acceptance: []string{"no duplicate"}, ProjectID: "p", BaseRevision: "abc", VerificationProfile: "test", Priority: "P2"}
	task, _, err := svc.Submit(ctx, "crash-task", contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-a", "supervisor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	mgr := effects.New(s)
	intent := effectIntent(task, attempt, "op-crash-window", `{"path":"file.txt"}`)
	if _, decision, err := mgr.Plan(ctx, intent); err != nil || decision != effects.DecisionDispatch {
		t.Fatalf("plan: decision=%s err=%v", decision, err)
	}
	if _, err := mgr.AuthorizeDispatch(ctx, intent.OperationID, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := effects.New(reopened)
	_, decision, err := restarted.Plan(ctx, intent)
	if decision != effects.DecisionReconcile || !errors.Is(err, store.ErrEffectReconcile) {
		t.Fatalf("crash-window retry must reconcile, decision=%s err=%v", decision, err)
	}
	if _, err := restarted.ObserveApplied(ctx, intent.OperationID, json.RawMessage(`{"observed":"exists"}`)); err != nil {
		t.Fatal(err)
	}
	_, decision, err = restarted.Plan(ctx, intent)
	if err != nil || decision != effects.DecisionAlreadyApplied {
		t.Fatalf("applied observation must suppress redispatch: decision=%s err=%v", decision, err)
	}
}

func TestT15CrashAfterPhysicalLocalEffectDoesNotBlindRedispatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "mar.db")
	marker := filepath.Join(root, "effect.txt")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewTaskService(s)
	ctx := context.Background()
	if _, _, err := svc.RegisterProject(ctx, "t15-project", filepath.Join(root, "repo")); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{Goal: "t15", Acceptance: []string{"no blind duplicate"}, ProjectID: "t15-project", BaseRevision: "abc", VerificationProfile: "test", Priority: "P2"}
	task, _, err := svc.Submit(ctx, "t15-task", contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-a", "supervisor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	mgr := effects.New(s)
	intent := effectIntent(task, attempt, "op-t15-real-file", `{"path":"effect.txt","action":"append-once"}`)
	if _, decision, err := mgr.Plan(ctx, intent); err != nil || decision != effects.DecisionDispatch {
		t.Fatalf("initial plan: decision=%s err=%v", decision, err)
	}
	if _, err := mgr.AuthorizeDispatch(ctx, intent.OperationID, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	// Physical effect happens, then MAR loses the chance to durably observe it.
	if err := os.WriteFile(marker, []byte("once"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := effects.New(reopened)
	_, decision, err := restarted.Plan(ctx, intent)
	if decision != effects.DecisionReconcile || !errors.Is(err, store.ErrEffectReconcile) {
		t.Fatalf("restart must reconcile rather than redispatch: decision=%s err=%v", decision, err)
	}
	actual, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != "once" {
		t.Fatalf("unexpected physical effect state before observation: %q", actual)
	}
	if _, err := restarted.ObserveApplied(ctx, intent.OperationID, json.RawMessage(`{"file_exists":true}`)); err != nil {
		t.Fatal(err)
	}
	_, decision, err = restarted.Plan(ctx, intent)
	if err != nil || decision != effects.DecisionAlreadyApplied {
		t.Fatalf("observed effect must remain suppressed: decision=%s err=%v", decision, err)
	}
	actual, err = os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != "once" {
		t.Fatalf("effect was physically repeated: %q", actual)
	}
}

func TestOperationIDCannotBeReusedWithDifferentPayload(t *testing.T) {
	s, _, mgr, task, attempt := effectHarness(t)
	defer s.Close()
	ctx := context.Background()
	intent := effectIntent(task, attempt, "op-same", `{"value":1}`)
	if _, _, err := mgr.Plan(ctx, intent); err != nil {
		t.Fatal(err)
	}
	changed := effectIntent(task, attempt, "op-same", `{"value":2}`)
	if _, _, err := mgr.Plan(ctx, changed); !errors.Is(err, store.ErrEffectIntentConflict) {
		t.Fatalf("changed payload reuse must conflict, got %v", err)
	}
}

func TestFencedAttemptCanStillPlanDispatchedEffectForReconciliation(t *testing.T) {
	s, svc, mgr, task, attempt := effectHarness(t)
	defer s.Close()
	ctx := context.Background()
	intent := effectIntent(task, attempt, "op-fenced-reconcile", `{"value":1}`)
	if _, _, err := mgr.Plan(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AuthorizeDispatch(ctx, intent.OperationID, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if err := svc.LogicalFenceAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	_, decision, err := mgr.Plan(ctx, intent)
	if decision != effects.DecisionReconcile || !errors.Is(err, store.ErrEffectReconcile) {
		t.Fatalf("fenced dispatched effect must remain reconcilable, decision=%s err=%v", decision, err)
	}
}

func TestFencedAttemptCannotPlanPreparedEffectForDispatch(t *testing.T) {
	s, svc, mgr, task, attempt := effectHarness(t)
	defer s.Close()
	ctx := context.Background()
	intent := effectIntent(task, attempt, "op-fenced-prepared", `{"value":1}`)
	if _, _, err := mgr.Plan(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if err := svc.LogicalFenceAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mgr.Plan(ctx, intent); !errors.Is(err, store.ErrStaleAttempt) {
		t.Fatalf("fenced prepared effect must not be dispatchable, got %v", err)
	}
}

func TestStaleAttemptCannotAuthorizePreparedEffect(t *testing.T) {
	s, svc, mgr, task, attempt := effectHarness(t)
	defer s.Close()
	ctx := context.Background()
	intent := effectIntent(task, attempt, "op-stale", `{"value":1}`)
	if _, _, err := mgr.Plan(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if err := svc.LogicalFenceAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AuthorizeDispatch(ctx, intent.OperationID, task.ID, attempt.ID, attempt.RunEpoch); !errors.Is(err, store.ErrStaleAttempt) {
		t.Fatalf("stale attempt authorized effect dispatch: %v", err)
	}
}

func TestObservedNotAppliedRequiresExplicitRearmBeforeRedispatch(t *testing.T) {
	s, _, mgr, task, attempt := effectHarness(t)
	defer s.Close()
	ctx := context.Background()
	intent := effectIntent(task, attempt, "op-not-applied", `{"value":1}`)
	if _, _, err := mgr.Plan(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AuthorizeDispatch(ctx, intent.OperationID, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ObserveNotApplied(ctx, intent.OperationID, json.RawMessage(`{"reason":"missing"}`)); err != nil {
		t.Fatal(err)
	}
	_, decision, err := mgr.Plan(ctx, intent)
	if err != nil || decision != effects.DecisionObservedNotApplied {
		t.Fatalf("expected explicit not-applied state, decision=%s err=%v", decision, err)
	}
	rearmed, err := mgr.RearmAfterNotApplied(ctx, intent.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if rearmed.State != domain.EffectPrepared || rearmed.ReconciliationCount != 1 {
		t.Fatalf("unexpected rearmed effect: %+v", rearmed)
	}
	_, decision, err = mgr.Plan(ctx, intent)
	if err != nil || decision != effects.DecisionDispatch {
		t.Fatalf("reconciled not-applied effect should be dispatchable: %s %v", decision, err)
	}
}

func TestConcurrentPrepareCreatesOneImmutableEffect(t *testing.T) {
	s, _, mgr, task, attempt := effectHarness(t)
	defer s.Close()
	ctx := context.Background()
	intent := effectIntent(task, attempt, "op-concurrent", `{"value":1}`)
	const n = 12
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, decision, err := mgr.Plan(ctx, intent)
			if err != nil || decision != effects.DecisionDispatch {
				errs <- errors.New("unexpected concurrent plan result")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	record, err := s.GetEffect(ctx, intent.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != domain.EffectPrepared || record.IntentHash == "" {
		t.Fatalf("unexpected effect record: %+v", record)
	}
}
