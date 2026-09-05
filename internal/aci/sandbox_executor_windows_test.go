//go:build windows

package aci

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsSandboxExecutorIsSelfHostingSafeAndRunsGit(t *testing.T) {
	root := t.TempDir()
	runGitSetup(t, root, "init")
	runGitSetup(t, root, "config", "user.email", "mar-test@example.invalid")
	runGitSetup(t, root, "config", "user.name", "MAR Test")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitSetup(t, root, "add", "a.txt")
	runGitSetup(t, root, "commit", "-m", "base")

	executor, err := NewWindowsSandboxExecutor(root)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewContainedGitBroker()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{
		Root:                  root,
		TaskID:                "task-sandbox-git-integration",
		MaxCommandOutputBytes: 16 << 10,
		CommandTimeout:        15 * time.Second,
		GitBroker:             broker,
	}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.SelfHostingSafe() {
		t.Skip("Windows AppContainer host prerequisite is not prepared; sandbox remains fail-closed")
	}
	status, err := runtime.GitStatus(context.Background())
	if err != nil {
		t.Fatalf("sandbox git status failed: %v output=%s", err, status.Output)
	}
	if !strings.Contains(status.Output, "##") {
		t.Fatalf("unexpected sandbox git status: %q", status.Output)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := runtime.GitDiff(context.Background(), []string{"a.txt"})
	if err != nil {
		t.Fatalf("sandbox git diff failed: %v output=%s", err, diff.Output)
	}
	if !strings.Contains(diff.Output, "-one") || !strings.Contains(diff.Output, "+two") {
		t.Fatalf("unexpected sandbox git diff: %q", diff.Output)
	}
}

func TestWindowsSandboxExecutorRunsNativeGoToolchain(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module sandboxprobe\n\ngo 1.27.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package sandboxprobe\n\nimport \"testing\"\n\nfunc TestProbe(t *testing.T) {}\n"), 0o644); err != nil {
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
	runtime, err := New(Config{Root: root, TaskID: "task-sandbox-go", CommandTimeout: 30 * time.Second, GitBroker: broker}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.SelfHostingSafe() {
		t.Skip("Windows AppContainer host prerequisite is not prepared; sandbox remains fail-closed")
	}
	result, err := runtime.RunCommand(context.Background(), Command{Name: "go", Args: []string{"test", "./..."}})
	if err != nil {
		t.Fatalf("sandboxed go test failed: %v output=%s", err, result.Output)
	}
	if !strings.Contains(result.Output, "ok") {
		t.Fatalf("unexpected sandboxed go test output: %q", result.Output)
	}
}

func TestSandboxCommandEnvironmentDoesNotInheritAmbientSecrets(t *testing.T) {
	root := t.TempDir()
	executor, err := NewWindowsSandboxExecutor(root)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Root: root, TaskID: "task-sandbox-env"}, executor)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAR_AMBIENT_SECRET", "must-not-cross-boundary")
	commandPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	env, err := runtime.commandEnvironment(commandPath)
	if err != nil {
		t.Fatal(err)
	}
	joined := "\n" + strings.Join(env, "\n") + "\n"
	if strings.Contains(joined, "MAR_AMBIENT_SECRET=") || strings.Contains(joined, "must-not-cross-boundary") {
		t.Fatalf("ambient secret leaked into sandbox environment: %v", env)
	}
	for _, key := range []string{"USERPROFILE", "HOME", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP"} {
		value := envValue(env, key)
		if value == "" {
			t.Fatalf("sandbox environment missing %s", key)
		}
		if !inside(root, value) {
			t.Fatalf("%s escapes workspace: %q", key, value)
		}
	}
}

func envValue(env []string, key string) string {
	prefix := strings.ToUpper(key) + "="
	for _, item := range env {
		if strings.HasPrefix(strings.ToUpper(item), prefix) {
			return item[len(prefix):]
		}
	}
	return ""
}
