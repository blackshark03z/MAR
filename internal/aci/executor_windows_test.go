//go:build windows

package aci

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContainedHostExecutorIntegratesWithGitToolsButIsNotSelfHostingSafe(t *testing.T) {
	root := t.TempDir()
	runGitSetup(t, root, "init")
	runGitSetup(t, root, "config", "user.email", "mar-test@example.invalid")
	runGitSetup(t, root, "config", "user.name", "MAR Test")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitSetup(t, root, "add", "a.txt")
	runGitSetup(t, root, "commit", "-m", "base")

	broker, err := NewContainedGitBroker()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{
		Root:                         root,
		TaskID:                       "task-git-integration",
		MaxCommandOutputBytes:        8 << 10,
		CommandTimeout:               10 * time.Second,
		AllowTrustedCommandExecution: true,
		GitBroker:                    broker,
	}, NewContainedHostExecutor())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.SelfHostingSafe() {
		t.Fatal("Job Object containment must not be treated as security sandbox")
	}
	status, err := runtime.GitStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.Output, "##") {
		t.Fatalf("unexpected git status: %q", status.Output)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := runtime.GitDiff(context.Background(), []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Output, "-one") || !strings.Contains(diff.Output, "+two") {
		t.Fatalf("unexpected git diff: %q", diff.Output)
	}
}

func runGitSetup(t *testing.T, dir string, args ...string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, out)
	}
}
