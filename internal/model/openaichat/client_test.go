package openaichat_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mar/internal/model"
	"mar/internal/model/openaichat"
)

func TestClientMapsToolConversationAndUsage(t *testing.T) {
	const envName = "MAR_TEST_MODEL_API_KEY"
	t.Setenv(envName, "secret-test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-test-key" {
			t.Fatalf("unexpected auth header %q", got)
		}
		if got := r.Header.Get("X-Client-Request-Id"); got != "turn-1" {
			t.Fatalf("unexpected client request id %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		messages, ok := request["messages"].([]any)
		if !ok || len(messages) != 4 {
			t.Fatalf("unexpected messages: %#v", request["messages"])
		}
		tools, ok := request["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("unexpected tools: %#v", request["tools"])
		}
		if got := request["max_completion_tokens"]; got != float64(321) {
			t.Fatalf("unexpected max_completion_tokens: %#v", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "provider-request-7")
		_, _ = io.WriteString(w, `{"id":"resp-1","model":"router-model","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-2","type":"function","function":{"name":"run_test","arguments":"{\"pkg\":\"./...\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":120,"completion_tokens":15,"total_tokens":135}}`)
	}))
	defer server.Close()

	client, err := openaichat.New(openaichat.Config{BaseURL: server.URL + "/v1", APIKeyEnv: envName, RequestTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	gateway, _ := model.NewGateway(client)
	resp, err := gateway.Turn(context.Background(), model.TurnRequest{
		RequestID: "turn-1",
		Model:     "router-model",
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "You are a coding agent."},
			{Role: model.RoleUser, Content: "Run tests."},
			{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
			{Role: model.RoleTool, ToolCallID: "call-1", Content: "package a"},
		},
		Tools:           []model.ToolDefinition{{Name: "run_test", Description: "Run tests", Parameters: json.RawMessage(`{"type":"object","properties":{"pkg":{"type":"string"}}}`), Strict: true}},
		MaxOutputTokens: 321,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ProviderResponseID != "resp-1" || resp.ProviderRequestID != "provider-request-7" {
		t.Fatalf("unexpected provider ids: %+v", resp)
	}
	if resp.Usage.TotalTokens != 135 || len(resp.Message.ToolCalls) != 1 || resp.Message.ToolCalls[0].Name != "run_test" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestClientDoesNotRetryHTTPFailure(t *testing.T) {
	const envName = "MAR_TEST_MODEL_API_KEY_RETRY"
	t.Setenv(envName, "key")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "2")
		http.Error(w, "temporary upstream failure", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := openaichat.New(openaichat.Config{BaseURL: server.URL, APIKeyEnv: envName, RequestTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Turn(context.Background(), model.TurnRequest{RequestID: "r", Model: "m", Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}}})
	var httpErr *openaichat.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable || httpErr.RetryAfter != "2" {
		t.Fatalf("unexpected error: %#v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider request retried: %d", calls.Load())
	}
}

func TestClientHonorsContextCancellation(t *testing.T) {
	const envName = "MAR_TEST_MODEL_API_KEY_CANCEL"
	t.Setenv(envName, "key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()
	client, err := openaichat.New(openaichat.Config{BaseURL: server.URL, APIKeyEnv: envName, RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = client.Turn(ctx, model.TurnRequest{RequestID: "r", Model: "m", Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}}})
	if err == nil || (!errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded")) {
		t.Fatalf("expected context deadline error, got %v", err)
	}
}

func TestConfiguredTimeoutAppliesWithInjectedHTTPClient(t *testing.T) {
	const envName = "MAR_TEST_MODEL_API_KEY_CUSTOM_CLIENT_TIMEOUT"
	t.Setenv(envName, "key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer server.Close()
	client, err := openaichat.New(openaichat.Config{
		BaseURL:        server.URL,
		APIKeyEnv:      envName,
		RequestTimeout: 40 * time.Millisecond,
		HTTPClient:     &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = client.Turn(context.Background(), model.TurnRequest{RequestID: "r", Model: "m", Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}}})
	if err == nil || time.Since(started) > 300*time.Millisecond {
		t.Fatalf("configured timeout was not enforced, elapsed=%s err=%v", time.Since(started), err)
	}
}

func TestClientRejectsMissingKeyAndOversizedResponse(t *testing.T) {
	const missingEnv = "MAR_TEST_MODEL_MISSING_KEY"
	_ = os.Unsetenv(missingEnv)
	client, err := openaichat.New(openaichat.Config{BaseURL: "http://127.0.0.1", APIKeyEnv: missingEnv})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Turn(context.Background(), model.TurnRequest{RequestID: "r", Model: "m", Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("expected missing key error, got %v", err)
	}

	const envName = "MAR_TEST_MODEL_API_KEY_LARGE"
	t.Setenv(envName, "key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 256))
	}))
	defer server.Close()
	client, err = openaichat.New(openaichat.Config{BaseURL: server.URL, APIKeyEnv: envName, MaxResponseBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Turn(context.Background(), model.TurnRequest{RequestID: "r", Model: "m", Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected response bound error, got %v", err)
	}
}
