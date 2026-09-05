package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"mar/internal/domain"
	"mar/internal/service"
	"mar/internal/store"
)

func newHarness(t *testing.T) (*store.SQLite, *service.TaskService, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "mar.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	svc := service.NewTaskService(s)
	root := filepath.Join(t.TempDir(), "project")
	if _, _, err := svc.RegisterProject(context.Background(), "p1", root); err != nil {
		t.Fatal(err)
	}
	return s, svc, dbPath
}

func contract(goal string) domain.GoalContract {
	return domain.GoalContract{
		Goal:                goal,
		Acceptance:          []string{"task is durable", "submit is idempotent"},
		Boundaries:          []string{"no worker execution yet"},
		NonGoals:            []string{"MCP transport"},
		ProjectID:           "p1",
		BaseRevision:        "abc123",
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
		VerificationProfile: "kernel-unit",
		Priority:            "P2",
	}
}

func TestSubmitIsIdempotent(t *testing.T) {
	_, svc, _ := newHarness(t)
	ctx := context.Background()

	first, created, err := svc.Submit(ctx, "request-1", contract("durable submit"))
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first submit should create task")
	}

	second, created, err := svc.Submit(ctx, "request-1", contract("durable submit"))
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("idempotent retry must not create another task")
	}
	if first.ID != second.ID {
		t.Fatalf("expected same task id, got %s and %s", first.ID, second.ID)
	}
}

func TestIdempotencyKeyRejectsDifferentContract(t *testing.T) {
	_, svc, _ := newHarness(t)
	ctx := context.Background()

	if _, _, err := svc.Submit(ctx, "request-1", contract("goal A")); err != nil {
		t.Fatal(err)
	}
	_, _, err := svc.Submit(ctx, "request-1", contract("goal B"))
	if !errors.Is(err, store.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

func TestTaskPersistsAcrossStoreReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mar.db")
	ctx := context.Background()

	firstStore, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	firstSvc := service.NewTaskService(firstStore)
	if _, _, err := firstSvc.RegisterProject(ctx, "p1", filepath.Join(t.TempDir(), "project")); err != nil {
		t.Fatal(err)
	}
	task, _, err := firstSvc.Submit(ctx, "request-1", contract("persist me"))
	if err != nil {
		t.Fatal(err)
	}
	if err := firstStore.Close(); err != nil {
		t.Fatal(err)
	}

	secondStore, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	secondSvc := service.NewTaskService(secondStore)
	got, err := secondSvc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != task.ID || got.ContractHash != task.ContractHash || got.State != domain.TaskSubmitted {
		t.Fatalf("persisted task mismatch: %#v", got)
	}
}

func TestConcurrentDuplicateSubmitCreatesOneTask(t *testing.T) {
	_, svc, _ := newHarness(t)
	ctx := context.Background()

	const n = 12
	var wg sync.WaitGroup
	wg.Add(n)
	ids := make(chan string, n)
	created := make(chan bool, n)
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			task, wasCreated, err := svc.Submit(ctx, "concurrent-request", contract("concurrent durable submit"))
			if err != nil {
				errs <- err
				return
			}
			ids <- task.ID
			created <- wasCreated
		}()
	}
	wg.Wait()
	close(ids)
	close(created)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	var expected string
	for id := range ids {
		if expected == "" {
			expected = id
		}
		if id != expected {
			t.Fatalf("duplicate submit produced multiple task ids: %s != %s", id, expected)
		}
	}
	createdCount := 0
	for v := range created {
		if v {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("expected exactly one creator, got %d", createdCount)
	}
}

func TestGoalValidationRejectsMissingAcceptance(t *testing.T) {
	_, svc, _ := newHarness(t)
	g := contract("invalid")
	g.Acceptance = nil
	_, _, err := svc.Submit(context.Background(), "invalid-request", g)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSubmitRejectsUnknownProject(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mar.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	svc := service.NewTaskService(s)

	g := contract("unknown project")
	g.ProjectID = "missing"
	_, _, err = svc.Submit(context.Background(), "missing-project", g)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected unknown project error wrapping ErrNotFound, got %v", err)
	}
}
