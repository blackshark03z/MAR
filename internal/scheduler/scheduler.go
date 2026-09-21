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

type terminalWorkspaceReclaimer interface {
	ReclaimTerminal(context.Context, int) (int, error)
}

type diskPressureReclaimer interface {
	ReclaimDiskPressure(context.Context, int, bool) (int, int64, error)
}

type Config struct {
	AgingInterval            time.Duration
	WorkspaceRAMReservation  uint64
	WorkspaceDiskReservation uint64
	PressureReclaimLimit     int
}

func (c Config) withDefaults() Config {
	if c.PressureReclaimLimit <= 0 {
		c.PressureReclaimLimit = 8
	}
	return c
}

func (c Config) validate() error {
	if c.AgingInterval <= 0 {
		return errors.New("scheduler aging interval must be positive")
	}
	if c.PressureReclaimLimit <= 0 {
		return errors.New("scheduler pressure reclaim limit must be positive")
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
	Action                      StepAction
	TaskID                      string
	ProjectID                   string
	DenialReasons               []resourcegov.DenialReason
	Workspace                   *domain.Workspace
	ReclaimedTerminalWorkspaces int
	ReclaimedCacheBytes         int64
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
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Scheduler{store: s, governor: governor, workspace: workspace, cfg: cfg, now: time.Now}, nil
}

func (s *Scheduler) ReclaimTerminal(ctx context.Context, limit int) (int, error) {
	reclaimer, ok := s.workspace.(terminalWorkspaceReclaimer)
	if !ok {
		return 0, nil
	}
	return reclaimer.ReclaimTerminal(ctx, limit)
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
	reclaimedWorkspaces := 0
	var reclaimedCacheBytes int64
	var reclaimErr error
	if !decision.Allowed && hasDiskPressure(decision.Reasons) {
		if reclaimer, ok := s.workspace.(diskPressureReclaimer); ok {
			// Shared rebuildable caches are pruned only while MAR has no active
			// resource claims. Terminal workspace reclamation remains safe and
			// bounded regardless of other active work.
			allowCachePrune := len(s.governor.Active()) == 0
			reclaimedWorkspaces, reclaimedCacheBytes, reclaimErr = reclaimer.ReclaimDiskPressure(ctx, s.cfg.PressureReclaimLimit, allowCachePrune)
			if reclaimedWorkspaces > 0 || reclaimedCacheBytes > 0 {
				s.governor.InvalidateMARDiskUsageCache()
				lease, decision, err = s.governor.TryAcquire(ctx, claim)
				if err != nil {
					return StepResult{}, err
				}
			}
		}
	}
	if !decision.Allowed {
		result := StepResult{
			Action:                      ActionWaitingResource,
			TaskID:                      task.ID,
			ProjectID:                   task.Contract.ProjectID,
			DenialReasons:               append([]resourcegov.DenialReason(nil), decision.Reasons...),
			ReclaimedTerminalWorkspaces: reclaimedWorkspaces,
			ReclaimedCacheBytes:         reclaimedCacheBytes,
		}
		if reclaimErr != nil {
			return result, fmt.Errorf("disk-pressure reclaim: %w", reclaimErr)
		}
		return result, nil
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
		Action:                      ActionWorkspaceReady,
		TaskID:                      task.ID,
		ProjectID:                   task.Contract.ProjectID,
		Workspace:                   &workspace,
		ReclaimedTerminalWorkspaces: reclaimedWorkspaces,
		ReclaimedCacheBytes:         reclaimedCacheBytes,
	}, nil
}

func hasDiskPressure(reasons []resourcegov.DenialReason) bool {
	for _, reason := range reasons {
		if reason == resourcegov.DenyHostDiskReserve || reason == resourcegov.DenyMARDiskBudget {
			return true
		}
	}
	return false
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
			(rank == candidate.effective && task.UpdatedAt.Equal(candidate.task.UpdatedAt) && task.CreatedAt.Before(candidate.task.CreatedAt)) {
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
