//go:build windows

package orchestrator

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/processctl"
	"mar/internal/service"
	"mar/internal/store"
	"mar/internal/testsupport"
)

type acceptanceRestartRecoveryRunner struct {
	supervisor *processctl.Supervisor
}

func (r *acceptanceRestartRecoveryRunner) RunWorkspaceReady(context.Context, string, domain.Workspace) (RunOutcome, error) {
	return RunOutcome{}, nil
}

func (r *acceptanceRestartRecoveryRunner) RecoverAttemptTermination(ctx context.Context, attempt domain.ExecutionAttempt) (processctl.TerminationProof, bool, error) {
	return r.supervisor.RecoverTermination(ctx, processctl.AttemptRef{TaskID: attempt.TaskID, AttemptID: attempt.ID, RunEpoch: attempt.RunEpoch})
}

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

func TestAcceptanceT9ArmedNamedJobRecoversPhysicalProofBeforeReplacement(t *testing.T) {
	testsupport.RequireOutsideAppContainer(t)
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "mar.db")
	recoveryRoot := filepath.Join(root, "attempt-recovery")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewTaskService(s)
	if _, _, err := svc.RegisterProject(ctx, "t9-kernel-project", t.TempDir()); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "recover kernel-backed attempt fencing after daemon handle loss",
		Acceptance:          []string{"physical proof precedes replacement admission"},
		ProjectID:           "t9-kernel-project",
		BaseRevision:        "base-t9-kernel",
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
		VerificationProfile: "t9-profile",
		Priority:            "P2",
	}
	task, _, err := svc.Submit(ctx, "t9-kernel-submit", contract)
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			_ = s.Close()
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	workspace := domain.Workspace{
		ID:           "ws-t9-kernel",
		TaskID:       task.ID,
		ProjectID:    contract.ProjectID,
		Path:         t.TempDir(),
		BaseRevision: contract.BaseRevision,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if _, _, err := s.BeginWorkspace(ctx, workspace); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	if err := s.MarkWorkspaceReady(ctx, workspace.ID, task.ID, contract.BaseRevision, now); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "t9-kernel-worker", "t9-kernel-daemon", time.Minute)
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	cmd, err := exec.LookPath("cmd.exe")
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	originalSupervisor := processctl.NewSupervisorWithRecoveryRoot(recoveryRoot)
	tree, err := originalSupervisor.Start(processctl.Spec{
		Attempt: processctl.AttemptRef{TaskID: task.ID, AttemptID: attempt.ID, RunEpoch: attempt.RunEpoch},
		Path:    cmd,
		Args:    []string{"/c", "ping -n 30 127.0.0.1 > nul"},
	})
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	// Simulate loss of the daemon's in-memory tree handle. CloseUnverified is
	// deliberately not a physical proof; the durable ASSIGNED marker and named
	// kernel Job Object are the only recovery evidence available after reopen.
	if err := tree.CloseUnverified(); err != nil {
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
	runner := &acceptanceRestartRecoveryRunner{supervisor: processctl.NewSupervisorWithRecoveryRoot(recoveryRoot)}
	daemon, err := NewDaemon(reopened, restartedSvc, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{})
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
		t.Fatalf("restart lost attempt: ok=%v err=%v", ok, err)
	}
	if gotTask.State != domain.TaskBlocked || gotAttempt.AuthorityState != domain.AttemptPhysicallyTerminated || gotAttempt.TerminalStatus != "recovered-after-daemon-restart" {
		t.Fatalf("restart did not confirm kernel-backed physical proof conservatively: task=%s attempt=%+v", gotTask.State, gotAttempt)
	}
	if result, available, err := restartedSvc.Result(ctx, task.ID); err != nil || available {
		t.Fatalf("restart fabricated result before replacement: available=%v result=%+v err=%v", available, result, err)
	}
	if err := restartedSvc.RecoverForReplacement(ctx, task.ID); err != nil {
		t.Fatalf("physically proven attempt did not permit bounded replacement admission: %v", err)
	}
	gotTask, err = restartedSvc.Status(ctx, task.ID)
	if err != nil || gotTask.State != domain.TaskWorkspaceReady {
		t.Fatalf("replacement admission after proof mismatch: task=%+v err=%v", gotTask, err)
	}
}

func TestAcceptanceT9StaleVerifyingLeaseFailsClosed(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mar.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	svc := service.NewTaskService(s)
	projectRoot := t.TempDir()
	if _, _, err := svc.RegisterProject(ctx, "t9-stale-verifying", projectRoot); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "stale VERIFYING lease must fail closed",
		Acceptance:          []string{"stale verification cannot hang indefinitely"},
		ProjectID:           "t9-stale-verifying",
		BaseRevision:        "base-stale-verifying",
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
		VerificationProfile: "t9-profile",
		Priority:            "P0",
	}
	task, _, err := svc.Submit(ctx, "t9-stale-verifying-submit", contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	workspace := domain.Workspace{
		ID:           "ws-t9-stale-verifying",
		TaskID:       task.ID,
		ProjectID:    contract.ProjectID,
		Path:         t.TempDir(),
		BaseRevision: contract.BaseRevision,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if _, _, err := s.BeginWorkspace(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWorkspaceReady(ctx, workspace.ID, task.ID, contract.BaseRevision, now); err != nil {
		t.Fatal(err)
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "t9-worker", "t9-daemon", 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.TransitionForAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, domain.TaskVerifying); err != nil {
		t.Fatal(err)
	}
	// Verification progress renews the attempt beyond the original worker lease.
	if err := svc.HeartbeatAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)

	runner := &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	daemon, err := NewDaemon(s, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, runner, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{})
	if err != nil {
		t.Fatal(err)
	}
	activeCtx, cancel := context.WithCancel(context.Background())
	daemon.active[task.ID] = &activeExecution{cancel: cancel}
	defer daemon.removeActive(task.ID)

	if err := daemon.reconcileUnprovenAttempts(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-activeCtx.Done():
		t.Fatal("healthy VERIFYING heartbeat lease was fenced before expiry")
	default:
	}
	healthyTask, err := svc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if healthyTask.State != domain.TaskVerifying {
		t.Fatalf("healthy verification state = %s, want VERIFYING", healthyTask.State)
	}

	// Once the renewed verification lease itself expires, recovery must fail closed.
	time.Sleep(140 * time.Millisecond)
	if err := daemon.reconcileUnprovenAttempts(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-activeCtx.Done():
	default:
		t.Fatal("stale VERIFYING lease did not cancel active execution")
	}
	gotTask, err := svc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotAttempt, ok, err := s.CurrentAttemptByTask(ctx, task.ID)
	if err != nil || !ok {
		t.Fatalf("stale verifying attempt missing: ok=%v err=%v", ok, err)
	}
	if gotTask.State != domain.TaskBlocked {
		t.Fatalf("stale VERIFYING task state = %s, want BLOCKED", gotTask.State)
	}
	if gotAttempt.AuthorityState != domain.AttemptLogicallyFenced {
		t.Fatalf("stale VERIFYING attempt authority = %s, want logically fenced before any PHYSICALLY_TERMINATED claim", gotAttempt.AuthorityState)
	}
	if result, available, err := svc.Result(ctx, task.ID); err != nil || available {
		t.Fatalf("stale verification recovery fabricated result: available=%v result=%+v err=%v", available, result, err)
	}
}

