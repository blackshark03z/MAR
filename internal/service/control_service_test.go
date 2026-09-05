package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/store"
)

func newControlServiceHarness(t *testing.T) (*store.SQLite, *TaskService, domain.Task) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(db)
	ctx := context.Background()
	project, _, err := svc.RegisterProject(ctx, "control-project", t.TempDir())
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal: "exercise control plane", Acceptance: []string{"controls are durable"}, ProjectID: project.ID,
		BaseRevision: "base-control", VerificationProfile: "test", Priority: "P2",
		Authority: domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
	}
	task, _, err := svc.Submit(ctx, "control-submit", contract)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db, svc, task
}

func advanceControlTaskToAttempt(t *testing.T, svc *TaskService, taskID string) domain.ExecutionAttempt {
	t.Helper()
	ctx := context.Background()
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(ctx, taskID, state); err != nil {
			t.Fatal(err)
		}
	}
	attempt, err := svc.BeginAttempt(ctx, taskID, "worker-control", "supervisor-control", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func TestSteerIsDurableIdempotentAndCannotRewriteGoalContract(t *testing.T) {
	db, svc, task := newControlServiceHarness(t)
	defer db.Close()
	advanceControlTaskToAttempt(t, svc, task.ID)
	ctx := context.Background()

	control, created, err := svc.Steer(ctx, task.ID, "steer-1", domain.SteerPayload{Kind: domain.SteerContext, Message: "failure occurs only on Windows"})
	if err != nil || !created || control.Version != 1 || !control.IntegrityValid() {
		t.Fatalf("unexpected steer publication: created=%v control=%+v err=%v", created, control, err)
	}
	replayed, created, err := svc.Steer(ctx, task.ID, "steer-1", domain.SteerPayload{Kind: domain.SteerContext, Message: "failure occurs only on Windows"})
	if err != nil || created || replayed.ID != control.ID {
		t.Fatalf("steer idempotency failed: created=%v replayed=%+v err=%v", created, replayed, err)
	}
	_, _, err = svc.Steer(ctx, task.ID, "steer-1", domain.SteerPayload{Kind: domain.SteerPriority, Message: "different content"})
	if !errors.Is(err, store.ErrControlIdempotencyConflict) {
		t.Fatalf("same steer idempotency key accepted different content: %v", err)
	}
	current, err := svc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ContractHash != task.ContractHash || current.Contract.Goal != task.Contract.Goal {
		t.Fatalf("steering rewrote immutable Goal Contract: %+v", current.Contract)
	}
}

func TestInputRequiresInputRequiredAndAtomicallyResumesAttempt(t *testing.T) {
	db, svc, task := newControlServiceHarness(t)
	defer db.Close()
	attempt := advanceControlTaskToAttempt(t, svc, task.ID)
	ctx := context.Background()
	if _, _, err := svc.Input(ctx, task.ID, "input-too-early", domain.InputPayload{Message: "answer"}); !errors.Is(err, store.ErrStateConflict) {
		t.Fatalf("input outside INPUT_REQUIRED was accepted: %v", err)
	}
	if err := svc.TransitionForAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, domain.TaskInputRequired); err != nil {
		t.Fatal(err)
	}
	control, created, err := svc.Input(ctx, task.ID, "input-1", domain.InputPayload{Message: "choose option A"})
	if err != nil || !created || control.Kind != domain.ControlInput {
		t.Fatalf("input publication failed: created=%v control=%+v err=%v", created, control, err)
	}
	current, err := svc.Status(ctx, task.ID)
	if err != nil || current.State != domain.TaskRunning {
		t.Fatalf("input did not atomically resume active attempt: state=%s err=%v", current.State, err)
	}
	replayed, created, err := svc.Input(ctx, task.ID, "input-1", domain.InputPayload{Message: "choose option A"})
	if err != nil || created || replayed.ID != control.ID {
		t.Fatalf("input retry was not idempotent after resume: created=%v replayed=%+v err=%v", created, replayed, err)
	}
}

func TestCancelLogicallyFencesRunningAttemptBeforeFinalCancellation(t *testing.T) {
	db, svc, task := newControlServiceHarness(t)
	defer db.Close()
	attempt := advanceControlTaskToAttempt(t, svc, task.ID)
	ctx := context.Background()
	control, created, err := svc.Cancel(ctx, task.ID, "cancel-1", domain.CancelPayload{Reason: "owner requested stop"})
	if err != nil || !created || control.Kind != domain.ControlCancel {
		t.Fatalf("cancel request failed: created=%v control=%+v err=%v", created, control, err)
	}
	authoritative, err := svc.AttemptAuthoritative(ctx, task.ID, attempt.ID, attempt.RunEpoch)
	if err != nil || authoritative {
		t.Fatalf("cancel did not logically fence active attempt: authoritative=%v err=%v", authoritative, err)
	}
	current, err := svc.Status(ctx, task.ID)
	if err != nil || current.State == domain.TaskCancelled {
		t.Fatalf("running task finalized CANCELLED before physical termination: state=%s err=%v", current.State, err)
	}
	if err := svc.FinalizeCancellation(ctx, task.ID); !errors.Is(err, store.ErrPhysicalFenceRequired) {
		t.Fatalf("cancellation finalized without physical fence: %v", err)
	}
	if err := db.ConfirmAttemptTerminated(ctx, task.ID, attempt.ID, attempt.RunEpoch, "cancelled", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := svc.FinalizeCancellation(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	current, err = svc.Status(ctx, task.ID)
	if err != nil || current.State != domain.TaskCancelled {
		t.Fatalf("physically terminated cancellation did not finalize: state=%s err=%v", current.State, err)
	}
}

func TestSteerCancelUsesAuthoritativeCancellationPath(t *testing.T) {
	db, svc, task := newControlServiceHarness(t)
	defer db.Close()
	attempt := advanceControlTaskToAttempt(t, svc, task.ID)
	control, created, err := svc.Steer(context.Background(), task.ID, "steer-cancel", domain.SteerPayload{Kind: domain.SteerCancel, Message: "stop this task"})
	if err != nil || !created || control.Kind != domain.ControlCancel {
		t.Fatalf("steer cancellation did not use cancellation path: created=%v control=%+v err=%v", created, control, err)
	}
	authoritative, err := svc.AttemptAuthoritative(context.Background(), task.ID, attempt.ID, attempt.RunEpoch)
	if err != nil || authoritative {
		t.Fatalf("steer cancellation did not fence active attempt: authoritative=%v err=%v", authoritative, err)
	}
}

func TestPreAttemptCancelFinalizesImmediately(t *testing.T) {
	db, svc, task := newControlServiceHarness(t)
	defer db.Close()
	control, created, err := svc.Cancel(context.Background(), task.ID, "cancel-before", domain.CancelPayload{})
	if err != nil || !created || control.Version != 1 {
		t.Fatalf("pre-attempt cancel failed: created=%v control=%+v err=%v", created, control, err)
	}
	current, err := svc.Status(context.Background(), task.ID)
	if err != nil || current.State != domain.TaskCancelled {
		t.Fatalf("pre-attempt cancellation did not finalize safely: state=%s err=%v", current.State, err)
	}
}
func TestConcurrentClientsAppendOneMonotonicControlStream(t *testing.T) {
	db, svc, task := newControlServiceHarness(t)
	defer db.Close()
	advanceControlTaskToAttempt(t, svc, task.ID)

	const clients = 8
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, created, err := svc.Steer(context.Background(), task.ID, fmt.Sprintf("client-%d", i), domain.SteerPayload{
				Kind: domain.SteerContext, Message: fmt.Sprintf("fact-%d", i),
			})
			if err != nil {
				errs <- err
				return
			}
			if !created {
				errs <- errors.New("unique client control unexpectedly replayed")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	controls, err := svc.ControlsSince(context.Background(), task.ID, 0, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(controls) != clients {
		t.Fatalf("concurrent clients created %d controls, want %d", len(controls), clients)
	}
	for i, control := range controls {
		if control.Version != int64(i+1) || !control.IntegrityValid() {
			t.Fatalf("control stream is not one monotonic durable truth: index=%d control=%+v", i, control)
		}
	}
}
