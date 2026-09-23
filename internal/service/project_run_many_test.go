package service

import (
	"context"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestRunProjectCommandsStopsAtFirstFailure(t *testing.T) {
	svc, _, projectID, _ := newProjectContextFixture(t, "run-many", map[string]string{
		"go.mod": "module example.com/runmany\n\ngo 1.27\n",
	})
	if err := svc.store.PutProjectPolicy(context.Background(), domain.ProjectPolicy{
		ProjectID: projectID, LocalFileWrite: true, LocalGitWrite: true, NetworkAllowed: true, UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.RunProjectCommands(context.Background(), projectID, []ProjectVerifyCommand{
		{Executable: "git", Args: []string{"rev-parse", "HEAD"}, Cwd: ".", TimeoutSeconds: 30},
		{Executable: "git", Args: []string{"rev-parse", "--verify", "refs/heads/definitely-missing"}, Cwd: ".", TimeoutSeconds: 30},
		{Executable: "git", Args: []string{"status", "--porcelain"}, Cwd: ".", TimeoutSeconds: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed || result.FailedCommand != 2 || len(result.Results) != 2 {
		t.Fatalf("expected second command failure to stop batch: %+v", result)
	}
}

func TestRunProjectCommandsBoundsBatch(t *testing.T) {
	svc, _, projectID, _ := newProjectContextFixture(t, "run-many-bounds", map[string]string{
		"go.mod": "module example.com/runmanybounds\n\ngo 1.27\n",
	})
	commands := make([]ProjectVerifyCommand, maxFastCommandBatch+1)
	if _, err := svc.RunProjectCommands(context.Background(), projectID, commands); err == nil {
		t.Fatal("expected run_many command bound rejection")
	}
}
