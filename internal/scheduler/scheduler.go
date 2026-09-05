package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"mar/internal/domain"
	"mar/internal/resourcegov"
	"mar/internal/store"
)

type WorkspaceProvisioner interface {
	EnsureMutable(context.Context, string) (domain.Workspace, error)
}

type Config struct {
	AgingInterval            time.Duration
	WorkspaceRAMReservation  uint64
	WorkspaceDiskReservation uint64
}

func (c Config) validate() error {
	if c.AgingInterval <= 0 {
		return errors.New("scheduler aging interval must be positive")
	}
	return nil
}

type StepAction string

const (
	ActionIdle            StepAction = "IDLE"
	ActionWaitingResource StepAction = "WAITING_RESOURCE"
	ActionWorkspaceReady  StepAction = "WORKSPACE_READY"
	ActionBlocked         StepAction = "BLOCKED"
)

type StepResult struct {
	Action        StepAction
	TaskID        string
	ProjectID     string
	DenialReasons []resourcegov.DenialReason
	Workspace     *domain.Workspace
}

type Scheduler struct {
	store     *store.SQLite
	governor  *resourcegov.Governor
	workspace WorkspaceProvisioner
	cfg       Config
	now       func() time.Time
	mu        sync.Mutex
}

func New(s *store.SQLite, governor *resourcegov.Governor, workspace WorkspaceProvisioner, cfg Config) (*Scheduler, error) {
	if s == nil || governor == nil || workspace == nil {
		return nil, errors.New("store, resource governor, and workspace provisioner are required")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Scheduler{store: s, governor: governor, workspace: workspace, cfg: cfg, now: time.Now}, nil
}

// Step performs one authoritative scheduling decision. It is intentionally
// serialized: MAR has one coordination writer, while workers themselves run in
// parallel after admission.
func (s *Scheduler) Step(ctx context.Context) (StepResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	waiting, err := s.store.ListWaitingTasks(ctx)
	if err != nil {
		return StepResult{}, err
	}
	if len(waiting) == 0 {
		return StepResult{Action: ActionIdle}, nil
	}
	states, err := s.store.ListProjectScheduleStates(ctx)
	if err != nil {
		return StepResult{}, err
	}
	now := s.now().UTC()
	task := selectTask(waiting, states, now, s.cfg.AgingInterval)
	claim := resourcegov.Claim{
		ID:        "workspace:" + task.ID,
		ProjectID: task.Contract.ProjectID,
		Class:     resourcegov.WorkloadSearch,
		RAMBytes:  s.cfg.WorkspaceRAMReservation,
		DiskBytes: s.cfg.WorkspaceDiskReservation,
		Heavy:     false,
		Priority:  priorityRank(task.Contract.Priority),
	}
	lease, decision, err := s.governor.TryAcquire(ctx, claim)
	if err != nil {
		return StepResult{}, err
	}
	if !decision.Allowed {
		return StepResult{
			Action:        ActionWaitingResource,
			TaskID:        task.ID,
			ProjectID:     task.Contract.ProjectID,
			DenialReasons: append([]resourcegov.DenialReason(nil), decision.Reasons...),
		}, nil
	}
	defer lease.Release()

	workspace, err := s.workspace.EnsureMutable(ctx, task.ID)
	if err != nil {
		current, statusErr := s.store.GetTask(ctx, task.ID)
		if statusErr == nil && current.State == domain.TaskWaitingResource {
			_ = s.store.OrchestratorTransition(ctx, task.ID, domain.TaskWaitingResource, domain.TaskBlocked, now)
		}
		return StepResult{Action: ActionBlocked, TaskID: task.ID, ProjectID: task.Contract.ProjectID}, fmt.Errorf("prepare task workspace: %w", err)
	}
	if err := s.store.RecordProjectDispatch(ctx, task.Contract.ProjectID, now); err != nil {
		return StepResult{}, err
	}
	return StepResult{
		Action:    ActionWorkspaceReady,
		TaskID:    task.ID,
		ProjectID: task.Contract.ProjectID,
		Workspace: &workspace,
	}, nil
}

type projectCandidate struct {
	task         domain.Task
	effective    int
	lastDispatch *time.Time
}

func selectTask(tasks []domain.Task, states map[string]store.ProjectScheduleState, now time.Time, aging time.Duration) domain.Task {
	byProject := make(map[string]projectCandidate)
	for _, task := range tasks {
		rank := effectivePriority(task.Contract.Priority, task.UpdatedAt, now, aging)
		candidate, ok := byProject[task.Contract.ProjectID]
		if !ok || rank < candidate.effective || (rank == candidate.effective && task.UpdatedAt.Before(candidate.task.UpdatedAt)) ||
			(rank == candidate.effective && task.UpdatedAt.Equal(candidate.task.UpdatedAt) && task.ID < candidate.task.ID) {
			state := states[task.Contract.ProjectID]
			byProject[task.Contract.ProjectID] = projectCandidate{task: task, effective: rank, lastDispatch: state.LastDispatchedAt}
		}
	}
	candidates := make([]projectCandidate, 0, len(byProject))
	for _, candidate := range byProject {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.effective != b.effective {
			return a.effective < b.effective
		}
		if (a.lastDispatch == nil) != (b.lastDispatch == nil) {
			return a.lastDispatch == nil
		}
		if a.lastDispatch != nil && !a.lastDispatch.Equal(*b.lastDispatch) {
			return a.lastDispatch.Before(*b.lastDispatch)
		}
		if !a.task.UpdatedAt.Equal(b.task.UpdatedAt) {
			return a.task.UpdatedAt.Before(b.task.UpdatedAt)
		}
		if a.task.Contract.ProjectID != b.task.Contract.ProjectID {
			return a.task.Contract.ProjectID < b.task.Contract.ProjectID
		}
		return a.task.ID < b.task.ID
	})
	return candidates[0].task
}

func effectivePriority(priority string, waitingSince, now time.Time, aging time.Duration) int {
	base := priorityRank(priority)
	if now.Before(waitingSince) || aging <= 0 {
		return base
	}
	steps := int(now.Sub(waitingSince) / aging)
	if steps >= base {
		return 0
	}
	return base - steps
}

func priorityRank(priority string) int {
	switch strings.ToUpper(strings.TrimSpace(priority)) {
	case "P0", "CRITICAL":
		return 0
	case "P1", "HIGH":
		return 1
	case "P2", "NORMAL", "MEDIUM":
		return 2
	case "P3", "LOW":
		return 3
	default:
		return 3
	}
}
