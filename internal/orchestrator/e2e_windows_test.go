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
	"mar/internal/resourcegov"
	"mar/internal/scheduler"
	"mar/internal/store"
	"mar/internal/verification"
	"mar/internal/worker"
)

func TestRuntimeE2EMCPSubmitWorkerVerifyIntegrate(t *testing.T) {
	if os.Getenv("MAR_RUNTIME_E2E_WORKER") == "1" {
		t.Skip("worker helper process")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer e2e-key" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		call := modelCalls.Add(1)
		var toolName, arguments, callID string
		switch call {
		case 1:
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
				{Name: goExe, Args: []string{"test", "-count=1", "./..."}, Cwd: "."},
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
			"goal":                 "Create marker.txt containing MAR E2E OK.",
			"acceptance":           []string{"marker.txt exists in the authoritative project with the requested content"},
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
		Created bool        `json:"created"`
		Task    domain.Task `json:"task"`
	}
	raw, _ := json.Marshal(submit.StructuredContent)
	if err := json.Unmarshal(raw, &submitted); err != nil {
		t.Fatal(err)
	}
	if !submitted.Created || submitted.Task.ID == "" {
		t.Fatalf("unexpected MCP submit result: %s", raw)
	}

	var final domain.Task
	for {
		statusResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "status", Arguments: map[string]any{"task_id": submitted.Task.ID}})
		if err != nil {
			t.Fatal(err)
		}
		var status struct {
			Status struct {
				Task domain.Task `json:"task"`
			} `json:"status"`
		}
		raw, _ := json.Marshal(statusResult.StructuredContent)
		if err := json.Unmarshal(raw, &status); err != nil {
			t.Fatal(err)
		}
		final = status.Status.Task
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
	if modelCalls.Load() < 2 {
		t.Fatalf("worker did not complete expected model/tool loop: calls=%d", modelCalls.Load())
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
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
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
