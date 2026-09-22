package worker

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"mar/internal/agent"
	"mar/internal/domain"
)

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
