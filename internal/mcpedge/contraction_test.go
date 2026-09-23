package mcpedge

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var canonicalPublicTools = []string{"brain_respond", "brain_turn", "control", "project", "submit", "task"}
var legacyPublicAliases = []string{"project_context", "project_read", "status", "result", "inspect", "steer", "input", "cancel"}

func TestPublicToolSurfaceSchemaBudget(t *testing.T) {
	session := connectTestMCP(t, &fakeBackend{})
	listed, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 6 {
		t.Fatalf("listed tool count=%d want=6", len(listed.Tools))
	}
	raw, err := json.Marshal(listed.Tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 24576 {
		t.Fatalf("listed tool schema surface=%d bytes want<=24576", len(raw))
	}
}

func TestCanonicalProjectToolPreservesContextAndReadSemantics(t *testing.T) {
	session := connectTestMCP(t, &fakeBackend{})
	contextResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project", Arguments: map[string]any{"operation": "context"}})
	if err != nil || contextResult.IsError {
		t.Fatalf("canonical project context failed: err=%v result=%+v", err, contextResult)
	}
	raw, _ := json.Marshal(contextResult.StructuredContent)
	if !strings.Contains(string(raw), `"project_id":"mar"`) || !strings.Contains(string(raw), `"head":"abc123"`) {
		t.Fatalf("canonical project context lost bounded project identity: %s", raw)
	}
	readResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project", Arguments: map[string]any{"operation": "read", "path": "README.md"}})
	if err != nil || readResult.IsError {
		t.Fatalf("canonical project read failed: err=%v result=%+v", err, readResult)
	}
	raw, _ = json.Marshal(readResult.StructuredContent)
	if !strings.Contains(string(raw), "inferred-project") || !strings.Contains(string(raw), "README.md") {
		t.Fatalf("canonical project read changed semantics: %s", raw)
	}
	rangeResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project", Arguments: map[string]any{"operation": "read", "path": "README.md", "start_line": 2, "end_line": 2}})
	if err != nil || rangeResult.IsError {
		t.Fatalf("canonical ranged project read failed: err=%v result=%+v", err, rangeResult)
	}
	raw, _ = json.Marshal(rangeResult.StructuredContent)
	if !strings.Contains(string(raw), "second\\n") || !strings.Contains(string(raw), `"start_line":2`) || !strings.Contains(string(raw), `"end_line":2`) {
		t.Fatalf("canonical ranged read changed semantics: %s", raw)
	}
	searchResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project", Arguments: map[string]any{"operation": "search", "project_id": "mar", "query": "needle", "max_results": 10}})
	if err != nil || searchResult.IsError {
		t.Fatalf("canonical project search failed: err=%v result=%+v", err, searchResult)
	}
	raw, _ = json.Marshal(searchResult.StructuredContent)
	if !strings.Contains(string(raw), `"query":"needle"`) || !strings.Contains(string(raw), "README.md") {
		t.Fatalf("canonical project search changed semantics: %s", raw)
	}
	manyResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project", Arguments: map[string]any{"operation": "read_many", "reads": []any{map[string]any{"path": "README.md", "start_line": 1, "end_line": 1}, map[string]any{"path": "TASK.md", "start_line": 2, "end_line": 2}}}})
	if err != nil || manyResult.IsError {
		t.Fatalf("canonical project read_many failed: err=%v result=%+v", err, manyResult)
	}
	raw, _ = json.Marshal(manyResult.StructuredContent)
	if !strings.Contains(string(raw), "README.md") || !strings.Contains(string(raw), "TASK.md") || !strings.Contains(string(raw), `"start_line":1`) || !strings.Contains(string(raw), `"start_line":2`) {
		t.Fatalf("canonical project read_many changed semantics: %s", raw)
	}
	emptyResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project", Arguments: map[string]any{"operation": "read_many", "reads": []any{}}})
	if err != nil || !emptyResult.IsError {
		t.Fatalf("canonical project read_many must reject zero reads: err=%v result=%+v", err, emptyResult)
	}
	tooMany := make([]any, 17)
	for i := range tooMany {
		tooMany[i] = map[string]any{"path": "README.md"}
	}
	tooManyResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project", Arguments: map[string]any{"operation": "read_many", "reads": tooMany}})
	if err != nil || !tooManyResult.IsError {
		t.Fatalf("canonical project read_many must reject more than 16 reads: err=%v result=%+v", err, tooManyResult)
	}
}

func TestCanonicalTaskToolPreservesBoundedReadSemantics(t *testing.T) {
	session := connectTestMCP(t, &fakeBackend{})
	for _, op := range []string{"status", "result", "inspect"} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "task", Arguments: map[string]any{"operation": op, "task_id": "task-1"}})
		if err != nil || result.IsError || result.StructuredContent == nil {
			t.Fatalf("canonical task %s failed: err=%v result=%+v", op, err, result)
		}
	}
}

func TestCanonicalControlToolPreservesControlSemantics(t *testing.T) {
	backend := &fakeBackend{}
	session := connectTestMCP(t, backend)
	steer, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "control", Arguments: map[string]any{"operation": "steer", "task_id": "task-1", "idempotency_key": "steer-key", "kind": "context", "message": "fact"}})
	if err != nil || steer.IsError {
		t.Fatalf("canonical steer failed: err=%v result=%+v", err, steer)
	}
	if backend.steerTask != "task-1" || backend.steerKey != "steer-key" || backend.steer.Message != "fact" {
		t.Fatalf("canonical steer mapping mismatch: %+v", backend)
	}
	input, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "control", Arguments: map[string]any{"operation": "input", "task_id": "task-1", "idempotency_key": "input-key", "message": "answer"}})
	if err != nil || !input.IsError {
		t.Fatalf("canonical input must preserve application-state rejection as tool error: err=%v result=%+v", err, input)
	}
	cancel, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "control", Arguments: map[string]any{"operation": "cancel", "task_id": "task-1", "idempotency_key": "cancel-key", "reason": "owner"}})
	if err != nil || cancel.IsError {
		t.Fatalf("canonical cancel failed: err=%v result=%+v", err, cancel)
	}
}

func TestLegacyAliasesRemainCallableButUnlisted(t *testing.T) {
	session := connectTestMCP(t, &fakeBackend{})
	listed, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	listedNames := make(map[string]bool, len(listed.Tools))
	for _, tool := range listed.Tools {
		listedNames[tool.Name] = true
	}
	for _, alias := range legacyPublicAliases {
		if listedNames[alias] {
			t.Fatalf("legacy alias %s leaked into tools/list", alias)
		}
	}
	cases := []struct {
		name      string
		args      map[string]any
		wantError bool
	}{
		{"project_context", map[string]any{}, false},
		{"project_read", map[string]any{"path": "README.md"}, false},
		{"status", map[string]any{"task_id": "task-1"}, false},
		{"result", map[string]any{"task_id": "task-1"}, false},
		{"inspect", map[string]any{"task_id": "task-1"}, false},
		{"steer", map[string]any{"task_id": "task-1", "idempotency_key": "legacy-steer", "kind": "context", "message": "fact"}, false},
		{"input", map[string]any{"task_id": "task-1", "idempotency_key": "legacy-input", "message": "answer"}, true},
		{"cancel", map[string]any{"task_id": "task-1", "idempotency_key": "legacy-cancel", "reason": "owner"}, false},
	}
	for _, tc := range cases {
		result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		if callErr != nil {
			t.Fatalf("legacy alias %s escaped as protocol error: %v", tc.name, callErr)
		}
		if result.IsError != tc.wantError {
			t.Fatalf("legacy alias %s error=%v want=%v result=%+v", tc.name, result.IsError, tc.wantError, result)
		}
	}
}

func TestCanonicalPublicToolNamesRemainStable(t *testing.T) {
	got := append([]string(nil), canonicalPublicTools...)
	sort.Strings(got)
	want := []string{"brain_respond", "brain_turn", "control", "project", "submit", "task"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("canonical public names changed: got=%v want=%v", got, want)
		}
	}
}
