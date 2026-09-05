//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"sync"
	"time"

	"mar/internal/domain"
	"mar/internal/scheduler"
	"mar/internal/service"
)

type taskStateStore interface {
	ListTasksByState(context.Context, domain.TaskState) ([]domain.Task, error)
	GetWorkspaceByTask(context.Context, string) (domain.Workspace, error)
	CurrentAttemptByTask(context.Context, string) (domain.ExecutionAttempt, bool, error)
}

type preflightDriver interface {
	Drive(context.Context, string) error
}

type schedulerDriver interface {
	Step(context.Context) (scheduler.StepResult, error)
}

type readyTaskRunner interface {
	RunWorkspaceReady(context.Context, string, domain.Workspace) (RunOutcome, error)
}

type integrationRecoverer interface {
	RecoverPending(context.Context) error
}

type daemonTaskService interface {
	StatusSnapshot(context.Context, string) (service.TaskStatusSnapshot, error)
	RequirePhysicalRecovery(context.Context, string, string, int64) error
}

type DaemonConfig struct {
	PollInterval         time.Duration
	ControlPollInterval  time.Duration
	MaxConcurrentWorkers int
	MaxPreflightPerTick  int
	ErrorSink            func(error)
}

func (c DaemonConfig) withDefaults() DaemonConfig {
	if c.PollInterval <= 0 {
		c.PollInterval = 250 * time.Millisecond
	}
	if c.ControlPollInterval <= 0 {
		c.ControlPollInterval = 200 * time.Millisecond
	}
	if c.MaxConcurrentWorkers <= 0 {
		c.MaxConcurrentWorkers = 2
	}
	if c.MaxPreflightPerTick <= 0 {
		c.MaxPreflightPerTick = 8
	}
	if c.ErrorSink == nil {
		c.ErrorSink = func(error) {}
	}
	return c
}

type Daemon struct {
	store       taskStateStore
	service     daemonTaskService
	preflight   preflightDriver
	scheduler   schedulerDriver
	runner      readyTaskRunner
	integration integrationRecoverer
	cfg         DaemonConfig

	mu     sync.Mutex
	active map[string]context.CancelFunc
	wg     sync.WaitGroup
}

func NewDaemon(store taskStateStore, taskService daemonTaskService, preflight preflightDriver, scheduler schedulerDriver, runner readyTaskRunner, integration integrationRecoverer, cfg DaemonConfig) (*Daemon, error) {
	if store == nil || taskService == nil || preflight == nil || scheduler == nil || runner == nil || integration == nil {
		return nil, errors.New("daemon requires store, task service, preflight, scheduler, task runner and integration recovery")
	}
	cfg = cfg.withDefaults()
	return &Daemon{
		store:       store,
		service:     taskService,
		preflight:   preflight,
		scheduler:   scheduler,
		runner:      runner,
		integration: integration,
		cfg:         cfg,
		active:      make(map[string]context.CancelFunc),
	}, nil
}

// Run owns reconstructable orchestration only. Durable truth remains in SQLite;
// active contexts merely accelerate cancellation and are safe to lose on a
// daemon crash because startup reconciliation never assumes physical death.
func (d *Daemon) Run(ctx context.Context) error {
	if err := d.integration.RecoverPending(ctx); err != nil {
		return err
	}
	if err := d.reconcileUnprovenAttempts(ctx); err != nil {
		return err
	}
	d.step(ctx)
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			d.cancelAll()
			d.wg.Wait()
			return ctx.Err()
		case <-ticker.C:
			d.step(ctx)
		}
	}
}

func (d *Daemon) step(ctx context.Context) {
	if err := d.drivePreflight(ctx); err != nil {
		d.report(err)
	}
	if _, err := d.scheduler.Step(ctx); err != nil {
		d.report(err)
	}
	if err := d.launchReady(ctx); err != nil {
		d.report(err)
	}
}

func (d *Daemon) drivePreflight(ctx context.Context) error {
	remaining := d.cfg.MaxPreflightPerTick
	for _, state := range []domain.TaskState{domain.TaskSubmitted, domain.TaskPreflight} {
		tasks, err := d.store.ListTasksByState(ctx, state)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if remaining == 0 {
				return nil
			}
			remaining--
			if err := d.preflight.Drive(ctx, task.ID); err != nil {
				// Preflight itself persists a task-local BLOCKED outcome on validation
				// failure. Report it, but never let one bad Goal kill the daemon.
				d.report(err)
			}
		}
	}
	return nil
}

func (d *Daemon) launchReady(ctx context.Context) error {
	tasks, err := d.store.ListTasksByState(ctx, domain.TaskWorkspaceReady)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if d.activeCount() >= d.cfg.MaxConcurrentWorkers {
			return nil
		}
		if d.isActive(task.ID) {
			continue
		}
		workspace, err := d.store.GetWorkspaceByTask(ctx, task.ID)
		if err != nil {
			d.report(err)
			continue
		}
		d.launch(ctx, task.ID, workspace)
	}
	return nil
}

func (d *Daemon) launch(parent context.Context, taskID string, workspace domain.Workspace) {
	taskCtx, cancel := context.WithCancel(parent)
	d.mu.Lock()
	if _, exists := d.active[taskID]; exists || len(d.active) >= d.cfg.MaxConcurrentWorkers {
		d.mu.Unlock()
		cancel()
		return
	}
	d.active[taskID] = cancel
	d.mu.Unlock()

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer d.removeActive(taskID)
		monitorDone := make(chan struct{})
		go func() {
			defer close(monitorDone)
			d.monitorCancellation(taskCtx, taskID, cancel)
		}()
		_, err := d.runner.RunWorkspaceReady(taskCtx, taskID, workspace)
		cancel()
		<-monitorDone
		if err != nil && !errors.Is(err, context.Canceled) {
			d.report(err)
		}
	}()
}

func (d *Daemon) monitorCancellation(ctx context.Context, taskID string, cancel context.CancelFunc) {
	ticker := time.NewTicker(d.cfg.ControlPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snapshot, err := d.service.StatusSnapshot(ctx, taskID)
			if err != nil {
				d.report(err)
				cancel()
				return
			}
			if snapshot.CancelRequested {
				cancel()
				return
			}
		}
	}
}

func (d *Daemon) reconcileUnprovenAttempts(ctx context.Context) error {
	seen := make(map[string]struct{})
	states := []domain.TaskState{
		domain.TaskRunning,
		domain.TaskVerifying,
		domain.TaskReviewing,
		domain.TaskReadyToIntegrate,
		domain.TaskIntegrating,
		domain.TaskVerified,
		domain.TaskInputRequired,
		domain.TaskBlocked,
		domain.TaskRetryWait,
	}
	for _, state := range states {
		tasks, err := d.store.ListTasksByState(ctx, state)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if _, duplicate := seen[task.ID]; duplicate {
				continue
			}
			seen[task.ID] = struct{}{}
			attempt, ok, err := d.store.CurrentAttemptByTask(ctx, task.ID)
			if err != nil {
				return err
			}
			if !ok || attempt.AuthorityState == domain.AttemptPhysicallyTerminated {
				continue
			}
			if err := d.service.RequirePhysicalRecovery(ctx, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *Daemon) ActiveCount() int { return d.activeCount() }

func (d *Daemon) activeCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.active)
}

func (d *Daemon) isActive(taskID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.active[taskID]
	return ok
}

func (d *Daemon) removeActive(taskID string) {
	d.mu.Lock()
	cancel := d.active[taskID]
	delete(d.active, taskID)
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (d *Daemon) cancelAll() {
	d.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(d.active))
	for _, cancel := range d.active {
		cancels = append(cancels, cancel)
	}
	d.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (d *Daemon) report(err error) {
	if err != nil {
		d.cfg.ErrorSink(err)
	}
}
