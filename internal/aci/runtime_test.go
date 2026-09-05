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
	calls int
	resp  ExecResult
	err   error
}

func (f *fakeExecutor) IsolationLevel() IsolationLevel { return f.level }
func (f *fakeExecutor) Run(_ context.Context, _ string, spec ExecSpec) (ExecResult, error) {
	f.calls++
	f.last = spec
	return f.resp, f.err
}

func newTestRuntime(t *testing.T, executor Executor, allowTrusted bool) (*Runtime, string) {
	t.Helper()
	root := t.TempDir()
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

func TestWriteAndReplaceRequireExactRevision(t *testing.T) {
	r, root := newTestRuntime(t, nil, false)
	created, err := r.WriteFile("sub/new.txt", "ABSENT", []byte("alpha alpha\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !created.Created || created.BeforeHash != "" {
		t.Fatalf("unexpected create result: %+v", created)
	}
	if _, err := r.WriteFile("sub/new.txt", strings.Repeat("0", 64), []byte("wrong")); err == nil {
		t.Fatal("stale hash write was allowed")
	}
	patched, err := r.ReplaceExact("sub/new.txt", created.AfterHash, "alpha", "beta", 2)
	if err != nil {
		t.Fatal(err)
	}
	if patched.BeforeHash != created.AfterHash || patched.AfterHash == patched.BeforeHash {
		t.Fatalf("unexpected patch hashes: %+v", patched)
	}
	b, err := os.ReadFile(filepath.Join(root, "sub", "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "beta beta\n" {
		t.Fatalf("unexpected patched content %q", b)
	}
	if _, err := r.ReplaceExact("sub/new.txt", patched.AfterHash, "beta", "gamma", 1); err == nil {
		t.Fatal("exact-count mismatch was allowed")
	}
}

func TestWriteThroughSymlinkIsRejected(t *testing.T) {
	r, root := newTestRuntime(t, nil, false)
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable on this Windows host: %v", err)
	}
	if _, err := r.WriteFile("link/escape.txt", "ABSENT", []byte("x")); err == nil {
		t.Fatal("symlink write escape was allowed")
	}
}

func TestTrustedExecutorIsNotSelfHostingSafeAndDeniedByDefault(t *testing.T) {
	ex := &fakeExecutor{level: IsolationTrustedHost}
	r, _ := newTestRuntime(t, ex, false)
	if r.SelfHostingSafe() {
		t.Fatal("trusted host executor incorrectly marked self-hosting safe")
	}
	if _, err := r.RunCommand(context.Background(), Command{Name: "go", Args: []string{"test", "./..."}}); err == nil || !strings.Contains(err.Error(), "requires enforced sandbox") {
		t.Fatalf("trusted command execution was not denied: %v", err)
	}
	if ex.calls != 0 {
		t.Fatal("denied command reached executor")
	}
}

func TestSandboxedCommandPolicyRejectsDangerousShapes(t *testing.T) {
	ex := &fakeExecutor{level: IsolationEnforcedSandbox, resp: ExecResult{Output: "ok", ExitCode: 0}}
	r, _ := newTestRuntime(t, ex, false)
	if !r.SelfHostingSafe() {
		t.Fatal("enforced sandbox executor should be self-hosting safe")
	}
	bad := []Command{
		{Name: "powershell", Args: []string{"-Command", "whoami"}},
		{Name: "git", Args: []string{"reset", "--hard"}},
		{Name: "go", Args: []string{"test", "-exec=evil", "./..."}},
		{Name: "go", Args: []string{"build", "-o", "..\\escape.exe", "./cmd/mar"}},
	}
	for _, cmd := range bad {
		if _, err := r.RunCommand(context.Background(), cmd); err == nil {
			t.Fatalf("dangerous command allowed: %+v", cmd)
		}
	}
	if ex.calls != 0 {
		t.Fatal("rejected command reached executor")
	}
}

func TestSandboxedAllowedCommandUsesBoundedSpec(t *testing.T) {
	ex := &fakeExecutor{level: IsolationEnforcedSandbox, resp: ExecResult{Output: "PASS", ExitCode: 0}}
	r, root := newTestRuntime(t, ex, false)
	got, err := r.RunCommand(context.Background(), Command{Name: "go", Args: []string{"test", "./..."}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output != "PASS" || ex.calls != 1 {
		t.Fatalf("unexpected command result: %+v calls=%d", got, ex.calls)
	}
	if ex.last.Dir != root || ex.last.MaxOutputBytes != 128 {
		t.Fatalf("unexpected executor spec: %+v", ex.last)
	}
	if !containsEnvPrefix(ex.last.Env, "GOPROXY=off") || !containsEnvPrefix(ex.last.Env, "GOCACHE="+filepath.Join(root, ".mar", "go", "build")) {
		t.Fatalf("Go command environment not workspace-bounded: %v", ex.last.Env)
	}
}

func TestExecutorErrorIsReturned(t *testing.T) {
	want := errors.New("executor failed")
	ex := &fakeExecutor{level: IsolationEnforcedSandbox, err: want}
	r, _ := newTestRuntime(t, ex, false)
	_, err := r.RunCommand(context.Background(), Command{Name: "go", Args: []string{"test", "./..."}})
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func containsEnvPrefix(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}
