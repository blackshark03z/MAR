//go:build windows

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mar/internal/agent"
	"mar/internal/domain"
	"mar/internal/mcpedge"
	"mar/internal/model"
	"mar/internal/resourcegov"
	"mar/internal/scheduler"
	"mar/internal/store"
	"mar/internal/testsupport"
	"mar/internal/verification"
	"mar/internal/worker"
)

func TestRuntimeE2EMCPSubmitWorkerVerifyIntegrate(t *testing.T) {
	if os.Getenv("MAR_RUNTIME_E2E_WORKER") == "1" {
		t.Skip("worker helper process")
	}
	testsupport.RequireLoopbackTCP(t)

	// Keep the E2E bounded while allowing cold sandbox/toolchain startup; production command execution allows a wider five-minute budget.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	projectRoot := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/mar-e2e\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "main.go"), []byte("package smoke\n\nfunc Value() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMarkerOracleTest(t, projectRoot, "MAR E2E OK\n", "MAR_ACCEPTANCE_E2E_MARKER")
	runGitTest(t, projectRoot, "init", "-b", "main")
	runGitTest(t, projectRoot, "config", "user.name", "MAR E2E")
	runGitTest(t, projectRoot, "config", "user.email", "mar-e2e@local.invalid")
	runGitTest(t, projectRoot, "config", "core.autocrlf", "false")
	runGitTest(t, projectRoot, "add", "-A")
	runGitTest(t, projectRoot, "commit", "-m", "baseline")
	baseRevision := strings.TrimSpace(runGitTest(t, projectRoot, "rev-parse", "HEAD"))

	const apiKeyEnv = "MAR_RUNTIME_E2E_API_KEY"
	t.Setenv(apiKeyEnv, "e2e-key")
	t.Setenv("MAR_RUNTIME_E2E_WORKER", "1")
	var modelCalls atomic.Int32
	var observedSteer atomic.Bool
	var observedInput atomic.Bool
	firstProviderEntered := make(chan struct{})
	releaseFirstProvider := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer e2e-key" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		call := modelCalls.Add(1)
		var toolName, arguments, callID string
		switch call {
		case 1:
			close(firstProviderEntered)
			select {
			case <-releaseFirstProvider:
			case <-r.Context().Done():
				return
			}
			callID = "call-input"
			toolName = "request_input"
			encoded, _ := json.Marshal(map[string]any{"prompt": "Confirm the exact requested marker content before mutation."})
			arguments = string(encoded)
		case 2:
			var turnRequest struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(body, &turnRequest); err != nil {
				http.Error(w, "invalid model request", http.StatusBadRequest)
				return
			}
			var decisionState strings.Builder
			for _, message := range turnRequest.Messages {
				decisionState.WriteString(message.Content)
				decisionState.WriteByte('\n')
			}
			wire := decisionState.String()
			observedSteer.Store(strings.Contains(wire, `"kind":"STEER"`) && strings.Contains(wire, `"kind":"context"`) && strings.Contains(wire, "Keep the marker content exactly MAR E2E OK"))
			observedInput.Store(strings.Contains(wire, `"kind":"INPUT"`) && strings.Contains(wire, "Proceed with the exact requested marker content"))
			callID = "call-write"
			toolName = "write_file"
			encoded, _ := json.Marshal(map[string]any{"path": "marker.txt", "expected_sha256": "ABSENT", "content": "MAR E2E OK\n"})
			arguments = string(encoded)
		default:
			callID = "call-finish"
			toolName = "finish_task"
			encoded, _ := json.Marshal(map[string]any{"status": "completed_candidate", "summary": "Created the requested MAR end-to-end marker."})
			arguments = string(encoded)
		}
		payload := map[string]any{
			"id":    fmt.Sprintf("resp-%d", call),
			"model": "e2e-model",
			"choices": []any{map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []any{map[string]any{
						"id":       callID,
						"type":     "function",
						"function": map[string]any{"name": toolName, "arguments": arguments},
					}},
				},
				"finish_reason": "tool_calls",
			}},
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer provider.Close()

	goExe := findPortableGo(t)
	goRoot := filepath.Dir(filepath.Dir(goExe))
	goBin := filepath.Dir(goExe)
	dataRoot := filepath.Join(t.TempDir(), "mar-data")
	sharedModCache := filepath.Join(dataRoot, "runtime", "gomodcache")
	if err := os.MkdirAll(sharedModCache, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataRoot, "mar.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	runtime, err := NewRuntime(s, RuntimeConfig{
		DataRoot:        dataRoot,
		Executable:      os.Args[0],
		WorkerArguments: []string{"-test.run=^TestRuntimeE2EWorkerHelper$"},
		Provider: worker.ProviderConfig{
			BaseURL:        provider.URL + "/v1",
			APIKeyEnv:      apiKeyEnv,
			RequestTimeout: 10 * time.Second,
		},
		AgentProfile: agent.Profile{
			Model:            "e2e-model",
			ReasoningEffort:  "high",
			BaseInstructions: "Execute exactly the bounded MAR Goal Contract using the available coding tools.",
		},
		VerificationProfiles: []verification.Profile{{
			ID: "go-standard",
			Commands: []verification.Command{
				{Name: goExe, Args: []string{"test", "-v", "-count=1", "./..."}, Cwd: "."},
				{Name: goExe, Args: []string{"vet", "./..."}, Cwd: "."},
				{Name: goExe, Args: []string{"build", "./..."}, Cwd: "."},
			},
		}},
		SandboxReadPaths:  []string{goRoot, sharedModCache},
		WorkerPathEntries: []string{goBin},
		GoModuleCache:     sharedModCache,
		LeaseDuration:     20 * time.Second,
		WorkerStopTimeout: 10 * time.Second,
		ResourceGovernor: resourcegov.Config{
			MaxCPUPercent:           100,
			MaxMemoryLoadPercent:    100,
			MaxIOPressurePercent:    100,
			MinFreeRAMBytes:         1,
			MinFreeDiskBytes:        1,
			MaxMARDiskBytes:         1 << 30,
			MaxHeavyJobs:            2,
			MaxHeavyJobsPerProject:  1,
			MaxHeavyJobsInteractive: 1,
		},
		Scheduler: scheduler.Config{
			AgingInterval:            time.Minute,
			WorkspaceRAMReservation:  1,
			WorkspaceDiskReservation: 1,
		},
		Daemon: DaemonConfig{
			PollInterval:         25 * time.Millisecond,
			ControlPollInterval:  25 * time.Millisecond,
			MaxConcurrentWorkers: 1,
			MaxPreflightPerTick:  4,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.Service.RegisterProject(ctx, "e2e-project", projectRoot); err != nil {
		t.Fatal(err)
	}

	daemonCtx, stopDaemon := context.WithCancel(ctx)
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- runtime.Daemon.Run(daemonCtx) }()
	defer func() {
		stopDaemon()
		err := <-daemonDone
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("daemon shutdown: %v", err)
		}
	}()

	server, err := mcpedge.NewServer(runtime.Service)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "mar-e2e-test", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	submit, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "submit", Arguments: map[string]any{
		"idempotency_key": "e2e-submit-1",
		"contract": map[string]any{
			"goal":       "Create marker.txt containing MAR E2E OK.",
			"acceptance": []string{"marker.txt exists in the authoritative project with the requested content"},
			"acceptance_checks": []any{map[string]any{
				"criterion_index": 1,
				"scenario":        "run the project acceptance test against the sealed candidate",
				"oracle":          "output_contains:MAR_ACCEPTANCE_E2E_MARKER",
				"command_indexes": []int{1},
			}},
			"boundaries":           []string{"Only create marker.txt; do not change existing source files."},
			"non_goals":            []string{"No remote Git writes or deployment."},
			"project_id":           "e2e-project",
			"base_revision":        baseRevision,
			"authority":            map[string]any{"local_file_write": true, "local_git_write": true, "network_allowed": false, "remote_git_write": false, "deploy_allowed": false},
			"verification_profile": "go-standard",
			"priority":             "P2",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if submit.IsError {
		t.Fatalf("MCP submit returned tool error: %+v", submit.Content)
	}
	var submitted struct {
		Created bool `json:"created"`
		Task    struct {
			ID string `json:"task_id"`
		} `json:"task"`
	}
	raw, _ := json.Marshal(submit.StructuredContent)
	if err := json.Unmarshal(raw, &submitted); err != nil {
		t.Fatal(err)
	}
	if !submitted.Created || submitted.Task.ID == "" {
		t.Fatalf("unexpected MCP submit result: %s", raw)
	}

	select {
	case <-firstProviderEntered:
	case <-ctx.Done():
		t.Fatal("worker never reached first model turn")
	}
	steerResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "steer", Arguments: map[string]any{
		"task_id": submitted.Task.ID, "idempotency_key": "e2e-steer-1", "kind": "context", "message": "Keep the marker content exactly MAR E2E OK",
	}})
	if err != nil || steerResult.IsError {
		t.Fatalf("MCP steer failed: err=%v result=%+v", err, steerResult)
	}
	close(releaseFirstProvider)

	for {
		statusResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "status", Arguments: map[string]any{"task_id": submitted.Task.ID}})
		if err != nil {
			t.Fatal(err)
		}
		var status struct {
			Status struct {
				State domain.TaskState `json:"state"`
			} `json:"status"`
		}
		rawStatus, _ := json.Marshal(statusResult.StructuredContent)
		if err := json.Unmarshal(rawStatus, &status); err != nil {
			t.Fatal(err)
		}
		if status.Status.State == domain.TaskInputRequired {
			break
		}
		if status.Status.State == domain.TaskBlocked || status.Status.State == domain.TaskFailed || status.Status.State == domain.TaskCancelled || status.Status.State == domain.TaskComplete {
			t.Fatalf("task reached %s before INPUT_REQUIRED", status.Status.State)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for INPUT_REQUIRED; last state=%s", status.Status.State)
		case <-time.After(25 * time.Millisecond):
		}
	}
	inputResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "input", Arguments: map[string]any{
		"task_id": submitted.Task.ID, "idempotency_key": "e2e-input-1", "message": "Proceed with the exact requested marker content",
	}})
	if err != nil || inputResult.IsError {
		t.Fatalf("MCP input failed: err=%v result=%+v", err, inputResult)
	}

	var final domain.Task
	for {
		statusResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "status", Arguments: map[string]any{"task_id": submitted.Task.ID}})
		if err != nil {
			t.Fatal(err)
		}
		var status struct {
			Status struct {
				State domain.TaskState `json:"state"`
			} `json:"status"`
		}
		raw, _ := json.Marshal(statusResult.StructuredContent)
		if err := json.Unmarshal(raw, &status); err != nil {
			t.Fatal(err)
		}
		final = domain.Task{ID: submitted.Task.ID, State: status.Status.State}
		if final.State == domain.TaskComplete || final.State == domain.TaskBlocked || final.State == domain.TaskFailed || final.State == domain.TaskCancelled {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for E2E task; last state=%s", final.State)
		case <-time.After(50 * time.Millisecond):
		}
	}
	if final.State != domain.TaskComplete {
		inspection, _ := runtime.Service.Inspect(context.Background(), submitted.Task.ID)
		t.Fatalf("E2E task did not complete: state=%s task=%+v attempt=%+v result=%+v evidence=%+v", final.State, inspection.Task, inspection.Attempt, inspection.Result, inspection.Evidence)
	}
	marker, err := os.ReadFile(filepath.Join(projectRoot, "marker.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(marker) != "MAR E2E OK\n" {
		t.Fatalf("unexpected authoritative marker content %q", marker)
	}
	if got := strings.TrimSpace(runGitTest(t, projectRoot, "rev-parse", "HEAD")); got == baseRevision {
		t.Fatal("authoritative Git head did not advance to the verified candidate")
	}
	resultTool, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "result", Arguments: map[string]any{"task_id": submitted.Task.ID}})
	if err != nil {
		t.Fatal(err)
	}
	var resultPayload struct {
		Available bool              `json:"available"`
		Result    domain.TaskResult `json:"result"`
	}
	raw, _ = json.Marshal(resultTool.StructuredContent)
	if err := json.Unmarshal(raw, &resultPayload); err != nil {
		t.Fatal(err)
	}
	if !resultPayload.Available || resultPayload.Result.IntegrationStatus != "INTEGRATED" {
		t.Fatalf("unexpected final result: %s", raw)
	}
	if len(resultPayload.Result.ChangedAreas) != 1 || resultPayload.Result.ChangedAreas[0] != "marker.txt" {
		t.Fatalf("E2E candidate widened beyond requested marker: changed=%v", resultPayload.Result.ChangedAreas)
	}
	resources := resultPayload.Result.ResourceSummary
	if resources.AgentTurns != 3 || resources.AgentToolCalls != 3 || resources.ModelTotalTokens != 330 {
		t.Fatalf("E2E result lost agent resource accounting: %+v", resources)
	}
	inspection, err := runtime.Service.Inspect(context.Background(), submitted.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Attempt == nil || inspection.Attempt.RunEpoch != 1 || inspection.Task.RunEpoch != 1 {
		t.Fatalf("owner input restarted the execution attempt instead of resuming it: task_epoch=%d attempt=%+v", inspection.Task.RunEpoch, inspection.Attempt)
	}
	if !observedSteer.Load() || !observedInput.Load() {
		t.Fatalf("active worker did not consume durable steer/input controls: steer=%v input=%v", observedSteer.Load(), observedInput.Load())
	}
	if modelCalls.Load() < 3 {
		t.Fatalf("worker did not complete expected input/write/finish model loop: calls=%d", modelCalls.Load())
	}
}

func writeMarkerOracleTest(t *testing.T, projectRoot, want, observationMarker string) {
	t.Helper()
	source := fmt.Sprintf(`package smoke

import (
	"os"
	"testing"
)

func TestMarkerAcceptance(t *testing.T) {
	got, err := os.ReadFile("marker.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != %q {
		t.Fatalf("marker content = %%q", got)
	}
	t.Log(%q)
}
`, want, observationMarker)
	if err := os.WriteFile(filepath.Join(projectRoot, "main_test.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeE2EExternalHarnessWorkerVerifyIntegrate(t *testing.T) {
	if os.Getenv("MAR_RUNTIME_E2E_WORKER") == "1" {
		t.Skip("worker helper process")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	projectRoot := filepath.Join(t.TempDir(), "harness-project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/mar-harness-e2e\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "main.go"), []byte("package smoke\n\nfunc Value() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMarkerOracleTest(t, projectRoot, "MAR HARNESS OK\n", "MAR_ACCEPTANCE_HARNESS_E2E_MARKER")
	runGitTest(t, projectRoot, "init", "-b", "main")
	runGitTest(t, projectRoot, "config", "user.name", "MAR Harness E2E")
	runGitTest(t, projectRoot, "config", "user.email", "mar-harness-e2e@local.invalid")
	runGitTest(t, projectRoot, "config", "core.autocrlf", "false")
	runGitTest(t, projectRoot, "add", "-A")
	runGitTest(t, projectRoot, "commit", "-m", "baseline")
	baseRevision := strings.TrimSpace(runGitTest(t, projectRoot, "rev-parse", "HEAD"))

	t.Setenv("MAR_RUNTIME_E2E_WORKER", "1")
	goExe := findPortableGo(t)
	goRoot := filepath.Dir(filepath.Dir(goExe))
	goBin := filepath.Dir(goExe)
	harnessExe, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	harnessReadRoot := filepath.Dir(harnessExe)
	dataRoot := filepath.Join(t.TempDir(), "mar-harness-data")
	sharedModCache := filepath.Join(dataRoot, "runtime", "gomodcache")
	if err := os.MkdirAll(sharedModCache, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(dataRoot, "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	runtime, err := NewRuntime(s, RuntimeConfig{
		DataRoot: dataRoot, Executable: os.Args[0], WorkerArguments: []string{"-test.run=^TestRuntimeE2EWorkerHelper$"},
		Provider:          worker.ProviderConfig{BrainMode: worker.BrainHarness},
		HarnessExecutable: harnessExe,
		HarnessArguments:  []string{"-test.run=^TestRuntimeE2EExternalHarnessHelper$"},
		VerificationProfiles: []verification.Profile{{ID: "harness-go", Commands: []verification.Command{
			{Name: goExe, Args: []string{"test", "-v", "-count=1", "./..."}, Cwd: "."},
		}}},
		SandboxReadPaths: []string{goRoot, sharedModCache, harnessReadRoot}, WorkerPathEntries: []string{goBin}, GoModuleCache: sharedModCache,
		LeaseDuration: 20 * time.Second, WorkerStopTimeout: 10 * time.Second,
		ResourceGovernor: resourcegov.Config{MaxCPUPercent: 100, MaxMemoryLoadPercent: 100, MaxIOPressurePercent: 100, MinFreeRAMBytes: 1, MinFreeDiskBytes: 1, MaxMARDiskBytes: 1 << 30, MaxHeavyJobs: 2, MaxHeavyJobsPerProject: 1, MaxHeavyJobsInteractive: 1},
		Scheduler:        scheduler.Config{AgingInterval: time.Minute, WorkspaceRAMReservation: 1, WorkspaceDiskReservation: 1},
		Daemon:           DaemonConfig{PollInterval: 25 * time.Millisecond, ControlPollInterval: 25 * time.Millisecond, MaxConcurrentWorkers: 1, MaxPreflightPerTick: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.Service.RegisterProject(ctx, "harness-e2e-project", projectRoot); err != nil {
		t.Fatal(err)
	}

	daemonCtx, stopDaemon := context.WithCancel(ctx)
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- runtime.Daemon.Run(daemonCtx) }()
	defer func() {
		stopDaemon()
		if err := <-daemonDone; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("daemon shutdown: %v", err)
		}
	}()

	task, _, err := runtime.Service.Submit(ctx, "harness-e2e-submit-1", domain.GoalContract{
		Goal:       "Create marker.txt containing MAR HARNESS OK.",
		Acceptance: []string{"marker.txt exists in the authoritative project with the requested content"},
		AcceptanceChecks: []domain.AcceptanceCheck{{
			CriterionIndex: 1,
			Scenario:       "run the project acceptance test against the sealed candidate",
			Oracle:         "output_contains:MAR_ACCEPTANCE_HARNESS_E2E_MARKER",
			CommandIndexes: []int{1},
		}},
		Boundaries: []string{"Only create marker.txt; do not change existing source files."},
		NonGoals:   []string{"No remote Git writes or deployment."},
		ProjectID:  "harness-e2e-project", BaseRevision: baseRevision,
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true, NetworkAllowed: false, RemoteGitWrite: false, DeployAllowed: false},
		VerificationProfile: "harness-go", Priority: "P2",
	})
	if err != nil {
		t.Fatal(err)
	}

	var final domain.Task
	deadline := time.Now().Add(75 * time.Second)
	for time.Now().Before(deadline) {
		final, err = runtime.Service.Status(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if final.State == domain.TaskComplete {
			break
		}
		if final.State == domain.TaskBlocked || final.State == domain.TaskFailed || final.State == domain.TaskCancelled {
			inspection, _ := runtime.Service.Inspect(context.Background(), task.ID)
			t.Fatalf("external harness task reached terminal failure %s: %+v", final.State, inspection)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if final.State != domain.TaskComplete {
		inspection, _ := runtime.Service.Inspect(context.Background(), task.ID)
		t.Fatalf("external harness task did not complete: task=%+v inspection=%+v", final, inspection)
	}

	marker, err := os.ReadFile(filepath.Join(projectRoot, "marker.txt"))
	if err != nil || string(marker) != "MAR HARNESS OK\n" {
		t.Fatalf("unexpected authoritative harness marker: content=%q err=%v", marker, err)
	}
	if got := strings.TrimSpace(runGitTest(t, projectRoot, "rev-parse", "HEAD")); got == baseRevision {
		t.Fatal("external harness verified candidate was not integrated")
	}
	result, available, err := runtime.Service.Result(ctx, task.ID)
	if err != nil || !available || result.IntegrationStatus != "INTEGRATED" || result.Verdict != domain.ResultVerified {
		t.Fatalf("external harness result mismatch: result=%+v available=%v err=%v", result, available, err)
	}
	if result.ResourceSummary.AgentTurns != 0 || result.ResourceSummary.AgentToolCalls != 0 || result.ResourceSummary.ModelTotalTokens != 0 {
		t.Fatalf("external harness unexpectedly consumed MAR cognition budget: %+v", result.ResourceSummary)
	}
	inspection, err := runtime.Service.Inspect(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Attempt == nil || inspection.Attempt.RunEpoch != 1 || inspection.Attempt.AuthorityState != domain.AttemptPhysicallyTerminated || inspection.BrainTurn != nil {
		t.Fatalf("external harness final authority/cognition state mismatch: %+v", inspection)
	}
}

func TestRuntimeE2EExternalHarnessHelper(t *testing.T) {
	exactRun := false
	for _, arg := range os.Args {
		if arg == "-test.run=^TestRuntimeE2EExternalHarnessHelper$" {
			exactRun = true
			break
		}
	}
	if !exactRun {
		t.Skip("not running as external harness helper")
	}
	if err := os.WriteFile("marker.txt", []byte("MAR HARNESS OK\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fmt.Println("MAR external harness helper completed")
}

func TestRuntimeE2EWebBrainMCPWorkerVerifyIntegrate(t *testing.T) {
	if os.Getenv("MAR_RUNTIME_E2E_WORKER") == "1" {
		t.Skip("worker helper process")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	projectRoot := filepath.Join(t.TempDir(), "web-project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/mar-web-e2e\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "main.go"), []byte("package smoke\n\nfunc Value() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMarkerOracleTest(t, projectRoot, "MAR WEB BRAIN OK\n", "MAR_ACCEPTANCE_WEB_E2E_MARKER")
	runGitTest(t, projectRoot, "init", "-b", "main")
	runGitTest(t, projectRoot, "config", "user.name", "MAR Web E2E")
	runGitTest(t, projectRoot, "config", "user.email", "mar-web-e2e@local.invalid")
	runGitTest(t, projectRoot, "config", "core.autocrlf", "false")
	runGitTest(t, projectRoot, "add", "-A")
	runGitTest(t, projectRoot, "commit", "-m", "baseline")
	baseRevision := strings.TrimSpace(runGitTest(t, projectRoot, "rev-parse", "HEAD"))

	t.Setenv("MAR_RUNTIME_E2E_WORKER", "1")
	goExe := findPortableGo(t)
	goRoot := filepath.Dir(filepath.Dir(goExe))
	goBin := filepath.Dir(goExe)
	dataRoot := filepath.Join(t.TempDir(), "mar-web-data")
	sharedModCache := filepath.Join(dataRoot, "runtime", "gomodcache")
	if err := os.MkdirAll(sharedModCache, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(dataRoot, "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runtime, err := NewRuntime(s, RuntimeConfig{
		DataRoot: dataRoot, Executable: os.Args[0], WorkerArguments: []string{"-test.run=^TestRuntimeE2EWorkerHelper$"},
		Provider:     worker.ProviderConfig{BrainMode: worker.BrainWeb},
		AgentProfile: agent.Profile{BaseInstructions: "Execute exactly the bounded MAR Goal Contract using the available coding tools."},
		VerificationProfiles: []verification.Profile{{ID: "go-standard", Commands: []verification.Command{
			{Name: goExe, Args: []string{"test", "-v", "-count=1", "./..."}, Cwd: "."},
			{Name: goExe, Args: []string{"vet", "./..."}, Cwd: "."},
			{Name: goExe, Args: []string{"build", "./..."}, Cwd: "."},
		}}},
		SandboxReadPaths: []string{goRoot, sharedModCache}, WorkerPathEntries: []string{goBin}, GoModuleCache: sharedModCache,
		LeaseDuration: 20 * time.Second, WorkerStopTimeout: 10 * time.Second,
		ResourceGovernor: resourcegov.Config{MaxCPUPercent: 100, MaxMemoryLoadPercent: 100, MaxIOPressurePercent: 100, MinFreeRAMBytes: 1, MinFreeDiskBytes: 1, MaxMARDiskBytes: 1 << 30, MaxHeavyJobs: 2, MaxHeavyJobsPerProject: 1, MaxHeavyJobsInteractive: 1},
		Scheduler:        scheduler.Config{AgingInterval: time.Minute, WorkspaceRAMReservation: 1, WorkspaceDiskReservation: 1},
		Daemon:           DaemonConfig{PollInterval: 25 * time.Millisecond, ControlPollInterval: 25 * time.Millisecond, MaxConcurrentWorkers: 1, MaxPreflightPerTick: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.Service.RegisterProject(ctx, "web-e2e-project", projectRoot); err != nil {
		t.Fatal(err)
	}
	daemonCtx, stopDaemon := context.WithCancel(ctx)
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- runtime.Daemon.Run(daemonCtx) }()
	defer func() {
		stopDaemon()
		if err := <-daemonDone; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("daemon shutdown: %v", err)
		}
	}()

	server, err := mcpedge.NewServer(runtime.Service)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "mar-web-brain-e2e", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	submit, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "submit", Arguments: map[string]any{
		"idempotency_key": "web-e2e-submit-1",
		"contract": map[string]any{
			"goal":       "Create marker.txt containing MAR WEB BRAIN OK.",
			"acceptance": []string{"marker.txt exists in the authoritative project with the requested content"},
			"acceptance_checks": []any{map[string]any{
				"criterion_index": 1,
				"scenario":        "run the project acceptance test against the sealed candidate",
				"oracle":          "output_contains:MAR_ACCEPTANCE_WEB_E2E_MARKER",
				"command_indexes": []int{1},
			}},
			"boundaries": []string{"Only create marker.txt; do not change existing source files."},
			"non_goals":  []string{"No remote Git writes or deployment."},
			"project_id": "web-e2e-project", "base_revision": baseRevision,
			"authority":            map[string]any{"local_file_write": true, "local_git_write": true, "network_allowed": false, "remote_git_write": false, "deploy_allowed": false},
			"verification_profile": "go-standard", "priority": "P2",
		},
	}})
	if err != nil || submit.IsError {
		t.Fatalf("web brain submit failed: err=%v result=%+v", err, submit)
	}
	var submitted struct {
		Task struct {
			ID string `json:"task_id"`
		} `json:"task"`
	}
	raw, _ := json.Marshal(submit.StructuredContent)
	if err := json.Unmarshal(raw, &submitted); err != nil || submitted.Task.ID == "" {
		t.Fatalf("decode submitted web task: err=%v raw=%s", err, raw)
	}

	brainTurns := 0
	for {
		statusResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "status", Arguments: map[string]any{"task_id": submitted.Task.ID}})
		if err != nil {
			t.Fatal(err)
		}
		var status struct {
			Status struct {
				State domain.TaskState `json:"state"`
			} `json:"status"`
		}
		rawStatus, _ := json.Marshal(statusResult.StructuredContent)
		if err := json.Unmarshal(rawStatus, &status); err != nil {
			t.Fatal(err)
		}
		switch status.Status.State {
		case domain.TaskInputRequired:
			turnResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "brain_turn", Arguments: map[string]any{"task_id": submitted.Task.ID}})
			if err != nil || turnResult.IsError {
				t.Fatalf("brain_turn failed: err=%v result=%+v", err, turnResult)
			}
			var envelope struct {
				Available bool           `json:"available"`
				Turn      domain.WebTurn `json:"turn"`
			}
			rawTurn, _ := json.Marshal(turnResult.StructuredContent)
			if err := json.Unmarshal(rawTurn, &envelope); err != nil || !envelope.Available || !envelope.Turn.IntegrityValid() {
				t.Fatalf("invalid pending web brain turn: err=%v raw=%s turn=%+v", err, rawTurn, envelope.Turn)
			}
			var req model.TurnRequest
			if err := json.Unmarshal(envelope.Turn.Request, &req); err != nil {
				t.Fatal(err)
			}
			if req.Model != "" || req.ReasoningEffort != "" || len(req.Messages) < 2 || len(req.Tools) == 0 {
				t.Fatalf("web brain request must not own model/reasoning configuration and must preserve context/tools: %+v", req)
			}
			brainTurns++
			var call model.ToolCall
			switch brainTurns {
			case 1:
				args, _ := json.Marshal(map[string]any{"path": "marker.txt", "expected_sha256": "ABSENT", "content": "MAR WEB BRAIN OK\n"})
				call = model.ToolCall{ID: "web-call-write", Name: "write_file", Arguments: string(args)}
			case 2:
				args, _ := json.Marshal(map[string]any{"status": "completed_candidate", "summary": "Created the requested marker through GPT Web brain mode."})
				call = model.ToolCall{ID: "web-call-finish", Name: "finish_task", Arguments: string(args)}
			default:
				t.Fatalf("unexpected extra web brain turn %d", brainTurns)
			}
			respond, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "brain_respond", Arguments: map[string]any{
				"task_id": submitted.Task.ID, "turn_id": envelope.Turn.ID,
				"tool_calls": []any{map[string]any{"id": call.ID, "name": call.Name, "arguments": call.Arguments}}, "finish_reason": "tool_calls",
			}})
			if err != nil || respond.IsError {
				t.Fatalf("brain_respond failed: err=%v result=%+v", err, respond)
			}
		case domain.TaskComplete:
			goto complete
		case domain.TaskBlocked, domain.TaskFailed, domain.TaskCancelled:
			inspection, _ := runtime.Service.Inspect(context.Background(), submitted.Task.ID)
			if inspection.Attempt != nil {
				t.Fatalf("web brain task reached terminal failure %s: attempt=%+v inspection=%+v", status.Status.State, *inspection.Attempt, inspection)
			}
			t.Fatalf("web brain task reached terminal failure %s: %+v", status.Status.State, inspection)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for web brain task; last state=%s", status.Status.State)
		case <-time.After(25 * time.Millisecond):
		}
	}

complete:
	if brainTurns != 2 {
		t.Fatalf("expected exactly two GPT Web brain turns, got %d", brainTurns)
	}
	marker, err := os.ReadFile(filepath.Join(projectRoot, "marker.txt"))
	if err != nil || string(marker) != "MAR WEB BRAIN OK\n" {
		t.Fatalf("unexpected authoritative web brain marker: content=%q err=%v", marker, err)
	}
	if got := strings.TrimSpace(runGitTest(t, projectRoot, "rev-parse", "HEAD")); got == baseRevision {
		t.Fatal("web brain verified candidate was not integrated")
	}
	result, available, err := runtime.Service.Result(ctx, submitted.Task.ID)
	if err != nil || !available || result.IntegrationStatus != "INTEGRATED" || result.Verdict != domain.ResultVerified {
		t.Fatalf("web brain result mismatch: result=%+v available=%v err=%v", result, available, err)
	}
	if result.ResourceSummary.AgentTurns != 2 || result.ResourceSummary.AgentToolCalls != 2 || result.ResourceSummary.ModelTotalTokens <= 0 {
		t.Fatalf("web brain resource accounting mismatch: %+v", result.ResourceSummary)
	}
	inspection, err := runtime.Service.Inspect(ctx, submitted.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Attempt == nil || inspection.Attempt.RunEpoch != 1 || inspection.Attempt.AuthorityState != domain.AttemptPhysicallyTerminated || inspection.BrainTurn != nil {
		t.Fatalf("web brain final authority/turn state mismatch: %+v", inspection)
	}
}

func TestRuntimeE2EWebWaitCapacityAllowsThirdReadyTask(t *testing.T) {
	testsupport.RequireOutsideAppContainer(t)
	if os.Getenv("MAR_RUNTIME_E2E_WORKER") == "1" {
		t.Skip("worker helper process")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	t.Setenv("MAR_RUNTIME_E2E_WORKER", "1")

	goExe := findPortableGo(t)
	goRoot := filepath.Dir(filepath.Dir(goExe))
	goBin := filepath.Dir(goExe)
	dataRoot := filepath.Join(t.TempDir(), "mar-web-wait-data")
	sharedModCache := filepath.Join(dataRoot, "runtime", "gomodcache")
	if err := os.MkdirAll(sharedModCache, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(dataRoot, "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runtime, err := NewRuntime(s, RuntimeConfig{
		DataRoot: dataRoot, Executable: os.Args[0], WorkerArguments: []string{"-test.run=^TestRuntimeE2EWorkerHelper$"},
		Provider:             worker.ProviderConfig{BrainMode: worker.BrainWeb},
		AgentProfile:         agent.Profile{BaseInstructions: "Execute exactly the bounded MAR Goal Contract using the available coding tools."},
		VerificationProfiles: []verification.Profile{{ID: "web-wait-noop", Commands: []verification.Command{{Name: goExe, Args: []string{"test", "./..."}, Cwd: "."}}}},
		SandboxReadPaths:     []string{goRoot, sharedModCache}, WorkerPathEntries: []string{goBin}, GoModuleCache: sharedModCache,
		LeaseDuration: 20 * time.Second, WorkerStopTimeout: 10 * time.Second,
		ResourceGovernor: resourcegov.Config{MaxCPUPercent: 100, MaxMemoryLoadPercent: 100, MaxIOPressurePercent: 100, MinFreeRAMBytes: 1, MinFreeDiskBytes: 1, MaxMARDiskBytes: 1 << 30, MaxHeavyJobs: 2, MaxHeavyJobsPerProject: 1, MaxHeavyJobsInteractive: 2},
		Scheduler:        scheduler.Config{AgingInterval: time.Minute, WorkspaceRAMReservation: 1, WorkspaceDiskReservation: 1},
		Daemon:           DaemonConfig{PollInterval: 25 * time.Millisecond, ControlPollInterval: 25 * time.Millisecond, MaxConcurrentWorkers: 2, MaxPreflightPerTick: 8},
	})
	if err != nil {
		t.Fatal(err)
	}

	type projectFixture struct {
		id   string
		root string
		base string
	}
	createProject := func(id string) projectFixture {
		root := filepath.Join(t.TempDir(), id)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/"+id+"\n\ngo 1.27\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package smoke\n\nfunc Value() int { return 1 }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitTest(t, root, "init", "-b", "main")
		runGitTest(t, root, "config", "user.name", "MAR Web Wait E2E")
		runGitTest(t, root, "config", "user.email", "mar-web-wait@local.invalid")
		runGitTest(t, root, "config", "core.autocrlf", "false")
		runGitTest(t, root, "add", "-A")
		runGitTest(t, root, "commit", "-m", "baseline")
		base := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
		if _, _, err := runtime.Service.RegisterProject(ctx, id, root); err != nil {
			t.Fatal(err)
		}
		return projectFixture{id: id, root: root, base: base}
	}
	projects := []projectFixture{createProject("web-wait-a"), createProject("web-wait-b"), createProject("web-wait-c")}

	daemonCtx, stopDaemon := context.WithCancel(ctx)
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- runtime.Daemon.Run(daemonCtx) }()
	defer func() {
		stopDaemon()
		select {
		case err := <-daemonDone:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("daemon shutdown: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("daemon did not drain Web-wait workers")
		}
	}()

	submit := func(p projectFixture, key, marker string) domain.Task {
		task, _, err := runtime.Service.Submit(ctx, key, domain.GoalContract{
			Goal: "Create " + marker + " containing MAR WEB WAIT OK.", Acceptance: []string{marker + " exists with the requested content"},
			Boundaries: []string{"Only create the requested marker file."}, NonGoals: []string{"No remote Git writes or deployment."},
			ProjectID: p.id, BaseRevision: p.base,
			Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true, NetworkAllowed: false, RemoteGitWrite: false, DeployAllowed: false},
			VerificationProfile: "web-wait-noop", Priority: "P2",
		})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	waitState := func(taskID string, want domain.TaskState, timeout time.Duration) domain.Task {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			task, err := runtime.Service.Status(ctx, taskID)
			if err != nil {
				t.Fatal(err)
			}
			if task.State == want {
				return task
			}
			if task.State == domain.TaskBlocked || task.State == domain.TaskFailed || task.State == domain.TaskCancelled {
				inspection, inspectErr := runtime.Service.Inspect(context.Background(), taskID)
				t.Fatalf("task %s reached terminal state %s before %s: inspection=%+v inspect_err=%v", taskID, task.State, want, inspection, inspectErr)
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("task %s did not reach %s", taskID, want)
		return domain.Task{}
	}
	waitCapacity := func(want int, timeout time.Duration) {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if runtime.Daemon.capacityCount() == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("capacity count did not reach %d; active=%d capacity=%d claims=%d", want, runtime.Daemon.ActiveCount(), runtime.Daemon.capacityCount(), len(runtime.Daemon.governor.Active()))
	}

	taskA := submit(projects[0], "web-wait-capacity-a", "marker-a.txt")
	taskB := submit(projects[1], "web-wait-capacity-b", "marker-b.txt")
	waitState(taskA.ID, domain.TaskInputRequired, 20*time.Second)
	waitState(taskB.ID, domain.TaskInputRequired, 20*time.Second)
	waitCapacity(0, 5*time.Second)
	if runtime.Daemon.ActiveCount() != 2 || len(runtime.Daemon.governor.Active()) != 0 {
		t.Fatalf("two pending Web workers did not yield heavy capacity: active=%d capacity=%d claims=%d", runtime.Daemon.ActiveCount(), runtime.Daemon.capacityCount(), len(runtime.Daemon.governor.Active()))
	}

	taskC := submit(projects[2], "web-wait-capacity-c", "marker-c.txt")
	waitState(taskC.ID, domain.TaskInputRequired, 20*time.Second)
	if runtime.Daemon.ActiveCount() != 3 || runtime.Daemon.capacityCount() != 1 {
		t.Fatalf("third READY task did not obtain yielded execution capacity: active=%d capacity=%d", runtime.Daemon.ActiveCount(), runtime.Daemon.capacityCount())
	}

	turnA, available, err := runtime.Service.PendingWebTurn(ctx, taskA.ID)
	if err != nil || !available {
		t.Fatalf("pending turn A unavailable: turn=%+v available=%v err=%v", turnA, available, err)
	}
	args, _ := json.Marshal(map[string]any{"path": "marker-a.txt", "expected_sha256": "ABSENT", "content": "MAR WEB WAIT OK\n"})
	message := model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "web-wait-write-a", Name: "write_file", Arguments: string(args)}}}
	if _, created, err := runtime.Service.RespondWebTurn(ctx, taskA.ID, turnA.ID, message, "tool_calls"); err != nil || !created {
		t.Fatalf("respond Web turn A: created=%v err=%v", created, err)
	}

	var nextA domain.WebTurn
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		turn, ok, err := runtime.Service.PendingWebTurn(ctx, taskA.ID)
		if err != nil {
			t.Fatal(err)
		}
		if ok && turn.ID != turnA.ID {
			nextA = turn
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if nextA.ID == "" {
		t.Fatal("parked Web worker A did not reacquire capacity and produce its next exact turn")
	}
	workspaceA, err := s.GetWorkspaceByTask(ctx, taskA.ID)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(workspaceA.Path, "marker-a.txt"))
	if err != nil || string(marker) != "MAR WEB WAIT OK\n" {
		t.Fatalf("reacquired worker did not execute bounded mutation: content=%q err=%v", marker, err)
	}
	attemptA, ok, err := s.CurrentAttemptByTask(ctx, taskA.ID)
	if err != nil || !ok || attemptA.RunEpoch != 1 || attemptA.AuthorityState != domain.AttemptActive {
		t.Fatalf("Web waiter resumed with wrong attempt authority: attempt=%+v ok=%v err=%v", attemptA, ok, err)
	}
	statusA, err := runtime.Service.Status(ctx, taskA.ID)
	if err != nil || statusA.RunEpoch != 1 || statusA.State != domain.TaskInputRequired {
		t.Fatalf("Web waiter resumed into wrong durable task state: task=%+v err=%v", statusA, err)
	}
}

func TestRuntimeE2EWorkerHelper(t *testing.T) {
	if os.Getenv("MAR_RUNTIME_E2E_WORKER") != "1" {
		t.Skip("not running as MAR worker helper")
	}
	if err := worker.RunChild(context.Background(), os.Stdin, os.Stdout); err != nil {
		t.Fatal(err)
	}
}

func runGitTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	git := testsupport.RequireExecutable(t, "git")
	cmd := exec.Command(git, append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func findPortableGo(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for root := filepath.Clean(cwd); ; root = filepath.Dir(root) {
		candidate := filepath.Join(root, ".mar", "runtime", "go-portable", "go", "bin", "go.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
	}
	if path, err := exec.LookPath("go"); err == nil {
		return path
	}
	t.Fatal("Go executable not found for MAR runtime E2E test")
	return ""
}
