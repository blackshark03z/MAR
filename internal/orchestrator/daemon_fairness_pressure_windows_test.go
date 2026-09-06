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
)

type blockingSchedulerDriver struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingSchedulerDriver) Step(ctx context.Context) (scheduler.StepResult, error) {
	s.once.Do(func() { close(s.entered) })
	select {
	case <-ctx.Done():
		return scheduler.StepResult{}, ctx.Err()
	case <-s.release:
		return scheduler.StepResult{Action: scheduler.ActionIdle}, nil
	}
}

func TestExecutionAdmissionBalancesProjectsBeforeFillingBacklog(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tasks := map[string]domain.Task{}
	workspaces := map[string]domain.Workspace{}
	for _, tc := range []struct{ id, project string }{{"a1", "project-a"}, {"a2", "project-a"}, {"b1", "project-b"}} {
		task, ws := daemonTaskFixture(tc.id, tc.project)
		tasks[task.ID], workspaces[task.ID] = task, ws
	}
	store := &fakeDaemonStore{tasks: tasks, workspace: workspaces, attempts: map[string]domain.ExecutionAttempt{}}
	governor := daemonGovernor(t, &mutableDaemonSensor{snapshot: healthyDaemonSnapshot()}, 2, 2)
	runner := newTrackingReadyRunner()
	daemon, err := NewDaemon(store, &fakeDaemonService{store: store}, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, governor, DaemonConfig{MaxConcurrentWorkers: 2, ExecutionRAMReservation: 1, ExecutionDiskReservation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.launchReady(ctx); err != nil {
		t.Fatal(err)
	}
	waitTrackingCount(t, runner, 2, 0)
	projects := map[string]int{}
	for _, claim := range governor.Active() {
		projects[claim.ProjectID]++
	}
	if projects["project-a"] != 1 || projects["project-b"] != 1 {
		t.Fatalf("execution admission let one backlog monopolize capacity: %v", projects)
	}
	drainTrackingDaemon(t, daemon, cancel)
}

func TestResourcePressureMonitorRunsWhileSchedulerStepIsBlocked(t *testing.T) {
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sensor := &mutableDaemonSensor{snapshot: healthyDaemonSnapshot()}
	governor := daemonGovernor(t, sensor, 1, 1)
	task, ws := daemonTaskFixture("pressure-independent", "project-pressure")
	store := &fakeDaemonStore{tasks: map[string]domain.Task{task.ID: task}, workspace: map[string]domain.Workspace{task.ID: ws}, attempts: map[string]domain.ExecutionAttempt{}}
	runner := newTrackingReadyRunner()
	schedulerDriver := &blockingSchedulerDriver{entered: make(chan struct{}), release: make(chan struct{})}
	daemon, err := NewDaemon(store, &fakeDaemonService{store: store}, fakePreflightDriver{}, schedulerDriver, runner, &fakeIntegrationRecoverer{}, governor, DaemonConfig{PollInterval: time.Second, ResourcePollInterval: 10 * time.Millisecond, MaxConcurrentWorkers: 1, ExecutionDiskReservation: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.launchReady(runCtx); err != nil {
		t.Fatal(err)
	}
	waitTrackingCount(t, runner, 1, 0)

	host := healthyDaemonSnapshot()
	host.FreeDiskBytes = 15 // above the 1-byte reserve, but below reserve + active 20-byte claim.
	sensor.set(host)
	done := make(chan error, 1)
	go func() { done <- daemon.Run(runCtx) }()
	select {
	case <-schedulerDriver.entered:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not enter blocking step")
	}
	waitTrackingCount(t, runner, 1, 1)
	deadline := time.Now().Add(time.Second)
	for len(governor.Active()) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(governor.Active()) != 0 {
		t.Fatalf("pressure monitor did not release active execution claim: %+v", governor.Active())
	}
	close(schedulerDriver.release)
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("daemon shutdown mismatch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not stop after blocked scheduler was released")
	}
}
