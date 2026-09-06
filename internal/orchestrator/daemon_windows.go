//go:build windows

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"mar/internal/domain"
	"mar/internal/resourcegov"
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
	RecoverForReplacement(context.Context, string) error
	ExhaustRetryBudget(context.Context, string) error
}

type DaemonConfig struct {
	PollInterval             time.Duration
	ControlPollInterval      time.Duration
	RetryDelay               time.Duration
	MaxAttempts              int64
	MaxConcurrentWorkers     int
	MaxPreflightPerTick      int
	ResourcePollInterval     time.Duration
	ExecutionRAMReservation  uint64
	ExecutionDiskReservation uint64
	ErrorSink                func(error)
}

func (c DaemonConfig) withDefaults() DaemonConfig {
	if c.PollInterval <= 0 {
		c.PollInterval = 250 * time.Millisecond
	}
	if c.ControlPollInterval <= 0 {
		c.ControlPollInterval = 200 * time.Millisecond
	}
	if c.RetryDelay <= 0 {
		c.RetryDelay = time.Second
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.MaxConcurrentWorkers <= 0 {
		c.MaxConcurrentWorkers = 2
	}
	if c.MaxPreflightPerTick <= 0 {
		c.MaxPreflightPerTick = 8
	}
	if c.ResourcePollInterval <= 0 {
		c.ResourcePollInterval = time.Second
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
	governor    *resourcegov.Governor
	cfg         DaemonConfig

	mu     sync.Mutex
	active map[string]context.CancelFunc
	wg     sync.WaitGroup
}

func NewDaemon(store taskStateStore, taskService daemonTaskService, preflight preflightDriver, scheduler schedulerDriver, runner readyTaskRunner, integration integrationRecoverer, governor *resourcegov.Governor, cfg DaemonConfig) (*Daemon, error) {
	if store == nil || taskService == nil || preflight == nil || scheduler == nil || runner == nil || integration == nil || governor == nil {
		return nil, errors.New("daemon requires store, task service, preflight, scheduler, task runner, integration recovery and resource governor")
	}
	cfg = cfg.withDefaults()
	return &Daemon{
		store:       store,
		service:     taskService,
		preflight:   preflight,
		scheduler:   scheduler,
		runner:      runner,
		integration: integration,
		governor:    governor,
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
	pressureCtx, stopPressure := context.WithCancel(ctx)
	pressureDone := make(chan struct{})
	go func() {
		defer close(pressureDone)
		d.runResourcePressureLoop(pressureCtx)
	}()
	defer func() {
		stopPressure()
		<-pressureDone
	}()

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

func (d *Daemon) runResourcePressureLoop(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.ResourcePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.enforceResourcePressure(ctx); err != nil {
				d.report(err)
			}
		}
	}
}

func (d *Daemon) step(ctx context.Context) {
	if err := d.drivePreflight(ctx); err != nil {
		d.report(err)
	}
	if err := d.driveBlockedChoices(ctx); err != nil {
		d.report(err)
	}
	if err := d.driveRetries(ctx); err != nil {
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

func (d *Daemon) driveBlockedChoices(ctx context.Context) error {
	tasks, err := d.store.ListTasksByState(ctx, domain.TaskBlocked)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		snapshot, err := d.service.StatusSnapshot(ctx, task.ID)
		if err != nil {
			return err
		}
		control := snapshot.LatestControl
		if control == nil || control.Kind != domain.ControlSteer || !control.CreatedAt.After(task.UpdatedAt) {
			continue
		}
		var payload domain.SteerPayload
		if err := json.Unmarshal(control.Payload, &payload); err != nil {
			return err
		}
		if err := payload.Validate(); err != nil {
			return err
		}
		if payload.Kind != domain.SteerBlockedChoice {
			continue
		}
		attempt, ok, err := d.store.CurrentAttemptByTask(ctx, task.ID)
		if err != nil {
			return err
		}
		if ok && attempt.AuthorityState != domain.AttemptPhysicallyTerminated {
			continue
		}
		// Recovering to WORKSPACE_READY updates task.UpdatedAt after this control,
		// so the same blocked-choice command cannot trigger an unbounded replay if
		// the replacement later blocks again.
		if err := d.service.RecoverForReplacement(ctx, task.ID); err != nil {
			return err
		}
	}
	return nil
}

func (d *Daemon) driveRetries(ctx context.Context) error {
	tasks, err := d.store.ListTasksByState(ctx, domain.TaskRetryWait)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, task := range tasks {
		attempt, ok, err := d.store.CurrentAttemptByTask(ctx, task.ID)
		if err != nil {
			return err
		}
		if ok && attempt.AuthorityState != domain.AttemptPhysicallyTerminated {
			// Never turn retry policy into a second-writer admission path. Normal
			// TaskRunner finalization or restart reconciliation must prove physical
			// termination first.
			continue
		}
		if task.RunEpoch >= d.cfg.MaxAttempts {
			if err := d.service.ExhaustRetryBudget(ctx, task.ID); err != nil {
				return err
			}
			continue
		}
		if !task.UpdatedAt.IsZero() && now.Sub(task.UpdatedAt) < d.cfg.RetryDelay {
			continue
		}
		if err := d.service.RecoverForReplacement(ctx, task.ID); err != nil {
			return err
		}
	}
	return nil
}

func (d *Daemon) launchReady(ctx context.Context) error {
	tasks, err := d.store.ListTasksByState(ctx, domain.TaskWorkspaceReady)
	if err != nil {
		return err
	}
	pending := make([]domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if !d.isActive(task.ID) {
			pending = append(pending, task)
		}
	}
	for d.activeCount() < d.cfg.MaxConcurrentWorkers && len(pending) > 0 {
		activeByProject := make(map[string]int)
		for _, claim := range d.governor.Active() {
			if claim.Heavy {
				activeByProject[claim.ProjectID]++
			}
		}
		sort.SliceStable(pending, func(i, j int) bool {
			a, b := pending[i], pending[j]
			if activeByProject[a.Contract.ProjectID] != activeByProject[b.Contract.ProjectID] {
				return activeByProject[a.Contract.ProjectID] < activeByProject[b.Contract.ProjectID]
			}
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				return a.UpdatedAt.Before(b.UpdatedAt)
			}
			if a.Contract.ProjectID != b.Contract.ProjectID {
				return a.Contract.ProjectID < b.Contract.ProjectID
			}
			return a.ID < b.ID
		})

		launched := false
		for i, task := range pending {
			lease, decision, acquireErr := d.governor.TryAcquire(ctx, resourcegov.Claim{
				ID:        "execution:" + task.ID,
				ProjectID: task.Contract.ProjectID,
				Class:     resourcegov.WorkloadBuild,
				RAMBytes:  d.cfg.ExecutionRAMReservation,
				DiskBytes: d.cfg.ExecutionDiskReservation,
				Heavy:     true,
			})
			if acquireErr != nil {
				d.report(acquireErr)
				continue
			}
			if !decision.Allowed {
				continue
			}
			workspace, workspaceErr := d.store.GetWorkspaceByTask(ctx, task.ID)
			if workspaceErr != nil {
				lease.Release()
				d.report(workspaceErr)
				pending = append(pending[:i], pending[i+1:]...)
				launched = true
				break
			}
			d.launch(ctx, task.ID, workspace, lease)
			pending = append(pending[:i], pending[i+1:]...)
			launched = true
			break
		}
		if !launched {
			break
		}
	}
	return nil
}
func (d *Daemon) launch(parent context.Context, taskID string, workspace domain.Workspace, lease *resourcegov.Lease) {
	taskCtx, cancel := context.WithCancel(parent)
	d.mu.Lock()
	if _, exists := d.active[taskID]; exists || len(d.active) >= d.cfg.MaxConcurrentWorkers {
		d.mu.Unlock()
		cancel()
		lease.Release()
		return
	}
	d.active[taskID] = cancel
	d.mu.Unlock()

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer lease.Release()
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

func (d *Daemon) enforceResourcePressure(ctx context.Context) error {
	decision, err := d.governor.Pressure(ctx)
	if err != nil {
		return err
	}
	if !hasPressureReason(decision.Reasons, resourcegov.DenyHostDiskReserve) && !hasPressureReason(decision.Reasons, resourcegov.DenyMARDiskBudget) {
		return nil
	}
	if d.activeCount() == 0 {
		return nil
	}
	d.cancelAll()
	return fmt.Errorf("active work stopped because disk pressure threatened the configured reserve: %v", decision.Reasons)
}

func hasPressureReason(reasons []resourcegov.DenialReason, want resourcegov.DenialReason) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
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
