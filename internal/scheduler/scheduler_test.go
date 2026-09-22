package scheduler

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/resourcegov"
	"mar/internal/service"
	"mar/internal/store"
	"mar/internal/testsupport"
	"mar/internal/workspace"
)

type staticSensor struct{ snapshot resourcegov.Snapshot }

func (s *staticSensor) Snapshot(context.Context) (resourcegov.Snapshot, error) {
	return s.snapshot, nil
}

type fakeWorkspace struct {
	store              *store.SQLite
	mu                 sync.Mutex
	calls              []string
	readOnlyCalls      []string
	err                error
	terminalCalls      int
	terminalWorkspaces int
	terminalReclaimErr error
	cacheCalls         int
	cacheBytes         int64
	cacheReclaimHook   func()
	cacheReclaimErr    error
	checkpointCalls    int
	checkpointCount    int
	checkpointBytes    int64
	checkpointHook     func()
	checkpointErr      error
}

func (f *fakeWorkspace) EnsureMutable(ctx context.Context, taskID string) (domain.Workspace, error) {
	f.mu.Lock()
	f.calls = append(f.calls, taskID)
	f.mu.Unlock()
	if f.err != nil {
		return domain.Workspace{}, f.err
	}
	if err := f.store.OrchestratorTransition(ctx, taskID, domain.TaskWaitingResource, domain.TaskWorkspaceReady, time.Now().UTC()); err != nil {
		return domain.Workspace{}, err
	}
	task, err := f.store.GetTask(ctx, taskID)
	if err != nil {
		return domain.Workspace{}, err
	}
	return domain.Workspace{ID: "fake-" + taskID, TaskID: taskID, ProjectID: task.Contract.ProjectID, State: domain.WorkspaceReady}, nil
}

func (f *fakeWorkspace) EnsureReadOnly(ctx context.Context, taskID string) (domain.Workspace, error) {
	f.mu.Lock()
	f.calls = append(f.calls, taskID)
	f.readOnlyCalls = append(f.readOnlyCalls, taskID)
	f.mu.Unlock()
	if f.err != nil {
		return domain.Workspace{}, f.err
	}
	if err := f.store.OrchestratorTransition(ctx, taskID, domain.TaskWaitingResource, domain.TaskWorkspaceReady, time.Now().UTC()); err != nil {
		return domain.Workspace{}, err
	}
	task, err := f.store.GetTask(ctx, taskID)
	if err != nil {
		return domain.Workspace{}, err
	}
	return domain.Workspace{ID: "fake-readonly-" + taskID, TaskID: taskID, ProjectID: task.Contract.ProjectID, State: domain.WorkspaceReady}, nil
}

func (f *fakeWorkspace) ReclaimTerminal(context.Context, int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminalCalls++
	return f.terminalWorkspaces, f.terminalReclaimErr
}

func (f *fakeWorkspace) CheckpointBlockedWorkspaces(context.Context, int) (int, int64, error) {
	f.mu.Lock()
	f.checkpointCalls++
	hook := f.checkpointHook
	count, bytes, err := f.checkpointCount, f.checkpointBytes, f.checkpointErr
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return count, bytes, err
}

func (f *fakeWorkspace) PruneRebuildableCaches(context.Context) (int64, error) {
	f.mu.Lock()
	f.cacheCalls++
	hook := f.cacheReclaimHook
	bytes, err := f.cacheBytes, f.cacheReclaimErr
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return bytes, err
}

func healthyGovernor(t *testing.T, sensor *staticSensor) *resourcegov.Governor {
	t.Helper()
	g, err := resourcegov.New(sensor, resourcegov.Config{
		MaxCPUPercent:           80,
		MaxMemoryLoadPercent:    80,
		MaxIOPressurePercent:    90,
		MinFreeRAMBytes:         100,
		MinFreeDiskBytes:        1000,
		MaxMARDiskBytes:         100_000,
		MaxHeavyJobs:            2,
		MaxHeavyJobsPerProject:  1,
		MaxHeavyJobsInteractive: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func healthyHost() resourcegov.Snapshot {
	return resourcegov.Snapshot{
		CPUKnown:          true,
		CPUPercent:        10,
		MemoryLoadPercent: 20,
		TotalRAMBytes:     100_000,
		AvailableRAMBytes: 80_000,
		FreeDiskBytes:     1_000_000,
		TotalDiskBytes:    2_000_000,
		MARDiskUsedBytes:  1000,
	}
}

func schedulerHarness(t *testing.T, snapshot resourcegov.Snapshot) (*store.SQLite, *service.TaskService, *Scheduler, *fakeWorkspace) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	sensor := &staticSensor{snapshot: snapshot}
	workspace := &fakeWorkspace{store: s}
	sch, err := New(s, healthyGovernor(t, sensor), workspace, Config{
		AgingInterval:            time.Hour,
		WorkspaceRAMReservation:  10,
		WorkspaceDiskReservation: 100,
	})
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	return s, service.NewTaskService(s), sch, workspace
}

func queueTask(t *testing.T, svc *service.TaskService, projectID, key, priority string) domain.Task {
	t.Helper()
	return queueTaskWithAuthority(t, svc, projectID, key, priority, domain.Authority{})
}

func queueTaskWithAuthority(t *testing.T, svc *service.TaskService, projectID, key, priority string, authority domain.Authority) domain.Task {
	t.Helper()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), projectID)
	if _, _, err := svc.RegisterProject(ctx, projectID, root); err != nil {
		if !errors.Is(err, store.ErrProjectConflict) {
			t.Fatal(err)
		}
	}
	contract := domain.GoalContract{
		Goal:                "scheduler test",
		Acceptance:          []string{"scheduled"},
		ProjectID:           projectID,
		BaseRevision:        "abc",
		VerificationProfile: "test",
		Priority:            priority,
		Authority:           authority,
	}
	task, _, err := svc.Submit(ctx, key, contract)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AdvancePreExecution(ctx, task.ID, domain.TaskPreflight); err != nil {
		t.Fatal(err)
	}
	if err := svc.AdvancePreExecution(ctx, task.ID, domain.TaskWaitingResource); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSchedulerRoutesWorkspaceByMutationAuthority(t *testing.T) {
	t.Run("strict read-only uses shared snapshot provisioner", func(t *testing.T) {
		s, svc, sch, workspace := schedulerHarness(t, healthyHost())
		defer s.Close()
		task := queueTaskWithAuthority(t, svc, "route-readonly", "route-readonly", "P2", domain.Authority{})

		result, err := sch.Step(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if result.TaskID != task.ID || result.Action != ActionWorkspaceReady {
			t.Fatalf("unexpected scheduler result: %+v", result)
		}

		workspace.mu.Lock()
		defer workspace.mu.Unlock()
		if len(workspace.readOnlyCalls) != 1 || workspace.readOnlyCalls[0] != task.ID {
			t.Fatalf("strict read-only task did not use EnsureReadOnly: %+v", workspace.readOnlyCalls)
		}
	})

	t.Run("file-write authority stays on mutable provisioner", func(t *testing.T) {
		s, svc, sch, workspace := schedulerHarness(t, healthyHost())
		defer s.Close()
		task := queueTaskWithAuthority(t, svc, "route-mutable", "route-mutable", "P2", domain.Authority{LocalFileWrite: true})

		result, err := sch.Step(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if result.TaskID != task.ID || result.Action != ActionWorkspaceReady {
			t.Fatalf("unexpected scheduler result: %+v", result)
		}

		workspace.mu.Lock()
		defer workspace.mu.Unlock()
		if len(workspace.readOnlyCalls) != 0 {
			t.Fatalf("mutation-capable task unexpectedly used EnsureReadOnly: %+v", workspace.readOnlyCalls)
		}
		if len(workspace.calls) != 1 || workspace.calls[0] != task.ID {
			t.Fatalf("mutation-capable task did not use EnsureMutable: %+v", workspace.calls)
		}
	})
}

func TestSelectTaskPreservesInputOrderWhenAllSchedulingKeysTie(t *testing.T) {
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	stamp := now.Add(-time.Minute)
	first := domain.Task{ID: "task-z-first", CreatedAt: stamp, UpdatedAt: stamp, Contract: domain.GoalContract{ProjectID: "project-a", Priority: "P2"}}
	second := domain.Task{ID: "task-a-second", CreatedAt: stamp, UpdatedAt: stamp, Contract: domain.GoalContract{ProjectID: "project-a", Priority: "P2"}}
	got := selectTask([]domain.Task{first, second}, nil, now, time.Hour)
	if got.ID != first.ID {
		t.Fatalf("exact scheduling ties must preserve stable input order, got %s want %s", got.ID, first.ID)
	}
}

func TestSchedulerPreservesSubmissionFIFOWhenTimestampsTie(t *testing.T) {
	s, svc, sch, _ := schedulerHarness(t, healthyHost())
	defer s.Close()
	ctx := context.Background()
	if _, _, err := svc.RegisterProject(ctx, "project-tie", filepath.Join(t.TempDir(), "project-tie")); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{Goal: "scheduler tie test", Acceptance: []string{"scheduled"}, ProjectID: "project-tie", BaseRevision: "abc", VerificationProfile: "test", Priority: "P2"}
	hash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 9, 20, 0, 0, 0, 123, time.UTC)
	first := domain.Task{ID: "task-z-first", IdempotencyKey: "tie-first", Contract: contract, ContractHash: hash, State: domain.TaskSubmitted, CreatedAt: stamp, UpdatedAt: stamp}
	second := domain.Task{ID: "task-a-second", IdempotencyKey: "tie-second", Contract: contract, ContractHash: hash, State: domain.TaskSubmitted, CreatedAt: stamp, UpdatedAt: stamp}
	for _, task := range []domain.Task{first, second} {
		if _, created, err := s.SubmitTask(ctx, task); err != nil || !created {
			t.Fatalf("submit tied task %s: created=%v err=%v", task.ID, created, err)
		}
		if err := s.OrchestratorTransition(ctx, task.ID, domain.TaskSubmitted, domain.TaskPreflight, stamp); err != nil {
			t.Fatal(err)
		}
		if err := s.OrchestratorTransition(ctx, task.ID, domain.TaskPreflight, domain.TaskWaitingResource, stamp); err != nil {
			t.Fatal(err)
		}
	}
	waiting, err := s.ListWaitingTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 2 || waiting[0].ID != first.ID || waiting[1].ID != second.ID {
		t.Fatalf("persistent tie order is not insertion FIFO: %+v", waiting)
	}
	got, err := sch.Step(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != first.ID {
		t.Fatalf("scheduler did not preserve persisted FIFO, got %+v want %s", got, first.ID)
	}
}

func TestSchedulerPersistsProjectFairnessAcrossInstances(t *testing.T) {
	s, svc, sch, workspace := schedulerHarness(t, healthyHost())
	defer s.Close()
	a1 := queueTask(t, svc, "project-a", "a1", "P2")
	_ = queueTask(t, svc, "project-a", "a2", "P2")
	b1 := queueTask(t, svc, "project-b", "b1", "P2")

	first, err := sch.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.TaskID != a1.ID || first.Action != ActionWorkspaceReady {
		t.Fatalf("expected oldest project-a task first, got %+v", first)
	}

	// Reconstruct the scheduler to prove fairness is not only in-memory state.
	sensor := &staticSensor{snapshot: healthyHost()}
	restarted, err := New(s, healthyGovernor(t, sensor), workspace, Config{AgingInterval: time.Hour, WorkspaceRAMReservation: 10, WorkspaceDiskReservation: 100})
	if err != nil {
		t.Fatal(err)
	}
	second, err := restarted.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.TaskID != b1.ID {
		t.Fatalf("expected never-dispatched project-b before project-a backlog, got %+v", second)
	}
}

func TestAcceptanceT6ThreeProjectsAreFairAndLazilyProvisioned(t *testing.T) {
	s, svc, sch, workspace := schedulerHarness(t, healthyHost())
	defer s.Close()
	want := map[string]bool{
		queueTask(t, svc, "t6-project-a", "t6-a", "P2").ID: true,
		queueTask(t, svc, "t6-project-b", "t6-b", "P2").ID: true,
		queueTask(t, svc, "t6-project-c", "t6-c", "P2").ID: true,
	}
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		result, err := sch.Step(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if result.Action != ActionWorkspaceReady || !want[result.TaskID] || seen[result.TaskID] {
			t.Fatalf("three-project fairness mismatch at step %d: %+v seen=%v", i+1, result, seen)
		}
		seen[result.TaskID] = true
		workspace.mu.Lock()
		calls := len(workspace.calls)
		workspace.mu.Unlock()
		if calls != i+1 {
			t.Fatalf("scheduler eagerly provisioned unrelated projects: step=%d workspace_calls=%d", i+1, calls)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("not all projects received a dispatch opportunity: %v", seen)
	}
}

func TestIdleSchedulerBackfillsBlockedWorkspaceCheckpoints(t *testing.T) {
	s, _, sch, workspace := schedulerHarness(t, healthyHost())
	defer s.Close()
	workspace.checkpointCount = idleCheckpointBatch
	workspace.checkpointBytes = 8192

	result, err := sch.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ActionIdle {
		t.Fatalf("idle maintenance changed scheduler action: %+v", result)
	}
	if result.CheckpointedBlockedWorkspaces != idleCheckpointBatch || result.CheckpointedWorkspaceBytes != 8192 {
		t.Fatalf("idle checkpoint evidence missing: %+v", result)
	}
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.checkpointCalls != 1 {
		t.Fatalf("idle checkpoint maintenance calls=%d want 1", workspace.checkpointCalls)
	}
}

func TestIdleSchedulerSkipsCheckpointBackfillWhileResourceClaimIsActive(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sensor := &staticSensor{snapshot: healthyHost()}
	workspace := &fakeWorkspace{store: s, checkpointCount: idleCheckpointBatch, checkpointBytes: 8192}
	governor := healthyGovernor(t, sensor)
	lease, decision, err := governor.TryAcquire(context.Background(), resourcegov.Claim{
		ID: "active-idle-test", ProjectID: "active-project", Class: resourcegov.WorkloadUnitTest, RAMBytes: 1,
	})
	if err != nil || !decision.Allowed {
		t.Fatalf("seed active claim: decision=%+v err=%v", decision, err)
	}
	defer lease.Release()
	sch, err := New(s, governor, workspace, Config{AgingInterval: time.Hour, WorkspaceRAMReservation: 10, WorkspaceDiskReservation: 100})
	if err != nil {
		t.Fatal(err)
	}

	result, err := sch.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ActionIdle {
		t.Fatalf("active-claim idle step changed action: %+v", result)
	}
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.checkpointCalls != 0 {
		t.Fatalf("idle checkpoint ran while a resource claim was active: %d", workspace.checkpointCalls)
	}
}

func TestResourceDenialLeavesTaskWaiting(t *testing.T) {
	host := healthyHost()
	host.FreeDiskBytes = 1050
	s, svc, sch, workspace := schedulerHarness(t, host)
	defer s.Close()
	task := queueTask(t, svc, "project-a", "denied", "P2")
	result, err := sch.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ActionWaitingResource || result.TaskID != task.ID || len(result.DenialReasons) == 0 {
		t.Fatalf("expected resource wait, got %+v", result)
	}
	got, _ := svc.Status(context.Background(), task.ID)
	if got.State != domain.TaskWaitingResource {
		t.Fatalf("resource denial mutated task state: %+v", got)
	}
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if len(workspace.calls) != 0 {
		t.Fatal("workspace provisioner called despite denied resource claim")
	}
}

func TestDiskPressureReclaimsAndRetriesAdmission(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	svc := service.NewTaskService(s)
	sensor := &staticSensor{snapshot: healthyHost()}
	sensor.snapshot.MARDiskUsedBytes = 99_950
	workspace := &fakeWorkspace{store: s, terminalWorkspaces: 2, cacheBytes: 4096}
	workspace.cacheReclaimHook = func() { sensor.snapshot.MARDiskUsedBytes = 1_000 }
	governor := healthyGovernor(t, sensor)
	sch, err := New(s, governor, workspace, Config{AgingInterval: time.Hour, WorkspaceRAMReservation: 10, WorkspaceDiskReservation: 100})
	if err != nil {
		t.Fatal(err)
	}
	task := queueTask(t, svc, "project-pressure", "pressure-retry", "P2")
	result, err := sch.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ActionWorkspaceReady || result.TaskID != task.ID {
		t.Fatalf("pressure reclaim did not recover admission: %+v", result)
	}
	if result.ReclaimedTerminalWorkspaces != 2 || result.ReclaimedCacheBytes != 4096 {
		t.Fatalf("reclaim evidence missing from result: %+v", result)
	}
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.terminalCalls != 1 || workspace.cacheCalls != 1 {
		t.Fatalf("unexpected pressure reclaim invocation: terminal=%d cache=%d", workspace.terminalCalls, workspace.cacheCalls)
	}
}

func TestDiskPressureCheckpointsBlockedWorkspaceWhenEarlierReclaimIsInsufficient(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	svc := service.NewTaskService(s)
	sensor := &staticSensor{snapshot: healthyHost()}
	sensor.snapshot.MARDiskUsedBytes = 99_950
	workspace := &fakeWorkspace{store: s, checkpointCount: 2, checkpointBytes: 8192}
	workspace.checkpointHook = func() { sensor.snapshot.MARDiskUsedBytes = 1_000 }
	governor := healthyGovernor(t, sensor)
	sch, err := New(s, governor, workspace, Config{AgingInterval: time.Hour, WorkspaceRAMReservation: 10, WorkspaceDiskReservation: 100})
	if err != nil {
		t.Fatal(err)
	}
	task := queueTask(t, svc, "project-checkpoint-pressure", "checkpoint-pressure", "P2")
	result, err := sch.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ActionWorkspaceReady || result.TaskID != task.ID {
		t.Fatalf("blocked checkpoint reclaim did not recover admission: %+v", result)
	}
	if result.CheckpointedBlockedWorkspaces != 2 || result.CheckpointedWorkspaceBytes != 8192 {
		t.Fatalf("checkpoint reclaim evidence missing: %+v", result)
	}
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.terminalCalls != 1 || workspace.cacheCalls != 1 || workspace.checkpointCalls != 1 {
		t.Fatalf("unexpected reclaim sequence: terminal=%d cache=%d checkpoint=%d", workspace.terminalCalls, workspace.cacheCalls, workspace.checkpointCalls)
	}
}

func TestDiskPressureDoesNotPruneSharedCacheWhileAnotherClaimIsActive(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	svc := service.NewTaskService(s)
	sensor := &staticSensor{snapshot: healthyHost()}
	sensor.snapshot.MARDiskUsedBytes = 99_950
	workspace := &fakeWorkspace{store: s, terminalWorkspaces: 1, cacheBytes: 4096}
	governor := healthyGovernor(t, sensor)
	activeLease, decision, err := governor.TryAcquire(context.Background(), resourcegov.Claim{ID: "active-worker", ProjectID: "other-project", Class: resourcegov.WorkloadUnitTest, RAMBytes: 1, DiskBytes: 0})
	if err != nil || !decision.Allowed {
		t.Fatalf("seed active claim: decision=%+v err=%v", decision, err)
	}
	defer activeLease.Release()
	sch, err := New(s, governor, workspace, Config{AgingInterval: time.Hour, WorkspaceRAMReservation: 10, WorkspaceDiskReservation: 100})
	if err != nil {
		t.Fatal(err)
	}
	_ = queueTask(t, svc, "project-pressure-active", "pressure-active", "P2")
	result, err := sch.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ActionWaitingResource {
		t.Fatalf("terminal-only reclaim unexpectedly bypassed disk pressure: %+v", result)
	}
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.terminalCalls != 1 || workspace.cacheCalls != 0 {
		t.Fatalf("shared cache prune ran with active claim: terminal=%d cache=%d", workspace.terminalCalls, workspace.cacheCalls)
	}
}

func TestNonDiskResourceDenialDoesNotRunPressureReclaimer(t *testing.T) {
	host := healthyHost()
	host.AvailableRAMBytes = 50
	s, svc, sch, workspace := schedulerHarness(t, host)
	defer s.Close()
	_ = queueTask(t, svc, "project-memory", "memory-denied", "P2")
	result, err := sch.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ActionWaitingResource {
		t.Fatalf("expected memory resource wait, got %+v", result)
	}
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.terminalCalls != 0 || workspace.cacheCalls != 0 || workspace.checkpointCalls != 0 {
		t.Fatalf("non-disk denial invoked disk-pressure cleanup: terminal=%d cache=%d checkpoint=%d", workspace.terminalCalls, workspace.cacheCalls, workspace.checkpointCalls)
	}
}

func TestWorkspaceFailureBlocksTask(t *testing.T) {
	s, svc, sch, workspace := schedulerHarness(t, healthyHost())
	defer s.Close()
	task := queueTask(t, svc, "project-a", "workspace-fail", "P2")
	workspace.err = errors.New("git unavailable")
	result, err := sch.Step(context.Background())
	if err == nil || result.Action != ActionBlocked {
		t.Fatalf("workspace failure should block task: result=%+v err=%v", result, err)
	}
	got, _ := svc.Status(context.Background(), task.ID)
	if got.State != domain.TaskBlocked {
		t.Fatalf("workspace failure did not durably block task: %+v", got)
	}
}

func TestConcurrentStepNeverDispatchesSameTaskTwice(t *testing.T) {
	s, svc, sch, workspace := schedulerHarness(t, healthyHost())
	defer s.Close()
	_ = queueTask(t, svc, "project-a", "one", "P2")
	_ = queueTask(t, svc, "project-b", "two", "P2")
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan StepResult, 2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			result, err := sch.Step(context.Background())
			if err == nil {
				results <- result
			}
		}()
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for result := range results {
		if seen[result.TaskID] {
			t.Fatalf("task dispatched twice: %s", result.TaskID)
		}
		seen[result.TaskID] = true
	}
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if len(workspace.calls) != 2 {
		t.Fatalf("expected two distinct workspace dispatches, got %v", workspace.calls)
	}
}

func TestSchedulerVerticalFlowCreatesRealIsolatedWorkspace(t *testing.T) {
	ctx := context.Background()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, repo, "add", "seed.txt")
	gitTestRun(t, repo, "-c", "user.name=MAR Test", "-c", "user.email=mar@example.invalid", "commit", "-m", "seed")
	base := gitTestOut(t, repo, "rev-parse", "HEAD")

	s, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	svc := service.NewTaskService(s)
	if _, _, err := svc.RegisterProject(ctx, "real-project", repo); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "real scheduler workspace",
		Acceptance:          []string{"workspace ready"},
		ProjectID:           "real-project",
		BaseRevision:        base,
		VerificationProfile: "test",
		Priority:            "P2",
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
	}
	task, _, err := svc.Submit(ctx, "real-flow", contract)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AdvancePreExecution(ctx, task.ID, domain.TaskPreflight); err != nil {
		t.Fatal(err)
	}
	if err := svc.AdvancePreExecution(ctx, task.ID, domain.TaskWaitingResource); err != nil {
		t.Fatal(err)
	}

	sensor := &staticSensor{snapshot: healthyHost()}
	governor := healthyGovernor(t, sensor)
	manager, err := workspace.NewManager(s, filepath.Join(t.TempDir(), "mar-data"))
	if err != nil {
		t.Fatal(err)
	}
	sch, err := New(s, governor, manager, Config{AgingInterval: time.Hour, WorkspaceRAMReservation: 10, WorkspaceDiskReservation: 100})
	if err != nil {
		t.Fatal(err)
	}
	result, err := sch.Step(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ActionWorkspaceReady || result.Workspace == nil {
		t.Fatalf("unexpected vertical result: %+v", result)
	}
	if result.Workspace.HeadRevision != base {
		t.Fatalf("workspace head %s != base %s", result.Workspace.HeadRevision, base)
	}
	if _, err := os.Stat(filepath.Join(result.Workspace.Path, "seed.txt")); err != nil {
		t.Fatalf("real worktree content missing: %v", err)
	}
	got, err := svc.Status(ctx, task.ID)
	if err != nil || got.State != domain.TaskWorkspaceReady {
		t.Fatalf("task not ready after vertical flow: %+v err=%v", got, err)
	}
	if active := governor.Active(); len(active) != 0 {
		t.Fatalf("workspace resource lease leaked after provisioning: %+v", active)
	}
}

func TestEffectivePriorityAgesWithoutExtraDurableState(t *testing.T) {
	now := time.Now()
	if got := effectivePriority("P3", now.Add(-3*time.Hour), now, time.Hour); got != 0 {
		t.Fatalf("P3 should age to P0-equivalent, got rank %d", got)
	}
	if got := effectivePriority("P1", now.Add(-30*time.Minute), now, time.Hour); got != 1 {
		t.Fatalf("P1 should not age before interval, got %d", got)
	}
}

func gitTestRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	git := testsupport.RequireExecutable(t, "git")
	cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitTestOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	git := testsupport.RequireExecutable(t, "git")
	cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
