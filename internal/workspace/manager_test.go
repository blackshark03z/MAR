package workspace_test

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
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/service"
	"mar/internal/store"
	"mar/internal/workspace"
)

func TestEnsureMutableCreatesDetachedWorktreeAtExactBase(t *testing.T) {
	ctx := context.Background()
	repo, base := makeRepo(t)
	s, svc, manager := workspaceHarness(t, repo)
	defer s.Close()
	task := waitingTask(t, svc, "workspace-create", repo, base)

	ws, err := manager.EnsureMutable(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ws.State != domain.WorkspaceReady || ws.HeadRevision != base || ws.BaseRevision != base {
		t.Fatalf("unexpected workspace: %+v", ws)
	}
	if _, err := os.Stat(filepath.Join(ws.Path, "seed.txt")); err != nil {
		t.Fatalf("worktree content missing: %v", err)
	}
	gotTask, err := svc.Status(ctx, task.ID)
	if err != nil || gotTask.State != domain.TaskWorkspaceReady {
		t.Fatalf("task not advanced with workspace: task=%+v err=%v", gotTask, err)
	}
	gotHead := gitOut(t, ws.Path, "rev-parse", "HEAD")
	if gotHead != base {
		t.Fatalf("worktree head %s != base %s", gotHead, base)
	}
}

func TestEnsureMutableIsIdempotentAndConcurrent(t *testing.T) {
	ctx := context.Background()
	repo, base := makeRepo(t)
	s, svc, manager := workspaceHarness(t, repo)
	defer s.Close()
	task := waitingTask(t, svc, "workspace-concurrent", repo, base)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	paths := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ws, err := manager.EnsureMutable(ctx, task.ID)
			if err != nil {
				errs <- err
				return
			}
			paths <- ws.Path
		}()
	}
	wg.Wait()
	close(paths)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent ensure failed: %v", err)
	}
	var want string
	for path := range paths {
		if want == "" {
			want = path
		}
		if !strings.EqualFold(filepath.Clean(want), filepath.Clean(path)) {
			t.Fatalf("multiple workspaces returned: %q vs %q", want, path)
		}
	}
	if got := countWorktrees(t, repo); got != 2 { // main + one task worktree
		t.Fatalf("expected exactly two registered worktrees, got %d", got)
	}
}

func TestDifferentTasksReceiveDifferentWorktrees(t *testing.T) {
	ctx := context.Background()
	repo, base := makeRepo(t)
	s, svc, manager := workspaceHarness(t, repo)
	defer s.Close()
	a := waitingTaskForProject(t, svc, "workspace-a", "shared-project", repo, base)
	b := waitingTaskForProject(t, svc, "workspace-b", "shared-project", repo, base)
	wa, err := manager.EnsureMutable(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	wb, err := manager.EnsureMutable(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(filepath.Clean(wa.Path), filepath.Clean(wb.Path)) {
		t.Fatal("mutable tasks share a worktree")
	}
	if got := countWorktrees(t, repo); got != 3 {
		t.Fatalf("expected main + 2 task worktrees, got %d", got)
	}
}

func TestCancelledPreAttemptWorkspaceCanBeRemoved(t *testing.T) {
	ctx := context.Background()
	repo, base := makeRepo(t)
	s, svc, manager := workspaceHarness(t, repo)
	defer s.Close()
	task := waitingTask(t, svc, "workspace-cleanup", repo, base)
	ws, err := manager.EnsureMutable(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CancelBeforeAttempt(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.RemoveTerminal(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("workspace path still exists after cleanup: %v", err)
	}
	stored, err := s.GetWorkspaceByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != domain.WorkspaceRemoved || stored.RemovedAt == nil {
		t.Fatalf("workspace not durably removed: %+v", stored)
	}
	if got := countWorktrees(t, repo); got != 1 {
		t.Fatalf("expected only main worktree after cleanup, got %d", got)
	}
}

func TestCleanupRefusesMutationCapableAttempt(t *testing.T) {
	ctx := context.Background()
	repo, base := makeRepo(t)
	s, svc, manager := workspaceHarness(t, repo)
	defer s.Close()
	task := waitingTask(t, svc, "workspace-active-cleanup", repo, base)
	if _, err := manager.EnsureMutable(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-a", "supervisor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.TransitionForAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, domain.TaskCancelled); err != nil {
		t.Fatal(err)
	}
	if err := manager.RemoveTerminal(ctx, task.ID); !errors.Is(err, store.ErrPhysicalFenceRequired) {
		t.Fatalf("mutation-capable workspace cleanup must be blocked, got %v", err)
	}
}

func TestPreparingWorkspaceReconcilesObservableGitSideEffect(t *testing.T) {
	ctx := context.Background()
	repo, base := makeRepo(t)
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.NewTaskService(db)
	dataRoot := filepath.Join(t.TempDir(), "mar-data")
	manager, err := workspace.NewManager(db, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	task := waitingTask(t, svc, "workspace-reconcile", repo, base)
	projectHash := sha256.Sum256([]byte(task.Contract.ProjectID))
	path := filepath.Join(dataRoot, "workspaces", hex.EncodeToString(projectHash[:8]), task.ID)
	now := time.Now().UTC()
	intent := domain.Workspace{
		ID:           "workspace-crash-window",
		TaskID:       task.ID,
		ProjectID:    task.Contract.ProjectID,
		Path:         path,
		BaseRevision: base,
		State:        domain.WorkspacePreparing,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if _, created, err := db.BeginWorkspace(ctx, intent); err != nil || !created {
		t.Fatalf("persist preparing workspace: created=%v err=%v", created, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "worktree", "add", "--detach", path, base)

	recovered, err := manager.EnsureMutable(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != domain.WorkspaceReady || recovered.Path != path || recovered.HeadRevision != base {
		t.Fatalf("preparing workspace was not reconciled: %+v", recovered)
	}
	gotTask, err := svc.Status(ctx, task.ID)
	if err != nil || gotTask.State != domain.TaskWorkspaceReady {
		t.Fatalf("task not finalized with reconciled worktree: %+v err=%v", gotTask, err)
	}
}

func TestUnregisteredPreexistingWorkspacePathIsNotOverwritten(t *testing.T) {
	ctx := context.Background()
	repo, base := makeRepo(t)
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.NewTaskService(db)
	dataRoot := filepath.Join(t.TempDir(), "mar-data")
	manager, err := workspace.NewManager(db, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	task := waitingTask(t, svc, "workspace-preexisting", repo, base)
	projectHash := sha256.Sum256([]byte(task.Contract.ProjectID))
	path := filepath.Join(dataRoot, "workspaces", hex.EncodeToString(projectHash[:8]), task.ID)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "do-not-overwrite.txt")
	if err := os.WriteFile(marker, []byte("owner-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnsureMutable(ctx, task.ID); err == nil {
		t.Fatal("pre-existing unregistered path should block workspace creation")
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "owner-data" {
		t.Fatalf("pre-existing data was modified: %q err=%v", content, err)
	}
	stored, err := db.GetWorkspaceByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != domain.WorkspaceFailed {
		t.Fatalf("unsafe path should leave durable FAILED workspace, got %+v", stored)
	}
}

func TestInvalidBaseDoesNotCreateWorkspace(t *testing.T) {
	ctx := context.Background()
	repo, _ := makeRepo(t)
	s, svc, manager := workspaceHarness(t, repo)
	defer s.Close()
	task := waitingTask(t, svc, "workspace-invalid-base", repo, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if _, err := manager.EnsureMutable(ctx, task.ID); err == nil {
		t.Fatal("invalid base revision should fail")
	}
	if got := countWorktrees(t, repo); got != 1 {
		t.Fatalf("invalid base unexpectedly created a worktree, count=%d", got)
	}
}

func workspaceHarness(t *testing.T, repo string) (*store.SQLite, *service.TaskService, *workspace.Manager) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewTaskService(s)
	manager, err := workspace.NewManager(s, filepath.Join(t.TempDir(), "mar-data"))
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	return s, svc, manager
}

func waitingTask(t *testing.T, svc *service.TaskService, key, repo, base string) domain.Task {
	t.Helper()
	return waitingTaskForProject(t, svc, key, "project-"+key, repo, base)
}

func waitingTaskForProject(t *testing.T, svc *service.TaskService, key, projectID, repo, base string) domain.Task {
	t.Helper()
	ctx := context.Background()
	if _, _, err := svc.RegisterProject(ctx, projectID, repo); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "workspace test",
		Acceptance:          []string{"isolated"},
		ProjectID:           projectID,
		BaseRevision:        base,
		VerificationProfile: "test",
		Priority:            "P2",
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
	}
	task, _, err := svc.Submit(ctx, key, contract)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AdvancePreExecution(ctx, task.ID, domain.TaskPreflight); err != nil {
		t.Fatal(err)
	}
	if err := svc.AdvancePreExecution(ctx, task.ID, domain.TaskWaitingResource); err != nil {
		t.Fatal(err)
	}
	return task
}

func makeRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "seed.txt")
	gitRun(t, repo, "-c", "user.name=MAR Test", "-c", "user.email=mar@example.invalid", "commit", "-m", "seed")
	return repo, gitOut(t, repo, "rev-parse", "HEAD")
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func countWorktrees(t *testing.T, repo string) int {
	t.Helper()
	out := gitOut(t, repo, "worktree", "list", "--porcelain")
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			count++
		}
	}
	return count
}

func TestHelperFormatting(t *testing.T) {
	if got := fmt.Sprintf("%s", domain.WorkspaceReady); got != "READY" {
		t.Fatal(got)
	}
}
