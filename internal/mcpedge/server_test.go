package mcpedge

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mar/internal/domain"
	"mar/internal/service"
)

type fakeBackend struct {
	steerTask string
	steerKey  string
	steer     domain.SteerPayload
}

func (f *fakeBackend) Submit(_ context.Context, key string, contract domain.GoalContract) (domain.Task, bool, error) {
	return domain.Task{ID: "task-submit", IdempotencyKey: key, Contract: contract, ContractHash: "hash", State: domain.TaskSubmitted, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}, true, nil
}
func (f *fakeBackend) StatusSnapshot(_ context.Context, taskID string) (service.TaskStatusSnapshot, error) {
	return service.TaskStatusSnapshot{Task: domain.Task{ID: taskID, State: domain.TaskRunning}}, nil
}
func (f *fakeBackend) Steer(_ context.Context, taskID, key string, payload domain.SteerPayload) (domain.TaskControl, bool, error) {
	f.steerTask, f.steerKey, f.steer = taskID, key, payload
	return domain.TaskControl{ID: "control-steer", TaskID: taskID, Version: 1, IdempotencyKey: key, Kind: domain.ControlSteer, Payload: []byte(`{"kind":"context","message":"fact"}`), IntegrityHash: "test", CreatedAt: time.Unix(1, 0).UTC()}, true, nil
}
func (f *fakeBackend) Input(context.Context, string, string, domain.InputPayload) (domain.TaskControl, bool, error) {
	return domain.TaskControl{}, false, errors.New("input rejected")
}
func (f *fakeBackend) Cancel(_ context.Context, taskID, key string, _ domain.CancelPayload) (domain.TaskControl, bool, error) {
	return domain.TaskControl{ID: "control-cancel", TaskID: taskID, Version: 1, IdempotencyKey: key, Kind: domain.ControlCancel, Payload: []byte(`{}`), IntegrityHash: "test", CreatedAt: time.Unix(1, 0).UTC()}, true, nil
}
func (f *fakeBackend) Result(context.Context, string) (domain.TaskResult, bool, error) {
	return domain.TaskResult{}, false, nil
}
func (f *fakeBackend) Inspect(_ context.Context, taskID string) (service.TaskInspection, error) {
	return service.TaskInspection{Task: domain.Task{ID: taskID}, Controls: []domain.TaskControl{}}, nil
}

type largeReadBackend struct{ fakeBackend }

func (b *largeReadBackend) Result(_ context.Context, taskID string) (domain.TaskResult, bool, error) {
	return domain.TaskResult{
		ID:                   "result-large",
		TaskID:               taskID,
		Version:              2,
		ChangedAreas:         []string{"api.go", "domain.go", "transform.go"},
		EvidenceID:           "evidence-large",
		VerificationExecuted: []string{"go test ./...", "go vet ./...", "go build ./..."},
		PassFailEvidence:     []string{strings.Repeat("large-evidence-", 12<<10)},
		UnresolvedRisks:      []string{},
		ResourceSummary:      domain.ResourceSummary{AgentTurns: 5, AgentToolCalls: 5, ModelTotalTokens: 550},
		CreatedAt:            time.Unix(2, 0).UTC(),
	}, true, nil
}

func (b *largeReadBackend) Inspect(ctx context.Context, taskID string) (service.TaskInspection, error) {
	result, _, _ := b.Result(ctx, taskID)
	return service.TaskInspection{Task: domain.Task{ID: taskID}, Result: &result, Controls: []domain.TaskControl{}}, nil
}

func connectTestMCP(t *testing.T, backend Backend) *mcp.ClientSession {
	t.Helper()
	server, err := NewServer(backend)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "mar-test-client", Version: "1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

func TestPublicMCPSurfaceIsExactlySevenTaskOrientedTools(t *testing.T) {
	session := connectTestMCP(t, &fakeBackend{})
	listed, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{"cancel", "input", "inspect", "result", "status", "steer", "submit"}
	if len(names) != len(want) {
		t.Fatalf("unexpected public tool count: got=%v want=%v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("public MCP surface mismatch: got=%v want=%v", names, want)
		}
	}
	for _, forbidden := range []string{"read_file", "write_file", "run_shell", "run_command"} {
		for _, name := range names {
			if name == forbidden {
				t.Fatalf("low-level worker primitive leaked to public MCP: %s", forbidden)
			}
		}
	}
}

func TestSteerToolMapsTypedArgumentsToDurableBackendCommand(t *testing.T) {
	backend := &fakeBackend{}
	session := connectTestMCP(t, backend)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "steer", Arguments: map[string]any{
		"task_id": "task-1", "idempotency_key": "steer-key", "kind": "context", "message": "fact",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("steer tool returned tool error: %+v", result.Content)
	}
	if backend.steerTask != "task-1" || backend.steerKey != "steer-key" || backend.steer.Kind != domain.SteerContext || backend.steer.Message != "fact" {
		t.Fatalf("steer tool mapping mismatch: task=%q key=%q payload=%+v", backend.steerTask, backend.steerKey, backend.steer)
	}
	if result.StructuredContent == nil {
		t.Fatal("typed MCP tool did not return structured content")
	}
}

func TestLargeResultAndInspectRemainValidStructuredJSON(t *testing.T) {
	session := connectTestMCP(t, &largeReadBackend{})
	for _, name := range []string{"result", "inspect"} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"task_id": "task-large"}})
		if err != nil {
			t.Fatalf("%s large payload escaped as protocol error: %v", name, err)
		}
		if result.IsError || result.StructuredContent == nil {
			t.Fatalf("%s large payload lost structured content: %+v", name, result)
		}
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil || !json.Valid(raw) {
			t.Fatalf("%s structured payload is invalid JSON: len=%d err=%v", name, len(raw), err)
		}
		if len(raw) < 100<<10 {
			t.Fatalf("%s regression payload was not large enough: %d bytes", name, len(raw))
		}
		if len(result.Content) == 0 {
			t.Fatalf("%s omitted MCP text fallback", name)
		}
	}
}

func TestToolApplicationErrorStaysToolVisibleNotProtocolCrash(t *testing.T) {
	session := connectTestMCP(t, &fakeBackend{})
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "input", Arguments: map[string]any{
		"task_id": "task-1", "idempotency_key": "input-key", "message": "answer",
	}})
	if err != nil {
		t.Fatalf("application error escaped as MCP protocol error: %v", err)
	}
	if !result.IsError || len(result.Content) == 0 {
		t.Fatalf("application error was not visible as a tool error: %+v", result)
	}
}
