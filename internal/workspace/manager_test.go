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

func TestEnsureMutablePinsManagedEOLPolicyAndCreatesCleanBaseline(t *testing.T) {
	ctx := context.Background()
	repo, base := makeRepo(t)
	// Reproduce the owner host: Git for Windows can provide autocrlf=true via
	// ambient configuration. A local setting makes the regression deterministic
	// on every test host; MAR's managed-workspace policy must override it.
	gitRun(t, repo, "config", "core.autocrlf", "true")

	s, svc, manager := workspaceHarness(t, repo)
	defer s.Close()
	task := waitingTask(t, svc, "workspace-eol-policy", repo, base)
	ws, err := manager.EnsureMutable(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(ws.Path, "seed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "seed\n" {
		t.Fatalf("managed worktree inherited host EOL conversion: %q", content)
	}
	status := gitOut(t, ws.Path, "-c", "core.autocrlf=false", "-c", "core.eol=lf", "status", "--porcelain=v1", "--untracked-files=all")
	if status != "" {
		t.Fatalf("managed worktree baseline is dirty under MAR Git policy: %q", status)
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

func TestWorkspacePathUsesCompactDeterministicKey(t *testing.T) {
	ctx := context.Background()
	repo, base := makeRepo(t)
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.NewTaskService(db)
	dataRoot := filepath.Join(t.TempDir(), strings.Repeat("long-parent-", 4), "mar-data")
	manager, err := workspace.NewManager(db, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	task := waitingTask(t, svc, "workspace-compact-path", repo, base)
	ws, err := manager.EnsureMutable(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(dataRoot, ws.Path)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(filepath.Clean(rel), string(os.PathSeparator))
	if len(parts) != 2 || parts[0] != "w" || len(parts[1]) != 32 {
		t.Fatalf("workspace path is not compact/deterministic: rel=%q path=%q", rel, ws.Path)
	}
	if strings.Contains(strings.ToLower(rel), strings.ToLower(task.ID)) {
		t.Fatalf("workspace path leaked full task id and widened the Windows path budget: %q", rel)
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

func TestAcceptanceT5FiveParallelTasksReceiveDistinctWorktreesAndMeasureDiskAmplification(t *testing.T) {
	ctx := context.Background()
	repo, base := makeRepo(t)
	s, svc, manager := workspaceHarness(t, repo)
	defer s.Close()

	const n = 5
	tasks := make([]domain.Task, 0, n)
	for i := 0; i < n; i++ {
		tasks = append(tasks, waitingTaskForProject(t, svc, fmt.Sprintf("t5-task-%d", i+1), "t5-shared-project", repo, base))
	}
	var wg sync.WaitGroup
	wg.Add(n)
	paths := make(chan string, n)
	errs := make(chan error, n)
	for _, task := range tasks {
		task := task
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
		t.Fatalf("parallel workspace creation failed: %v", err)
	}
	unique := map[string]struct{}{}
	var workspaceBytes int64
	for path := range paths {
		key := strings.ToLower(filepath.Clean(path))
		if _, duplicate := unique[key]; duplicate {
			t.Fatalf("T5 tasks shared mutable workspace %q", path)
		}
		unique[key] = struct{}{}
		workspaceBytes += treeBytes(t, path)
	}
	if len(unique) != n || countWorktrees(t, repo) != n+1 {
		t.Fatalf("T5 isolation mismatch: unique=%d worktrees=%d", len(unique), countWorktrees(t, repo))
	}
	baseBytes := treeBytes(t, repo)
	ratio := float64(workspaceBytes) / float64(maxInt64(baseBytes, 1))
	t.Logf("T5 disk amplification baseline_bytes=%d task_worktree_bytes=%d ratio=%.3f worktrees=%d", baseBytes, workspaceBytes, ratio, n)
}

func treeBytes(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	if err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return total
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
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
	taskHash := sha256.Sum256([]byte(task.ID))
	path := filepath.Join(dataRoot, "w", hex.EncodeToString(taskHash[:16]))
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

func TestPreparingWorkspaceRejectsDirtyBaseline(t *testing.T) {
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
	task := waitingTask(t, svc, "workspace-dirty-reconcile", repo, base)
	taskHash := sha256.Sum256([]byte(task.ID))
	path := filepath.Join(dataRoot, "w", hex.EncodeToString(taskHash[:16]))
	now := time.Now().UTC()
	intent := domain.Workspace{
		ID: "workspace-dirty-reconcile", TaskID: task.ID, ProjectID: task.Contract.ProjectID,
		Path: path, BaseRevision: base, State: domain.WorkspacePreparing, CreatedAt: now, UpdatedAt: now,
	}
	if _, created, err := db.BeginWorkspace(ctx, intent); err != nil || !created {
		t.Fatalf("persist preparing workspace: created=%v err=%v", created, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "-c", "core.autocrlf=false", "-c", "core.eol=lf", "worktree", "add", "--detach", path, base)
	if err := os.WriteFile(filepath.Join(path, "seed.txt"), []byte("owner-unexpected-change\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.EnsureMutable(ctx, task.ID); err == nil || !strings.Contains(err.Error(), "baseline is not clean") {
		t.Fatalf("dirty preparing workspace must fail closed, got %v", err)
	}
	stored, err := db.GetWorkspaceByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != domain.WorkspaceFailed {
		t.Fatalf("dirty baseline was not durably failed: %+v", stored)
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
	taskHash := sha256.Sum256([]byte(task.ID))
	path := filepath.Join(dataRoot, "w", hex.EncodeToString(taskHash[:16]))
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
	base := []string{"-c", "core.autocrlf=false", "-c", "core.eol=lf", "-C", dir}
	cmd := exec.Command("git", append(base, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	base := []string{"-c", "core.autocrlf=false", "-c", "core.eol=lf", "-C", dir}
	cmd := exec.Command("git", append(base, args...)...)
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
