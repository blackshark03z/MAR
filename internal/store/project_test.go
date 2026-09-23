package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestListProjectsReturnsRegisteredProjectsInStableOrder(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	for _, project := range []domain.Project{
		{ID: "zeta", Root: filepath.Join(t.TempDir(), "zeta"), CreatedAt: time.Now().UTC()},
		{ID: "alpha", Root: filepath.Join(t.TempDir(), "alpha"), CreatedAt: time.Now().UTC()},
	} {
		if _, _, err := db.RegisterProject(ctx, project); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := db.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("unexpected project count %d", len(projects))
	}
	if projects[0].ID != "alpha" || projects[1].ID != "zeta" {
		t.Fatalf("projects are not in stable id order: %+v", projects)
	}
}

func TestDeleteProjectFailsClosedWhenDurableTaskReferencesProject(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	project := domain.Project{ID: "durable-project", Root: filepath.Join(t.TempDir(), "repo"), CreatedAt: now}
	if _, _, err := db.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	policy := domain.ProjectPolicy{ProjectID: project.ID, LocalFileWrite: true, UpdatedAt: now}
	if err := db.PutProjectPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal: "preserve durable task authority", Acceptance: []string{"project detach fails closed"},
		ProjectID: project.ID, BaseRevision: "base", VerificationProfile: "test", Priority: "P2",
	}
	hash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: "durable-task", IdempotencyKey: "durable-task-key", Contract: contract, ContractHash: hash, State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
	if _, _, err := db.SubmitTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	if err := db.DeleteProject(ctx, project.ID); err == nil {
		t.Fatal("project deletion with durable task reference must fail closed")
	}
	if _, err := db.GetProject(ctx, project.ID); err != nil {
		t.Fatalf("failed project deletion removed project authority: %v", err)
	}
	if got, err := db.GetProjectPolicy(ctx, project.ID); err != nil || got.ProjectID != project.ID {
		t.Fatalf("failed project deletion did not roll back policy deletion: policy=%+v err=%v", got, err)
	}
	if got, err := db.GetTask(ctx, task.ID); err != nil || got.ID != task.ID {
		t.Fatalf("failed project deletion lost durable task: task=%+v err=%v", got, err)
	}
}

func TestDeleteProjectRemovesUnreferencedProjectAndPolicy(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	project := domain.Project{ID: "local-detach-test", Root: filepath.Join(t.TempDir(), "repo"), CreatedAt: time.Now().UTC()}
	if _, _, err := db.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := db.PutProjectPolicy(ctx, domain.ProjectPolicy{ProjectID: project.ID, LocalFileWrite: true, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteProject(ctx, project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetProject(ctx, project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected project removal, got %v", err)
	}
	if _, err := db.GetProjectPolicy(ctx, project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected policy removal, got %v", err)
	}
}
