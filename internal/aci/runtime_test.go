package aci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeExecutor struct {
	level IsolationLevel
	last  ExecSpec
	specs []ExecSpec
	calls int
	resp  ExecResult
	err   error
}

type fakeGitBroker struct{}

func (f *fakeGitBroker) Status(context.Context, string, string, int) (ExecResult, error) {
	return ExecResult{Output: "## main", ExitCode: 0}, nil
}

func (f *fakeGitBroker) Diff(context.Context, string, string, []string, int) (ExecResult, error) {
	return ExecResult{Output: "diff", ExitCode: 0}, nil
}

func (f *fakeExecutor) IsolationLevel() IsolationLevel { return f.level }
func (f *fakeExecutor) Run(_ context.Context, _ string, spec ExecSpec) (ExecResult, error) {
	f.calls++
	f.last = spec
	f.specs = append(f.specs, spec)
	return f.resp, f.err
}

func newTestRuntime(t *testing.T, executor Executor, allowTrusted bool) (*Runtime, string) {
	t.Helper()
	root := t.TempDir()
	var gitBroker GitBroker
	if executor != nil {
		gitBroker = &fakeGitBroker{}
	}
	r, err := New(Config{
		Root:                         root,
		TaskID:                       "task-test",
		MaxReadBytes:                 32,
		MaxWriteBytes:                64 << 10,
		MaxSearchResults:             2,
		MaxSearchFileBytes:           64 << 10,
		MaxCommandOutputBytes:        128,
		CommandTimeout:               time.Second,
		AllowTrustedCommandExecution: allowTrusted,
		GitBroker:                    gitBroker,
	}, executor)
	if err != nil {
		t.Fatal(err)
	}
	return r, root
}

func TestReadFileIsBoundedAndHashBound(t *testing.T) {
	r, root := newTestRuntime(t, nil, false)
	content := []byte("first line\nsecond line is deliberately long\nthird line\n")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := r.ReadFile("a.txt", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || len(got.Text) > 32 {
		t.Fatalf("read was not bounded: %+v", got)
	}
	sum := sha256.Sum256(content)
	if got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected hash %s", got.SHA256)
	}
}

func TestPathTraversalAndGitMetadataAreRejected(t *testing.T) {
	r, root := newTestRuntime(t, nil, false)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadFile("../outside.txt", 1, 1); err == nil {
		t.Fatal("path traversal was allowed")
	}
	if _, err := r.ReadFile(".git/config", 1, 1); err == nil {
		t.Fatal("direct .git read was allowed")
	}
	if _, err := r.WriteFile("../outside.txt", "ABSENT", []byte("x")); err == nil {
		t.Fatal("path traversal write was allowed")
	}
}

func TestSearchResultsAreBounded(t *testing.T) {
	r, root := newTestRuntime(t, nil, false)
	for i, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("needle line\n"), 0o644); err != nil {
			t.Fatal(i, err)
		}
	}
	got, err := r.SearchText("needle", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Matches) != 2 || !got.Truncated {
		t.Fatalf("unexpected search result: %+v", got)
	}
}

func TestSharedGoModuleCacheIsUsedWithoutMakingItTaskWritable(t *testing.T) {
	root := t.TempDir()
	shared := t.TempDir()
	fakeGo := filepath.Join(t.TempDir(), "go.exe")
	if err := os.WriteFile(fakeGo, []byte("not executed by fake executor"), 0o755); err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{level: IsolationEnforcedSandbox, resp: ExecResult{ExitCode: 0}}
	r, err := New(Config{
		Root:          root,
		TaskID:        "task-shared-modcache",
		GitBroker:     &fakeGitBroker{},
		GoModuleCache: shared,
	}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunCommand(context.Background(), Command{Name: fakeGo, Args: []string{"test", "./..."}}); err != nil {
		t.Fatal(err)
	}
	want := "GOMODCACHE=" + filepath.Clean(shared)
	found := false
	for _, item := range executor.last.Env {
		if strings.EqualFold(item, want) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("shared module cache was not propagated: env=%v", executor.last.Env)
	}
	if _, err := os.Stat(filepath.Join(root, ".mar", "go", "mod")); !os.IsNotExist(err) {
		t.Fatalf("task-local module cache should not be created when shared cache is configured: %v", err)
	}
}

func TestGoCommandPreseedsTelemetryOffInsideTaskProfile(t *testing.T) {
	root := t.TempDir()
	fakeGo := filepath.Join(t.TempDir(), "go.exe")
	if err := os.WriteFile(fakeGo, []byte("not executed by fake executor"), 0o755); err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{level: IsolationEnforcedSandbox, resp: ExecResult{ExitCode: 0}}
	r, err := New(Config{Root: root, TaskID: "task-go-telemetry", GitBroker: &fakeGitBroker{}}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunCommand(context.Background(), Command{Name: fakeGo, Args: []string{"test", "./..."}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunCommand(context.Background(), Command{Name: fakeGo, Args: []string{"vet", "./..."}}); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 2 || len(executor.specs) != 2 {
		t.Fatalf("Go telemetry preparation launched an unexpected subprocess: calls=%d specs=%d", executor.calls, len(executor.specs))
	}
	if got := strings.Join(executor.specs[0].Args, " "); got != "test ./..." {
		t.Fatalf("unexpected first user Go command: %q", got)
	}
	if got := strings.Join(executor.specs[1].Args, " "); got != "vet ./..." {
		t.Fatalf("unexpected second user Go command: %q", got)
	}
	modePath := filepath.Join(root, ".mar", "runtime", "profile", "AppData", "Roaming", "go", "telemetry", "mode")
	mode, err := os.ReadFile(modePath)
	if err != nil {
		t.Fatalf("task-scoped telemetry mode was not seeded: %v", err)
	}
	if strings.TrimSpace(string(mode)) != "off" {
		t.Fatalf("unexpected task-scoped telemetry mode: %q", mode)
	}
	wantProfile := "USERPROFILE=" + filepath.Join(root, ".mar", "runtime", "profile")
	foundProfile := false
	for _, item := range executor.specs[0].Env {
		if strings.EqualFold(item, wantProfile) {
			foundProfile = true
			break
		}
	}
	if !foundProfile {
		t.Fatalf("Go command did not use the task-local profile: env=%v", executor.specs[0].Env)
	}
}

func TestWriteAndReplaceRequireExactRevision(t *testing.T) {
	r, root := newTestRuntime(t, nil, false)
	path := filepath.Join(root, "x.txt")
	if err := os.WriteFile(path, []byte("alpha beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.WriteFile("x.txt", "wrong", []byte("nope")); err == nil {
		t.Fatal("write ignored hash precondition")
	}
	before := sha256.Sum256([]byte("alpha beta"))
	hash := hex.EncodeToString(before[:])
	if _, err := r.ReplaceExact("x.txt", hash, "alpha", "omega", 1); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "omega beta" {
		t.Fatalf("unexpected replacement: %q", got)
	}
}

func TestCommandCwdDotMeansWorkspaceRootAndEscapeStillFails(t *testing.T) {
	root := t.TempDir()
	fakeGo := filepath.Join(t.TempDir(), "go.exe")
	if err := os.WriteFile(fakeGo, []byte("not executed by fake executor"), 0o755); err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{level: IsolationEnforcedSandbox, resp: ExecResult{ExitCode: 0}}
	r, err := New(Config{Root: root, TaskID: "task-command-cwd", GitBroker: &fakeGitBroker{}}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunCommand(context.Background(), Command{Name: fakeGo, Args: []string{"test", "./..."}, Cwd: "."}); err != nil {
		t.Fatalf("workspace-root cwd was rejected: %v", err)
	}
	if filepath.Clean(executor.last.Dir) != filepath.Clean(root) {
		t.Fatalf("workspace-root cwd resolved incorrectly: got=%q want=%q", executor.last.Dir, root)
	}
	if _, err := r.RunCommand(context.Background(), Command{Name: fakeGo, Args: []string{"test", "./..."}, Cwd: ".."}); err == nil || !strings.Contains(err.Error(), "path escapes workspace") {
		t.Fatalf("cwd escape was not rejected: %v", err)
	}
}

func TestTrustedHostCommandExecutionIsDeniedByDefault(t *testing.T) {
	executor := &fakeExecutor{level: IsolationTrustedHost}
	r, _ := newTestRuntime(t, executor, false)
	_, err := r.RunCommand(context.Background(), Command{Name: "go", Args: []string{"test", "./..."}})
	if err == nil || !strings.Contains(err.Error(), "requires enforced sandbox") || executor.calls != 0 {
		t.Fatalf("trusted host command was not denied: err=%v calls=%d", err, executor.calls)
	}
}

func TestTrustedHostCommandExecutionCanBeExplicitlyEnabled(t *testing.T) {
	executor := &fakeExecutor{level: IsolationTrustedHost, resp: ExecResult{Output: "ok", ExitCode: 0}}
	r, _ := newTestRuntime(t, executor, true)
	if _, err := r.RunCommand(context.Background(), Command{Name: "go", Args: []string{"test", "./..."}}); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Host test environments may not have Go on PATH; the authority assertion
		// is that execution is no longer rejected solely for TRUSTED_HOST.
		if strings.Contains(err.Error(), "requires enforced sandbox") {
			t.Fatalf("explicit trusted execution was still rejected: %v", err)
		}
	}
}
