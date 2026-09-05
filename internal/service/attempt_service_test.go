package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/service"
	"mar/internal/store"
)

func readyTask(t *testing.T, svc *service.TaskService, key string) domain.Task {
	t.Helper()
	ctx := context.Background()
	task, _, err := svc.Submit(ctx, key, contract("attempt fencing"))
	if err != nil {
		t.Fatal(err)
	}
	for _, next := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(ctx, task.ID, next); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestBeginAttemptAdvancesEpochAndRunsTask(t *testing.T) {
	_, svc, _ := newHarness(t)
	task := readyTask(t, svc, "attempt-start")
	if task.RunEpoch != 0 || task.State != domain.TaskWorkspaceReady {
		t.Fatalf("unexpected ready task: %#v", task)
	}

	attempt, err := svc.BeginAttempt(context.Background(), task.ID, "worker-a", "supervisor-a", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.RunEpoch != 1 || attempt.AuthorityState != domain.AttemptActive {
		t.Fatalf("unexpected attempt: %#v", attempt)
	}
	got, err := svc.Status(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RunEpoch != 1 || got.State != domain.TaskRunning {
		t.Fatalf("task did not advance with attempt: %#v", got)
	}
}

func TestLogicalFenceDoesNotPermitReplacementUntilPhysicalTermination(t *testing.T) {
	_, svc, _ := newHarness(t)
	task := readyTask(t, svc, "physical-fence")
	ctx := context.Background()

	a, err := svc.BeginAttempt(ctx, task.ID, "worker-a", "supervisor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.LogicalFenceAttempt(ctx, task.ID, a.ID, a.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecoverForReplacement(ctx, task.ID); !errors.Is(err, store.ErrPhysicalFenceRequired) {
		t.Fatalf("logical fence must not unlock workspace, got %v", err)
	}
	if err := svc.HeartbeatAttempt(ctx, task.ID, a.ID, a.RunEpoch, time.Minute); !errors.Is(err, store.ErrStaleAttempt) {
		t.Fatalf("expected stale heartbeat after logical fence, got %v", err)
	}
}

func TestLogicalFenceRejectsFurtherHeartbeatBeforeReplacement(t *testing.T) {
	_, svc, _ := newHarness(t)
	task := readyTask(t, svc, "logical-fence")
	ctx := context.Background()
	a, err := svc.BeginAttempt(ctx, task.ID, "worker-a", "supervisor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.LogicalFenceAttempt(ctx, task.ID, a.ID, a.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if err := svc.HeartbeatAttempt(ctx, task.ID, a.ID, a.RunEpoch, time.Minute); !errors.Is(err, store.ErrStaleAttempt) {
		t.Fatalf("expected stale heartbeat after logical fence, got %v", err)
	}
}

func TestValidateAttemptAuthorityRejectsFencedAndWrongEpoch(t *testing.T) {
	_, svc, _ := newHarness(t)
	task := readyTask(t, svc, "attempt-authority-validation")
	ctx := context.Background()
	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-authority", "supervisor-authority", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateAttemptAuthority(ctx, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatalf("active current attempt rejected: %v", err)
	}
	if err := svc.ValidateAttemptAuthority(ctx, task.ID, attempt.ID, attempt.RunEpoch+1); !errors.Is(err, store.ErrStaleAttempt) {
		t.Fatalf("wrong epoch not fenced: %v", err)
	}
	if err := svc.LogicalFenceAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateAttemptAuthority(ctx, task.ID, attempt.ID, attempt.RunEpoch); !errors.Is(err, store.ErrStaleAttempt) {
		t.Fatalf("logically fenced attempt remained authoritative: %v", err)
	}
}

func TestConcurrentBeginAttemptAdmitsOnlyOneWriter(t *testing.T) {
	_, svc, _ := newHarness(t)
	task := readyTask(t, svc, "concurrent-attempt")
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	attempts := make(chan domain.ExecutionAttempt, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			a, err := svc.BeginAttempt(ctx, task.ID, "worker", "supervisor", time.Minute)
			if err != nil {
				errs <- err
				return
			}
			attempts <- a
		}()
	}
	wg.Wait()
	close(attempts)
	close(errs)

	successes := 0
	for range attempts {
		successes++
	}
	if successes != 1 {
		t.Fatalf("expected one admitted attempt, got %d", successes)
	}
	for err := range errs {
		if !errors.Is(err, store.ErrStateConflict) && !errors.Is(err, store.ErrPhysicalFenceRequired) {
			t.Fatalf("unexpected concurrent begin error: %v", err)
		}
	}
}
