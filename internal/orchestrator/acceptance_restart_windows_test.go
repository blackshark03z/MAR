//go:build windows

package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/service"
	"mar/internal/store"
)

func TestAcceptanceT9DaemonRestartReconcilesRealSQLiteAttemptWithoutFalseCompletion(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mar.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewTaskService(s)
	if _, _, err := svc.RegisterProject(ctx, "t9-project", t.TempDir()); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "survive daemon restart without false completion",
		Acceptance:          []string{"restart reconciles active attempt safely"},
		ProjectID:           "t9-project",
		BaseRevision:        "base-t9",
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
		VerificationProfile: "t9-profile",
		Priority:            "P2",
	}
	task, _, err := svc.Submit(ctx, "t9-submit", contract)
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			_ = s.Close()
			t.Fatal(err)
		}
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "t9-worker", "t9-daemon", time.Minute)
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restartedSvc := service.NewTaskService(reopened)
	daemon, err := NewDaemon(reopened, restartedSvc, fakePreflightDriver{}, &fakeSchedulerDriver{}, &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.reconcileUnprovenAttempts(ctx); err != nil {
		t.Fatal(err)
	}
	gotTask, err := restartedSvc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotAttempt, ok, err := reopened.CurrentAttemptByTask(ctx, task.ID)
	if err != nil || !ok {
		t.Fatalf("restart lost active attempt: ok=%v err=%v", ok, err)
	}
	if gotTask.State != domain.TaskBlocked || gotAttempt.ID != attempt.ID || gotAttempt.AuthorityState != domain.AttemptLogicallyFenced {
		t.Fatalf("restart did not fail closed: task=%s attempt=%+v", gotTask.State, gotAttempt)
	}
	if result, available, err := restartedSvc.Result(ctx, task.ID); err != nil || available {
		t.Fatalf("daemon restart fabricated completion evidence: available=%v result=%+v err=%v", available, result, err)
	}
}
