//go:build windows

package contextengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitRepositorySnapshotCapturesRevisionAndWorkingTreeState(t *testing.T) {
	root := t.TempDir()
	runGitContextTest(t, root, "init")
	runGitContextTest(t, root, "config", "user.email", "mar-context@example.invalid")
	runGitContextTest(t, root, "config", "user.name", "MAR Context Test")
	writeContextTestFile(t, root, "clean.txt", "clean\n")
	writeContextTestFile(t, root, "both.txt", "base\n")
	writeContextTestFile(t, root, "staged.txt", "base\n")
	writeContextTestFile(t, root, ".gitignore", "ignored.txt\n")
	runGitContextTest(t, root, "add", ".")
	runGitContextTest(t, root, "commit", "-m", "base")
	revision := strings.TrimSpace(runGitContextTest(t, root, "rev-parse", "HEAD"))

	writeContextTestFile(t, root, "both.txt", "staged\n")
	runGitContextTest(t, root, "add", "both.txt")
	writeContextTestFile(t, root, "both.txt", "working\n")
	writeContextTestFile(t, root, "staged.txt", "staged\n")
	runGitContextTest(t, root, "add", "staged.txt")
	writeContextTestFile(t, root, "untracked.txt", "new\n")
	writeContextTestFile(t, root, " leading.txt", "leading\n")
	writeContextTestFile(t, root, "ignored.txt", "ignore me\n")

	repo, err := NewGitRepository(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snapshot, err := repo.Snapshot(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != revision {
		t.Fatalf("revision mismatch: got %s want %s", snapshot.Revision, revision)
	}
	got := make(map[string]string, len(snapshot.Files))
	for _, file := range snapshot.Files {
		got[file.Path] = file.Status
	}
	want := map[string]string{
		".gitignore":    "clean",
		"clean.txt":     "clean",
		"both.txt":      "modified+staged",
		"staged.txt":    "staged",
		"untracked.txt": "untracked",
		" leading.txt":  "untracked",
	}
	for path, status := range want {
		if got[path] != status {
			t.Fatalf("status %s: got %q want %q; snapshot=%v", path, got[path], status, snapshot.Files)
		}
	}
	if _, ok := got["ignored.txt"]; ok {
		t.Fatalf("ignored file leaked into snapshot: %v", snapshot.Files)
	}
}

func TestGitRepositorySnapshotFailsWhenOutputBoundIsExceeded(t *testing.T) {
	root := t.TempDir()
	runGitContextTest(t, root, "init")
	runGitContextTest(t, root, "config", "user.email", "mar-context@example.invalid")
	runGitContextTest(t, root, "config", "user.name", "MAR Context Test")
	writeContextTestFile(t, root, "base.txt", "base\n")
	runGitContextTest(t, root, "add", "base.txt")
	runGitContextTest(t, root, "commit", "-m", "base")
	for i := 0; i < 80; i++ {
		writeContextTestFile(t, root, fmt.Sprintf("%024d-%03d-context-file.txt", i, i), "x\n")
	}
	repo, err := NewGitRepository(128)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Snapshot(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "output exceeded configured bound") {
		t.Fatalf("expected bounded-output failure, got %v", err)
	}
}

func TestGitRepositorySnapshotHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	runGitContextTest(t, root, "init")
	runGitContextTest(t, root, "config", "user.email", "mar-context@example.invalid")
	runGitContextTest(t, root, "config", "user.name", "MAR Context Test")
	writeContextTestFile(t, root, "base.txt", "base\n")
	runGitContextTest(t, root, "add", "base.txt")
	runGitContextTest(t, root, "commit", "-m", "base")
	repo, err := NewGitRepository(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = repo.Snapshot(ctx, root)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestEngineBuildsBoundedContextFromRealGitSnapshot(t *testing.T) {
	root := t.TempDir()
	runGitContextTest(t, root, "init")
	runGitContextTest(t, root, "config", "user.email", "mar-context@example.invalid")
	runGitContextTest(t, root, "config", "user.name", "MAR Context Test")
	writeContextTestFile(t, root, "go.mod", "module example.com/realctx\n\ngo 1.27.0\n")
	writeContextTestFile(t, root, "cmd/app/main.go", "package main\n\nimport \"example.com/realctx/internal/worker\"\n\nfunc main() { worker.RunWorker() }\n")
	writeContextTestFile(t, root, "internal/worker/worker.go", "package worker\n\nfunc RunWorker() int { return 1 }\n")
	writeContextTestFile(t, root, "internal/worker/state.go", "package worker\n\ntype WorkerState struct{ Ready bool }\n")
	runGitContextTest(t, root, "add", ".")
	runGitContextTest(t, root, "commit", "-m", "base")
	revision := strings.TrimSpace(runGitContextTest(t, root, "rev-parse", "HEAD"))
	writeContextTestFile(t, root, "internal/worker/worker.go", "package worker\n\nfunc RunWorker() int { return 2 }\n")

	repo, err := NewGitRepository(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(repo, Config{MaxPackBytes: 8 << 10, MaxEntries: 4, MaxScanFiles: 32, MaxScanBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := engine.Build(context.Background(), Request{
		Root:             root,
		Contract:         testContract("Repair RunWorker worker behavior", revision),
		ExpectedRevision: revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Entries) == 0 || pack.Entries[0].Path != "internal/worker/worker.go" {
		t.Fatalf("real snapshot did not rank modified symbol file first: %+v", pack.Entries)
	}
	if pack.Entries[0].Status != "modified" || !containsReasonPrefix(pack.Entries[0].Reasons, "symbol:RunWorker") {
		t.Fatalf("missing real snapshot status/symbol evidence: %+v", pack.Entries[0])
	}
	foundCaller := false
	for _, entry := range pack.Entries {
		if entry.Path == "cmd/app/main.go" {
			foundCaller = true
			break
		}
	}
	if !foundCaller {
		t.Fatalf("dependency/caller context missing from real pack: %+v", pack.Entries)
	}
	if pack.Revision != revision || pack.Bytes != len(pack.Render()) || pack.Bytes > 8<<10 {
		t.Fatalf("real context pack identity/bounds invalid: %+v", pack)
	}
}

func runGitContextTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeContextTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
