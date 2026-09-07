package store

import (
	"context"
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
