package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectGitStatusAndDiffExposeBoundedOwnerChanges(t *testing.T) {
	svc, root, projectID, head := newProjectContextFixture(t, "git-inspect", map[string]string{
		"go.mod":      "module example.com/gitinspect\n\ngo 1.27\n",
		"tracked.txt": "base\n",
	})
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := svc.ProjectGitStatus(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Head != head || !strings.Contains(status.Porcelain, "tracked.txt") || !strings.Contains(status.Porcelain, "untracked.txt") {
		t.Fatalf("unexpected status: %+v", status)
	}
	diff, err := svc.ProjectGitDiff(context.Background(), projectID, "tracked.txt")
	if err != nil {
		t.Fatal(err)
	}
	if diff.ProjectID != projectID || diff.Path != "tracked.txt" || !strings.Contains(diff.Diff, "-base") || !strings.Contains(diff.Diff, "+changed") {
		t.Fatalf("unexpected diff: %+v", diff)
	}
}

func TestProjectGitInspectionRejectsMissingProjectAndEscapedPath(t *testing.T) {
	svc, _, projectID, _ := newProjectContextFixture(t, "git-inspect-bounds", map[string]string{
		"go.mod": "module example.com/gitinspectbounds\n\ngo 1.27\n",
	})
	if _, err := svc.ProjectGitStatus(context.Background(), ""); err == nil {
		t.Fatal("expected missing project_id to fail")
	}
	if _, err := svc.ProjectGitDiff(context.Background(), projectID, ".."); err == nil {
		t.Fatal("expected escaped path to fail")
	}
}

func TestBoundProjectGitOutput(t *testing.T) {
	value := strings.Repeat("x", maxProjectGitOutputBytes+10)
	got, truncated := boundProjectGitOutput(value)
	if !truncated || len(got) != maxProjectGitOutputBytes {
		t.Fatalf("unexpected bounded output: len=%d truncated=%v", len(got), truncated)
	}
}
