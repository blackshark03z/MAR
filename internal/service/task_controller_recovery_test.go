package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/store"
)

func TestRecoverBlockedChoiceBeforeWorkspaceReturnsToPreflight(t *testing.T) {
	db, svc, _ := newHarness(t)
	ctx := context.Background()
	task, _, err := svc.Submit(ctx, "controller-preworkspace", contract("pre-workspace recovery"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AdvancePreExecution(ctx, task.ID, domain.TaskPreflight); err != nil {
		t.Fatal(err)
	}
	if err := db.BlockTask(ctx, task.ID, domain.TaskPreflight, domain.TaskBlocker{Phase: domain.BlockerPhasePreflight, Code: "PREFLIGHT_FAILED", Detail: "fixture", Recovery: "retry preflight"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecoverBlockedChoice(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.TaskPreflight || got.RunEpoch != 0 {
		t.Fatalf("expected PREFLIGHT epoch0, got state=%s epoch=%d", got.State, got.RunEpoch)
	}
	if _, err := db.GetWorkspaceByTask(ctx, task.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unexpected workspace after pre-workspace recovery: %v", err)
	}
	if _, ok, err := db.CurrentTaskBlocker(ctx, task.ID); err != nil || ok {
		t.Fatalf("blocker not cleared: ok=%v err=%v", ok, err)
	}
}

func TestReconcileWorkspaceReadyWithoutWorkspaceReturnsToPreflight(t *testing.T) {
	_, svc, _ := newHarness(t)
	ctx := context.Background()
	task, _, err := svc.Submit(ctx, "controller-missing-workspace", contract("workspace invariant"))
	if err != nil {
		t.Fatal(err)
	}
	for _, next := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(ctx, task.ID, next); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.ReconcileWorkspaceReady(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.TaskPreflight || got.RunEpoch != 0 {
		t.Fatalf("expected repaired PREFLIGHT epoch0, got state=%s epoch=%d", got.State, got.RunEpoch)
	}
}

func TestStatusSnapshotSurfacesTypedBlocker(t *testing.T) {
	db, svc, _ := newHarness(t)
	ctx := context.Background()
	task, _, err := svc.Submit(ctx, "controller-blocker-status", contract("typed blocker status"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AdvancePreExecution(ctx, task.ID, domain.TaskPreflight); err != nil {
		t.Fatal(err)
	}
	if err := db.BlockTask(ctx, task.ID, domain.TaskPreflight, domain.TaskBlocker{Phase: domain.BlockerPhasePreflight, Code: "PREFLIGHT_FAILED", Detail: "fixture detail", Recovery: "retry preflight"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.StatusSnapshot(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.Detail, "PREFLIGHT/PREFLIGHT_FAILED") || snapshot.NextAction != "retry preflight" {
		t.Fatalf("typed blocker not surfaced: detail=%q next=%q", snapshot.Detail, snapshot.NextAction)
	}
}
