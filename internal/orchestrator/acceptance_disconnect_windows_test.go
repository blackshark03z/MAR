//go:build windows

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestAcceptanceT7ClientDisconnectDoesNotCancelActiveTask(t *testing.T) {
	if os.Getenv("MAR_RUNTIME_E2E_WORKER") == "1" {
		t.Skip("worker helper process")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	projectRoot := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/mar-t7\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "main.go"), []byte("package t7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, projectRoot, "init", "-b", "main")
	runGitTest(t, projectRoot, "config", "user.name", "MAR T7")
	runGitTest(t, projectRoot, "config", "user.email", "mar-t7@local.invalid")
	runGitTest(t, projectRoot, "add", "-A")
	runGitTest(t, projectRoot, "commit", "-m", "baseline")
	baseRevision := strings.TrimSpace(runGitTest(t, projectRoot, "rev-parse", "HEAD"))

	const apiKeyEnv = "MAR_T7_MODEL_API_KEY"
	t.Setenv(apiKeyEnv, "t7-key")
	t.Setenv("MAR_RUNTIME_E2E_WORKER", "1")
	providerEntered := make(chan struct{})
	releaseProvider := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		select {
		case <-providerEntered:
		default:
			close(providerEntered)
		}
		select {
		case <-releaseProvider:
		case <-r.Context().Done():
			return
		}
		arguments, _ := json.Marshal(map[string]any{"status": "blocked", "summary": "safe block after client disconnect"})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "t7-response", "model": "t7-model",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "t7-finish", "type": "function", "function": map[string]any{"name": "finish_task", "arguments": string(arguments)}}}}, "finish_reason": "tool_calls"}},
			"usage":   map[string]any{"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110},
		})
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
	s, err := store.Open(filepath.Join(dataRoot, "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runtime, err := NewRuntime(s, RuntimeConfig{
		DataRoot: dataRoot, Executable: os.Args[0], WorkerArguments: []string{"-test.run=^TestRuntimeE2EWorkerHelper$"},
		Provider:             worker.ProviderConfig{BaseURL: provider.URL + "/v1", APIKeyEnv: apiKeyEnv, RequestTimeout: 30 * time.Second},
		AgentProfile:         agent.Profile{Model: "t7-model", ReasoningEffort: "high", BaseInstructions: "Finish safely within the immutable goal."},
		VerificationProfiles: []verification.Profile{{ID: "go-standard", Commands: []verification.Command{{Name: goExe, Args: []string{"test", "./..."}, Cwd: "."}}}},
		SandboxReadPaths:     []string{goRoot, sharedModCache}, WorkerPathEntries: []string{goBin}, GoModuleCache: sharedModCache,
		LeaseDuration: 20 * time.Second, WorkerStopTimeout: 10 * time.Second,
		ResourceGovernor: resourcegov.Config{MaxCPUPercent: 100, MaxMemoryLoadPercent: 100, MaxIOPressurePercent: 100, MinFreeRAMBytes: 1, MinFreeDiskBytes: 1, MaxMARDiskBytes: 1 << 30, MaxHeavyJobs: 1, MaxHeavyJobsPerProject: 1, MaxHeavyJobsInteractive: 1},
		Scheduler:        scheduler.Config{AgingInterval: time.Minute, WorkspaceRAMReservation: 1, WorkspaceDiskReservation: 1},
		Daemon:           DaemonConfig{PollInterval: 25 * time.Millisecond, ControlPollInterval: 25 * time.Millisecond, MaxConcurrentWorkers: 1, MaxPreflightPerTick: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.Service.RegisterProject(ctx, "t7-project", projectRoot); err != nil {
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
	client := mcp.NewClient(&mcp.Implementation{Name: "mar-t7-client", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	submit, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "submit", Arguments: map[string]any{
		"idempotency_key": "t7-submit",
		"contract":        map[string]any{"goal": "Remain coherent if the client disconnects during execution.", "acceptance": []string{"task safely blocks after disconnect"}, "boundaries": []string{"Do not modify source; only demonstrate lifecycle continuity."}, "non_goals": []string{"No remote Git writes or deployment."}, "project_id": "t7-project", "base_revision": baseRevision, "authority": map[string]any{"local_file_write": true, "local_git_write": true, "network_allowed": false, "remote_git_write": false, "deploy_allowed": false}, "verification_profile": "go-standard", "priority": "P2"},
	}})
	if err != nil || submit.IsError {
		t.Fatalf("submit failed: err=%v content=%+v", err, submit.Content)
	}
	var submitted struct {
		Task domain.Task `json:"task"`
	}
	raw, _ := json.Marshal(submit.StructuredContent)
	if err := json.Unmarshal(raw, &submitted); err != nil {
		t.Fatal(err)
	}
	if submitted.Task.ID == "" {
		t.Fatalf("submit lost task identity: %s", raw)
	}
	select {
	case <-providerEntered:
	case <-ctx.Done():
		t.Fatal("worker never reached active model turn")
	}
	if err := clientSession.Close(); err != nil {
		t.Fatal(err)
	}
	close(releaseProvider)

	for {
		task, err := runtime.Service.Status(ctx, submitted.Task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if task.State == domain.TaskBlocked {
			inspection, err := runtime.Service.Inspect(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if inspection.Attempt == nil || inspection.Attempt.AuthorityState != domain.AttemptPhysicallyTerminated {
				t.Fatalf("client disconnect left mutation-capable worker: attempt=%+v", inspection.Attempt)
			}
			return
		}
		if task.State == domain.TaskComplete || task.State == domain.TaskFailed || task.State == domain.TaskCancelled {
			t.Fatalf("client disconnect produced unexpected terminal state %s", task.State)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("task lost progress after client disconnect; last_state=%s", task.State)
		case <-time.After(25 * time.Millisecond):
		}
	}
}
