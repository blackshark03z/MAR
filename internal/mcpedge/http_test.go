package mcpedge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRemoteHTTPStreamableClientSeesOnlyPublicMARSurface(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var mu sync.Mutex
	var methods []string
	handler, err := NewRemoteHTTPHandler(&fakeBackend{}, RemoteHTTPOptions{
		PathToken: token,
		Observe: func(event RemoteHTTPEvent) {
			mu.Lock()
			defer mu.Unlock()
			if event.JSONRPCMethod != "" {
				methods = append(methods, event.JSONRPCMethod)
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
	want := []string{"brain_respond", "brain_turn", "cancel", "input", "inspect", "result", "status", "steer", "submit"}
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

func containsRemoteMethod(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
