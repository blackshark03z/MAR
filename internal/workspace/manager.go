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
	"mar/internal/retention"
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
	if workspace.State == domain.WorkspaceCheckpointed {
		workspace, _, err = m.store.BeginWorkspaceRehydrate(ctx, task.ID, m.now().UTC())
		if err != nil {
			return domain.Workspace{}, err
		}
	}
	if workspace.State != domain.WorkspacePreparing {
		return domain.Workspace{}, fmt.Errorf("workspace %s is not creatable from state %s", workspace.ID, workspace.State)
	}
	workspaceRevision := workspace.BaseRevision
	if strings.TrimSpace(workspace.HeadRevision) != "" {
		workspaceRevision = workspace.HeadRevision
	}

	if !created {
		if ok, head, inspectErr := m.registeredWorktree(ctx, task.ID, repoRoot, workspace.Path); inspectErr != nil {
			return domain.Workspace{}, inspectErr
		} else if ok {
			if head != workspaceRevision {
				return domain.Workspace{}, fmt.Errorf("preparing worktree head %s differs from expected revision %s", head, workspaceRevision)
			}
			if err := m.requireCleanBaseline(ctx, task.ID, workspace); err != nil {
				_ = m.store.MarkWorkspaceFailed(ctx, workspace.ID, task.ID, err.Error(), m.now().UTC())
				return domain.Workspace{}, err
			}
			if err := m.markWorkspaceReady(ctx, workspace, head); err != nil {
				return domain.Workspace{}, err
			}
			return m.store.GetWorkspaceByTask(ctx, task.ID)
		}
	}

	if err := m.createWorktree(ctx, task.ID, repoRoot, workspace.Path, workspaceRevision); err != nil {
		// A command can report failure after Git has already made the side effect.
		// Reconcile observable Git truth before classifying creation as failed.
		if ok, head, inspectErr := m.registeredWorktree(ctx, task.ID, repoRoot, workspace.Path); inspectErr == nil && ok && head == workspaceRevision {
			if cleanErr := m.requireCleanBaseline(ctx, task.ID, workspace); cleanErr == nil {
				if markErr := m.markWorkspaceReady(ctx, workspace, head); markErr != nil {
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
	if !ok || head != workspaceRevision {
		failure := fmt.Sprintf("created worktree failed verification: registered=%v head=%s expected=%s", ok, head, workspaceRevision)
		_ = m.store.MarkWorkspaceFailed(ctx, workspace.ID, task.ID, failure, m.now().UTC())
		return domain.Workspace{}, errors.New(failure)
	}
	if err := m.requireCleanBaseline(ctx, task.ID, workspace); err != nil {
		_ = m.store.MarkWorkspaceFailed(ctx, workspace.ID, task.ID, err.Error(), m.now().UTC())
		return domain.Workspace{}, err
	}
	if err := m.markWorkspaceReady(ctx, workspace, head); err != nil {
		return domain.Workspace{}, err
	}
	return m.store.GetWorkspaceByTask(ctx, task.ID)
}

func (m *Manager) markWorkspaceReady(ctx context.Context, workspace domain.Workspace, head string) error {
	checkpoint, ok, err := m.store.LatestWorkspaceCheckpoint(ctx, workspace.TaskID)
	if err != nil {
		return err
	}
	if ok && checkpoint.State == domain.WorkspaceCheckpointRehydrating && checkpoint.SnapshotRevision == head {
		return m.store.FinishWorkspaceRehydrate(ctx, workspace.TaskID, checkpoint.ID, head, m.now().UTC())
	}
	return m.store.MarkWorkspaceReady(ctx, workspace.ID, workspace.TaskID, head, m.now().UTC())
}

func (m *Manager) ReconcileCheckpointTransactions(ctx context.Context, limit int) (int, error) {
	taskIDs, err := m.store.ListWorkspaceCheckpointRecoveryCandidates(ctx, limit)
	if err != nil {
		return 0, err
	}
	reconciled := 0
	var firstErr error
	for _, taskID := range taskIDs {
		if err := m.reconcileCheckpointTransaction(ctx, taskID); err != nil {
			// A task-local checkpoint invariant must remain fail-closed without
			// turning one damaged/stale workspace into global scheduler starvation.
			if errors.Is(err, ErrCheckpointUnsafe) || errors.Is(err, store.ErrStateConflict) || errors.Is(err, store.ErrPhysicalFenceRequired) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("reconcile workspace checkpoint for task %s: %w", taskID, err)
			}
			continue
		}
		reconciled++
	}
	return reconciled, firstErr
}

func (m *Manager) reconcileCheckpointTransaction(ctx context.Context, taskID string) error {
	workspace, err := m.store.GetWorkspaceByTask(ctx, taskID)
	if err != nil {
		return err
	}
	if workspace.State != domain.WorkspaceCheckpointing {
		return nil
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
	checkpoint, hasCheckpoint, err := m.store.LatestWorkspaceCheckpoint(ctx, taskID)
	if err != nil {
		return err
	}
	registered, actualHead, err := m.registeredWorktree(ctx, taskID, repoRoot, workspace.Path)
	if err != nil {
		return err
	}

	// Crash before a durable snapshot was recorded leaves no replacement
	// authority behind. If the managed worktree still exists, restore READY
	// even when its HEAD drifted: no source is discarded, and the normal
	// checkpoint admission guard will continue to reject that drift fail-closed.
	if !hasCheckpoint {
		if !registered {
			return ErrCheckpointUnsafe
		}
		return m.store.AbortWorkspaceCheckpoint(ctx, taskID, m.now().UTC())
	}
	if checkpoint.State != domain.WorkspaceCheckpointCaptured ||
		checkpoint.OriginalHead != workspace.HeadRevision ||
		checkpoint.WorkspaceID != workspace.ID {
		return ErrCheckpointUnsafe
	}
	refRevision, err := m.git(ctx, taskID, repoRoot, "rev-parse", "--verify", checkpoint.RefName+"^{commit}")
	if err != nil || strings.TrimSpace(refRevision) != checkpoint.SnapshotRevision {
		return ErrCheckpointUnsafe
	}

	// Crash after durable snapshot capture: finish the original compaction
	// transaction. The private ref makes this safe even if the worktree was
	// already removed before the process died.
	if registered {
		if actualHead != checkpoint.OriginalHead {
			return ErrCheckpointUnsafe
		}
		output, removeErr := m.git(ctx, taskID, repoRoot, "worktree", "remove", "--force", workspace.Path)
		stillRegistered, _, inspectErr := m.registeredWorktree(ctx, taskID, repoRoot, workspace.Path)
		if inspectErr != nil {
			return inspectErr
		}
		if stillRegistered {
			if removeErr != nil {
				return fmt.Errorf("checkpoint recovery worktree remove: %w: %s", removeErr, strings.TrimSpace(output))
			}
			return errors.New("checkpoint recovery worktree remains registered after removal")
		}
	}
	if _, statErr := os.Stat(workspace.Path); statErr == nil {
		if err := os.RemoveAll(workspace.Path); err != nil {
			return err
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	_, _ = m.git(ctx, taskID, repoRoot, "worktree", "prune")
	return m.store.FinishWorkspaceCheckpoint(ctx, taskID, checkpoint.ID, m.now().UTC())
}

func (m *Manager) CheckpointBlockedWorkspaces(ctx context.Context, limit int) (int, int64, error) {
	if limit <= 0 {
		return 0, 0, nil
	}
	candidateLimit := limit * 8
	taskIDs, err := m.store.ListBlockedWorkspaceCheckpointCandidates(ctx, candidateLimit)
	if err != nil {
		return 0, 0, err
	}
	var count int
	var bytes int64
	var firstErr error
	for _, taskID := range taskIDs {
		if count >= limit {
			break
		}
		_, freed, checkpointErr := m.CheckpointBlocked(ctx, taskID)
		if checkpointErr != nil {
			if errors.Is(checkpointErr, ErrCheckpointUnsafe) || errors.Is(checkpointErr, store.ErrStateConflict) || errors.Is(checkpointErr, store.ErrWorkspaceRemovalUnsafe) || errors.Is(checkpointErr, store.ErrPhysicalFenceRequired) {
				continue
			}
			if firstErr == nil {
				firstErr = checkpointErr
			}
			continue
		}
		count++
		bytes += freed
	}
	return count, bytes, firstErr
}

var ErrCheckpointUnsafe = errors.New("workspace checkpoint is unsafe for current physical Git truth")

func hasIrreproducibleIgnoredMaterial(raw string) bool {
	for _, token := range strings.Split(raw, "\x00") {
		rel := strings.TrimSpace(token)
		if rel == "" {
			continue
		}
		if isRebuildableTaskLocalIgnoredPath(rel) {
			continue
		}
		return true
	}
	return false
}

func isRebuildableTaskLocalIgnoredPath(rel string) bool {
	normalized := filepath.ToSlash(filepath.Clean(strings.TrimSpace(rel)))
	normalized = strings.TrimPrefix(normalized, "./")
	roots := []string{
		".mar/go/build",
		".mar/go/mod",
		".mar/go/tmp",
		".mar/runtime/profile",
		".mar/runtime/tmp",
		".mar/runtime/python-cache",
	}
	for _, root := range roots {
		if normalized == root || strings.HasPrefix(normalized, root+"/") {
			return true
		}
	}
	return false
}

func (m *Manager) CheckpointBlocked(ctx context.Context, taskID string) (domain.WorkspaceCheckpoint, int64, error) {
	workspace, err := m.store.BeginWorkspaceCheckpoint(ctx, taskID, m.now().UTC())
	if err != nil {
		return domain.WorkspaceCheckpoint{}, 0, err
	}
	recorded := false
	removed := false
	defer func() {
		if !recorded && !removed {
			_ = m.store.AbortWorkspaceCheckpoint(context.Background(), taskID, m.now().UTC())
		}
	}()

	project, err := m.store.GetProject(ctx, workspace.ProjectID)
	if err != nil {
		return domain.WorkspaceCheckpoint{}, 0, err
	}
	lock := m.projectLock(project.ID)
	lock.Lock()
	defer lock.Unlock()

	repoRoot, err := m.gitTopLevel(ctx, taskID, project.Root)
	if err != nil {
		return domain.WorkspaceCheckpoint{}, 0, err
	}
	registered, actualHead, err := m.registeredWorktree(ctx, taskID, repoRoot, workspace.Path)
	if err != nil {
		return domain.WorkspaceCheckpoint{}, 0, err
	}
	if !registered || actualHead != workspace.HeadRevision {
		return domain.WorkspaceCheckpoint{}, 0, ErrCheckpointUnsafe
	}
	ignored, err := m.git(ctx, taskID, workspace.Path, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return domain.WorkspaceCheckpoint{}, 0, err
	}
	if hasIrreproducibleIgnoredMaterial(ignored) {
		return domain.WorkspaceCheckpoint{}, 0, ErrCheckpointUnsafe
	}
	status, err := m.git(ctx, taskID, workspace.Path, "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=all")
	if err != nil {
		return domain.WorkspaceCheckpoint{}, 0, err
	}
	statusSum := sha256.Sum256([]byte(status))
	dirty := strings.TrimSpace(status) != ""
	snapshot := actualHead
	if dirty {
		tmpRoot := filepath.Join(m.dataRoot, "checkpoints", "tmp")
		if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
			return domain.WorkspaceCheckpoint{}, 0, err
		}
		tmp, err := os.CreateTemp(tmpRoot, "index-*")
		if err != nil {
			return domain.WorkspaceCheckpoint{}, 0, err
		}
		indexPath := tmp.Name()
		if err := tmp.Close(); err != nil {
			return domain.WorkspaceCheckpoint{}, 0, err
		}
		_ = os.Remove(indexPath)
		defer os.Remove(indexPath)
		env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
		if _, err := m.gitEnv(ctx, taskID, workspace.Path, env, "read-tree", actualHead); err != nil {
			return domain.WorkspaceCheckpoint{}, 0, err
		}
		if _, err := m.gitEnv(ctx, taskID, workspace.Path, env, "add", "-A", "--", "."); err != nil {
			return domain.WorkspaceCheckpoint{}, 0, err
		}
		tree, err := m.gitEnv(ctx, taskID, workspace.Path, env, "write-tree")
		if err != nil {
			return domain.WorkspaceCheckpoint{}, 0, err
		}
		snapshot, err = m.gitEnv(ctx, taskID, workspace.Path, env,
			"-c", "user.name=MAR Checkpoint",
			"-c", "user.email=mar-checkpoint@local.invalid",
			"commit-tree", strings.TrimSpace(tree), "-p", actualHead, "-m", "MAR workspace checkpoint "+taskID)
		if err != nil {
			return domain.WorkspaceCheckpoint{}, 0, err
		}
		snapshot = strings.TrimSpace(snapshot)
	}
	now := m.now().UTC()
	checkpointID := deterministicID("workspace-checkpoint", taskID+"|"+snapshot+"|"+now.Format(time.RFC3339Nano))
	refName := "refs/mar/checkpoints/" + shortHash(taskID) + "/" + shortHash(checkpointID)
	if _, err := m.git(ctx, taskID, repoRoot, "update-ref", refName, snapshot); err != nil {
		return domain.WorkspaceCheckpoint{}, 0, err
	}
	checkpoint := domain.WorkspaceCheckpoint{
		ID: checkpointID, TaskID: taskID, WorkspaceID: workspace.ID, ProjectID: workspace.ProjectID,
		OriginalHead: actualHead, SnapshotRevision: snapshot, RefName: refName,
		StatusHash: hex.EncodeToString(statusSum[:]), Dirty: dirty, CreatedAt: now,
	}
	checkpoint, err = m.store.RecordWorkspaceCheckpoint(ctx, checkpoint)
	if err != nil {
		_, _ = m.git(context.Background(), taskID, repoRoot, "update-ref", "-d", refName)
		return domain.WorkspaceCheckpoint{}, 0, err
	}
	recorded = true

	before := directoryBytes(workspace.Path)
	output, removeErr := m.git(ctx, taskID, repoRoot, "worktree", "remove", "--force", workspace.Path)
	registered, _, inspectErr := m.registeredWorktree(ctx, taskID, repoRoot, workspace.Path)
	if inspectErr != nil {
		return domain.WorkspaceCheckpoint{}, 0, inspectErr
	}
	if registered {
		if removeErr != nil {
			return domain.WorkspaceCheckpoint{}, 0, fmt.Errorf("checkpoint worktree remove: %w: %s", removeErr, strings.TrimSpace(output))
		}
		return domain.WorkspaceCheckpoint{}, 0, errors.New("checkpoint worktree remains registered after removal")
	}
	if _, statErr := os.Stat(workspace.Path); statErr == nil {
		if err := os.RemoveAll(workspace.Path); err != nil {
			return domain.WorkspaceCheckpoint{}, 0, err
		}
	} else if !os.IsNotExist(statErr) {
		return domain.WorkspaceCheckpoint{}, 0, statErr
	}
	removed = true
	_, _ = m.git(ctx, taskID, repoRoot, "worktree", "prune")
	if err := m.store.FinishWorkspaceCheckpoint(ctx, taskID, checkpoint.ID, m.now().UTC()); err != nil {
		return domain.WorkspaceCheckpoint{}, 0, err
	}
	checkpoint.State = domain.WorkspaceCheckpointCompacted
	return checkpoint, before, nil
}

func directoryBytes(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.Type().IsRegular() {
			if info, infoErr := entry.Info(); infoErr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
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
	return m.store.FinishWorkspaceRemoval(ctx, workspace.ID, deterministicID("result-workspace-removed", workspace.TaskID), m.now().UTC())
}

func (m *Manager) ReclaimTerminal(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	candidateLimit := limit * 4
	if candidateLimit < limit {
		candidateLimit = limit
	}
	taskIDs, err := m.store.ListTerminalWorkspaceRemovalCandidates(ctx, candidateLimit)
	if err != nil {
		return 0, err
	}
	reclaimed := 0
	var firstErr error
	for _, taskID := range taskIDs {
		if reclaimed >= limit {
			break
		}
		if err := m.RemoveTerminal(ctx, taskID); err != nil {
			if errors.Is(err, store.ErrWorkspaceRemovalUnsafe) || errors.Is(err, store.ErrPhysicalFenceRequired) || errors.Is(err, store.ErrStateConflict) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("reclaim terminal workspace for task %s: %w", taskID, err)
			}
			continue
		}
		reclaimed++
	}
	return reclaimed, firstErr
}

// PruneRebuildableCaches clears only Phase-1 positive-allowlist cache
// contents. The scheduler must call this through the ResourceGovernor's
// idle-exclusive admission gate; Manager deliberately does not infer idleness.
func (m *Manager) PruneRebuildableCaches(context.Context) (int64, error) {
	result, err := retention.PrunePressureCaches(m.dataRoot)
	return result.FreedBytes, err
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
	return m.gitEnv(ctx, taskID, repoRoot, nil, args...)
}

func (m *Manager) gitEnv(ctx context.Context, taskID, repoRoot string, env []string, args ...string) (string, error) {
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
		Env:         env,
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
