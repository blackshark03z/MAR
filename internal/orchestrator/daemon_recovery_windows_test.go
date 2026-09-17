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
	workspace := domain.Workspace{ID: "workspace-blocked-choice", TaskID: task.ID, State: domain.WorkspaceReady}
	raw, err := json.Marshal(domain.SteerPayload{Kind: domain.SteerBlockedChoice, Message: "Use option B"})
	if err != nil {
		t.Fatal(err)
	}
	control := domain.TaskControl{ID: "control-blocked-choice", TaskID: task.ID, Version: 1, IdempotencyKey: "choice-1", Kind: domain.ControlSteer, Payload: raw, CreatedAt: now.Add(-time.Second)}
	store := &fakeDaemonStore{tasks: map[string]domain.Task{task.ID: task}, workspace: map[string]domain.Workspace{task.ID: workspace}, attempts: map[string]domain.ExecutionAttempt{task.ID: attempt}}
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
	if svc.blockedChoiceCalls != 1 {
		t.Fatalf("blocked choice did not use phase-aware recovery exactly once: calls=%d", svc.blockedChoiceCalls)
	}
	if err := daemon.driveBlockedChoices(context.Background()); err != nil {
		t.Fatal(err)
	}
	if svc.blockedChoiceCalls != 1 {
		t.Fatalf("same blocked choice replayed recovery: calls=%d", svc.blockedChoiceCalls)
	}
}

func TestDaemonBlockedChoiceBeforeWorkspaceReturnsToPreflight(t *testing.T) {
	now := time.Now().UTC()
	task := domain.Task{ID: "task-pre-workspace-blocked-choice", State: domain.TaskBlocked, RunEpoch: 0, UpdatedAt: now.Add(-2 * time.Second)}
	raw, err := json.Marshal(domain.SteerPayload{Kind: domain.SteerBlockedChoice, Message: "preflight prerequisite resolved"})
	if err != nil {
		t.Fatal(err)
	}
	control := domain.TaskControl{ID: "control-pre-workspace", TaskID: task.ID, Version: 1, IdempotencyKey: "choice-pre-workspace-1", Kind: domain.ControlSteer, Payload: raw, CreatedAt: now.Add(-time.Second)}
	store := &fakeDaemonStore{tasks: map[string]domain.Task{task.ID: task}, workspace: map[string]domain.Workspace{}, attempts: map[string]domain.ExecutionAttempt{}}
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
	_, hasWorkspace := store.workspace[task.ID]
	store.mu.Unlock()
	if got.State != domain.TaskPreflight || got.RunEpoch != 0 || hasWorkspace {
		t.Fatalf("pre-workspace blocked choice skipped lifecycle phase: state=%s epoch=%d workspace=%v", got.State, got.RunEpoch, hasWorkspace)
	}
}

func TestDaemonWorkspaceReadyWithoutWorkspaceReconcilesBeforeLaunch(t *testing.T) {
	task := domain.Task{ID: "task-workspace-ready-missing", State: domain.TaskWorkspaceReady, RunEpoch: 0, UpdatedAt: time.Now().UTC()}
	store := &fakeDaemonStore{tasks: map[string]domain.Task{task.ID: task}, workspace: map[string]domain.Workspace{}, attempts: map[string]domain.ExecutionAttempt{}}
	svc := &fakeDaemonService{store: store}
	runner := &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	daemon, err := NewDaemon(store, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{MaxConcurrentWorkers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.launchReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	got := store.tasks[task.ID]
	store.mu.Unlock()
	if got.State != domain.TaskPreflight || svc.reconcileCalls != 1 {
		t.Fatalf("missing workspace was not reconciled: state=%s calls=%d", got.State, svc.reconcileCalls)
	}
	select {
	case <-runner.started:
		t.Fatal("worker launched before WORKSPACE_READY invariant was repaired")
	default:
	}
}

func TestDaemonBlockedChoiceRetriesVerifiedIntegrationWithoutReplacement(t *testing.T) {
	now := time.Now().UTC()
	task := domain.Task{ID: "task-verified-integration-retry", State: domain.TaskBlocked, RunEpoch: 1, UpdatedAt: now.Add(-2 * time.Second)}
	attempt := domain.ExecutionAttempt{ID: "attempt-verified-integration-retry", TaskID: task.ID, RunEpoch: 1, AuthorityState: domain.AttemptPhysicallyTerminated}
	raw, err := json.Marshal(domain.SteerPayload{Kind: domain.SteerBlockedChoice, Message: "external integration prerequisite resolved"})
	if err != nil {
		t.Fatal(err)
	}
	control := domain.TaskControl{ID: "control-verified-integration-retry", TaskID: task.ID, Version: 1, IdempotencyKey: "choice-verified-integration-1", Kind: domain.ControlSteer, Payload: raw, CreatedAt: now.Add(-time.Second)}
	store := &fakeDaemonStore{tasks: map[string]domain.Task{task.ID: task}, workspace: map[string]domain.Workspace{}, attempts: map[string]domain.ExecutionAttempt{task.ID: attempt}}
	svc := &fakeDaemonService{store: store, latestControl: &control}
	integration := &fakeIntegrationRecoverer{retryHandled: true}
	daemon, err := NewDaemon(store, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}, integration, healthyDaemonGovernor(t), DaemonConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.driveBlockedChoices(context.Background()); err != nil {
		t.Fatal(err)
	}
	if integration.retryCalls != 1 {
		t.Fatalf("verified integration retry was not offered exactly once: calls=%d", integration.retryCalls)
	}
	if svc.blockedChoiceCalls != 0 || svc.retryCalls != 0 {
		t.Fatalf("verified integration retry incorrectly opened coding recovery: blocked=%d retry=%d", svc.blockedChoiceCalls, svc.retryCalls)
	}
	store.mu.Lock()
	got := store.tasks[task.ID]
	store.mu.Unlock()
	if got.RunEpoch != 1 || got.State != domain.TaskBlocked {
		t.Fatalf("daemon dispatch mutated coding epoch/state itself: state=%s run_epoch=%d", got.State, got.RunEpoch)
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
