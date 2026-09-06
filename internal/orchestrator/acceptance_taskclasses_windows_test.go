//go:build windows

package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

type acceptanceToolStep struct {
	name string
	args map[string]any
}

type acceptanceTaskClass struct {
	id                    string
	goal                  string
	acceptance            []string
	files                 map[string]string
	steps                 []acceptanceToolStep
	wantFiles             map[string]string
	wantChanged           []string
	mustObserveFailedTest bool
}

func TestAcceptanceT1ToT4TaskClasses(t *testing.T) {
	if os.Getenv("MAR_RUN_SELF_HOSTING_ACCEPTANCE") != "1" {
		t.Skip("set MAR_RUN_SELF_HOSTING_ACCEPTANCE=1 to run the heavy self-hosting acceptance benchmark")
	}
	if os.Getenv("MAR_RUNTIME_E2E_WORKER") == "1" {
		t.Skip("worker helper process")
	}

	for _, scenario := range acceptanceTaskClasses() {
		t.Run(scenario.id, func(t *testing.T) {
			runAcceptanceTaskClass(t, scenario)
		})
	}
}

func acceptanceTaskClasses() []acceptanceTaskClass {
	tinyCalc := "package tiny\n\nfunc Value() int { return 1 }\n"
	mediumSum := "package medium\n\nfunc Sum(a, b int) int { return a - b }\n"
	mediumFormat := "package medium\n\nimport \"fmt\"\n\nfunc Label(v int) string { return fmt.Sprintf(\"value:%d\", v) }\n"
	largeDomain := "package feature\n\ntype Record struct {\n\tValue int\n}\n"
	largeTransform := "package feature\n\nfunc Transform(r Record) int { return r.Value }\n"
	largeAPI := "package feature\n\nfunc Result(v int) int { return Transform(Record{Value: v}) }\n"
	refactorLibrary := "package normalize\n\nimport \"strings\"\n\nfunc legacyNormalize(s string) string { return strings.TrimSpace(s) }\n"
	refactorA := "package normalize\n\nfunc NormalizeA(s string) string { return legacyNormalize(s) }\n"
	refactorB := "package normalize\n\nfunc NormalizeB(s string) string { return legacyNormalize(s) }\n"

	return []acceptanceTaskClass{
		{
			id:         "T1-tiny-fix",
			goal:       "Fix Value so the localized unit test passes without changing unrelated files.",
			acceptance: []string{"Value returns 2 and the Go verification profile passes"},
			files: map[string]string{
				"go.mod":       "module example.com/mar-accept-t1\n\ngo 1.27\n",
				"calc.go":      tinyCalc,
				"calc_test.go": "package tiny\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) { if Value() != 2 { t.Fatal(\"want 2\") } }\n",
			},
			steps: []acceptanceToolStep{
				{name: "replace_exact", args: map[string]any{"path": "calc.go", "expected_sha256": acceptanceSHA(tinyCalc), "search": "return 1", "replacement": "return 2", "expected_count": 1}},
				acceptanceFinish("tiny fix ready"),
			},
			wantFiles:   map[string]string{"calc.go": "package tiny\n\nfunc Value() int { return 2 }\n"},
			wantChanged: []string{"calc.go"},
		},
		{
			id:         "T2-medium-bug-repair-loop",
			goal:       "Repair the arithmetic and label defects, using the failing test as evidence before declaring a candidate.",
			acceptance: []string{"Sum and Label satisfy the targeted regression test and the full Go verification profile passes"},
			files: map[string]string{
				"go.mod":      "module example.com/mar-accept-t2\n\ngo 1.27\n",
				"sum.go":      mediumSum,
				"format.go":   mediumFormat,
				"app_test.go": "package medium\n\nimport \"testing\"\n\nfunc TestBehavior(t *testing.T) {\n\tif Sum(2, 3) != 5 { t.Fatal(\"bad sum\") }\n\tif Label(5) != \"sum=5\" { t.Fatal(\"bad label\") }\n}\n",
			},
			steps: []acceptanceToolStep{
				{name: "run_command", args: map[string]any{"name": "go", "args": []string{"test", "./..."}, "cwd": "."}},
				{name: "replace_exact", args: map[string]any{"path": "sum.go", "expected_sha256": acceptanceSHA(mediumSum), "search": "a - b", "replacement": "a + b", "expected_count": 1}},
				{name: "replace_exact", args: map[string]any{"path": "format.go", "expected_sha256": acceptanceSHA(mediumFormat), "search": "value:%d", "replacement": "sum=%d", "expected_count": 1}},
				{name: "run_command", args: map[string]any{"name": "go", "args": []string{"test", "./..."}, "cwd": "."}},
				acceptanceFinish("medium repair ready"),
			},
			wantFiles: map[string]string{
				"sum.go":    "package medium\n\nfunc Sum(a, b int) int { return a + b }\n",
				"format.go": "package medium\n\nimport \"fmt\"\n\nfunc Label(v int) string { return fmt.Sprintf(\"sum=%d\", v) }\n",
			},
			wantChanged:           []string{"format.go", "sum.go"},
			mustObserveFailedTest: true,
		},
		{
			id:         "T3-large-multi-file",
			goal:       "Add a factor-aware transformation across the domain, transform, and API layers so Result(3) returns 6.",
			acceptance: []string{"The cross-module implementation changes all required layers and the Go verification profile passes"},
			files: map[string]string{
				"go.mod":       "module example.com/mar-accept-t3\n\ngo 1.27\n",
				"domain.go":    largeDomain,
				"transform.go": largeTransform,
				"api.go":       largeAPI,
				"api_test.go":  "package feature\n\nimport \"testing\"\n\nfunc TestResult(t *testing.T) { if Result(3) != 6 { t.Fatal(\"want 6\") } }\n",
			},
			steps: []acceptanceToolStep{
				{name: "replace_exact", args: map[string]any{"path": "domain.go", "expected_sha256": acceptanceSHA(largeDomain), "search": "\tValue int\n", "replacement": "\tValue  int\n\tFactor int\n", "expected_count": 1}},
				{name: "replace_exact", args: map[string]any{"path": "transform.go", "expected_sha256": acceptanceSHA(largeTransform), "search": "return r.Value", "replacement": "return r.Value * r.Factor", "expected_count": 1}},
				{name: "replace_exact", args: map[string]any{"path": "api.go", "expected_sha256": acceptanceSHA(largeAPI), "search": "Record{Value: v}", "replacement": "Record{Value: v, Factor: 2}", "expected_count": 1}},
				{name: "run_command", args: map[string]any{"name": "go", "args": []string{"test", "./..."}, "cwd": "."}},
				acceptanceFinish("large multi-file change ready"),
			},
			wantFiles: map[string]string{
				"domain.go":    "package feature\n\ntype Record struct {\n\tValue  int\n\tFactor int\n}\n",
				"transform.go": "package feature\n\nfunc Transform(r Record) int { return r.Value * r.Factor }\n",
				"api.go":       "package feature\n\nfunc Result(v int) int { return Transform(Record{Value: v, Factor: 2}) }\n",
			},
			wantChanged: []string{"api.go", "domain.go", "transform.go"},
		},
		{
			id:         "T4-deep-refactor",
			goal:       "Rename the legacy normalization primitive across all call sites while preserving behavior.",
			acceptance: []string{"No legacyNormalize call sites remain and all behavior-preserving verification passes"},
			files: map[string]string{
				"go.mod":            "module example.com/mar-accept-t4\n\ngo 1.27\n",
				"library.go":        refactorLibrary,
				"consumer_a.go":     refactorA,
				"consumer_b.go":     refactorB,
				"normalize_test.go": "package normalize\n\nimport \"testing\"\n\nfunc TestNormalize(t *testing.T) {\n\tif NormalizeA(\" x \" ) != \"x\" { t.Fatal(\"A\") }\n\tif NormalizeB(\" y \" ) != \"y\" { t.Fatal(\"B\") }\n}\n",
			},
			steps: []acceptanceToolStep{
				{name: "run_command", args: map[string]any{"name": "go", "args": []string{"test", "./..."}, "cwd": "."}},
				{name: "replace_exact", args: map[string]any{"path": "library.go", "expected_sha256": acceptanceSHA(refactorLibrary), "search": "legacyNormalize", "replacement": "normalizedName", "expected_count": 1}},
				{name: "replace_exact", args: map[string]any{"path": "consumer_a.go", "expected_sha256": acceptanceSHA(refactorA), "search": "legacyNormalize", "replacement": "normalizedName", "expected_count": 1}},
				{name: "replace_exact", args: map[string]any{"path": "consumer_b.go", "expected_sha256": acceptanceSHA(refactorB), "search": "legacyNormalize", "replacement": "normalizedName", "expected_count": 1}},
				{name: "run_command", args: map[string]any{"name": "go", "args": []string{"test", "./..."}, "cwd": "."}},
				acceptanceFinish("refactor ready"),
			},
			wantFiles: map[string]string{
				"library.go":    "package normalize\n\nimport \"strings\"\n\nfunc normalizedName(s string) string { return strings.TrimSpace(s) }\n",
				"consumer_a.go": "package normalize\n\nfunc NormalizeA(s string) string { return normalizedName(s) }\n",
				"consumer_b.go": "package normalize\n\nfunc NormalizeB(s string) string { return normalizedName(s) }\n",
			},
			wantChanged: []string{"consumer_a.go", "consumer_b.go", "library.go"},
		},
	}
}

func acceptanceFinish(summary string) acceptanceToolStep {
	return acceptanceToolStep{name: "finish_task", args: map[string]any{"status": "completed_candidate", "summary": summary}}
}

func acceptanceSHA(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func runAcceptanceTaskClass(t *testing.T, scenario acceptanceTaskClass) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	started := time.Now()
	projectRoot := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range scenario.files {
		full := filepath.Join(projectRoot, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitTest(t, projectRoot, "init", "-b", "main")
	runGitTest(t, projectRoot, "config", "user.name", "MAR Acceptance")
	runGitTest(t, projectRoot, "config", "user.email", "mar-acceptance@local.invalid")
	runGitTest(t, projectRoot, "config", "core.autocrlf", "false")
	runGitTest(t, projectRoot, "add", "-A")
	runGitTest(t, projectRoot, "commit", "-m", "baseline")
	baseRevision := strings.TrimSpace(runGitTest(t, projectRoot, "rev-parse", "HEAD"))

	apiKeyEnv := "MAR_ACCEPTANCE_MODEL_API_KEY"
	t.Setenv(apiKeyEnv, "acceptance-key")
	t.Setenv("MAR_RUNTIME_E2E_WORKER", "1")
	requestBodies := make([]string, 0, len(scenario.steps))
	var requestMu sync.Mutex
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer acceptance-key" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		requestMu.Lock()
		requestBodies = append(requestBodies, string(body))
		index := len(requestBodies) - 1
		requestMu.Unlock()
		if index >= len(scenario.steps) {
			http.Error(w, "script exhausted", http.StatusInternalServerError)
			return
		}
		step := scenario.steps[index]
		t.Logf("%s provider_step=%d tool=%s", scenario.id, index+1, step.name)
		arguments, _ := json.Marshal(step.args)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    fmt.Sprintf("%s-response-%d", scenario.id, index+1),
			"model": "acceptance-model",
			"choices": []any{map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []any{map[string]any{
						"id":   fmt.Sprintf("%s-call-%d", scenario.id, index+1),
						"type": "function",
						"function": map[string]any{
							"name":      step.name,
							"arguments": string(arguments),
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110},
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
		DataRoot:        dataRoot,
		Executable:      os.Args[0],
		WorkerArguments: []string{"-test.run=^TestRuntimeE2EWorkerHelper$"},
		Provider: worker.ProviderConfig{
			BaseURL:        provider.URL + "/v1",
			APIKeyEnv:      apiKeyEnv,
			RequestTimeout: 10 * time.Second,
		},
		AgentProfile: agent.Profile{
			Model:            "acceptance-model",
			ReasoningEffort:  "high",
			BaseInstructions: "Execute exactly the bounded acceptance Goal Contract using the available coding tools.",
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
		Scheduler: scheduler.Config{AgingInterval: time.Minute, WorkspaceRAMReservation: 1, WorkspaceDiskReservation: 1},
		Daemon: DaemonConfig{
			PollInterval:         250 * time.Millisecond,
			ControlPollInterval:  200 * time.Millisecond,
			MaxConcurrentWorkers: 1,
			MaxPreflightPerTick:  4,
			ErrorSink: func(err error) {
				t.Logf("%s daemon_error=%v", scenario.id, err)
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	projectID := "acceptance-" + strings.ToLower(strings.ReplaceAll(scenario.id, "_", "-"))
	if _, _, err := runtime.Service.RegisterProject(ctx, projectID, projectRoot); err != nil {
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
	client := mcp.NewClient(&mcp.Implementation{Name: "mar-acceptance", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	submit, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "submit", Arguments: map[string]any{
		"idempotency_key": "acceptance-submit-" + scenario.id,
		"contract": map[string]any{
			"goal":                 scenario.goal,
			"acceptance":           scenario.acceptance,
			"boundaries":           []string{"Modify only files necessary for this acceptance scenario."},
			"non_goals":            []string{"No remote Git writes or deployment."},
			"project_id":           projectID,
			"base_revision":        baseRevision,
			"authority":            map[string]any{"local_file_write": true, "local_git_write": true, "network_allowed": false, "remote_git_write": false, "deploy_allowed": false},
			"verification_profile": "go-standard",
			"priority":             "P2",
		},
	}})
	if err != nil || submit.IsError {
		t.Fatalf("MCP submit failed: err=%v content=%+v", err, submit.Content)
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
		t.Fatalf("unexpected submit result: %s", raw)
	}

	var final domain.Task
	for {
		final, err = runtime.Service.Status(ctx, submitted.Task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if final.State == domain.TaskComplete || final.State == domain.TaskBlocked || final.State == domain.TaskFailed || final.State == domain.TaskCancelled {
			break
		}
		select {
		case <-ctx.Done():
			inspection, _ := runtime.Service.Inspect(context.Background(), submitted.Task.ID)
			requestMu.Lock()
			providerCalls := len(requestBodies)
			requestMu.Unlock()
			t.Fatalf("timed out waiting for %s; last state=%s provider_calls=%d attempt=%+v checkpoint=%+v", scenario.id, final.State, providerCalls, inspection.Attempt, inspection.Checkpoint)
		case <-time.After(40 * time.Millisecond):
		}
	}
	if final.State != domain.TaskComplete {
		inspection, _ := runtime.Service.Inspect(context.Background(), submitted.Task.ID)
		t.Fatalf("%s did not complete: state=%s attempt=%+v result=%+v evidence=%+v", scenario.id, final.State, inspection.Attempt, inspection.Result, inspection.Evidence)
	}

	resultTool, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "result", Arguments: map[string]any{"task_id": submitted.Task.ID}})
	if err != nil || resultTool.IsError {
		t.Fatalf("MCP result failed: err=%v result=%+v", err, resultTool)
	}
	var resultPayload struct {
		Available bool              `json:"available"`
		Result    domain.TaskResult `json:"result"`
	}
	raw, _ = json.Marshal(resultTool.StructuredContent)
	if err := json.Unmarshal(raw, &resultPayload); err != nil {
		t.Fatal(err)
	}
	if !resultPayload.Available || resultPayload.Result.IntegrationStatus != "INTEGRATED" || resultPayload.Result.Verdict != domain.ResultVerified {
		t.Fatalf("%s produced unexpected result: %s", scenario.id, raw)
	}
	if strings.TrimSpace(runGitTest(t, projectRoot, "rev-parse", "HEAD")) == baseRevision {
		t.Fatalf("%s did not advance authoritative head", scenario.id)
	}
	for path, want := range scenario.wantFiles {
		got, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("%s final %s mismatch:\nwant=%q\ngot =%q", scenario.id, path, want, got)
		}
	}
	gotChanged := append([]string(nil), resultPayload.Result.ChangedAreas...)
	wantChanged := append([]string(nil), scenario.wantChanged...)
	sort.Strings(gotChanged)
	sort.Strings(wantChanged)
	if strings.Join(gotChanged, "\n") != strings.Join(wantChanged, "\n") {
		t.Fatalf("%s changed-area mismatch: got=%v want=%v", scenario.id, gotChanged, wantChanged)
	}
	resources := resultPayload.Result.ResourceSummary
	if resources.AgentTurns != len(scenario.steps) || resources.AgentToolCalls != len(scenario.steps) || resources.ModelTotalTokens != int64(110*len(scenario.steps)) {
		t.Fatalf("%s resource metrics mismatch: %+v steps=%d", scenario.id, resources, len(scenario.steps))
	}
	if scenario.mustObserveFailedTest {
		observed := false
		requestMu.Lock()
		bodies := append([]string(nil), requestBodies...)
		requestMu.Unlock()
		for _, body := range bodies[1:] {
			if strings.Contains(body, "sandboxed command exited with code 1") || strings.Contains(body, "exit code 1") || strings.Contains(body, "bad sum") {
				observed = true
				break
			}
		}
		if !observed {
			t.Fatalf("%s did not feed failing-test evidence into the repair loop", scenario.id)
		}
	}
	t.Logf("%s PASS wall=%s external_mcp_calls=2 model_turns=%d tool_calls=%d tokens=%d", scenario.id, time.Since(started).Round(time.Millisecond), resources.AgentTurns, resources.AgentToolCalls, resources.ModelTotalTokens)
}
