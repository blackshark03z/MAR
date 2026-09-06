//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/resourcegov"
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
	latestControl *domain.TaskControl
	recoveryCalls int
	retryCalls    int
	exhaustCalls  int
}

func (s *fakeDaemonService) StatusSnapshot(_ context.Context, taskID string) (service.TaskStatusSnapshot, error) {
	s.store.mu.Lock()
	task := s.store.tasks[taskID]
	s.store.mu.Unlock()
	return service.TaskStatusSnapshot{Task: task, LatestControl: s.latestControl, CancelRequested: s.cancel}, nil
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

func (s *fakeDaemonService) RecoverForReplacement(_ context.Context, taskID string) error {
	s.retryCalls++
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	task := s.store.tasks[taskID]
	if task.State != domain.TaskRetryWait && task.State != domain.TaskBlocked && task.State != domain.TaskRunning {
		return errors.New("replacement recovery state mismatch")
	}
	if attempt, ok := s.store.attempts[taskID]; ok && attempt.AuthorityState != domain.AttemptPhysicallyTerminated {
		return errors.New("retry attempted before physical termination")
	}
	task.State = domain.TaskWorkspaceReady
	task.UpdatedAt = time.Now().UTC()
	s.store.tasks[taskID] = task
	return nil
}

func (s *fakeDaemonService) ExhaustRetryBudget(_ context.Context, taskID string) error {
	s.exhaustCalls++
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	task := s.store.tasks[taskID]
	if task.State != domain.TaskRetryWait {
		return errors.New("retry exhaustion state mismatch")
	}
	task.State = domain.TaskBlocked
	s.store.tasks[taskID] = task
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

type mutableDaemonSensor struct {
	mu       sync.Mutex
	snapshot resourcegov.Snapshot
}

func (s *mutableDaemonSensor) Snapshot(context.Context) (resourcegov.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot, nil
}

func (s *mutableDaemonSensor) set(snapshot resourcegov.Snapshot) {
	s.mu.Lock()
	s.snapshot = snapshot
	s.mu.Unlock()
}

func healthyDaemonSnapshot() resourcegov.Snapshot {
	return resourcegov.Snapshot{
		CPUKnown:          true,
		CPUPercent:        1,
		MemoryLoadPercent: 10,
		TotalRAMBytes:     16 << 30,
		AvailableRAMBytes: 12 << 30,
		FreeDiskBytes:     100 << 30,
		TotalDiskBytes:    200 << 30,
		MARDiskUsedBytes:  1 << 30,
	}
}

func daemonGovernor(t *testing.T, sensor resourcegov.Sensor, maxHeavy, maxPerProject int) *resourcegov.Governor {
	t.Helper()
	governor, err := resourcegov.New(sensor, resourcegov.Config{
		MaxCPUPercent:           100,
		MaxMemoryLoadPercent:    100,
		MaxIOPressurePercent:    100,
		MinFreeRAMBytes:         1,
		MinFreeDiskBytes:        1,
		MaxMARDiskBytes:         1 << 40,
		MaxHeavyJobs:            maxHeavy,
		MaxHeavyJobsPerProject:  maxPerProject,
		MaxHeavyJobsInteractive: maxHeavy,
	})
	if err != nil {
		t.Fatal(err)
	}
	return governor
}

func healthyDaemonGovernor(t *testing.T) *resourcegov.Governor {
	return daemonGovernor(t, &mutableDaemonSensor{snapshot: healthyDaemonSnapshot()}, 8, 8)
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
	daemon, err := NewDaemon(store, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{})
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
	task := domain.Task{ID: "task-cancel", State: domain.TaskWorkspaceReady, Contract: domain.GoalContract{ProjectID: "project-cancel"}}
	workspace := domain.Workspace{ID: "workspace-cancel", TaskID: task.ID, State: domain.WorkspaceReady, Path: `D:\MAR\cancel-workspace`}
	store := &fakeDaemonStore{
		tasks:     map[string]domain.Task{task.ID: task},
		workspace: map[string]domain.Workspace{task.ID: workspace},
		attempts:  map[string]domain.ExecutionAttempt{},
	}
	svc := &fakeDaemonService{store: store, cancel: true}
	runner := &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	daemon, err := NewDaemon(store, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{PollInterval: 10 * time.Millisecond, ControlPollInterval: 5 * time.Millisecond, MaxConcurrentWorkers: 1})
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
	task := domain.Task{ID: "task-shutdown", State: domain.TaskWorkspaceReady, Contract: domain.GoalContract{ProjectID: "project-shutdown"}}
	workspace := domain.Workspace{ID: "workspace-shutdown", TaskID: task.ID, State: domain.WorkspaceReady, Path: `D:\MAR\shutdown-workspace`}
	store := &fakeDaemonStore{
		tasks:     map[string]domain.Task{task.ID: task},
		workspace: map[string]domain.Workspace{task.ID: workspace},
		attempts:  map[string]domain.ExecutionAttempt{},
	}
	runner := &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	daemon, err := NewDaemon(store, &fakeDaemonService{store: store}, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{PollInterval: 10 * time.Millisecond, ControlPollInterval: 10 * time.Millisecond, MaxConcurrentWorkers: 1})
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
