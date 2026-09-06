//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/agent"
	"mar/internal/domain"
	"mar/internal/resourcegov"
	"mar/internal/scheduler"
	"mar/internal/store"
	"mar/internal/verification"
	"mar/internal/worker"
)

func TestAcceptanceT8WorkerCrashDurablyBlocksRealRuntime(t *testing.T) {
	if os.Getenv("MAR_T8_CRASH_WORKER") == "1" {
		t.Skip("crash helper process")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectRoot := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, projectRoot, "init", "-b", "main")
	runGitTest(t, projectRoot, "config", "user.name", "MAR T8")
	runGitTest(t, projectRoot, "config", "user.email", "mar-t8@local.invalid")
	runGitTest(t, projectRoot, "add", "-A")
	runGitTest(t, projectRoot, "commit", "-m", "baseline")
	base := strings.TrimSpace(runGitTest(t, projectRoot, "rev-parse", "HEAD"))

	t.Setenv("MAR_T8_CRASH_WORKER", "1")
	t.Setenv("MAR_T8_DUMMY_KEY", "unused")
	dataRoot := filepath.Join(t.TempDir(), "mar-data")
	s, err := store.Open(filepath.Join(dataRoot, "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runtime, err := NewRuntime(s, RuntimeConfig{
		DataRoot:             dataRoot,
		Executable:           os.Args[0],
		WorkerArguments:      []string{"-test.run=^TestAcceptanceT8CrashWorkerHelper$"},
		Provider:             worker.ProviderConfig{BaseURL: "http://127.0.0.1:1/v1", APIKeyEnv: "MAR_T8_DUMMY_KEY", RequestTimeout: time.Second},
		AgentProfile:         agent.Profile{Model: "unused-model", BaseInstructions: "unused because the crash helper exits before the agent loop"},
		VerificationProfiles: []verification.Profile{{ID: "t8-noop", Commands: []verification.Command{{Name: "go", Args: []string{"test", "./..."}, Cwd: "."}}}},
		LeaseDuration:        10 * time.Second,
		WorkerStopTimeout:    5 * time.Second,
		ResourceGovernor:     resourcegov.Config{MaxCPUPercent: 100, MaxMemoryLoadPercent: 100, MaxIOPressurePercent: 100, MinFreeRAMBytes: 1, MinFreeDiskBytes: 1, MaxMARDiskBytes: 1 << 30, MaxHeavyJobs: 1, MaxHeavyJobsPerProject: 1, MaxHeavyJobsInteractive: 1},
		Scheduler:            scheduler.Config{AgingInterval: time.Minute, WorkspaceRAMReservation: 1, WorkspaceDiskReservation: 1},
		Daemon:               DaemonConfig{PollInterval: 10 * time.Millisecond, ControlPollInterval: 10 * time.Millisecond, ResourcePollInterval: 50 * time.Millisecond, MaxConcurrentWorkers: 1, MaxPreflightPerTick: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.Service.RegisterProject(ctx, "t8-project", projectRoot); err != nil {
		t.Fatal(err)
	}
	task, _, err := runtime.Service.Submit(ctx, "t8-submit", domain.GoalContract{Goal: "prove a crashing worker cannot fabricate completion", Acceptance: []string{"worker crash leaves coherent durable state"}, Boundaries: []string{"do not change authoritative project state"}, NonGoals: []string{"no deployment"}, ProjectID: "t8-project", BaseRevision: base, Authority: domain.Authority{LocalFileWrite: true, LocalGitWrite: true}, VerificationProfile: "t8-noop", Priority: "P2"})
	if err != nil {
		t.Fatal(err)
	}

	daemonCtx, stopDaemon := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- runtime.Daemon.Run(daemonCtx) }()
	defer func() {
		stopDaemon()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("daemon shutdown: %v", err)
		}
	}()

	var final domain.Task
	for {
		final, err = runtime.Service.Status(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if final.State == domain.TaskBlocked || final.State == domain.TaskComplete || final.State == domain.TaskFailed || final.State == domain.TaskCancelled {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for crashed worker reconciliation; state=%s", final.State)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if final.State != domain.TaskBlocked {
		t.Fatalf("worker crash reached unsafe terminal state: %s", final.State)
	}
	attempt, ok, err := s.CurrentAttemptByTask(ctx, task.ID)
	if err != nil || !ok {
		t.Fatalf("worker crash lost durable attempt: ok=%v err=%v", ok, err)
	}
	if attempt.AuthorityState != domain.AttemptPhysicallyTerminated {
		t.Fatalf("worker crash did not persist physical termination: %+v", attempt)
	}
	if result, available, err := runtime.Service.Result(ctx, task.ID); err != nil || available {
		t.Fatalf("worker crash fabricated a verified result: available=%v result=%+v err=%v", available, result, err)
	}
}

func TestAcceptanceT8CrashWorkerHelper(t *testing.T) {
	if os.Getenv("MAR_T8_CRASH_WORKER") != "1" {
		return
	}
	os.Exit(23)
}
