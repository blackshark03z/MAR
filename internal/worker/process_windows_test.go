//go:build windows

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mar/internal/agent"
	"mar/internal/domain"
	"mar/internal/processctl"
)

type fakeControlBackend struct {
	mu                  sync.Mutex
	authorityCalls      int
	heartbeatCalls      int
	latestCalls         int
	publishCalls        int
	controlsCalls       int
	inputCalls          int
	authoritative       bool
	latestCheckpoint    domain.SemanticCheckpoint
	checkpointAvailable bool
	controls            []domain.TaskControl
}

func (b *fakeControlBackend) AttemptAuthoritative(context.Context, string, string, int64) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.authorityCalls++
	return b.authoritative, nil
}

func (b *fakeControlBackend) HeartbeatAttempt(context.Context, string, string, int64, time.Duration) error {
	b.mu.Lock()
	b.heartbeatCalls++
	b.mu.Unlock()
	return nil
}

func (b *fakeControlBackend) LatestValidCheckpoint(context.Context, string) (domain.SemanticCheckpoint, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.latestCalls++
	return b.latestCheckpoint, b.checkpointAvailable, nil
}

func (b *fakeControlBackend) PublishCheckpoint(_ context.Context, taskID, attemptID string, epoch int64, revision string, payload domain.SemanticCheckpointPayload) (domain.SemanticCheckpoint, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.publishCalls++
	return domain.SemanticCheckpoint{ID: "checkpoint-from-daemon", TaskID: taskID, AttemptID: attemptID, RunEpoch: epoch, CurrentRevision: revision, Payload: payload, Version: 1, CreatedAt: time.Now().UTC()}, nil
}

func (b *fakeControlBackend) ControlsSince(context.Context, string, int64, int) ([]domain.TaskControl, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.controlsCalls++
	return append([]domain.TaskControl(nil), b.controls...), nil
}

func (b *fakeControlBackend) RequestInputForAttempt(context.Context, string, string, int64) error {
	b.mu.Lock()
	b.inputCalls++
	b.mu.Unlock()
	return nil
}

func TestWorkerStartFrameOwnsPayloadAcrossRepeatedOuterEncoding(t *testing.T) {
	start := workerProcessTestStart()
	start.Task.Contract.Goal = strings.Repeat("large-goal-", 2048)
	start.Task.Contract.Acceptance = []string{strings.Repeat("large-acceptance-", 1024)}
	for i := 0; i < 512; i++ {
		f, err := marshalFrame(frameStart, 0, "", start, "")
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(f.Payload) {
			t.Fatalf("frame %d payload became invalid before outer encode", i)
		}
		var wire bytes.Buffer
		if err := json.NewEncoder(&wire).Encode(f); err != nil {
			t.Fatalf("frame %d outer encode failed: %v", i, err)
		}
		var decoded frame
		if err := json.NewDecoder(&wire).Decode(&decoded); err != nil {
			t.Fatalf("frame %d outer decode failed: %v", i, err)
		}
		var roundTrip StartRequest
		if err := json.Unmarshal(decoded.Payload, &roundTrip); err != nil {
			t.Fatalf("frame %d payload round-trip failed: %v", i, err)
		}
		if roundTrip.Task.Contract.Goal != start.Task.Contract.Goal {
			t.Fatalf("frame %d payload changed across encoding", i)
		}
	}
}

func TestProcessRunnerBridgesWorkerRPCAndReturnsPhysicalProof(t *testing.T) {
	backend := &fakeControlBackend{authoritative: true}
	runner, err := NewProcessRunner(backend, processctl.NewSupervisor(), ProcessConfig{
		Executable:  os.Args[0],
		Arguments:   []string{"-test.run=TestWorkerProcessHelper"},
		Environment: append(os.Environ(), "MAR_WORKER_PROCESS_HELPER=normal"),
		StopTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, proof, err := runner.Run(context.Background(), workerProcessTestStart())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != agent.StatusCompletedCandidate || result.Summary != "helper complete" {
		t.Fatalf("unexpected worker result: %+v", result)
	}
	if !proof.Valid() || proof.Attempt().TaskID != "task-worker-process" || proof.Attempt().AttemptID != "attempt-worker-process" {
		t.Fatalf("invalid physical worker proof: valid=%v ref=%+v", proof.Valid(), proof.Attempt())
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.authorityCalls != 1 || backend.publishCalls != 1 {
		t.Fatalf("worker RPC was not served by daemon backend: authority=%d publish=%d", backend.authorityCalls, backend.publishCalls)
	}
}

func TestProcessRunnerRejectsWorkerRPCOutsideAssignedAttempt(t *testing.T) {
	backend := &fakeControlBackend{authoritative: true}
	runner, err := NewProcessRunner(backend, processctl.NewSupervisor(), ProcessConfig{
		Executable:  os.Args[0],
		Arguments:   []string{"-test.run=TestWorkerProcessHelper"},
		Environment: append(os.Environ(), "MAR_WORKER_PROCESS_HELPER=escape"),
		StopTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, proof, err := runner.Run(context.Background(), workerProcessTestStart())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != agent.StatusBlocked || !proof.Valid() {
		t.Fatalf("escaped RPC helper did not finish safely: result=%+v proof=%v", result, proof.Valid())
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.authorityCalls != 0 {
		t.Fatalf("escaped attempt identity reached daemon authority backend: %d", backend.authorityCalls)
	}
}

func TestProcessRunnerRejectsControlRPCOutsideAssignedTaskAndAttempt(t *testing.T) {
	backend := &fakeControlBackend{authoritative: true}
	runner, err := NewProcessRunner(backend, processctl.NewSupervisor(), ProcessConfig{
		Executable:  os.Args[0],
		Arguments:   []string{"-test.run=TestWorkerProcessHelper"},
		Environment: append(os.Environ(), "MAR_WORKER_PROCESS_HELPER=control_escape"),
		StopTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, proof, err := runner.Run(context.Background(), workerProcessTestStart())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != agent.StatusBlocked || !proof.Valid() {
		t.Fatalf("escaped control RPC helper did not finish safely: result=%+v proof=%v", result, proof.Valid())
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.controlsCalls != 0 || backend.inputCalls != 0 {
		t.Fatalf("escaped control identity reached daemon backend: controls=%d input=%d", backend.controlsCalls, backend.inputCalls)
	}
}

func TestProcessRunnerAbruptExitStillYieldsPhysicalProofWithoutFalseResult(t *testing.T) {
	backend := &fakeControlBackend{authoritative: true}
	runner, err := NewProcessRunner(backend, processctl.NewSupervisor(), ProcessConfig{
		Executable:  os.Args[0],
		Arguments:   []string{"-test.run=TestWorkerProcessHelper"},
		Environment: append(os.Environ(), "MAR_WORKER_PROCESS_HELPER=abrupt"),
		StopTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, proof, err := runner.Run(context.Background(), workerProcessTestStart())
	if err == nil {
		t.Fatal("abrupt worker exit must not produce a successful task result")
	}
	if result.Status != "" || !proof.Valid() {
		t.Fatalf("abrupt worker exit lost safe physical termination evidence: result=%+v proof=%v err=%v", result, proof.Valid(), err)
	}
}

func TestAcceptanceT8ParentTriggeredWorkerCrashYieldsPhysicalProofWithoutFalseResult(t *testing.T) {
	readyFile := filepath.Join(t.TempDir(), "worker.ready")
	crashFile := filepath.Join(t.TempDir(), "worker.crash")
	backend := &fakeControlBackend{authoritative: true}
	runner, err := NewProcessRunner(backend, processctl.NewSupervisor(), ProcessConfig{
		Executable: os.Args[0],
		Arguments:  []string{"-test.run=TestWorkerProcessHelper"},
		Environment: append(os.Environ(),
			"MAR_WORKER_PROCESS_HELPER=crash_on_signal",
			"MAR_WORKER_READY_FILE="+readyFile,
			"MAR_WORKER_CRASH_FILE="+crashFile,
		),
		StopTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	type runOutcome struct {
		result agent.Result
		proof  processctl.TerminationProof
		err    error
	}
	done := make(chan runOutcome, 1)
	go func() {
		result, proof, runErr := runner.Run(context.Background(), workerProcessTestStart())
		done <- runOutcome{result: result, proof: proof, err: runErr}
	}()
	waitWorkerSignalFile(t, readyFile)
	if err := os.WriteFile(crashFile, []byte("crash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case outcome := <-done:
		if outcome.err == nil {
			t.Fatal("crashed worker must not produce a successful task result")
		}
		if outcome.result.Status != "" || !outcome.proof.Valid() {
			t.Fatalf("worker crash lost coherent physical evidence: result=%+v proof=%v err=%v", outcome.result, outcome.proof.Valid(), outcome.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("process runner did not reconcile worker crash")
	}
}

func waitWorkerSignalFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("worker signal file not created: %s", path)
}

func TestWorkerProcessHelper(t *testing.T) {
	mode := os.Getenv("MAR_WORKER_PROCESS_HELPER")
	if mode == "" {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	var startFrame frame
	if err := decoder.Decode(&startFrame); err != nil {
		t.Fatal(err)
	}
	if startFrame.Type != frameStart {
		t.Fatalf("unexpected start frame: %+v", startFrame)
	}
	var start StartRequest
	if err := json.Unmarshal(startFrame.Payload, &start); err != nil {
		t.Fatal(err)
	}
	if mode == "abrupt" {
		return
	}
	if mode == "crash_on_signal" {
		readyFile := os.Getenv("MAR_WORKER_READY_FILE")
		crashFile := os.Getenv("MAR_WORKER_CRASH_FILE")
		if readyFile == "" || crashFile == "" {
			t.Fatal("worker crash signal paths are required")
		}
		if err := os.WriteFile(readyFile, []byte("ready\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for {
			if _, err := os.Stat(crashFile); err == nil {
				os.Exit(23)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if mode == "control_escape" {
		controlsRequest, err := marshalFrame(frameRequest, 1, methodControlsSince, controlsSinceRequest{TaskID: "other-task", AfterVersion: 0, Limit: 32}, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := encoder.Encode(controlsRequest); err != nil {
			t.Fatal(err)
		}
		var response frame
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.Error == "" {
			t.Fatal("daemon accepted escaped controls_since request")
		}
		inputRequest, err := marshalFrame(frameRequest, 2, methodEnterInputRequired, inputRequiredRequest{TaskID: start.Task.ID, AttemptID: "other-attempt", RunEpoch: start.Attempt.RunEpoch}, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := encoder.Encode(inputRequest); err != nil {
			t.Fatal(err)
		}
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.Error == "" {
			t.Fatal("daemon accepted escaped INPUT_REQUIRED request")
		}
		terminal, _ := marshalFrame(frameResult, 0, "", agent.Result{Status: agent.StatusBlocked, Summary: "control escape rejected"}, "")
		if err := encoder.Encode(terminal); err != nil {
			t.Fatal(err)
		}
		return
	}

	identity := authorityRequest{TaskID: start.Task.ID, AttemptID: start.Attempt.ID, RunEpoch: start.Attempt.RunEpoch}
	if mode == "escape" {
		identity.TaskID = "other-task"
	}
	request, err := marshalFrame(frameRequest, 1, methodAttemptAuthoritative, identity, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.Encode(request); err != nil {
		t.Fatal(err)
	}
	var response frame
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if mode == "escape" {
		if response.Error == "" {
			t.Fatal("daemon accepted escaped worker authority request")
		}
		terminal, _ := marshalFrame(frameResult, 0, "", agent.Result{Status: agent.StatusBlocked, Summary: "escape rejected"}, "")
		if err := encoder.Encode(terminal); err != nil {
			t.Fatal(err)
		}
		return
	}
	if response.Error != "" {
		t.Fatalf("authority request failed: %s", response.Error)
	}
	var authority authorityResponse
	if err := json.Unmarshal(response.Payload, &authority); err != nil || !authority.Authoritative {
		t.Fatalf("authority response invalid: %+v err=%v", authority, err)
	}

	payload := domain.SemanticCheckpointPayload{
		CompletedWork:        []string{"worker protocol reached daemon"},
		CurrentHypothesis:    "process boundary works",
		ChangedAreas:         []string{},
		VerificationStatus:   "not started",
		Blockers:             []string{},
		RemainingWork:        []string{"finish"},
		NextAction:           "finish",
		CriticalEvidenceRefs: []string{"rpc"},
	}
	checkpointRequest, err := marshalFrame(frameRequest, 2, methodPublishCheckpoint, publishCheckpointRequest{
		TaskID: start.Task.ID, AttemptID: start.Attempt.ID, RunEpoch: start.Attempt.RunEpoch,
		CurrentRevision: start.Task.Contract.BaseRevision, Payload: payload,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.Encode(checkpointRequest); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" {
		t.Fatalf("checkpoint publish failed: %s", response.Error)
	}
	terminal, _ := marshalFrame(frameResult, 0, "", agent.Result{Status: agent.StatusCompletedCandidate, Summary: "helper complete"}, "")
	if err := encoder.Encode(terminal); err != nil {
		t.Fatal(err)
	}
}

func workerProcessTestStart() StartRequest {
	contract := domain.GoalContract{
		Goal:                "exercise worker process protocol",
		Acceptance:          []string{"worker is physically contained"},
		Boundaries:          []string{},
		NonGoals:            []string{},
		ProjectID:           "project-worker-process",
		BaseRevision:        "base-worker-process",
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
		VerificationProfile: "go-standard",
		Priority:            "P2",
	}
	return StartRequest{
		Task:          domain.Task{ID: "task-worker-process", Contract: contract, ContractHash: "hash", State: domain.TaskRunning, RunEpoch: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
		Attempt:       domain.ExecutionAttempt{ID: "attempt-worker-process", TaskID: "task-worker-process", RunEpoch: 1, WorkerID: "worker", SupervisorID: "supervisor", AuthorityState: domain.AttemptActive, StartedAt: time.Now().UTC(), HeartbeatAt: time.Now().UTC(), LeaseDeadline: time.Now().UTC().Add(time.Minute)},
		WorkspacePath: tWorkerWorkspacePlaceholder(),
		Provider:      ProviderConfig{BaseURL: "https://provider.invalid/v1", APIKeyEnv: "MAR_TEST_API_KEY"},
		AgentProfile:  agent.Profile{Model: "test-model", BaseInstructions: "You are the MAR coding worker."},
	}
}

func tWorkerWorkspacePlaceholder() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
