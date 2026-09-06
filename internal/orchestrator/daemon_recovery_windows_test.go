//go:build windows

package orchestrator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestDaemonBlockedChoiceResumesReplacementExactlyOnce(t *testing.T) {
	now := time.Now().UTC()
	task := domain.Task{ID: "task-blocked-choice", State: domain.TaskBlocked, RunEpoch: 1, UpdatedAt: now.Add(-2 * time.Second)}
	attempt := domain.ExecutionAttempt{ID: "attempt-blocked-choice", TaskID: task.ID, RunEpoch: 1, AuthorityState: domain.AttemptPhysicallyTerminated}
	raw, err := json.Marshal(domain.SteerPayload{Kind: domain.SteerBlockedChoice, Message: "Use option B"})
	if err != nil {
		t.Fatal(err)
	}
	control := domain.TaskControl{ID: "control-blocked-choice", TaskID: task.ID, Version: 1, IdempotencyKey: "choice-1", Kind: domain.ControlSteer, Payload: raw, CreatedAt: now.Add(-time.Second)}
	store := &fakeDaemonStore{tasks: map[string]domain.Task{task.ID: task}, workspace: map[string]domain.Workspace{}, attempts: map[string]domain.ExecutionAttempt{task.ID: attempt}}
	svc := &fakeDaemonService{store: store, latestControl: &control}
	daemon, err := NewDaemon(store, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.driveBlockedChoices(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	got := store.tasks[task.ID]
	got.State = domain.TaskBlocked
	got.UpdatedAt = now
	store.tasks[task.ID] = got
	store.mu.Unlock()
	if svc.retryCalls != 1 {
		t.Fatalf("blocked choice did not admit exactly one replacement: retry_calls=%d", svc.retryCalls)
	}
	if err := daemon.driveBlockedChoices(context.Background()); err != nil {
		t.Fatal(err)
	}
	if svc.retryCalls != 1 {
		t.Fatalf("same blocked choice replayed replacement: retry_calls=%d", svc.retryCalls)
	}
}

func TestDaemonRetryWaitRecoversOnlyAfterPhysicalTermination(t *testing.T) {
	now := time.Now().UTC()
	task := domain.Task{ID: "task-retry", State: domain.TaskRetryWait, RunEpoch: 1, UpdatedAt: now.Add(-2 * time.Second)}
	attempt := domain.ExecutionAttempt{ID: "attempt-retry", TaskID: task.ID, RunEpoch: 1, AuthorityState: domain.AttemptPhysicallyTerminated}
	store := &fakeDaemonStore{tasks: map[string]domain.Task{task.ID: task}, workspace: map[string]domain.Workspace{}, attempts: map[string]domain.ExecutionAttempt{task.ID: attempt}}
	svc := &fakeDaemonService{store: store}
	daemon, err := NewDaemon(store, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{RetryDelay: time.Millisecond, MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.driveRetries(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	got := store.tasks[task.ID]
	store.mu.Unlock()
	if got.State != domain.TaskWorkspaceReady || svc.retryCalls != 1 || svc.exhaustCalls != 0 {
		t.Fatalf("retry was not safely re-admitted: state=%s retry=%d exhaust=%d", got.State, svc.retryCalls, svc.exhaustCalls)
	}

	unsafe := domain.Task{ID: "task-retry-unsafe", State: domain.TaskRetryWait, RunEpoch: 1, UpdatedAt: now.Add(-2 * time.Second)}
	unsafeAttempt := domain.ExecutionAttempt{ID: "attempt-retry-unsafe", TaskID: unsafe.ID, RunEpoch: 1, AuthorityState: domain.AttemptLogicallyFenced}
	store.tasks[unsafe.ID] = unsafe
	store.attempts[unsafe.ID] = unsafeAttempt
	before := svc.retryCalls
	if err := daemon.driveRetries(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	unsafeGot := store.tasks[unsafe.ID]
	store.mu.Unlock()
	if unsafeGot.State != domain.TaskRetryWait || svc.retryCalls != before {
		t.Fatalf("retry bypassed physical fencing: state=%s retry_calls=%d before=%d", unsafeGot.State, svc.retryCalls, before)
	}
}

func TestDaemonRetryBudgetExhaustionBlocksWithoutReplacement(t *testing.T) {
	task := domain.Task{ID: "task-retry-exhausted", State: domain.TaskRetryWait, RunEpoch: 3, UpdatedAt: time.Now().UTC().Add(-2 * time.Second)}
	attempt := domain.ExecutionAttempt{ID: "attempt-retry-exhausted", TaskID: task.ID, RunEpoch: 3, AuthorityState: domain.AttemptPhysicallyTerminated}
	store := &fakeDaemonStore{tasks: map[string]domain.Task{task.ID: task}, workspace: map[string]domain.Workspace{}, attempts: map[string]domain.ExecutionAttempt{task.ID: attempt}}
	svc := &fakeDaemonService{store: store}
	daemon, err := NewDaemon(store, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{RetryDelay: time.Millisecond, MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.driveRetries(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	got := store.tasks[task.ID]
	store.mu.Unlock()
	if got.State != domain.TaskBlocked || svc.exhaustCalls != 1 || svc.retryCalls != 0 {
		t.Fatalf("retry budget did not fail closed: state=%s exhaust=%d retry=%d", got.State, svc.exhaustCalls, svc.retryCalls)
	}
}
