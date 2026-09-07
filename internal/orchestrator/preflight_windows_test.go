//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mar/internal/domain"
	"mar/internal/service"
	"mar/internal/store"
	"mar/internal/verification"
)

type fakePreflightGit struct {
	root    string
	base    string
	baseErr error
}

func (g fakePreflightGit) Run(_ context.Context, _ string, _ string, args ...string) (string, error) {
	if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--show-toplevel" {
		return g.root, nil
	}
	if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--verify" {
		if g.baseErr != nil {
			return "", g.baseErr
		}
		return g.base, nil
	}
	return "", errors.New("unexpected Git command")
}

func preflightFixture(t *testing.T, profileID string) (*store.SQLite, *service.TaskService, domain.Task, string, *verification.Registry) {
	t.Helper()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.invalid/preflight\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	svc := service.NewTaskService(s)
	if _, _, err := svc.RegisterProject(ctx, "project-1", root); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "make a bounded change",
		Acceptance:          []string{"targeted behavior passes"},
		Boundaries:          []string{"local project only"},
		NonGoals:            []string{},
		ProjectID:           "project-1",
		BaseRevision:        "0123456789abcdef",
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
		VerificationProfile: profileID,
		Priority:            "P2",
	}
	task, _, err := svc.Submit(ctx, "preflight-key", contract)
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := verification.NewRegistry(verification.Profile{
		ID:       "go-standard",
		Commands: []verification.Command{{Name: "go", Args: []string{"test", "./..."}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, svc, task, root, profiles
}

func TestPreflightValidatesGitBaseBeforeWaitingResource(t *testing.T) {
	s, svc, task, root, profiles := preflightFixture(t, "go-standard")
	preflight, err := newPreflightWithGit(s, svc, profiles, fakePreflightGit{root: root, base: "abcdef0123456789"})
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight.Drive(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.TaskWaitingResource {
		t.Fatalf("preflight state mismatch: got=%s", got.State)
	}
	waiting, err := s.ListTasksByState(context.Background(), domain.TaskWaitingResource)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || waiting[0].ID != task.ID {
		t.Fatalf("state listing mismatch: %+v", waiting)
	}
}

func TestPreflightRejectsUnsupportedAuthorityBeforeQueueing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*domain.Authority)
	}{
		{name: "network", mutate: func(a *domain.Authority) { a.NetworkAllowed = true }},
		{name: "remote-git", mutate: func(a *domain.Authority) { a.RemoteGitWrite = true }},
		{name: "deploy", mutate: func(a *domain.Authority) { a.DeployAllowed = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, svc, baseTask, root, profiles := preflightFixture(t, "go-standard")
			contract := baseTask.Contract
			tc.mutate(&contract.Authority)
			task, _, err := svc.Submit(context.Background(), "unsupported-"+tc.name, contract)
			if err != nil {
				t.Fatal(err)
			}
			preflight, err := newPreflightWithGit(s, svc, profiles, fakePreflightGit{root: root, base: "abcdef0123456789"})
			if err != nil {
				t.Fatal(err)
			}
			if err := preflight.Drive(context.Background(), task.ID); err == nil {
				t.Fatal("unsupported authority entered the resource queue")
			}
			got, err := s.GetTask(context.Background(), task.ID)
			if err != nil || got.State != domain.TaskBlocked {
				t.Fatalf("unsupported authority did not block in preflight: task=%+v err=%v", got, err)
			}
		})
	}
}

func TestPreflightRejectsNonGoProjectBeforeQueueing(t *testing.T) {
	s, svc, task, root, profiles := preflightFixture(t, "go-standard")
	if err := os.Remove(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	preflight, err := newPreflightWithGit(s, svc, profiles, fakePreflightGit{root: root, base: "abcdef0123456789"})
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight.Drive(context.Background(), task.ID); err == nil {
		t.Fatal("non-Go project entered the resource queue")
	}
	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil || got.State != domain.TaskBlocked {
		t.Fatalf("unsupported project did not block in preflight: task=%+v err=%v", got, err)
	}
}

func TestPreflightInvalidBaseBlocksInsteadOfQueueing(t *testing.T) {
	s, svc, task, root, profiles := preflightFixture(t, "go-standard")
	preflight, err := newPreflightWithGit(s, svc, profiles, fakePreflightGit{root: root, baseErr: errors.New("unknown revision")})
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight.Drive(context.Background(), task.ID); err == nil {
		t.Fatal("expected invalid-base preflight failure")
	}
	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.TaskBlocked {
		t.Fatalf("invalid base must block, got=%s", got.State)
	}
}

func TestPreflightProjectPolicyBlocksAuthorityWidening(t *testing.T) {
	s, svc, task, root, profiles := preflightFixture(t, "go-standard")
	if _, err := svc.UpdateProjectPolicy(context.Background(), task.Contract.ProjectID, false, true); err != nil {
		t.Fatal(err)
	}
	preflight, err := newPreflightWithGit(s, svc, profiles, fakePreflightGit{root: root, base: "abcdef0123456789"})
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight.Drive(context.Background(), task.ID); err == nil {
		t.Fatal("expected project-policy preflight failure")
	}
	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.TaskBlocked {
		t.Fatalf("project policy widening must block before queue, got=%s", got.State)
	}
}

func TestPreflightUnknownVerificationProfileBlocks(t *testing.T) {
	s, svc, task, root, profiles := preflightFixture(t, "missing-profile")
	preflight, err := newPreflightWithGit(s, svc, profiles, fakePreflightGit{root: root, base: "abcdef0123456789"})
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight.Drive(context.Background(), task.ID); err == nil {
		t.Fatal("expected missing-profile preflight failure")
	}
	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.TaskBlocked {
		t.Fatalf("missing profile must block, got=%s", got.State)
	}
}
