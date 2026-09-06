//go:build windows

package orchestrator

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mar/internal/agent"
	"mar/internal/domain"
	"mar/internal/resourcegov"
	"mar/internal/scheduler"
	"mar/internal/service"
	"mar/internal/store"
	"mar/internal/verification"
	"mar/internal/worker"
)

func TestAcceptanceT9ActualDaemonCrashReconcilesWithoutFalseCompletion(t *testing.T) {
	if os.Getenv("MAR_T9_DAEMON_HELPER") == "1" {
		t.Skip("daemon helper process")
	}
	root := t.TempDir()
	projectRoot := filepath.Join(root, "project")
	dataRoot := filepath.Join(root, "mar-data")
	dbPath := filepath.Join(dataRoot, "mar.db")
	taskFile := filepath.Join(root, "task.id")
	workerMarker := filepath.Join(root, "worker.marker")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, projectRoot, "init", "-b", "main")
	runGitTest(t, projectRoot, "config", "user.name", "MAR T9")
	runGitTest(t, projectRoot, "config", "user.email", "mar-t9@local.invalid")
	runGitTest(t, projectRoot, "add", "-A")
	runGitTest(t, projectRoot, "commit", "-m", "baseline")

	var output bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestAcceptanceT9DaemonHostHelper$")
	cmd.Env = append(os.Environ(),
		"MAR_T9_DAEMON_HELPER=1",
		"MAR_T9_PROJECT_ROOT="+projectRoot,
		"MAR_T9_DATA_ROOT="+dataRoot,
		"MAR_T9_DB="+dbPath,
		"MAR_T9_TASK_FILE="+taskFile,
		"MAR_T9_WORKER_MARKER="+workerMarker,
	)
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	taskID := waitT9File(t, taskFile, 15*time.Second, &output)
	firstMarker := waitT9MarkerChange(t, workerMarker, "", 15*time.Second, &output)
	_ = waitT9MarkerChange(t, workerMarker, firstMarker, 5*time.Second, &output)

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill daemon helper: %v\n%s", err, output.String())
	}
	_ = cmd.Wait()
	stable := waitT9MarkerStable(t, workerMarker, 3*time.Second)
	time.Sleep(250 * time.Millisecond)
	after, err := os.ReadFile(workerMarker)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != stable {
		t.Fatalf("worker kept mutating after daemon crash: stable=%q after=%q", stable, string(after))
	}

	reopened, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	svc := service.NewTaskService(reopened)
	daemon, err := NewDaemon(reopened, svc, fakePreflightDriver{}, &fakeSchedulerDriver{}, &fakeReadyRunner{started: make(chan struct{}), stopped: make(chan struct{})}, &fakeIntegrationRecoverer{}, healthyDaemonGovernor(t), DaemonConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.reconcileUnprovenAttempts(context.Background()); err != nil {
		t.Fatal(err)
	}
	gotTask, err := svc.Status(context.Background(), strings.TrimSpace(taskID))
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := reopened.CurrentAttemptByTask(context.Background(), gotTask.ID)
	if err != nil || !ok {
		t.Fatalf("restart lost crashed daemon attempt: ok=%v err=%v", ok, err)
	}
	if gotTask.State != domain.TaskBlocked || attempt.AuthorityState != domain.AttemptLogicallyFenced {
		t.Fatalf("daemon crash did not reconcile fail-closed: task=%s attempt=%+v", gotTask.State, attempt)
	}
	if result, available, err := svc.Result(context.Background(), gotTask.ID); err != nil || available {
		t.Fatalf("daemon crash fabricated completion: available=%v result=%+v err=%v", available, result, err)
	}
}

func TestAcceptanceT9DaemonHostHelper(t *testing.T) {
	if os.Getenv("MAR_T9_DAEMON_HELPER") != "1" {
		return
	}
	ctx := context.Background()
	projectRoot := os.Getenv("MAR_T9_PROJECT_ROOT")
	dataRoot := os.Getenv("MAR_T9_DATA_ROOT")
	dbPath := os.Getenv("MAR_T9_DB")
	base := strings.TrimSpace(runGitTest(t, projectRoot, "rev-parse", "HEAD"))
	if err := os.Setenv("MAR_T9_DUMMY_KEY", "unused"); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runtime, err := NewRuntime(s, RuntimeConfig{
		DataRoot:             dataRoot,
		Executable:           os.Args[0],
		WorkerArguments:      []string{"-test.run=^TestAcceptanceT9WorkerSleepHelper$"},
		Provider:             worker.ProviderConfig{BaseURL: "http://127.0.0.1:1/v1", APIKeyEnv: "MAR_T9_DUMMY_KEY", RequestTimeout: time.Second},
		AgentProfile:         agent.Profile{Model: "unused-model", BaseInstructions: "unused because the worker helper only proves daemon crash containment"},
		VerificationProfiles: []verification.Profile{{ID: "t9-noop", Commands: []verification.Command{{Name: "go", Args: []string{"test", "./..."}, Cwd: "."}}}},
		LeaseDuration:        10 * time.Second,
		WorkerStopTimeout:    5 * time.Second,
		ResourceGovernor:     resourcegov.Config{MaxCPUPercent: 100, MaxMemoryLoadPercent: 100, MaxIOPressurePercent: 100, MinFreeRAMBytes: 1, MinFreeDiskBytes: 1, MaxMARDiskBytes: 1 << 30, MaxHeavyJobs: 1, MaxHeavyJobsPerProject: 1, MaxHeavyJobsInteractive: 1},
		Scheduler:            scheduler.Config{AgingInterval: time.Minute, WorkspaceRAMReservation: 1, WorkspaceDiskReservation: 1},
		Daemon:               DaemonConfig{PollInterval: 10 * time.Millisecond, ControlPollInterval: 10 * time.Millisecond, ResourcePollInterval: 50 * time.Millisecond, MaxConcurrentWorkers: 1, MaxPreflightPerTick: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.Service.RegisterProject(ctx, "t9-real-project", projectRoot); err != nil {
		t.Fatal(err)
	}
	task, _, err := runtime.Service.Submit(ctx, "t9-real-submit", domain.GoalContract{Goal: "remain coherent if the MAR daemon crashes during active work", Acceptance: []string{"restart has no false completion"}, Boundaries: []string{"do not mutate authoritative project state"}, NonGoals: []string{"no deployment"}, ProjectID: "t9-real-project", BaseRevision: base, Authority: domain.Authority{LocalFileWrite: true, LocalGitWrite: true}, VerificationProfile: "t9-noop", Priority: "P2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("MAR_T9_TASK_FILE"), []byte(task.ID), 0o600); err != nil {
		t.Fatal(err)
	}
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- runtime.Daemon.Run(ctx) }()
	select {}
}

func TestAcceptanceT9WorkerSleepHelper(t *testing.T) {
	if os.Getenv("MAR_T9_DAEMON_HELPER") != "1" {
		return
	}
	marker := os.Getenv("MAR_T9_WORKER_MARKER")
	for i := 1; ; i++ {
		if err := os.WriteFile(marker, []byte(strconv.Itoa(i)), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitT9File(t *testing.T, path string, timeout time.Duration, output *bytes.Buffer) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if value, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(value)) != "" {
			return string(value)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; helper output=%s", path, output.String())
	return ""
}

func waitT9MarkerChange(t *testing.T, path, previous string, timeout time.Duration, output *bytes.Buffer) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if value, err := os.ReadFile(path); err == nil && string(value) != previous && len(value) > 0 {
			return string(value)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("worker marker did not change from %q; helper output=%s", previous, output.String())
	return ""
}

func waitT9MarkerStable(t *testing.T, path string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var previous string
	for time.Now().Before(deadline) {
		value, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		current := string(value)
		if current == previous && current != "" {
			time.Sleep(150 * time.Millisecond)
			confirm, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(confirm) == current {
				return current
			}
		}
		previous = current
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("worker marker never stabilized after daemon crash: %q", previous)
	return ""
}
