package mcpedge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mar/internal/testsupport"
)

func TestRemoteHTTPStreamableClientSeesOnlyPublicMARSurface(t *testing.T) {
	testsupport.RequireLoopbackTCP(t)
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var mu sync.Mutex
	var methods []string
	var sessionIDs []string
	handler, err := NewRemoteHTTPHandler(&fakeBackend{}, RemoteHTTPOptions{
		PathToken: token,
		Observe: func(event RemoteHTTPEvent) {
			mu.Lock()
			defer mu.Unlock()
			if event.JSONRPCMethod != "" {
				methods = append(methods, event.JSONRPCMethod)
			}
			if event.SessionID != "" {
				sessionIDs = append(sessionIDs, event.SessionID)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "mar-remote-http-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             ts.URL + "/mcp/" + token,
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{"action", "brain_respond", "brain_turn", "control", "project", "submit", "task"}
	if len(names) != len(want) {
		t.Fatalf("remote MCP public tool count mismatch: got=%v want=%v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("remote MCP public surface mismatch: got=%v want=%v", names, want)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !containsRemoteMethod(methods, "initialize") || !containsRemoteMethod(methods, "tools/list") {
		t.Fatalf("remote MCP telemetry missed initialization/list-tools: %v", methods)
	}
	if len(sessionIDs) == 0 {
		t.Fatalf("stateful MCP telemetry did not observe an authoritative session id: methods=%v", methods)
	}
}

func TestRemoteHTTPStatelessAdvertisesModernMCPProtocol(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	handler, err := NewRemoteHTTPHandler(&fakeBackend{}, RemoteHTTPOptions{PathToken: token, Stateless: true})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"jsonrpc":"2.0","id":"discover-modern","method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"chatgpt-probe","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+token, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "server/discover")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("modern discovery status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"supportedVersions":["2026-07-28"`) {
		t.Fatalf("modern discovery did not advertise 2026-07-28: %s", rec.Body.String())
	}
}

func TestRemoteHTTPHealthRequiresSameCapabilityTokenAndDoesNotEnterMCP(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	handler, err := NewRemoteHTTPHandler(&fakeBackend{}, RemoteHTTPOptions{PathToken: token})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want int
	}{{"/health/" + token, http.StatusNoContent}, {"/health/wrong-token", http.StatusNotFound}} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("health path %q status=%d want=%d body=%s", tc.path, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestRemoteHTTPRejectsUnknownCapabilityPathBeforeMCP(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	handler, err := NewRemoteHTTPHandler(&fakeBackend{}, RemoteHTTPOptions{PathToken: token})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/not-the-token", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown remote MCP capability path status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRemoteHTTPRejectsForeignBrowserOrigin(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	handler, err := NewRemoteHTTPHandler(&fakeBackend{}, RemoteHTTPOptions{PathToken: token})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+token, nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin reached MCP handler: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRemoteHTTPAllowsClaudeOriginToReachMCPTransport(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	handler, err := NewRemoteHTTPHandler(&fakeBackend{}, RemoteHTTPOptions{PathToken: token})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+token, nil)
	req.Header.Set("Origin", "https://claude.ai")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusNotFound {
		t.Fatalf("Claude Origin did not reach MCP transport: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRemoteHTTPToolObserverCapturesOnlyBoundedMetadata(t *testing.T) {
	testsupport.RequireLoopbackTCP(t)
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const sentinel = "SHADOW_PRIVATE_SENTINEL_9F2C7A"
	var mu sync.Mutex
	var events []ToolCallEvent
	handler, err := NewRemoteHTTPHandler(&fakeBackend{}, RemoteHTTPOptions{
		PathToken: token,
		ObserveTool: func(event ToolCallEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	defer ts.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "mar-tool-observer-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp/" + token, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project", Arguments: map[string]any{"operation": "context", "project_id": "mar", "query": sentinel}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("project context call failed: result=%+v err=%v", result, err)
	}
	mu.Lock()
	got := append([]ToolCallEvent(nil), events...)
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("tool observer event count=%d want=2 events=%+v", len(got), got)
	}
	if got[0].Phase != "start" || got[1].Phase != "complete" || got[0].CallID == 0 || got[0].CallID != got[1].CallID {
		t.Fatalf("tool observer lifecycle mismatch: %+v", got)
	}
	for _, event := range got {
		if event.Tool != "project" || event.Operation != "context" || event.ProjectID != "mar" {
			t.Fatalf("tool observer metadata mismatch: %+v", event)
		}
	}
	if got[1].Outcome != "ok" || got[1].DurationMS < 0 {
		t.Fatalf("tool observer completion mismatch: %+v", got[1])
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), sentinel) {
		t.Fatalf("tool observer leaked request content: %s", raw)
	}
}

func TestRemoteHTTPToolObserverMarksToolLevelError(t *testing.T) {
	testsupport.RequireLoopbackTCP(t)
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var mu sync.Mutex
	var events []ToolCallEvent
	handler, err := NewRemoteHTTPHandler(&fakeBackend{}, RemoteHTTPOptions{
		PathToken: token,
		ObserveTool: func(event ToolCallEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	defer ts.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "mar-tool-observer-error-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp/" + token, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project", Arguments: map[string]any{"operation": "definitely_invalid_observability_probe", "project_id": "mar"}})
	if err != nil {
		t.Fatalf("tool-level error escaped transport: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected MCP tool-level IsError result, got %+v", result)
	}
	mu.Lock()
	got := append([]ToolCallEvent(nil), events...)
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("tool observer event count=%d want=2 events=%+v", len(got), got)
	}
	if got[1].Phase != "complete" || got[1].Outcome != "error" {
		t.Fatalf("tool observer failed to classify tool-level error: %+v", got[1])
	}
}

func TestRemoteHTTPToolObserverPanicDoesNotBreakToolCall(t *testing.T) {
	testsupport.RequireLoopbackTCP(t)
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	handler, err := NewRemoteHTTPHandler(&fakeBackend{}, RemoteHTTPOptions{
		PathToken: token,
		ObserveTool: func(ToolCallEvent) {
			panic("synthetic observer failure")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	defer ts.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "mar-tool-observer-panic-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp/" + token, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project", Arguments: map[string]any{"operation": "context", "project_id": "mar"}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("observer panic affected tool call: result=%+v err=%v", result, err)
	}
}

func containsRemoteMethod(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
