//go:build windows

package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/resourcegov"
)

type trackingReadyRunner struct {
	mu      sync.Mutex
	started []string
	stopped []string
	change  chan struct{}
}

func newTrackingReadyRunner() *trackingReadyRunner {
	return &trackingReadyRunner{change: make(chan struct{}, 64)}
}

func (r *trackingReadyRunner) RunWorkspaceReady(ctx context.Context, taskID string, _ domain.Workspace) (RunOutcome, error) {
	r.mu.Lock()
	r.started = append(r.started, taskID)
	r.mu.Unlock()
	r.change <- struct{}{}
	<-ctx.Done()
	r.mu.Lock()
	r.stopped = append(r.stopped, taskID)
	r.mu.Unlock()
	r.change <- struct{}{}
	return RunOutcome{TaskID: taskID}, ctx.Err()
}

func (r *trackingReadyRunner) counts() (started, stopped int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.started), len(r.stopped)
}

func waitTrackingCount(t *testing.T, runner *trackingReadyRunner, wantStarted, wantStopped int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		started, stopped := runner.counts()
		if started >= wantStarted && stopped >= wantStopped {
			return
		}
		select {
		case <-runner.change:
		case <-time.After(10 * time.Millisecond):
		}
	}
	started, stopped := runner.counts()
	t.Fatalf("runner counts did not converge: started=%d/%d stopped=%d/%d", started, wantStarted, stopped, wantStopped)
}

func daemonTaskFixture(id, project string) (domain.Task, domain.Workspace) {
	task := domain.Task{ID: id, State: domain.TaskWorkspaceReady, Contract: domain.GoalContract{ProjectID: project}}
	workspace := domain.Workspace{ID: "ws-" + id, TaskID: id, ProjectID: project, State: domain.WorkspaceReady, Path: `D:\MAR\` + id}
	return task, workspace
}

func drainTrackingDaemon(t *testing.T, daemon *Daemon, cancel context.CancelFunc) {
	t.Helper()
	cancel()
	done := make(chan struct{})
	go func() { daemon.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("workers did not drain")
	}
}

func TestAcceptanceT5FiveTasksSameProjectCanExecuteConcurrentlyWhenConfigured(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tasks := map[string]domain.Task{}
	workspaces := map[string]domain.Workspace{}
	for i := 1; i <= 5; i++ {
		id := string(rune('0' + i))
		task, ws := daemonTaskFixture("t5-"+id, "t5-project")
		tasks[task.ID], workspaces[task.ID] = task, ws
	}
	store := &fakeDaemonStore{tasks: tasks, workspace: workspaces, attempts: map[string]domain.ExecutionAttempt{}}
	governor := daemonGovernor(t, &mutableDaemonSensor{snapshot: healthyDaemonSnapshot()}, 5, 5)
	runner := newTrackingReadyRunner()
	daemon, err := NewDaemon(store, &fakeDaemonService{store: store}, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, governor, DaemonConfig{MaxConcurrentWorkers: 5, ExecutionRAMReservation: 1, ExecutionDiskReservation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.launchReady(ctx); err != nil {
		t.Fatal(err)
	}
	waitTrackingCount(t, runner, 5, 0)
	if daemon.ActiveCount() != 5 || len(governor.Active()) != 5 {
		t.Fatalf("T5 did not reach five concurrent same-project workers: active=%d claims=%d", daemon.ActiveCount(), len(governor.Active()))
	}
	drainTrackingDaemon(t, daemon, cancel)
	if daemon.ActiveCount() != 0 || len(governor.Active()) != 0 {
		t.Fatalf("T5 parallel execution leaked resources: active=%d claims=%d", daemon.ActiveCount(), len(governor.Active()))
	}
}

func TestDaemonExecutionClaimsEnforceProjectHeavyCapacityThroughWorkerLifetime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tasks := map[string]domain.Task{}
	workspaces := map[string]domain.Workspace{}
	for _, tc := range []struct{ id, project string }{{"a1", "project-a"}, {"a2", "project-a"}, {"b1", "project-b"}} {
		task, ws := daemonTaskFixture(tc.id, tc.project)
		tasks[tc.id], workspaces[tc.id] = task, ws
	}
	store := &fakeDaemonStore{tasks: tasks, workspace: workspaces, attempts: map[string]domain.ExecutionAttempt{}}
	sensor := &mutableDaemonSensor{snapshot: healthyDaemonSnapshot()}
	governor := daemonGovernor(t, sensor, 3, 1)
	runner := newTrackingReadyRunner()
	daemon, err := NewDaemon(store, &fakeDaemonService{store: store}, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, governor, DaemonConfig{MaxConcurrentWorkers: 3, ExecutionRAMReservation: 1, ExecutionDiskReservation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.launchReady(ctx); err != nil {
		t.Fatal(err)
	}
	waitTrackingCount(t, runner, 2, 0)
	claims := governor.Active()
	projects := map[string]int{}
	for _, claim := range claims {
		projects[claim.ProjectID]++
	}
	if daemon.ActiveCount() != 2 || len(claims) != 2 || projects["project-a"] != 1 || projects["project-b"] != 1 {
		t.Fatalf("per-project heavy capacity was not enforced: active=%d claims=%+v projects=%v", daemon.ActiveCount(), claims, projects)
	}
	drainTrackingDaemon(t, daemon, cancel)
}

func TestAcceptanceT6ThreeProjectsRespectInteractiveResponsivenessCap(t *testing.T) {
	host := healthyDaemonSnapshot()
	host.UserInteractive = true
	sensor := &mutableDaemonSensor{snapshot: host}
	governor, err := resourcegov.New(sensor, resourcegov.Config{MaxCPUPercent: 100, MaxMemoryLoadPercent: 100, MaxIOPressurePercent: 100, MinFreeRAMBytes: 1, MinFreeDiskBytes: 1, MaxMARDiskBytes: 1 << 40, MaxHeavyJobs: 3, MaxHeavyJobsPerProject: 3, MaxHeavyJobsInteractive: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	tasks := map[string]domain.Task{}
	workspaces := map[string]domain.Workspace{}
	for _, project := range []string{"t6-a", "t6-b", "t6-c"} {
		task, ws := daemonTaskFixture("task-"+project, project)
		tasks[task.ID], workspaces[task.ID] = task, ws
	}
	store := &fakeDaemonStore{tasks: tasks, workspace: workspaces, attempts: map[string]domain.ExecutionAttempt{}}
	runner := newTrackingReadyRunner()
	daemon, err := NewDaemon(store, &fakeDaemonService{store: store}, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, governor, DaemonConfig{MaxConcurrentWorkers: 3, ExecutionRAMReservation: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.launchReady(ctx); err != nil {
		t.Fatal(err)
	}
	waitTrackingCount(t, runner, 1, 0)
	time.Sleep(50 * time.Millisecond)
	if started, _ := runner.counts(); started != 1 || daemon.ActiveCount() != 1 {
		t.Fatalf("interactive host admitted too many heavy workers: started=%d active=%d", started, daemon.ActiveCount())
	}
	claims := governor.Active()
	if len(claims) != 1 || claims[0].RAMBytes != 64<<20 {
		t.Fatalf("T6 RAM reservation/accounting mismatch: %+v", claims)
	}
	drainTrackingDaemon(t, daemon, cancel)
}

func TestDaemonMemoryPressureBlocksActualWorkerLaunch(t *testing.T) {
	host := healthyDaemonSnapshot()
	host.MemoryLoadPercent = 95
	sensor := &mutableDaemonSensor{snapshot: host}
	governor, err := resourcegov.New(sensor, resourcegov.Config{MaxCPUPercent: 100, MaxMemoryLoadPercent: 80, MaxIOPressurePercent: 100, MinFreeRAMBytes: 1, MinFreeDiskBytes: 1, MaxMARDiskBytes: 1 << 40, MaxHeavyJobs: 1, MaxHeavyJobsPerProject: 1, MaxHeavyJobsInteractive: 1})
	if err != nil {
		t.Fatal(err)
	}
	task, ws := daemonTaskFixture("memory-task", "project-memory")
	store := &fakeDaemonStore{tasks: map[string]domain.Task{task.ID: task}, workspace: map[string]domain.Workspace{task.ID: ws}, attempts: map[string]domain.ExecutionAttempt{}}
	runner := newTrackingReadyRunner()
	daemon, err := NewDaemon(store, &fakeDaemonService{store: store}, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, governor, DaemonConfig{MaxConcurrentWorkers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.launchReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if started, _ := runner.counts(); started != 0 || daemon.ActiveCount() != 0 || len(governor.Active()) != 0 {
		t.Fatalf("memory pressure reached actual worker launch: started=%d active=%d claims=%+v", started, daemon.ActiveCount(), governor.Active())
	}
}

func TestDaemonDiskPressureCancelsActiveExecutionAndReleasesLease(t *testing.T) {
	ctx := context.Background()
	sensor := &mutableDaemonSensor{snapshot: healthyDaemonSnapshot()}
	governor, err := resourcegov.New(sensor, resourcegov.Config{MaxCPUPercent: 100, MaxMemoryLoadPercent: 100, MaxIOPressurePercent: 100, MinFreeRAMBytes: 1, MinFreeDiskBytes: 100, MaxMARDiskBytes: 1 << 40, MaxHeavyJobs: 1, MaxHeavyJobsPerProject: 1, MaxHeavyJobsInteractive: 1})
	if err != nil {
		t.Fatal(err)
	}
	task, ws := daemonTaskFixture("disk-task", "project-disk")
	store := &fakeDaemonStore{tasks: map[string]domain.Task{task.ID: task}, workspace: map[string]domain.Workspace{task.ID: ws}, attempts: map[string]domain.ExecutionAttempt{}}
	runner := newTrackingReadyRunner()
	daemon, err := NewDaemon(store, &fakeDaemonService{store: store}, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, governor, DaemonConfig{MaxConcurrentWorkers: 1, ExecutionDiskReservation: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.launchReady(ctx); err != nil {
		t.Fatal(err)
	}
	waitTrackingCount(t, runner, 1, 0)
	host := healthyDaemonSnapshot()
	host.FreeDiskBytes = 50
	sensor.set(host)
	if err := daemon.enforceResourcePressure(ctx); err == nil {
		t.Fatal("disk reserve threat must stop active growth")
	}
	waitTrackingCount(t, runner, 1, 1)
	done := make(chan struct{})
	go func() { daemon.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("disk-pressure cancellation did not drain worker")
	}
	if daemon.ActiveCount() != 0 || len(governor.Active()) != 0 {
		t.Fatalf("disk-pressure stop leaked mutation-capable execution: active=%d claims=%+v", daemon.ActiveCount(), governor.Active())
	}
}
