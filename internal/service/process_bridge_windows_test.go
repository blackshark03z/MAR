//go:build windows

package service_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/processctl"
)

func TestPhysicalTerminationProofUnlocksReplacement(t *testing.T) {
	_, svc, _ := newHarness(t)
	task := readyTask(t, svc, "physical-proof-bridge")
	ctx := context.Background()

	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-a", "supervisor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	supervisor := processctl.NewSupervisor()
	tree, err := supervisor.Start(processctl.Spec{
		Attempt: processctl.AttemptRef{TaskID: task.ID, AttemptID: attempt.ID, RunEpoch: attempt.RunEpoch},
		Path:    os.Args[0],
		Args:    []string{"-test.run=TestServiceProcessHelper"},
		Env:     append(os.Environ(), "MAR_SERVICE_HELPER=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.CloseUnverified()

	if err := svc.LogicalFenceAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecoverForReplacement(ctx, task.ID); err == nil {
		t.Fatal("replacement must remain blocked before physical proof")
	}

	termCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	proof, err := tree.TerminateAndConfirm(termCtx)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmAttemptProcessTermination(ctx, proof, "terminated-by-supervisor"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecoverForReplacement(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	replacement, err := svc.BeginAttempt(ctx, task.ID, "worker-b", "supervisor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.RunEpoch != attempt.RunEpoch+1 {
		t.Fatalf("expected replacement epoch %d, got %d", attempt.RunEpoch+1, replacement.RunEpoch)
	}
	if err := svc.HeartbeatAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, time.Minute); err == nil {
		t.Fatal("stale attempt heartbeat must be rejected after replacement admission")
	}
}

func TestInvalidTerminationProofCannotUnlockReplacement(t *testing.T) {
	_, svc, _ := newHarness(t)
	task := readyTask(t, svc, "invalid-physical-proof")
	ctx := context.Background()

	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-a", "supervisor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.LogicalFenceAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	var zero processctl.TerminationProof
	if err := svc.ConfirmAttemptProcessTermination(ctx, zero, "forged"); err == nil {
		t.Fatal("zero/forged termination proof must be rejected")
	}
	if err := svc.RecoverForReplacement(ctx, task.ID); err == nil {
		t.Fatal("replacement must remain blocked after invalid proof")
	}
}

func TestOldAttemptCannotPhysicallyMutateWorkspaceAfterReplacementAdmission(t *testing.T) {
	_, svc, _ := newHarness(t)
	task := readyTask(t, svc, "physical-writer-fence")
	ctx := context.Background()

	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-a", "supervisor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "workspace-marker.txt")
	supervisor := processctl.NewSupervisor()
	tree, err := supervisor.Start(processctl.Spec{
		Attempt: processctl.AttemptRef{TaskID: task.ID, AttemptID: attempt.ID, RunEpoch: attempt.RunEpoch},
		Path:    os.Args[0],
		Args:    []string{"-test.run=TestServiceProcessHelper"},
		Env: append(os.Environ(),
			"MAR_SERVICE_HELPER=writer",
			"MAR_SERVICE_MARKER="+marker,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.CloseUnverified()

	before := waitForMarkerChange(t, marker, "")
	if err := svc.LogicalFenceAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecoverForReplacement(ctx, task.ID); err == nil {
		t.Fatal("replacement must remain blocked while old writer is physically active")
	}

	termCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	proof, err := tree.TerminateAndConfirm(termCtx)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmAttemptProcessTermination(ctx, proof, "terminated-by-supervisor"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecoverForReplacement(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	replacement, err := svc.BeginAttempt(ctx, task.ID, "worker-b", "supervisor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.RunEpoch != attempt.RunEpoch+1 {
		t.Fatalf("expected replacement epoch %d, got %d", attempt.RunEpoch+1, replacement.RunEpoch)
	}

	atAdmission, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(atAdmission) == before {
		// The writer may have been killed immediately after the first observed write;
		// equality is fine. We only care that it cannot mutate after B is admitted.
	}
	time.Sleep(200 * time.Millisecond)
	after, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(atAdmission) {
		t.Fatalf("stale attempt physically mutated workspace after replacement admission: before=%q after=%q", string(atAdmission), string(after))
	}
}

func waitForMarkerChange(t *testing.T, path, previous string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil && string(b) != previous && len(b) > 0 {
			return string(b)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("helper did not mutate marker file: %s", path)
	return ""
}

func TestServiceProcessHelper(t *testing.T) {
	mode := os.Getenv("MAR_SERVICE_HELPER")
	if mode == "" {
		return
	}
	if mode == "writer" {
		marker := os.Getenv("MAR_SERVICE_MARKER")
		for i := 1; ; i++ {
			_ = os.WriteFile(marker, []byte(fmt.Sprintf("%d", i)), 0o600)
			time.Sleep(20 * time.Millisecond)
		}
	}
	for {
		time.Sleep(time.Second)
	}
}
