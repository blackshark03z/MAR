package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mar/internal/domain"
	"mar/internal/processctl"
	"mar/internal/store"
)

type Manager struct {
	store    *store.SQLite
	dataRoot string
	gitPath  string
	now      func() time.Time

	locksMu sync.Mutex
	locks   map[string]*sync.Mutex
}

func NewManager(s *store.SQLite, dataRoot string) (*Manager, error) {
	if s == nil {
		return nil, errors.New("workspace store is required")
	}
	if strings.TrimSpace(dataRoot) == "" {
		return nil, errors.New("workspace data root is required")
	}
	abs, err := filepath.Abs(dataRoot)
	if err != nil {
		return nil, err
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git executable is required: %w", err)
	}
	return &Manager{store: s, dataRoot: filepath.Clean(abs), gitPath: gitPath, now: time.Now, locks: make(map[string]*sync.Mutex)}, nil
}

func (m *Manager) EnsureMutable(ctx context.Context, taskID string) (domain.Workspace, error) {
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return domain.Workspace{}, err
	}
	project, err := m.store.GetProject(ctx, task.Contract.ProjectID)
	if err != nil {
		return domain.Workspace{}, err
	}
	lock := m.projectLock(project.ID)
	lock.Lock()
	defer lock.Unlock()

	repoRoot, err := m.gitTopLevel(ctx, task.ID, project.Root)
	if err != nil {
		return domain.Workspace{}, err
	}
	if !samePathFold(repoRoot, project.Root) {
		return domain.Workspace{}, fmt.Errorf("registered project root %q is not Git toplevel %q", project.Root, repoRoot)
	}
	resolvedBase, err := m.resolveCommit(ctx, task.ID, repoRoot, task.Contract.BaseRevision)
	if err != nil {
		return domain.Workspace{}, err
	}

	workspace := domain.Workspace{
		ID:           deterministicID("workspace", task.ID),
		TaskID:       task.ID,
		ProjectID:    project.ID,
		Path:         m.workspacePath(task.ID),
		BaseRevision: resolvedBase,
		State:        domain.WorkspacePreparing,
		CreatedAt:    m.now().UTC(),
		UpdatedAt:    m.now().UTC(),
	}
	if err := m.ensureManagedPath(workspace.Path); err != nil {
		return domain.Workspace{}, err
	}

	existing, created, err := m.store.BeginWorkspace(ctx, workspace)
	if err != nil {
		if errors.Is(err, store.ErrStateConflict) {
			if existingReady, getErr := m.store.GetWorkspaceByTask(ctx, task.ID); getErr == nil && existingReady.State == domain.WorkspaceReady {
				return existingReady, nil
			}
		}
		return domain.Workspace{}, err
	}
	workspace = existing
	if workspace.State == domain.WorkspaceReady {
		return workspace, nil
	}
	if workspace.State != domain.WorkspacePreparing {
		return domain.Workspace{}, fmt.Errorf("workspace %s is not creatable from state %s", workspace.ID, workspace.State)
	}

	if !created {
		if ok, head, inspectErr := m.registeredWorktree(ctx, task.ID, repoRoot, workspace.Path); inspectErr != nil {
			return domain.Workspace{}, inspectErr
		} else if ok {
			if head != workspace.BaseRevision {
				return domain.Workspace{}, fmt.Errorf("preparing worktree head %s differs from expected base %s", head, workspace.BaseRevision)
			}
			if err := m.requireCleanBaseline(ctx, task.ID, workspace); err != nil {
				_ = m.store.MarkWorkspaceFailed(ctx, workspace.ID, task.ID, err.Error(), m.now().UTC())
				return domain.Workspace{}, err
			}
			if err := m.store.MarkWorkspaceReady(ctx, workspace.ID, task.ID, head, m.now().UTC()); err != nil {
				return domain.Workspace{}, err
			}
			return m.store.GetWorkspaceByTask(ctx, task.ID)
		}
	}

	if err := m.createWorktree(ctx, task.ID, repoRoot, workspace.Path, workspace.BaseRevision); err != nil {
		// A command can report failure after Git has already made the side effect.
		// Reconcile observable Git truth before classifying creation as failed.
		if ok, head, inspectErr := m.registeredWorktree(ctx, task.ID, repoRoot, workspace.Path); inspectErr == nil && ok && head == workspace.BaseRevision {
			if cleanErr := m.requireCleanBaseline(ctx, task.ID, workspace); cleanErr == nil {
				if markErr := m.store.MarkWorkspaceReady(ctx, workspace.ID, task.ID, head, m.now().UTC()); markErr != nil {
					return domain.Workspace{}, markErr
				}
				return m.store.GetWorkspaceByTask(ctx, task.ID)
			} else {
				_ = m.store.MarkWorkspaceFailed(ctx, workspace.ID, task.ID, cleanErr.Error(), m.now().UTC())
				return domain.Workspace{}, cleanErr
			}
		}
		_ = m.store.MarkWorkspaceFailed(ctx, workspace.ID, task.ID, err.Error(), m.now().UTC())
		return domain.Workspace{}, err
	}

	ok, head, err := m.registeredWorktree(ctx, task.ID, repoRoot, workspace.Path)
	if err != nil {
		return domain.Workspace{}, err
	}
	if !ok || head != workspace.BaseRevision {
		failure := fmt.Sprintf("created worktree failed verification: registered=%v head=%s expected=%s", ok, head, workspace.BaseRevision)
		_ = m.store.MarkWorkspaceFailed(ctx, workspace.ID, task.ID, failure, m.now().UTC())
		return domain.Workspace{}, errors.New(failure)
	}
	if err := m.requireCleanBaseline(ctx, task.ID, workspace); err != nil {
		_ = m.store.MarkWorkspaceFailed(ctx, workspace.ID, task.ID, err.Error(), m.now().UTC())
		return domain.Workspace{}, err
	}
	if err := m.store.MarkWorkspaceReady(ctx, workspace.ID, task.ID, head, m.now().UTC()); err != nil {
		return domain.Workspace{}, err
	}
	return m.store.GetWorkspaceByTask(ctx, task.ID)
}

func (m *Manager) RemoveTerminal(ctx context.Context, taskID string) error {
	workspace, err := m.store.BeginWorkspaceRemoval(ctx, taskID, m.now().UTC())
	if err != nil {
		return err
	}
	if workspace.State == domain.WorkspaceRemoved {
		return nil
	}
	if err := m.ensureManagedPath(workspace.Path); err != nil {
		return err
	}
	project, err := m.store.GetProject(ctx, workspace.ProjectID)
	if err != nil {
		return err
	}
	lock := m.projectLock(project.ID)
	lock.Lock()
	defer lock.Unlock()

	repoRoot, err := m.gitTopLevel(ctx, taskID, project.Root)
	if err != nil {
		return err
	}
	output, removeErr := m.git(ctx, taskID, repoRoot, "worktree", "remove", "--force", workspace.Path)
	registered, _, inspectErr := m.registeredWorktree(ctx, taskID, repoRoot, workspace.Path)
	if inspectErr != nil {
		return inspectErr
	}
	if registered {
		if removeErr != nil {
			return fmt.Errorf("git worktree remove: %w: %s", removeErr, strings.TrimSpace(output))
		}
		return errors.New("worktree remains registered after removal")
	}
	if _, statErr := os.Stat(workspace.Path); statErr == nil {
		if err := os.RemoveAll(workspace.Path); err != nil {
			return fmt.Errorf("remove residual managed workspace: %w", err)
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	_, _ = m.git(ctx, taskID, repoRoot, "worktree", "prune")
	return m.store.FinishWorkspaceRemoval(ctx, workspace.ID, m.now().UTC())
}

func (m *Manager) createWorktree(ctx context.Context, taskID, repoRoot, path, base string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("workspace path already exists but is not a reconciled worktree: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	output, err := m.git(ctx, taskID, repoRoot, "worktree", "add", "--detach", path, base)
	if err != nil {
		return fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

func (m *Manager) gitTopLevel(ctx context.Context, taskID, root string) (string, error) {
	output, err := m.git(ctx, taskID, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("project is not a Git repository: %w: %s", err, strings.TrimSpace(output))
	}
	return filepath.Clean(strings.TrimSpace(output)), nil
}

func (m *Manager) resolveCommit(ctx context.Context, taskID, repoRoot, revision string) (string, error) {
	output, err := m.git(ctx, taskID, repoRoot, "rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve base revision %q: %w: %s", revision, err, strings.TrimSpace(output))
	}
	return strings.TrimSpace(output), nil
}

func (m *Manager) registeredWorktree(ctx context.Context, taskID, repoRoot, wantPath string) (bool, string, error) {
	output, err := m.git(ctx, taskID, repoRoot, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, "", fmt.Errorf("list worktrees: %w", err)
	}
	var currentPath, currentHead string
	for _, token := range strings.Split(output, "\x00") {
		switch {
		case strings.HasPrefix(token, "worktree "):
			if currentPath != "" && samePathFold(currentPath, wantPath) {
				return true, currentHead, nil
			}
			currentPath = strings.TrimPrefix(token, "worktree ")
			currentHead = ""
		case strings.HasPrefix(token, "HEAD "):
			currentHead = strings.TrimPrefix(token, "HEAD ")
		case token == "":
			if currentPath != "" && samePathFold(currentPath, wantPath) {
				return true, currentHead, nil
			}
			currentPath, currentHead = "", ""
		}
	}
	if currentPath != "" && samePathFold(currentPath, wantPath) {
		return true, currentHead, nil
	}
	return false, "", nil
}

func (m *Manager) git(ctx context.Context, taskID, repoRoot string, args ...string) (string, error) {
	// Managed workspaces must not inherit host/user line-ending policy. Pin a
	// deterministic LF default while still allowing repository .gitattributes
	// to override individual paths (for example eol=crlf).
	cmdArgs := []string{"-c", "core.autocrlf=false", "-c", "core.eol=lf", "-C", repoRoot}
	cmdArgs = append(cmdArgs, args...)
	operation := "workspace-git"
	if len(args) > 0 {
		operation += ":" + args[0]
	}
	return processctl.RunContainedCommand(ctx, processctl.CommandSpec{
		TaskID:      taskID,
		OperationID: operation,
		Path:        m.gitPath,
		Args:        cmdArgs,
		Dir:         repoRoot,
	})
}

func (m *Manager) requireCleanBaseline(ctx context.Context, taskID string, workspace domain.Workspace) error {
	output, err := m.git(ctx, taskID, workspace.Path,
		"status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=all",
	)
	if err != nil {
		return fmt.Errorf("verify managed workspace baseline: %w", err)
	}
	if strings.TrimSpace(output) != "" {
		return fmt.Errorf("managed workspace baseline is not clean at %s: %s", workspace.BaseRevision, strings.TrimSpace(output))
	}
	return nil
}

func (m *Manager) workspacePath(taskID string) string {
	return filepath.Join(m.dataRoot, "w", workspacePathKey(taskID))
}

func workspacePathKey(taskID string) string {
	sum := sha256.Sum256([]byte(taskID))
	return hex.EncodeToString(sum[:16])
}

func (m *Manager) ensureManagedPath(path string) error {
	rel, err := filepath.Rel(m.dataRoot, path)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("workspace path escapes managed data root: %s", path)
	}
	return nil
}

func (m *Manager) projectLock(projectID string) *sync.Mutex {
	m.locksMu.Lock()
	defer m.locksMu.Unlock()
	lock := m.locks[projectID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.locks[projectID] = lock
	}
	return lock
}

func deterministicID(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + "-" + hex.EncodeToString(sum[:12])
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func samePathFold(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
