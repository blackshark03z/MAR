package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/service"
	"mar/internal/store"
)

func activeArtifactHarness(t *testing.T) (*store.SQLite, *service.TaskService, string, string, int64) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := service.NewTaskService(db)
	if _, _, err := svc.RegisterProject(context.Background(), "artifact-project", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	c := contract("persist observation evidence")
	c.ProjectID = "artifact-project"
	task, _, err := svc.Submit(context.Background(), "artifact-task", c)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AdvancePreExecution(context.Background(), task.ID, "PREFLIGHT"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AdvancePreExecution(context.Background(), task.ID, "WAITING_RESOURCE"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AdvancePreExecution(context.Background(), task.ID, "WORKSPACE_READY"); err != nil {
		t.Fatal(err)
	}
	attempt, err := svc.BeginAttempt(context.Background(), task.ID, "artifact-worker", "artifact-supervisor", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return db, svc, task.ID, attempt.ID, attempt.RunEpoch
}

func TestObservationArtifactPersistsFilesystemContentAndIndexedIntegrityMetadata(t *testing.T) {
	_, svc, taskID, attemptID, epoch := activeArtifactHarness(t)
	raw := strings.Repeat("durable-evidence-", 200)
	a, err := svc.PersistObservation(context.Background(), taskID, attemptID, epoch, "call-large", "tool_observation", raw, int64(len(raw)), true)
	if err != nil {
		t.Fatal(err)
	}
	if a.Handle == "" || a.SHA256 == "" || a.CapturedBytes != int64(len(raw)) || a.SourceBytes != int64(len(raw)) || !a.Complete || a.Truncated {
		t.Fatalf("unexpected artifact metadata: %+v", a)
	}
	chunk, err := svc.ReadObservationArtifact(context.Background(), taskID, attemptID, epoch, a.Handle, 0, 16<<10)
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Data != raw || !chunk.EOF || chunk.Artifact.SHA256 != a.SHA256 {
		t.Fatalf("artifact round trip mismatch: %+v", chunk)
	}
}

func TestObservationArtifactRejectsStaleAttemptAndBoundsHandleReads(t *testing.T) {
	_, svc, taskID, attemptID, epoch := activeArtifactHarness(t)
	a, err := svc.PersistObservation(context.Background(), taskID, attemptID, epoch, "call-bounded", "tool_observation", strings.Repeat("x", 100), 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadObservationArtifact(context.Background(), taskID, attemptID, epoch+1, a.Handle, 0, 16); !errors.Is(err, store.ErrStaleAttempt) {
		t.Fatalf("wrong epoch read did not fail closed: %v", err)
	}
	if _, err := svc.ReadObservationArtifact(context.Background(), taskID, attemptID, epoch, a.Handle, 0, 16<<10+1); err == nil {
		t.Fatal("oversized artifact read was accepted")
	}
	chunk, err := svc.ReadObservationArtifact(context.Background(), taskID, attemptID, epoch, a.Handle, 10, 16)
	if err != nil || len(chunk.Data) != 16 || chunk.NextOffset != 26 {
		t.Fatalf("bounded artifact read mismatch: chunk=%+v err=%v", chunk, err)
	}
	if err := svc.LogicalFenceAttempt(context.Background(), taskID, attemptID, epoch); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PersistObservation(context.Background(), taskID, attemptID, epoch, "call-stale", "tool_observation", "stale", 5, true); !errors.Is(err, store.ErrStaleAttempt) {
		t.Fatalf("stale artifact write did not fail closed: %v", err)
	}
	// Exact-identity historical reads remain available after fencing.
	if _, err := svc.ReadObservationArtifact(context.Background(), taskID, attemptID, epoch, a.Handle, 0, 16); err != nil {
		t.Fatalf("historical exact-identity read failed: %v", err)
	}
}
