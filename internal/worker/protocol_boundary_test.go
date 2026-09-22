package worker

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"mar/internal/agent"
	"mar/internal/domain"
)

func TestExternalHarnessInputBindsDurableTaskIntent(t *testing.T) {
	contract := domain.GoalContract{
		Goal: "implement bounded task", Acceptance: []string{"verified"},
		ProjectID: "project-harness", BaseRevision: "base-harness",
		VerificationProfile: "test", Priority: "P2",
	}
	hash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	req := StartRequest{
		Task:    domain.Task{ID: "task-harness", State: domain.TaskRunning, RunEpoch: 3, Contract: contract, ContractHash: hash},
		Attempt: domain.ExecutionAttempt{ID: "attempt-harness", TaskID: "task-harness", RunEpoch: 3},
	}
	input, err := req.ExternalHarnessInput()
	if err != nil {
		t.Fatal(err)
	}
	if input.Schema != ExternalHarnessInputSchema || input.TaskID != req.Task.ID || input.AttemptID != req.Attempt.ID || input.RunEpoch != 3 || input.ContractHash != hash {
		t.Fatalf("external harness input identity mismatch: %+v", input)
	}
	if !reflect.DeepEqual(input.GoalContract, contract) {
		t.Fatalf("external harness input changed Goal Contract: %+v", input.GoalContract)
	}

	req.Task.Contract.Goal = "tampered"
	if _, err := req.ExternalHarnessInput(); err == nil {
		t.Fatal("external harness input accepted a Goal Contract that no longer matches durable contract hash")
	}
}

func TestHarnessConfigContainsOnlyCompatibilityCognition(t *testing.T) {
	req := StartRequest{
		Provider:          ProviderConfig{BrainMode: BrainHarness},
		AgentProfile:      agent.Profile{Model: "legacy-model", BaseInstructions: "legacy"},
		AgentConfig:       agent.Config{MaxTurns: 3},
		HarnessExecutable: `C:\tools\harness.exe`,
		HarnessArguments:  []string{"--run"},
	}
	harness := req.HarnessConfig()
	if harness.Provider.BrainMode != BrainHarness || harness.AgentProfile.Model != "legacy-model" || harness.AgentConfig.MaxTurns != 3 {
		t.Fatalf("compatibility cognition projection changed: %+v", harness)
	}
	if reflect.TypeOf(harness).NumField() != 3 {
		t.Fatalf("HarnessConfig regained external execution fields: %+v", harness)
	}
}

func TestStartRequestBoundaryProjectionPreservesLegacyWireShape(t *testing.T) {
	req := StartRequest{
		Task:          domain.Task{ID: "task-boundary"},
		Attempt:       domain.ExecutionAttempt{ID: "attempt-boundary", TaskID: "task-boundary", RunEpoch: 1},
		WorkspacePath: "C:\\workspace",
		Provider: ProviderConfig{
			BrainMode:      BrainProvider,
			BaseURL:        "https://provider.invalid/v1",
			APIKeyEnv:      "MAR_TEST_KEY",
			RequestTimeout: 3 * time.Second,
		},
		AgentProfile:          agent.Profile{Model: "test-model", BaseInstructions: "bounded"},
		AgentConfig:           agent.Config{MaxTurns: 7},
		SandboxReadPaths:      []string{"C:\\read-only"},
		GoModuleCache:         "C:\\gomod",
		GoBuildCache:          "C:\\gobuild",
		CommandTimeout:        9 * time.Second,
		MemoryPressurePercent: 71,
	}

	harness := req.HarnessConfig()
	if harness.Provider.BaseURL != req.Provider.BaseURL || harness.AgentProfile.Model != req.AgentProfile.Model || harness.AgentConfig.MaxTurns != req.AgentConfig.MaxTurns {
		t.Fatalf("harness projection changed cognition config: %+v", harness)
	}
	execution := req.ExecutionConfig()
	if execution.Task.ID != req.Task.ID || execution.Attempt.ID != req.Attempt.ID || execution.WorkspacePath != req.WorkspacePath {
		t.Fatalf("execution projection changed governed identity: %+v", execution)
	}
	execution.SandboxReadPaths[0] = "C:\\mutated"
	if req.SandboxReadPaths[0] == execution.SandboxReadPaths[0] {
		t.Fatal("execution projection did not own sandbox path slice")
	}

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"task", "attempt", "workspace_path", "provider", "agent_profile", "agent_config", "sandbox_read_paths"} {
		if _, ok := top[key]; !ok {
			t.Fatalf("legacy worker start wire key %q disappeared: %s", key, raw)
		}
	}
	if _, nested := top["harness"]; nested {
		t.Fatalf("boundary projection leaked new harness wire envelope: %s", raw)
	}
	if _, nested := top["execution"]; nested {
		t.Fatalf("boundary projection leaked new execution wire envelope: %s", raw)
	}

	var roundTrip StartRequest
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundTrip.Provider, req.Provider) || roundTrip.AgentProfile.Model != req.AgentProfile.Model || roundTrip.AgentConfig.MaxTurns != req.AgentConfig.MaxTurns {
		t.Fatalf("legacy worker start cognition fields changed across JSON round trip: %+v", roundTrip)
	}
}
