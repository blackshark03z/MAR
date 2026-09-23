package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"mar/internal/domain"
)

func stringPointer(value string) *string {
	return &value
}

func TestProjectApplyAndVerifyBatch(t *testing.T) {
	svc, _, projectID, _ := newProjectContextFixture(t, "apply-verify", map[string]string{
		"go.mod":      "module example.com/applyverify\n\ngo 1.27\n",
		"tracked.txt": "base\n",
	})
	if err := svc.store.PutProjectPolicy(context.Background(), domain.ProjectPolicy{
		ProjectID: projectID, LocalFileWrite: true, LocalGitWrite: true, NetworkAllowed: true, UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256([]byte("base\n"))
	result, err := svc.ApplyAndVerifyProject(context.Background(), projectID, []ProjectOwnedChange{
		{Path: "tracked.txt", ExpectedSHA256: hex.EncodeToString(before[:]), Search: "base", Replacement: "changed", ExpectedCount: 1},
		{Path: "created.txt", ExpectedSHA256: "ABSENT", Content: stringPointer("created\n")},
	}, []ProjectVerifyCommand{
		{Executable: "git", Args: []string{"diff", "--check"}, Cwd: ".", TimeoutSeconds: 30, MaxOutputBytes: 64 << 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || len(result.Changes) != 2 || len(result.Verification) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Changes[0].Operation != "patch" || result.Changes[1].Operation != "write" {
		t.Fatalf("unexpected change operations: %+v", result.Changes)
	}
}

func TestProjectApplyAndVerifyStopsAtVerificationFailure(t *testing.T) {
	svc, _, projectID, _ := newProjectContextFixture(t, "apply-verify-fail", map[string]string{
		"go.mod":      "module example.com/applyverifyfail\n\ngo 1.27\n",
		"tracked.txt": "base\n",
	})
	if err := svc.store.PutProjectPolicy(context.Background(), domain.ProjectPolicy{
		ProjectID: projectID, LocalFileWrite: true, LocalGitWrite: true, NetworkAllowed: true, UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256([]byte("base\n"))
	result, err := svc.ApplyAndVerifyProject(context.Background(), projectID, []ProjectOwnedChange{
		{Path: "tracked.txt", ExpectedSHA256: hex.EncodeToString(before[:]), Search: "base", Replacement: "changed", ExpectedCount: 1},
	}, []ProjectVerifyCommand{
		{Executable: "git", Args: []string{"rev-parse", "--verify", "refs/heads/definitely-missing"}, Cwd: ".", TimeoutSeconds: 30},
		{Executable: "git", Args: []string{"status", "--porcelain"}, Cwd: ".", TimeoutSeconds: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed || result.FailedVerification != 1 || len(result.Verification) != 1 {
		t.Fatalf("expected first verification failure to stop batch: %+v", result)
	}
}

func TestProjectApplyAndVerifyBoundsVerificationTasks(t *testing.T) {
	svc, _, projectID, _ := newProjectContextFixture(t, "apply-verify-bounds", map[string]string{
		"go.mod":      "module example.com/applyverifybounds\n\ngo 1.27\n",
		"tracked.txt": "base\n",
	})
	before := sha256.Sum256([]byte("base\n"))
	verify := make([]ProjectVerifyCommand, maxFastVerifyTasks+1)
	_, err := svc.ApplyAndVerifyProject(context.Background(), projectID, []ProjectOwnedChange{
		{Path: "tracked.txt", ExpectedSHA256: hex.EncodeToString(before[:]), Search: "base", Replacement: "changed", ExpectedCount: 1},
	}, verify)
	if err == nil {
		t.Fatal("expected verification bound rejection")
	}
}
