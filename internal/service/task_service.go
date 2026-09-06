package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mar/internal/domain"
	"mar/internal/store"
)

type TaskService struct {
	store *store.SQLite
	now   func() time.Time
}

func NewTaskService(s *store.SQLite) *TaskService {
	return &TaskService{store: s, now: time.Now}
}

func (s *TaskService) RegisterProject(ctx context.Context, id, root string) (domain.Project, bool, error) {
	id = strings.TrimSpace(id)
	root = strings.TrimSpace(root)
	if id == "" {
		return domain.Project{}, false, errors.New("project id is required")
	}
	if root == "" {
		return domain.Project{}, false, errors.New("project root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return domain.Project{}, false, fmt.Errorf("resolve project root: %w", err)
	}
	p := domain.Project{ID: id, Root: filepath.Clean(abs), CreatedAt: s.now().UTC()}
	return s.store.RegisterProject(ctx, p)
}

func (s *TaskService) Submit(ctx context.Context, idempotencyKey string, contract domain.GoalContract) (domain.Task, bool, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return domain.Task{}, false, errors.New("idempotency key is required")
	}
	if err := contract.Validate(); err != nil {
		return domain.Task{}, false, fmt.Errorf("invalid goal contract: %w", err)
	}
	if _, err := s.store.GetProject(ctx, contract.ProjectID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return domain.Task{}, false, fmt.Errorf("unknown project %q: %w", contract.ProjectID, err)
		}
		return domain.Task{}, false, err
	}
	hash, err := contract.Hash()
	if err != nil {
		return domain.Task{}, false, fmt.Errorf("hash goal contract: %w", err)
	}
	now := s.now().UTC()
	task := domain.Task{
		ID:             newID("task"),
		IdempotencyKey: idempotencyKey,
		Contract:       contract,
		ContractHash:   hash,
		State:          domain.TaskSubmitted,
		RunEpoch:       0,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return s.store.SubmitTask(ctx, task)
}

func (s *TaskService) Status(ctx context.Context, taskID string) (domain.Task, error) {
	if strings.TrimSpace(taskID) == "" {
		return domain.Task{}, errors.New("task id is required")
	}
	return s.store.GetTask(ctx, taskID)
}

func (s *TaskService) CancelBeforeAttempt(ctx context.Context, taskID string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	switch task.State {
	case domain.TaskSubmitted, domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady:
		return s.store.OrchestratorTransition(ctx, taskID, task.State, domain.TaskCancelled, s.now().UTC())
	default:
		return store.ErrStateConflict
	}
}

func (s *TaskService) AdvancePreExecution(ctx context.Context, taskID string, to domain.TaskState) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if !allowedPreExecutionTransition(task.State, to) {
		return store.ErrStateConflict
	}
	return s.store.OrchestratorTransition(ctx, taskID, task.State, to, s.now().UTC())
}

func (s *TaskService) BeginAttempt(ctx context.Context, taskID, workerID, supervisorID string, lease time.Duration) (domain.ExecutionAttempt, error) {
	if strings.TrimSpace(workerID) == "" || strings.TrimSpace(supervisorID) == "" {
		return domain.ExecutionAttempt{}, errors.New("worker_id and supervisor_id are required")
	}
	if lease <= 0 {
		return domain.ExecutionAttempt{}, errors.New("lease duration must be positive")
	}
	now := s.now().UTC()
	return s.store.BeginAttempt(ctx, taskID, newID("attempt"), workerID, supervisorID, now, now.Add(lease))
}

func (s *TaskService) ValidateAttemptAuthority(ctx context.Context, taskID, attemptID string, epoch int64) error {
	return s.store.ValidateAttemptAuthority(ctx, taskID, attemptID, epoch)
}

// AttemptAuthoritative exposes a provider-neutral read-only authority predicate
// for worker runtimes. A stale/fenced attempt is not an infrastructure error.
func (s *TaskService) AttemptAuthoritative(ctx context.Context, taskID, attemptID string, epoch int64) (bool, error) {
	err := s.store.ValidateAttemptAuthority(ctx, taskID, attemptID, epoch)
	if errors.Is(err, store.ErrStaleAttempt) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

const maxSemanticCheckpointBytes = 64 << 10

func (s *TaskService) PublishCheckpoint(ctx context.Context, taskID, attemptID string, epoch int64, currentRevision string, payload domain.SemanticCheckpointPayload) (domain.SemanticCheckpoint, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return domain.SemanticCheckpoint{}, fmt.Errorf("encode semantic checkpoint: %w", err)
	}
	if len(encoded) > maxSemanticCheckpointBytes {
		return domain.SemanticCheckpoint{}, fmt.Errorf("semantic checkpoint exceeds %d bytes", maxSemanticCheckpointBytes)
	}
	return s.store.PublishCheckpoint(ctx, newID("checkpoint"), taskID, attemptID, epoch, currentRevision, payload, s.now().UTC())
}

func (s *TaskService) LatestValidCheckpoint(ctx context.Context, taskID string) (domain.SemanticCheckpoint, bool, error) {
	return s.store.LatestValidCheckpoint(ctx, taskID)
}

func (s *TaskService) HeartbeatAttempt(ctx context.Context, taskID, attemptID string, epoch int64, lease time.Duration) error {
	if lease <= 0 {
		return errors.New("lease duration must be positive")
	}
	now := s.now().UTC()
	return s.store.HeartbeatAttempt(ctx, taskID, attemptID, epoch, now, now.Add(lease))
}

// LogicalFenceAttempt revokes MAR logical authority. It does not prove that the
// OS process tree has stopped mutating the workspace.
func (s *TaskService) LogicalFenceAttempt(ctx context.Context, taskID, attemptID string, epoch int64) error {
	return s.store.LogicalFenceAttempt(ctx, taskID, attemptID, epoch, s.now().UTC())
}

func (s *TaskService) RequirePhysicalRecovery(ctx context.Context, taskID, attemptID string, epoch int64) error {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(attemptID) == "" || epoch <= 0 {
		return errors.New("task id, attempt id and positive epoch are required")
	}
	return s.store.RequirePhysicalRecovery(ctx, taskID, attemptID, epoch, s.now().UTC())
}

func (s *TaskService) RecoverForReplacement(ctx context.Context, taskID string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	switch task.State {
	case domain.TaskRunning, domain.TaskBlocked, domain.TaskRetryWait:
		return s.store.RecoverTaskToWorkspaceReady(ctx, taskID, task.State, s.now().UTC())
	default:
		return store.ErrStateConflict
	}
}

// ExhaustRetryBudget converts a safely terminated RETRY_WAIT task into BLOCKED.
// It is deliberately an orchestrator transition, so a mutation-capable prior
// attempt prevents the transition rather than allowing retry policy to hide an
// unresolved physical-fencing problem.
func (s *TaskService) ExhaustRetryBudget(ctx context.Context, taskID string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task.State != domain.TaskRetryWait {
		return store.ErrStateConflict
	}
	return s.store.OrchestratorTransition(ctx, taskID, domain.TaskRetryWait, domain.TaskBlocked, s.now().UTC())
}

func (s *TaskService) TransitionForAttempt(ctx context.Context, taskID, attemptID string, epoch int64, to domain.TaskState) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if !allowedAttemptTransition(task.State, to) {
		return store.ErrStateConflict
	}
	return s.store.TransitionTaskForAttempt(ctx, taskID, attemptID, epoch, task.State, to, s.now().UTC())
}

// RequestInputForAttempt is the worker's narrow lifecycle capability for
// pausing the current ACTIVE attempt while preserving the immutable Goal.
func (s *TaskService) RequestInputForAttempt(ctx context.Context, taskID, attemptID string, epoch int64) error {
	return s.TransitionForAttempt(ctx, taskID, attemptID, epoch, domain.TaskInputRequired)
}

func allowedPreExecutionTransition(from, to domain.TaskState) bool {
	return (from == domain.TaskSubmitted && to == domain.TaskPreflight) ||
		(from == domain.TaskPreflight && to == domain.TaskWaitingResource) ||
		(from == domain.TaskWaitingResource && to == domain.TaskWorkspaceReady)
}

func allowedAttemptTransition(from, to domain.TaskState) bool {
	switch from {
	case domain.TaskRunning:
		return to == domain.TaskVerifying || to == domain.TaskInputRequired || to == domain.TaskBlocked || to == domain.TaskRetryWait || to == domain.TaskFailed || to == domain.TaskCancelled
	case domain.TaskVerifying:
		return to == domain.TaskReviewing || to == domain.TaskVerified || to == domain.TaskBlocked || to == domain.TaskFailed
	case domain.TaskReviewing:
		return to == domain.TaskReadyToIntegrate || to == domain.TaskBlocked || to == domain.TaskFailed
	default:
		return false
	}
}

func newID(prefix string) string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	// UUIDv4-compatible version/variant bits while keeping generation dependency-free.
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return prefix + "-" + hex.EncodeToString(buf)
}
