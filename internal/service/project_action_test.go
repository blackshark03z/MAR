package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestProjectActionPatchRunAndGitFlow(t *testing.T) {
	svc, root, projectID, _ := newProjectContextFixture(t, "fast-action", map[string]string{
		"go.mod":      "module example.com/fastaction\n\ngo 1.27\n",
		"tracked.txt": "base\n",
	})
	if err := svc.store.PutProjectPolicy(context.Background(), domain.ProjectPolicy{
		ProjectID: projectID, LocalFileWrite: true, LocalGitWrite: true, NetworkAllowed: true, UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256([]byte("base\n"))
	patch, err := svc.ApplyProjectPatch(context.Background(), ProjectPatchRequest{
		ProjectID: projectID, Path: "tracked.txt", ExpectedSHA256: hex.EncodeToString(before[:]),
		Search: "base", Replacement: "changed", ExpectedCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if patch.Replacements != 1 || patch.BeforeSHA256 == patch.AfterSHA256 {
		t.Fatalf("unexpected patch result: %+v", patch)
	}
	run, err := svc.RunProjectCommand(context.Background(), projectID, "git", []string{"status", "--porcelain"}, ".", 30, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if run.ExitCode != 0 || !strings.Contains(run.Output, "tracked.txt") {
		t.Fatalf("unexpected run result: %+v", run)
	}
	stage, err := svc.StageProjectPaths(context.Background(), projectID, []string{"tracked.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if stage.Operation != "git_stage" {
		t.Fatalf("unexpected stage result: %+v", stage)
	}
	commit, err := svc.CommitProject(context.Background(), projectID, "fast path test")
	if err != nil {
		t.Fatal(err)
	}
	if commit.Revision == "" {
		t.Fatalf("missing commit revision: %+v", commit)
	}
	bounded, err := svc.RunProjectCommand(context.Background(), projectID, "git", []string{"rev-parse", "HEAD"}, ".", 30, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !bounded.OutputTruncated || len(bounded.Output) != 5 {
		t.Fatalf("expected bounded command output: %+v", bounded)
	}
	if _, err := svc.PushProject(context.Background(), projectID, "origin"); err == nil || !strings.Contains(err.Error(), "RemoteGitWrite") {
		t.Fatalf("expected remote policy rejection, got %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "tracked.txt")); err != nil || string(raw) != "changed\n" {
		t.Fatalf("unexpected patched file: %q err=%v", raw, err)
	}
}

func TestProjectActionFailsClosedOnPolicyHashAndTraversal(t *testing.T) {
	svc, _, projectID, _ := newProjectContextFixture(t, "fast-action-fail", map[string]string{
		"go.mod":      "module example.com/fastactionfail\n\ngo 1.27\n",
		"tracked.txt": "base\n",
	})
	if err := svc.store.PutProjectPolicy(context.Background(), domain.ProjectPolicy{
		ProjectID: projectID, LocalFileWrite: true, LocalGitWrite: true, NetworkAllowed: true, UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyProjectPatch(context.Background(), ProjectPatchRequest{
		ProjectID: projectID, Path: "tracked.txt", ExpectedSHA256: strings.Repeat("0", 64),
		Search: "base", Replacement: "changed", ExpectedCount: 1,
	}); err == nil || !strings.Contains(err.Error(), "revision mismatch") {
		t.Fatalf("expected hash mismatch, got %v", err)
	}
	if _, err := svc.RunProjectCommand(context.Background(), projectID, "git", []string{"status"}, "..", 30, 1024); err == nil {
		t.Fatal("expected cwd traversal rejection")
	}
	if _, err := svc.StageProjectPaths(context.Background(), projectID, []string{"../escape.txt"}); err == nil {
		t.Fatal("expected git path traversal rejection")
	}
	if err := svc.store.PutProjectPolicy(context.Background(), domain.ProjectPolicy{ProjectID: projectID, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256([]byte("base\n"))
	if _, err := svc.ApplyProjectPatch(context.Background(), ProjectPatchRequest{
		ProjectID: projectID, Path: "tracked.txt", ExpectedSHA256: hex.EncodeToString(before[:]),
		Search: "base", Replacement: "changed", ExpectedCount: 1,
	}); err == nil || !strings.Contains(err.Error(), "local_file_write") {
		t.Fatalf("expected policy rejection, got %v", err)
	}
}

func TestProjectActionWriteCreatesAndReplacesWithRevisionGuard(t *testing.T) {
	svc, root, projectID, _ := newProjectContextFixture(t, "fast-write", map[string]string{
		"go.mod": "module example.com/fastwrite\n\ngo 1.27\n",
	})
	if err := svc.store.PutProjectPolicy(context.Background(), domain.ProjectPolicy{
		ProjectID: projectID, LocalFileWrite: true, LocalGitWrite: true, NetworkAllowed: true, UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.WriteProjectFile(context.Background(), ProjectWriteRequest{
		ProjectID: projectID, Path: "created.txt", ExpectedSHA256: "ABSENT", Content: "first\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Created || created.BeforeSHA256 != "" || created.Bytes != len("first\n") {
		t.Fatalf("unexpected create result: %+v", created)
	}
	first := sha256.Sum256([]byte("first\n"))
	replaced, err := svc.WriteProjectFile(context.Background(), ProjectWriteRequest{
		ProjectID: projectID, Path: "created.txt", ExpectedSHA256: hex.EncodeToString(first[:]), Content: "second\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Created || replaced.BeforeSHA256 != hex.EncodeToString(first[:]) {
		t.Fatalf("unexpected replace result: %+v", replaced)
	}
	raw, err := os.ReadFile(filepath.Join(root, "created.txt"))
	if err != nil || string(raw) != "second\n" {
		t.Fatalf("unexpected written file: %q err=%v", raw, err)
	}
	if _, err := svc.WriteProjectFile(context.Background(), ProjectWriteRequest{
		ProjectID: projectID, Path: "created.txt", ExpectedSHA256: "ABSENT", Content: "bad\n",
	}); err == nil {
		t.Fatal("expected ABSENT precondition to reject existing file")
	}
	if _, err := svc.WriteProjectFile(context.Background(), ProjectWriteRequest{
		ProjectID: projectID, Path: "../escape.txt", ExpectedSHA256: "ABSENT", Content: "bad\n",
	}); err == nil {
		t.Fatal("expected write traversal rejection")
	}
}
