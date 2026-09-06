//go:build windows

package worker

import (
	"context"
	"encoding/json"
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

	"mar/internal/agent"
	"mar/internal/domain"
	"mar/internal/processctl"
	"mar/internal/service"
	"mar/internal/store"
)

func TestProcessRunnerRealWorkerReturnsAfterFailingGoTool(t *testing.T) {
	if os.Getenv("MAR_REAL_WORKER_FAIL_HELPER") == "1" {
		t.Skip("worker helper process")
	}
	source := t.TempDir()
	files := map[string]string{
		"go.mod":      "module realworkerfail\n\ngo 1.27\n",
		"sum.go":      "package medium\n\nfunc Sum(a, b int) int { return a - b }\n",
		"format.go":   "package medium\n\nimport \"fmt\"\n\nfunc Label(v int) string { return fmt.Sprintf(\"value:%d\", v) }\n",
		"app_test.go": "package medium\n\nimport \"testing\"\n\nfunc TestBehavior(t *testing.T) {\n\tif Sum(2, 3) != 5 { t.Fatal(\"bad sum\") }\n\tif Label(5) != \"sum=5\" { t.Fatal(\"bad label\") }\n}\n",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(source, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runWorkerGit(t, source, "init", "-b", "main")
	runWorkerGit(t, source, "config", "user.name", "MAR Worker Test")
	runWorkerGit(t, source, "config", "user.email", "mar-worker-test@local.invalid")
	runWorkerGit(t, source, "add", "-A")
	runWorkerGit(t, source, "commit", "-m", "baseline")
	base := strings.TrimSpace(runWorkerGit(t, source, "rev-parse", "HEAD"))
	dataRoot := t.TempDir()
	contract := domain.GoalContract{
		Goal:       "observe failing test through real worker boundary",
		Acceptance: []string{"failing test evidence reaches the next model turn"},
		Boundaries: []string{"do not mutate files"},
		ProjectID:  "real-worker-project", BaseRevision: base,
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
		VerificationProfile: "go-standard", Priority: "P2",
	}
	db, err := store.Open(filepath.Join(dataRoot, "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.NewTaskService(db)
	if _, _, err := svc.RegisterProject(context.Background(), contract.ProjectID, source); err != nil {
		t.Fatal(err)
	}
	task, _, err := svc.Submit(context.Background(), "real-worker-submit", contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(context.Background(), task.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	attempt, err := svc.BeginAttempt(context.Background(), task.ID, "worker", "supervisor", 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	task, err = svc.Status(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(dataRoot, "workspaces", "project", task.ID)
	if err := os.MkdirAll(filepath.Dir(workspace), 0o755); err != nil {
		t.Fatal(err)
	}
	runWorkerGit(t, source, "worktree", "add", "--detach", workspace, base)
	defer exec.Command("git", "-C", source, "worktree", "remove", "--force", workspace).Run()

	goExe := workerPortableGo(t)
	goRoot := filepath.Dir(filepath.Dir(goExe))
	goBin := filepath.Dir(goExe)
	sharedCache := filepath.Join(dataRoot, "runtime", "gomodcache")
	if err := os.MkdirAll(sharedCache, 0o755); err != nil {
		t.Fatal(err)
	}
	const apiKeyEnv = "MAR_REAL_WORKER_FAIL_API_KEY"
	t.Setenv(apiKeyEnv, "worker-key")
	var providerCalls atomic.Int32
	var sawFailure atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		call := providerCalls.Add(1)
		var name string
		var args any
		if call == 1 {
			name = "run_command"
			args = map[string]any{"name": "go", "args": []string{"test", "./..."}, "cwd": "."}
		} else {
			sawFailure.Store(strings.Contains(string(body), "sandboxed command exited with code 1") || strings.Contains(string(body), "bad sum"))
			name = "finish_task"
			args = map[string]any{"status": "blocked", "summary": "failing test evidence returned", "blocker": "diagnostic test complete"}
		}
		encoded, _ := json.Marshal(args)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": fmt.Sprintf("worker-response-%d", call), "model": "worker-test-model",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": fmt.Sprintf("call-%d", call), "type": "function", "function": map[string]any{"name": name, "arguments": string(encoded)}}}}, "finish_reason": "tool_calls"}},
			"usage":   map[string]any{"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110},
		})
	}))
	defer provider.Close()

	start := StartRequest{
		Task: task, Attempt: attempt, WorkspacePath: workspace,
		Provider:         ProviderConfig{BaseURL: provider.URL + "/v1", APIKeyEnv: apiKeyEnv, RequestTimeout: 10 * time.Second},
		AgentProfile:     agent.Profile{Model: "worker-test-model", ReasoningEffort: "high", BaseInstructions: "Execute only the bounded worker diagnostic goal."},
		AgentConfig:      agent.Config{MaxTurns: 4, MaxToolCalls: 4, MaxToolCallsPerTurn: 1, MaxTotalTokens: 1000, MaxOutputTokensPerTurn: 200, MaxContextBytes: 96 << 10, MaxResumeBytes: 96 << 10, MaxRequestBytes: 512 << 10, MaxAssistantBytes: 96 << 10, MaxObservationBytes: 48 << 10, MaxDuration: 150 * time.Second},
		SandboxReadPaths: []string{goRoot, sharedCache}, GoModuleCache: sharedCache,
	}
	t.Setenv("MAR_REAL_WORKER_FAIL_HELPER", "1")
	t.Setenv("PATH", goBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	runner, err := NewProcessRunner(svc, processctl.NewSupervisor(), ProcessConfig{
		Executable: os.Args[0], Arguments: []string{"-test.run=^TestProcessRunnerRealWorkerHelper$"},
		Environment:   os.Environ(),
		LeaseDuration: 20 * time.Second, StopTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	pollCtx, stopPoll := context.WithCancel(ctx)
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-pollCtx.Done():
				return
			case <-ticker.C:
				_, _ = svc.Status(pollCtx, task.ID)
			}
		}
	}()
	started := time.Now()
	result, proof, err := runner.Run(ctx, start)
	stopPoll()
	<-pollDone
	if err != nil {
		t.Fatalf("real worker boundary failed: duration=%s calls=%d result=%+v proof=%v err=%v", time.Since(started), providerCalls.Load(), result, proof.Valid(), err)
	}
	if result.Status != agent.StatusBlocked || !proof.Valid() {
		t.Fatalf("unexpected real worker terminal result: %+v proof=%v", result, proof.Valid())
	}
	if providerCalls.Load() != 2 || !sawFailure.Load() {
		t.Fatalf("failing tool evidence did not reach second model turn: calls=%d saw_failure=%v", providerCalls.Load(), sawFailure.Load())
	}
}

func TestProcessRunnerRealWorkerHelper(t *testing.T) {
	if os.Getenv("MAR_REAL_WORKER_FAIL_HELPER") != "1" {
		return
	}
	if err := RunChild(context.Background(), os.Stdin, os.Stdout); err != nil {
		t.Fatal(err)
	}
}

func runWorkerGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func workerPortableGo(t *testing.T) string {
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
	t.Fatal("portable Go executable not found")
	return ""
}
