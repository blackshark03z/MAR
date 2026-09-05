//go:build windows

package worker

import (
	"context"
	"encoding/json"
	"os"
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
	authoritative       bool
	latestCheckpoint    domain.SemanticCheckpoint
	checkpointAvailable bool
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
