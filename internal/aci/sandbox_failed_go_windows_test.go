//go:build windows

package aci

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsSandboxExecutorReturnsPromptlyForFailingGoTest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module sandboxfailprobe\n\ngo 1.27.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fail_test.go"), []byte("package sandboxfailprobe\n\nimport \"testing\"\n\nfunc TestFail(t *testing.T) { t.Fatal(\"intentional failure\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	goBin := filepath.Join(os.Getenv("ProgramFiles"), "Go", "bin")
	t.Setenv("PATH", goBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	executor, err := NewWindowsSandboxExecutor(root)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewContainedGitBroker()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Root: root, TaskID: "task-sandbox-go-failure", CommandTimeout: 120 * time.Second, GitBroker: broker}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.SelfHostingSafe() {
		t.Skip("Windows AppContainer host prerequisite is not prepared; sandbox remains fail-closed")
	}
	result, err := runtime.RunCommand(context.Background(), Command{Name: "go", Args: []string{"test", "./..."}})
	if err == nil {
		t.Fatalf("failing go test unexpectedly succeeded: %+v", result)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("failing go test hit the bounded command deadline instead of returning failure evidence: err=%v output=%s", err, result.Output)
	}
	if result.ExitCode == 0 || !strings.Contains(result.Output, "intentional failure") {
		t.Fatalf("failing go test lost exit/output evidence: result=%+v err=%v", result, err)
	}
}

func TestWindowsSandboxPortableGoSharedCacheReturnsPromptlyForFailingTest(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":      "module portablefailprobe\n\ngo 1.27\n",
		"sum.go":      "package medium\n\nfunc Sum(a, b int) int { return a - b }\n",
		"format.go":   "package medium\n\nimport \"fmt\"\n\nfunc Label(v int) string { return fmt.Sprintf(\"value:%d\", v) }\n",
		"app_test.go": "package medium\n\nimport \"testing\"\n\nfunc TestBehavior(t *testing.T) {\n\tif Sum(2, 3) != 5 { t.Fatal(\"bad sum\") }\n\tif Label(5) != \"sum=5\" { t.Fatal(\"bad label\") }\n}\n",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	goExe := findPortableGoForACI(t)
	goRoot := filepath.Dir(filepath.Dir(goExe))
	goBin := filepath.Dir(goExe)
	sharedModCache := filepath.Join(t.TempDir(), "gomodcache")
	if err := os.MkdirAll(sharedModCache, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", goBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	executor, err := NewWindowsSandboxExecutor(root, goRoot, sharedModCache)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewContainedGitBroker()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Root: root, TaskID: "task-portable-go-failure", CommandTimeout: 120 * time.Second, GitBroker: broker, GoModuleCache: sharedModCache}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.SelfHostingSafe() {
		t.Skip("Windows AppContainer host prerequisite is not prepared; sandbox remains fail-closed")
	}
	result, err := runtime.RunCommand(context.Background(), Command{Name: "go", Args: []string{"test", "./..."}})
	if err == nil {
		t.Fatalf("failing portable go test unexpectedly succeeded: %+v", result)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("failing portable go test hit the bounded command deadline instead of returning failure evidence: err=%v output=%s", err, result.Output)
	}
	if result.ExitCode == 0 || !strings.Contains(result.Output, "bad sum") {
		t.Fatalf("failing portable go test lost exit/output evidence: result=%+v err=%v", result, err)
	}
}

func TestWindowsSandboxLinkedWorktreeReturnsPromptlyForFailingGoTest(t *testing.T) {
	source := t.TempDir()
	files := map[string]string{
		"go.mod":      "module linkedfailprobe\n\ngo 1.27\n",
		"sum.go":      "package medium\n\nfunc Sum(a, b int) int { return a - b }\n",
		"format.go":   "package medium\n\nimport \"fmt\"\n\nfunc Label(v int) string { return fmt.Sprintf(\"value:%d\", v) }\n",
		"app_test.go": "package medium\n\nimport \"testing\"\n\nfunc TestBehavior(t *testing.T) { if Sum(2, 3) != 5 { t.Fatal(\"bad sum\") }; if Label(5) != \"sum=5\" { t.Fatal(\"bad label\") } }\n",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(source, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runACIGit(t, source, "init", "-b", "main")
	runACIGit(t, source, "config", "user.name", "MAR ACI Test")
	runACIGit(t, source, "config", "user.email", "mar-aci@local.invalid")
	runACIGit(t, source, "add", "-A")
	runACIGit(t, source, "commit", "-m", "baseline")
	workspace := filepath.Join(t.TempDir(), "worktree")
	runACIGit(t, source, "worktree", "add", "--detach", workspace, "HEAD")
	defer exec.Command("git", "-C", source, "worktree", "remove", "--force", workspace).Run()

	goExe := findPortableGoForACI(t)
	goRoot := filepath.Dir(filepath.Dir(goExe))
	goBin := filepath.Dir(goExe)
	sharedModCache := filepath.Join(t.TempDir(), "gomodcache")
	if err := os.MkdirAll(sharedModCache, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", goBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	executor, err := NewWindowsSandboxExecutor(workspace, goRoot, sharedModCache)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewContainedGitBroker()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Root: workspace, TaskID: "task-linked-go-failure", CommandTimeout: 120 * time.Second, GitBroker: broker, GoModuleCache: sharedModCache}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.SelfHostingSafe() {
		t.Skip("sandbox host prerequisite unavailable")
	}
	result, runErr := runtime.RunCommand(context.Background(), Command{Name: "go", Args: []string{"test", "./..."}})
	if runErr == nil {
		t.Fatalf("linked-worktree failing go test unexpectedly succeeded: %+v", result)
	}
	if errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("linked-worktree failing go test hit the bounded command deadline instead of returning failure evidence: err=%v output=%s", runErr, result.Output)
	}
	if result.ExitCode == 0 || !strings.Contains(result.Output, "bad sum") {
		t.Fatalf("linked-worktree failing go test lost failure evidence: result=%+v err=%v", result, runErr)
	}
}

func runACIGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func findPortableGoForACI(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for root := filepath.Clean(cwd); ; root = filepath.Dir(root) {
		candidate := filepath.Join(root, ".mar", "runtime", "go-portable", "go", "bin", "go.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
	}
	t.Fatal("portable Go executable not found")
	return ""
}
