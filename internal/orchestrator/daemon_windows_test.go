//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/processctl"
	"mar/internal/resourcegov"
	"mar/internal/scheduler"
	"mar/internal/service"
	"mar/internal/testsupport"
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

func (s *fakeDaemonStore) ListBlockedTasksWithFreshSteer(ctx context.Context) ([]domain.Task, error) {
	return s.ListTasksByState(ctx, domain.TaskBlocked)
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
	store              *fakeDaemonStore
	cancel             bool
	latestControl      *domain.TaskControl
	recoveryCalls      int
	confirmCalls       int
	blockedChoiceCalls int
	reconcileCalls     int
	retryCalls         int
	exhaustCalls       int
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

func (s *fakeDaemonService) RequireExpiredPhysicalRecovery(_ context.Context, taskID, attemptID string, epoch int64) (bool, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	attempt := s.store.attempts[taskID]
	if attempt.ID != attemptID || attempt.RunEpoch != epoch {
		return false, errors.New("recovery identity mismatch")
	}
	if attempt.AuthorityState == domain.AttemptPhysicallyTerminated {
		return false, nil
	}
	if attempt.AuthorityState == domain.AttemptActive && attempt.LeaseDeadline.After(time.Now().UTC()) {
		return false, nil
	}
	s.recoveryCalls++
	task := s.store.tasks[taskID]
	task.State = domain.TaskBlocked
	s.store.tasks[taskID] = task
	attempt.AuthorityState = domain.AttemptLogicallyFenced
	s.store.attempts[taskID] = attempt
	return true, nil
}

func (s *fakeDaemonService) ConfirmAttemptProcessTermination(_ context.Context, proof processctl.TerminationProof, terminalStatus string) error {
	s.confirmCalls++
	if !proof.Valid() {
		return errors.New("invalid fake physical termination proof")
	}
	ref := proof.Attempt()
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	attempt, ok := s.store.attempts[ref.TaskID]
	if !ok || attempt.ID != ref.AttemptID || attempt.RunEpoch != ref.RunEpoch {
		return errors.New("physical termination proof identity mismatch")
	}
	confirmedAt := proof.ConfirmedAt()
	attempt.AuthorityState = domain.AttemptPhysicallyTerminated
	attempt.TerminatedAt = &confirmedAt
	attempt.TerminalStatus = terminalStatus
	s.store.attempts[ref.TaskID] = attempt
	return nil
}

func (s *fakeDaemonService) RecoverBlockedChoice(_ context.Context, taskID string) error {
	s.blockedChoiceCalls++
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	task := s.store.tasks[taskID]
	if task.State != domain.TaskBlocked {
		return errors.New("blocked-choice recovery state mismatch")
	}
	attempt, hasAttempt := s.store.attempts[taskID]
	workspace, hasWorkspace := s.store.workspace[taskID]
	if !hasWorkspace {
		if !hasAttempt && task.RunEpoch == 0 {
			task.State = domain.TaskPreflight
			task.UpdatedAt = time.Now().UTC()
			s.store.tasks[taskID] = task
			return nil
		}
		return errors.New("blocked task has no recoverable workspace")
	}
	if workspace.State != domain.WorkspaceReady {
		task.UpdatedAt = time.Now().UTC()
		s.store.tasks[taskID] = task
		return nil
	}
	if hasAttempt && attempt.AuthorityState != domain.AttemptPhysicallyTerminated {
		return errors.New("blocked choice attempted before physical termination")
	}
	task.State = domain.TaskWorkspaceReady
	task.UpdatedAt = time.Now().UTC()
	s.store.tasks[taskID] = task
	return nil
}

func (s *fakeDaemonService) ReconcileWorkspaceReady(_ context.Context, taskID string) error {
	s.reconcileCalls++
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	task := s.store.tasks[taskID]
	if task.State != domain.TaskWorkspaceReady {
		return errors.New("workspace-ready reconciliation state mismatch")
	}
	if workspace, ok := s.store.workspace[taskID]; ok && workspace.State == domain.WorkspaceReady {
		return nil
	}
	_, hasAttempt := s.store.attempts[taskID]
	if task.RunEpoch == 0 && !hasAttempt {
		if _, hasWorkspace := s.store.workspace[taskID]; !hasWorkspace {
			task.State = domain.TaskPreflight
			task.UpdatedAt = time.Now().UTC()
			s.store.tasks[taskID] = task
			return nil
		}
	}
	task.State = domain.TaskBlocked
	task.UpdatedAt = time.Now().UTC()
	s.store.tasks[taskID] = task
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

type fakeReclaimingScheduler struct {
	stepCalls    int
	reclaimCalls int
	lastLimit    int
	reclaimErr   error
}

func (s *fakeReclaimingScheduler) Step(context.Context) (scheduler.StepResult, error) {
	s.stepCalls++
	return scheduler.StepResult{Action: scheduler.ActionIdle}, nil
}

func (s *fakeReclaimingScheduler) ReclaimTerminal(_ context.Context, limit int) (int, error) {
	s.reclaimCalls++
	s.lastLimit = limit
	return 0, s.reclaimErr
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

type fakePhysicalRecoveryRunner struct {
	*fakeReadyRunner
	proof        processctl.TerminationProof
	available    bool
	err          error
	transientErr error
	recoverCalls int
}

func (r *fakePhysicalRecoveryRunner) RecoverAttemptTermination(context.Context, domain.ExecutionAttempt) (processctl.TerminationProof, bool, error) {
	r.recoverCalls++
	if r.recoverCalls == 1 && r.transientErr != nil {
		return processctl.TerminationProof{}, false, r.transientErr
	}
	return r.proof, r.available, r.err
}

func validPhysicalProofForAttempt(t *testing.T, attempt domain.ExecutionAttempt) processctl.TerminationProof {
	t.Helper()
	testsupport.RequireOutsideAppContainer(t)
	cmd, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := processctl.NewSupervisor().Start(processctl.Spec{
		Attempt: processctl.AttemptRef{TaskID: attempt.TaskID, AttemptID: attempt.ID, RunEpoch: attempt.RunEpoch},
		Path:    cmd,
		Args:    []string{"/c", "exit", "0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.CloseUnverified()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proof, err := tree.WaitAndConfirm(ctx)
	if err != nil || !proof.Valid() {
		t.Fatalf("build physical proof: proof=%+v err=%v", proof, err)
	}
	return proof
}

type fakeIntegrationRecoverer struct {
	calls        int
	retryCalls   int
	retryHandled bool
	retryErr     error
}

func (r *fakeIntegrationRecoverer) RecoverPending(context.Context) error {
	r.calls++
	return nil
}

func (r *fakeIntegrationRecoverer) RetryBlockedVerifiedIntegration(context.Context, string) (bool, error) {
	r.retryCalls++
	return r.retryHandled, r.retryErr
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

func TestDaemonTerminalWorkspaceReclaimIsBoundedAndBestEffort(t *testing.T) {
	store := &fakeDaemonStore{
		tasks:     map[string]domain.Task{},
		workspace: map[string]domain.Workspace{},
		attempts:  map[string]domain.ExecutionAttempt{},
	}
	schedulerDriver := &fakeReclaimingScheduler{reclaimErr: errors.New("fixture cleanup failure")}
	var reported []error
	daemon, err := NewDaemon(
		store,
		&fakeDaemonService{store: store},
		fakePreflightDriver{},
		schedulerDriver,
		&fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})},
		&fakeIntegrationRecoverer{},
		healthyDaemonGovernor(t),
		DaemonConfig{TerminalReclaimsPerTick: 3, ErrorSink: func(err error) { reported = append(reported, err) }},
	)
	if err != nil {
		t.Fatal(err)
	}
	daemon.step(context.Background())
	if schedulerDriver.reclaimCalls != 1 || schedulerDriver.lastLimit != 3 {
		t.Fatalf("workspace reclaim was not bounded by daemon config: calls=%d limit=%d", schedulerDriver.reclaimCalls, schedulerDriver.lastLimit)
	}
	if schedulerDriver.stepCalls != 1 {
		t.Fatalf("reclaim failure blocked normal scheduler step: calls=%d", schedulerDriver.stepCalls)
	}
	if len(reported) != 1 || !strings.Contains(reported[0].Error(), "reclaim terminal workspaces") {
		t.Fatalf("reclaim failure was not reported independently: %v", reported)
	}
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

func TestDaemonStartupConfirmsAvailableKernelRecoveryProofWithoutReplacement(t *testing.T) {
	task := domain.Task{ID: "task-recovery-proof", State: domain.TaskRunning, RunEpoch: 4}
	attempt := domain.ExecutionAttempt{ID: "attempt-recovery-proof", TaskID: task.ID, RunEpoch: 4, AuthorityState: domain.AttemptActive}
	store := &fakeDaemonStore{
		tasks:     map[string]domain.Task{task.ID: task},
		workspace: map[string]domain.Workspace{},
		attempts:  map[string]domain.ExecutionAttempt{task.ID: attempt},
	}
	svc := &fakeDaemonService{store: store}
	proof := validPhysicalProofForAttempt(t, attempt)
	runner := &fakePhysicalRecoveryRunner{
		fakeReadyRunner: &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})},
		proof:           proof,
		available:       true,
	}
	daemon, err := NewDaemon(store, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.reconcileUnprovenAttempts(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	gotTask := store.tasks[task.ID]
	gotAttempt := store.attempts[task.ID]
	store.mu.Unlock()
	if gotTask.State != domain.TaskBlocked {
		t.Fatalf("recovery proof unexpectedly re-admitted work: state=%s", gotTask.State)
	}
	if gotAttempt.AuthorityState != domain.AttemptPhysicallyTerminated || svc.confirmCalls != 1 {
		t.Fatalf("kernel recovery proof was not durably confirmed: attempt=%+v confirm_calls=%d", gotAttempt, svc.confirmCalls)
	}
	if svc.retryCalls != 0 {
		t.Fatalf("physical proof silently admitted a replacement worker: retry_calls=%d", svc.retryCalls)
	}
}

func TestDaemonPeriodicRecoverySkipsAttemptOwnedByLiveDaemon(t *testing.T) {
	task := domain.Task{ID: "task-live-recovery", State: domain.TaskRunning, RunEpoch: 2}
	attempt := domain.ExecutionAttempt{ID: "attempt-live-recovery", TaskID: task.ID, RunEpoch: 2, AuthorityState: domain.AttemptActive, LeaseDeadline: time.Now().UTC().Add(time.Minute)}
	store := &fakeDaemonStore{
		tasks:     map[string]domain.Task{task.ID: task},
		workspace: map[string]domain.Workspace{},
		attempts:  map[string]domain.ExecutionAttempt{task.ID: attempt},
	}
	svc := &fakeDaemonService{store: store}
	runner := &fakePhysicalRecoveryRunner{
		fakeReadyRunner: &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})},
		proof:           validPhysicalProofForAttempt(t, attempt),
		available:       true,
	}
	daemon, err := NewDaemon(
		store,
		svc,
		fakePreflightDriver{},
		&fakeSchedulerDriver{},
		runner,
		&fakeIntegrationRecoverer{},
		healthyDaemonGovernor(t),
		DaemonConfig{PhysicalRecoveryInterval: time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	daemon.active[task.ID] = &activeExecution{}
	daemon.lastPhysicalRecovery = time.Now().UTC().Add(-2 * time.Second)

	daemon.step(context.Background())

	store.mu.Lock()
	gotTask := store.tasks[task.ID]
	gotAttempt := store.attempts[task.ID]
	store.mu.Unlock()
	if gotTask.State != domain.TaskRunning || gotAttempt.AuthorityState != domain.AttemptActive {
		t.Fatalf("periodic recovery fenced live attempt: task=%s attempt=%s", gotTask.State, gotAttempt.AuthorityState)
	}
	if runner.recoverCalls != 0 || svc.confirmCalls != 0 {
		t.Fatalf("periodic recovery touched live attempt: recover=%d confirm=%d", runner.recoverCalls, svc.confirmCalls)
	}
}
func TestDaemonPeriodicRecoveryRetriesTransientStartupMissWithoutReplacement(t *testing.T) {
	task := domain.Task{ID: "task-recovery-transient", State: domain.TaskRunning, RunEpoch: 5}
	attempt := domain.ExecutionAttempt{ID: "attempt-recovery-transient", TaskID: task.ID, RunEpoch: 5, AuthorityState: domain.AttemptActive}
	store := &fakeDaemonStore{
		tasks:     map[string]domain.Task{task.ID: task},
		workspace: map[string]domain.Workspace{},
		attempts:  map[string]domain.ExecutionAttempt{task.ID: attempt},
	}
	svc := &fakeDaemonService{store: store}
	proof := validPhysicalProofForAttempt(t, attempt)
	runner := &fakePhysicalRecoveryRunner{
		fakeReadyRunner: &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})},
		proof:           proof,
		available:       true,
		transientErr:    errors.New("transient named job teardown"),
	}
	var reported []error
	daemon, err := NewDaemon(
		store,
		svc,
		fakePreflightDriver{},
		&fakeSchedulerDriver{},
		runner,
		&fakeIntegrationRecoverer{},
		healthyDaemonGovernor(t),
		DaemonConfig{PhysicalRecoveryInterval: time.Hour, ErrorSink: func(err error) { reported = append(reported, err) }},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := daemon.reconcileUnprovenAttempts(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	firstTask := store.tasks[task.ID]
	firstAttempt := store.attempts[task.ID]
	store.mu.Unlock()
	if firstTask.State != domain.TaskBlocked || firstAttempt.AuthorityState != domain.AttemptLogicallyFenced {
		t.Fatalf("transient recovery miss did not fail closed: task=%s attempt=%s", firstTask.State, firstAttempt.AuthorityState)
	}
	if runner.recoverCalls != 1 || svc.confirmCalls != 0 || svc.retryCalls != 0 {
		t.Fatalf("unexpected first recovery outcome: recover=%d confirm=%d retry=%d", runner.recoverCalls, svc.confirmCalls, svc.retryCalls)
	}
	if len(reported) != 1 || !strings.Contains(reported[0].Error(), "transient named job teardown") {
		t.Fatalf("transient recovery error was not reported: %v", reported)
	}

	daemon.lastPhysicalRecovery = time.Now().UTC().Add(-2 * time.Hour)
	daemon.step(context.Background())
	store.mu.Lock()
	gotTask := store.tasks[task.ID]
	gotAttempt := store.attempts[task.ID]
	store.mu.Unlock()
	if gotTask.State != domain.TaskBlocked {
		t.Fatalf("periodic physical recovery silently re-admitted task: %s", gotTask.State)
	}
	if gotAttempt.AuthorityState != domain.AttemptPhysicallyTerminated || gotAttempt.TerminalStatus != "recovered-after-daemon-restart" {
		t.Fatalf("periodic recovery did not confirm physical termination: %+v", gotAttempt)
	}
	if runner.recoverCalls != 2 || svc.confirmCalls != 1 || svc.retryCalls != 0 {
		t.Fatalf("periodic recovery admitted duplicate work: recover=%d confirm=%d retry=%d", runner.recoverCalls, svc.confirmCalls, svc.retryCalls)
	}

	daemon.step(context.Background())
	if runner.recoverCalls != 2 {
		t.Fatalf("physical recovery retried before the configured interval: calls=%d", runner.recoverCalls)
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
