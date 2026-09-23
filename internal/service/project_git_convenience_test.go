package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestProjectBranchListCreateAndWorktree(t *testing.T) {
	svc, root, projectID, head := newProjectContextFixture(t, "git-convenience", map[string]string{"go.mod": "module example.com/gitconvenience\n\ngo 1.27\n"})
	if err := svc.store.PutProjectPolicy(context.Background(), domain.ProjectPolicy{ProjectID: projectID, LocalGitWrite: true, LocalFileWrite: true, NetworkAllowed: true, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateProjectBranch(context.Background(), projectID, "feature/fast-path")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "feature/fast-path" || created.Revision != head {
		t.Fatalf("unexpected branch result: %+v", created)
	}
	listed, err := svc.ListProjectBranches(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(listed.Branches, "feature/fast-path") {
		t.Fatalf("created branch missing: %+v", listed)
	}
	worktree, err := svc.CreateProjectWorktree(context.Background(), projectID, head, "verify slice")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = runProjectGit(context.Background(), root, "worktree", "remove", "--force", worktree.Path)
		_ = os.RemoveAll(worktree.Path)
	})
	if worktree.Head != head || worktree.Baseline != head {
		t.Fatalf("unexpected worktree identity: %+v", worktree)
	}
	if _, err := os.Stat(worktree.Path); err != nil {
		t.Fatalf("worktree path missing: %v", err)
	}
}

func TestProjectGitConvenienceFailsClosed(t *testing.T) {
	svc, _, projectID, head := newProjectContextFixture(t, "git-convenience-deny", map[string]string{"go.mod": "module example.com/gitconveniencedeny\n\ngo 1.27\n"})
	if err := svc.store.PutProjectPolicy(context.Background(), domain.ProjectPolicy{ProjectID: projectID, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateProjectBranch(context.Background(), projectID, "feature/deny"); err == nil || !strings.Contains(err.Error(), "local_git_write") {
		t.Fatalf("expected branch policy denial, got %v", err)
	}
	if _, err := svc.CreateProjectWorktree(context.Background(), projectID, head, "deny"); err == nil || !strings.Contains(err.Error(), "local_git_write") {
		t.Fatalf("expected worktree policy denial, got %v", err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
