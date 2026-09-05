package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
