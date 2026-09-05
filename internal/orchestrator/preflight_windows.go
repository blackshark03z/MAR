//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"mar/internal/domain"
	"mar/internal/processctl"
	"mar/internal/service"
	"mar/internal/store"
	"mar/internal/verification"
)

type preflightGit interface {
	Run(context.Context, string, string, ...string) (string, error)
}

type containedPreflightGit struct{ path string }

func (g containedPreflightGit) Run(ctx context.Context, taskID, root string, args ...string) (string, error) {
	op := "preflight-git"
	if len(args) > 0 {
		op += ":" + strings.TrimSpace(args[0])
	}
	return processctl.RunContainedCommand(ctx, processctl.CommandSpec{
		TaskID:         taskID,
		OperationID:    op,
		Path:           g.path,
		Args:           append([]string{"-C", root}, args...),
		Dir:            root,
		MaxOutputBytes: 64 << 10,
	})
}

type Preflight struct {
	store    *store.SQLite
	service  *service.TaskService
	profiles *verification.Registry
	git      preflightGit
	now      func() time.Time
}

func NewPreflight(s *store.SQLite, taskService *service.TaskService, profiles *verification.Registry) (*Preflight, error) {
	if s == nil || taskService == nil || profiles == nil {
		return nil, errors.New("preflight requires store, task service and verification profiles")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("Git executable is required for preflight: %w", err)
	}
	return newPreflightWithGit(s, taskService, profiles, containedPreflightGit{path: gitPath})
}

func newPreflightWithGit(s *store.SQLite, taskService *service.TaskService, profiles *verification.Registry, git preflightGit) (*Preflight, error) {
	if s == nil || taskService == nil || profiles == nil || git == nil {
		return nil, errors.New("preflight requires store, task service, verification profiles and Git runner")
	}
	return &Preflight{store: s, service: taskService, profiles: profiles, git: git, now: time.Now}, nil
}

// Drive advances one submitted task through PREFLIGHT only after immutable
// contract/profile/Git base identity has been checked. Failure is durable and
// fail-closed: the task becomes BLOCKED rather than entering resource queues.
func (p *Preflight) Drive(ctx context.Context, taskID string) error {
	task, err := p.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task.State == domain.TaskSubmitted {
		if err := p.service.AdvancePreExecution(ctx, taskID, domain.TaskPreflight); err != nil {
			return err
		}
		task, err = p.store.GetTask(ctx, taskID)
		if err != nil {
			return err
		}
	}
	if task.State != domain.TaskPreflight {
		return store.ErrStateConflict
	}
	if err := p.validate(ctx, task); err != nil {
		blockErr := p.store.OrchestratorTransition(ctx, task.ID, domain.TaskPreflight, domain.TaskBlocked, p.now().UTC())
		return errors.Join(fmt.Errorf("preflight failed: %w", err), blockErr)
	}
	return p.service.AdvancePreExecution(ctx, task.ID, domain.TaskWaitingResource)
}

func (p *Preflight) validate(ctx context.Context, task domain.Task) error {
	goalHash, err := task.Contract.Hash()
	if err != nil {
		return err
	}
	if goalHash != task.ContractHash {
		return errors.New("durable Goal Contract hash mismatch")
	}
	if _, ok := p.profiles.Get(task.Contract.VerificationProfile); !ok {
		return fmt.Errorf("verification profile %q is not registered", task.Contract.VerificationProfile)
	}
	project, err := p.store.GetProject(ctx, task.Contract.ProjectID)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(project.Root)
	if err != nil {
		return err
	}
	root = filepath.Clean(root)
	observedTop, err := p.git.Run(ctx, task.ID, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("registered project is not a readable Git repository: %w", err)
	}
	if !strings.EqualFold(filepath.Clean(strings.TrimSpace(observedTop)), root) {
		return fmt.Errorf("registered project root %q is not Git toplevel %q", root, strings.TrimSpace(observedTop))
	}
	resolved, err := p.git.Run(ctx, task.ID, root, "rev-parse", "--verify", "--end-of-options", task.Contract.BaseRevision+"^{commit}")
	if err != nil {
		return fmt.Errorf("base revision %q is not resolvable: %w", task.Contract.BaseRevision, err)
	}
	if strings.TrimSpace(resolved) == "" {
		return errors.New("base revision resolved to empty Git identity")
	}
	return nil
}
