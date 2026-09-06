//go:build windows

package main

import (
	"bytes"
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
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mar/internal/domain"
	"mar/internal/service"
	"mar/internal/store"
	"mar/internal/worker"
)

func TestAcceptanceT7CLIStdioDisconnectLetsActiveWorkerReachSafeTerminal(t *testing.T) {
	if os.Getenv("MAR_T7_CLI_HELPER") == "1" {
		t.Skip("CLI helper process")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root := t.TempDir()
	projectRoot := filepath.Join(root, "project")
	dataRoot := filepath.Join(root, "mar-data")
	dbPath := filepath.Join(dataRoot, "mar.db")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/mar-t7\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "main.go"), []byte("package t7\n\nfunc Value() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t7Git(t, projectRoot, "init", "-b", "main")
	t7Git(t, projectRoot, "config", "user.name", "MAR T7")
	t7Git(t, projectRoot, "config", "user.email", "mar-t7@local.invalid")
	t7Git(t, projectRoot, "add", "-A")
	t7Git(t, projectRoot, "commit", "-m", "baseline")
	base := strings.TrimSpace(t7GitOut(t, projectRoot, "rev-parse", "HEAD"))

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.NewTaskService(db).RegisterProject(ctx, "t7-cli-project", projectRoot); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	providerEntered := make(chan struct{})
	releaseProvider := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer t7-key" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		_, _ = io.ReadAll(r.Body)
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
		arguments, _ := json.Marshal(map[string]any{"status": "blocked", "summary": "client disconnected; task stopped at a safe terminal boundary"})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "t7-response",
			"model":   "t7-model",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "t7-finish", "type": "function", "function": map[string]any{"name": "finish_task", "arguments": string(arguments)}}}}, "finish_reason": "tool_calls"}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
	defer provider.Close()

	goExe := t7PortableGo(t)
	var stderr bytes.Buffer
	helper := exec.Command(os.Args[0])
	helper.Env = append(os.Environ(),
		"MAR_T7_CLI_HELPER=1",
		"MAR_T7_DB="+dbPath,
		"MAR_T7_DATA_ROOT="+dataRoot,
		"MAR_T7_PROVIDER="+provider.URL+"/v1",
		"MAR_T7_GO="+goExe,
		"MAR_T7_CLI_KEY=t7-key",
	)
	helper.Stderr = &stderr
	transport := &mcp.CommandTransport{Command: helper, TerminateDuration: 10 * time.Second}
	client := mcp.NewClient(&mcp.Implementation{Name: "mar-t7-cli-acceptance", Version: "1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect CLI stdio transport: %v\n%s", err, stderr.String())
	}

	submit, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "submit", Arguments: map[string]any{
		"idempotency_key": "t7-cli-submit",
		"contract": map[string]any{
			"goal":                 "Safely reach a terminal boundary even if the ChatWeb/MCP client disconnects.",
			"acceptance":           []string{"task remains durable after client disconnect"},
			"boundaries":           []string{"do not mutate authoritative project files"},
			"non_goals":            []string{"no deployment"},
			"project_id":           "t7-cli-project",
			"base_revision":        base,
			"authority":            map[string]any{"local_file_write": true, "local_git_write": true, "network_allowed": false, "remote_git_write": false, "deploy_allowed": false},
			"verification_profile": "go-standard",
			"priority":             "P2",
		},
	}})
	if err != nil || submit.IsError {
		_ = session.Close()
		t.Fatalf("submit via CLI MCP failed: err=%v content=%+v stderr=%s", err, submit.Content, stderr.String())
	}
	var submitted struct {
		Task domain.Task `json:"task"`
	}
	raw, _ := json.Marshal(submit.StructuredContent)
	if err := json.Unmarshal(raw, &submitted); err != nil || submitted.Task.ID == "" {
		_ = session.Close()
		t.Fatalf("decode submitted task: err=%v raw=%s", err, raw)
	}

	select {
	case <-providerEntered:
	case <-ctx.Done():
		state := "unknown"
		if diagnosticDB, openErr := store.Open(dbPath); openErr == nil {
			if diagnosticTask, statusErr := service.NewTaskService(diagnosticDB).Status(context.Background(), submitted.Task.ID); statusErr == nil {
				state = string(diagnosticTask.State)
			} else {
				state = "status-error:" + statusErr.Error()
			}
			_ = diagnosticDB.Close()
		} else {
			state = "db-open-error:" + openErr.Error()
		}
		_ = session.Close()
		t.Fatalf("worker never reached active provider turn: %v state=%s stderr=%s", ctx.Err(), state, stderr.String())
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- session.Close() }()
	time.Sleep(100 * time.Millisecond)
	close(releaseProvider)
	select {
	case err := <-closeDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("close disconnected CLI client: %v stderr=%s", err, stderr.String())
		}
	case <-ctx.Done():
		t.Fatalf("CLI did not drain active worker after stdio EOF: %v stderr=%s", ctx.Err(), stderr.String())
	}

	reopened, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	svc := service.NewTaskService(reopened)
	final, err := svc.Status(context.Background(), submitted.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != domain.TaskBlocked {
		t.Fatalf("stdio disconnect did not preserve safe durable terminal state: %s stderr=%s", final.State, stderr.String())
	}
	attempt, ok, err := reopened.CurrentAttemptByTask(context.Background(), final.ID)
	if err != nil || !ok {
		t.Fatalf("stdio disconnect lost attempt: ok=%v err=%v", ok, err)
	}
	if attempt.AuthorityState != domain.AttemptPhysicallyTerminated {
		t.Fatalf("CLI exited before worker physical termination was durable: %+v", attempt)
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("MAR_T7_CLI_HELPER") == "1" {
		var err error
		if len(os.Args) > 1 && os.Args[1] == "worker-run" {
			err = worker.RunChild(context.Background(), os.Stdin, os.Stdout)
		} else {
			err = run(context.Background(), []string{
				"mcp-stdio",
				"-db", os.Getenv("MAR_T7_DB"),
				"-data-root", os.Getenv("MAR_T7_DATA_ROOT"),
				"-provider-base-url", os.Getenv("MAR_T7_PROVIDER"),
				"-api-key-env", "MAR_T7_CLI_KEY",
				"-model", "t7-model",
				"-go", os.Getenv("MAR_T7_GO"),
				"-max-workers", "1",
			})
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "T7 helper:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func t7PortableGo(t *testing.T) string {
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

func t7Git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func t7GitOut(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
