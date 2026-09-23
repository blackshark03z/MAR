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
