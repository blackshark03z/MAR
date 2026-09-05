//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/scheduler"
	"mar/internal/service"
)

type fakeDaemonStore struct {
	mu        sync.Mutex
	tasks     map[string]domain.Task
	workspace map[string]domain.Workspace
	attempts  map[string]domain.ExecutionAttempt
}

func (s *fakeDaemonStore) ListTasksByState(_ context.Context, state domain.TaskState) ([]domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Task
	for _, task := range s.tasks {
		if task.State == state {
			out = append(out, task)
		}
	}
	return out, nil
}

func (s *fakeDaemonStore) GetWorkspaceByTask(_ context.Context, taskID string) (domain.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspace, ok := s.workspace[taskID]
	if !ok {
		return domain.Workspace{}, errors.New("workspace not found")
	}
	return workspace, nil
}

func (s *fakeDaemonStore) CurrentAttemptByTask(_ context.Context, taskID string) (domain.ExecutionAttempt, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt, ok := s.attempts[taskID]
	return attempt, ok, nil
}

type fakeDaemonService struct {
	store         *fakeDaemonStore
	cancel        bool
	recoveryCalls int
}

func (s *fakeDaemonService) StatusSnapshot(context.Context, string) (service.TaskStatusSnapshot, error) {
	return service.TaskStatusSnapshot{CancelRequested: s.cancel}, nil
}

func (s *fakeDaemonService) RequirePhysicalRecovery(_ context.Context, taskID, attemptID string, epoch int64) error {
	s.recoveryCalls++
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	task := s.store.tasks[taskID]
	task.State = domain.TaskBlocked
	s.store.tasks[taskID] = task
	attempt := s.store.attempts[taskID]
	if attempt.ID != attemptID || attempt.RunEpoch != epoch {
		return errors.New("recovery identity mismatch")
	}
	attempt.AuthorityState = domain.AttemptLogicallyFenced
	s.store.attempts[taskID] = attempt
	return nil
}

type fakePreflightDriver struct{}

func (fakePreflightDriver) Drive(context.Context, string) error { return nil }

type fakeSchedulerDriver struct{ calls int }

func (s *fakeSchedulerDriver) Step(context.Context) (scheduler.StepResult, error) {
	s.calls++
	return scheduler.StepResult{Action: scheduler.ActionIdle}, nil
}

type fakeReadyRunner struct {
	started   chan struct{}
	stopped   chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

func (r *fakeReadyRunner) RunWorkspaceReady(ctx context.Context, taskID string, workspace domain.Workspace) (RunOutcome, error) {
	r.startOnce.Do(func() { close(r.started) })
	<-ctx.Done()
	r.stopOnce.Do(func() { close(r.stopped) })
	return RunOutcome{TaskID: taskID}, ctx.Err()
}

type fakeIntegrationRecoverer struct{ calls int }

func (r *fakeIntegrationRecoverer) RecoverPending(context.Context) error {
	r.calls++
	return nil
}

func TestDaemonStartupFencesUnprovenAttemptAndBlocksTask(t *testing.T) {
	task := domain.Task{ID: "task-recovery", State: domain.TaskRunning, RunEpoch: 3}
	attempt := domain.ExecutionAttempt{ID: "attempt-recovery", TaskID: task.ID, RunEpoch: 3, AuthorityState: domain.AttemptActive}
	store := &fakeDaemonStore{
		tasks:     map[string]domain.Task{task.ID: task},
		workspace: map[string]domain.Workspace{},
		attempts:  map[string]domain.ExecutionAttempt{task.ID: attempt},
	}
	svc := &fakeDaemonService{store: store}
	daemon, err := NewDaemon(store, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}, &fakeIntegrationRecoverer{}, DaemonConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.reconcileUnprovenAttempts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if svc.recoveryCalls != 1 {
		t.Fatalf("expected one recovery fence, got %d", svc.recoveryCalls)
	}
	store.mu.Lock()
	gotTask := store.tasks[task.ID]
	gotAttempt := store.attempts[task.ID]
	store.mu.Unlock()
	if gotTask.State != domain.TaskBlocked || gotAttempt.AuthorityState != domain.AttemptLogicallyFenced {
		t.Fatalf("fail-closed recovery mismatch: task=%s attempt=%s", gotTask.State, gotAttempt.AuthorityState)
	}
}

func TestDaemonDurableCancelWatcherCancelsActiveWorkerContext(t *testing.T) {
	task := domain.Task{ID: "task-cancel", State: domain.TaskWorkspaceReady}
	workspace := domain.Workspace{ID: "workspace-cancel", TaskID: task.ID, State: domain.WorkspaceReady, Path: `D:\MAR\cancel-workspace`}
	store := &fakeDaemonStore{
		tasks:     map[string]domain.Task{task.ID: task},
		workspace: map[string]domain.Workspace{task.ID: workspace},
		attempts:  map[string]domain.ExecutionAttempt{},
	}
	svc := &fakeDaemonService{store: store, cancel: true}
	runner := &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	daemon, err := NewDaemon(store, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, DaemonConfig{PollInterval: 10 * time.Millisecond, ControlPollInterval: 5 * time.Millisecond, MaxConcurrentWorkers: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx) }()
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	select {
	case <-runner.stopped:
	case <-time.After(time.Second):
		t.Fatal("durable cancellation did not cancel worker context")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("daemon shutdown mismatch: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("daemon did not drain after cancellation")
	}
}

func TestDaemonShutdownCancelsAndDrainsActiveWorkers(t *testing.T) {
	task := domain.Task{ID: "task-shutdown", State: domain.TaskWorkspaceReady}
	workspace := domain.Workspace{ID: "workspace-shutdown", TaskID: task.ID, State: domain.WorkspaceReady, Path: `D:\MAR\shutdown-workspace`}
	store := &fakeDaemonStore{
		tasks:     map[string]domain.Task{task.ID: task},
		workspace: map[string]domain.Workspace{task.ID: workspace},
		attempts:  map[string]domain.ExecutionAttempt{},
	}
	runner := &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	daemon, err := NewDaemon(store, &fakeDaemonService{store: store}, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, DaemonConfig{PollInterval: 10 * time.Millisecond, ControlPollInterval: 10 * time.Millisecond, MaxConcurrentWorkers: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx) }()
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	select {
	case <-runner.stopped:
	case <-time.After(time.Second):
		t.Fatal("daemon shutdown did not cancel active worker")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("daemon shutdown mismatch: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("daemon did not wait for active worker drain")
	}
	if daemon.ActiveCount() != 0 {
		t.Fatalf("active worker leaked after shutdown: %d", daemon.ActiveCount())
	}
}
