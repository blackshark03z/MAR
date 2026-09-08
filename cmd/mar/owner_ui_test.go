package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mar/internal/domain"
	"mar/internal/service"
	"mar/internal/store"
)

type fakeOwnerMCP struct {
	name string
	args map[string]any
}

func (f *fakeOwnerMCP) CallTool(_ context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	f.name = params.Name
	args, ok := params.Arguments.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected MCP argument type %T", params.Arguments)
	}
	f.args = args
	return &mcp.CallToolResult{StructuredContent: map[string]any{"created": true, "task": map[string]any{"id": "task-ui-test"}}}, nil
}

func TestOwnerUIRequiresLoopbackListen(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8787", "[::1]:8787", "localhost:8787"} {
		if err := requireLoopbackListen(address); err != nil {
			t.Fatalf("expected %s to be allowed: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8787", "192.168.1.10:8787"} {
		if err := requireLoopbackListen(address); err == nil {
			t.Fatalf("expected %s to be rejected", address)
		}
	}
}

func TestOwnerUIRuntimeSurfacesBrainReadinessWithoutSecret(t *testing.T) {
	t.Setenv("MAR_OWNER_UI_TEST_KEY", "super-secret-value")
	backend := &ownerUIBackend{
		brainMode:       "web",
		providerBaseURL: "https://provider.example/v1",
		apiKeyEnv:       "MAR_OWNER_UI_TEST_KEY",
		model:           "test-model",
		reasoning:       "high",
		executable:      "mar.exe",
		dataRoot:        t.TempDir(),
		sandboxCheck: func(context.Context, string, string) (bool, string) {
			return true, "prepared"
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/runtime", nil)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["web_brain_requires_mcp_client"] != true {
		t.Fatalf("expected web brain dependency, got %#v", payload)
	}
	if payload["provider_ready"] != true {
		t.Fatalf("expected configured provider alternative, got %#v", payload)
	}
	if !strings.Contains(fmt.Sprint(payload["brain_next_action"]), "MCP Link") || !strings.Contains(rec.Body.String(), "chatgpt-web") || !strings.Contains(rec.Body.String(), "claude-web") || !strings.Contains(rec.Body.String(), "openai-tunnel") {
		t.Fatalf("missing actionable Web MCP guidance: %#v", payload)
	}
	if strings.Contains(rec.Body.String(), "super-secret-value") {
		t.Fatal("runtime response leaked provider API key value")
	}
}

func TestOwnerUIProjectsExposeRegisteredProjectAndCurrentHead(t *testing.T) {
	root := t.TempDir()
	runOwnerGit(t, root, "init")
	runOwnerGit(t, root, "config", "user.email", "mar-owner-ui@example.invalid")
	runOwnerGit(t, root, "config", "user.name", "MAR Owner UI Test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/owner-ui-test\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runOwnerGit(t, root, "add", "README.md", "go.mod")
	runOwnerGit(t, root, "commit", "-m", "base")
	head := strings.TrimSpace(runOwnerGit(t, root, "rev-parse", "HEAD"))

	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.NewTaskService(db)
	if _, _, err := svc.RegisterProject(context.Background(), "mar", root); err != nil {
		t.Fatal(err)
	}

	backend := &ownerUIBackend{db: db, svc: svc}
	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Projects []ownerProjectView `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Projects) != 1 || payload.Projects[0].ID != "mar" || payload.Projects[0].Head != head || !payload.Projects[0].Supported || !payload.Projects[0].Policy.LocalFileWrite {
		t.Fatalf("unexpected project payload: %+v", payload.Projects)
	}
}

func TestOwnerUISubmitUsesBoundedMCPControlPlane(t *testing.T) {
	fake := &fakeOwnerMCP{}
	backend := &ownerUIBackend{session: fake, sessionToken: "test-owner-token"}
	body := []byte(`{
		"project_id":"mar",
		"base_revision":"abc123",
		"goal":"Update one document",
		"acceptance":["README explains the flow"],
		"boundaries":["Only edit README.md"],
		"non_goals":["No deployment"],
		"verification_profile":"go-docs",
		"priority":"P2",
		"local_file_write":true,
		"local_git_write":true,
		"network_allowed":false
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(body))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ownerSessionHeader, "test-owner-token")
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	if fake.name != "submit" {
		t.Fatalf("owner UI bypassed MCP submit: called %q", fake.name)
	}
	contract, ok := fake.args["contract"].(domain.GoalContract)
	if !ok {
		t.Fatalf("submit contract has unexpected type %T", fake.args["contract"])
	}
	if contract.ProjectID != "mar" || contract.VerificationProfile != "go-docs" {
		t.Fatalf("unexpected contract: %+v", contract)
	}
	if contract.Authority.RemoteGitWrite || contract.Authority.DeployAllowed || contract.Authority.NetworkAllowed {
		t.Fatalf("owner UI widened authority: %+v", contract.Authority)
	}
}

func TestOwnerUISubmitRejectsUnsupportedNetworkAuthorityBeforeMCP(t *testing.T) {
	fake := &fakeOwnerMCP{}
	backend := &ownerUIBackend{session: fake, sessionToken: "test-owner-token"}
	body := []byte(`{"project_id":"mar","base_revision":"abc","goal":"g","acceptance":["a"],"verification_profile":"go-standard","priority":"P2","network_allowed":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(body))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ownerSessionHeader, "test-owner-token")
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	if fake.name != "" {
		t.Fatalf("unsupported network request reached MCP tool %q", fake.name)
	}
}

func TestOwnerUISubmitRejectsUnknownVerificationProfileBeforeMCP(t *testing.T) {
	fake := &fakeOwnerMCP{}
	backend := &ownerUIBackend{session: fake, sessionToken: "test-owner-token"}
	body := []byte(`{"project_id":"mar","base_revision":"abc","goal":"g","acceptance":["a"],"verification_profile":"skip-tests","priority":"P2"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(body))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ownerSessionHeader, "test-owner-token")
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	if fake.name != "" {
		t.Fatalf("invalid request reached MCP tool %q", fake.name)
	}
}

func TestOwnerUIRejectsUnauthorizedMutationRequestsBeforeMCP(t *testing.T) {
	validSubmit := `{"project_id":"mar","base_revision":"abc","goal":"g","acceptance":["a"],"verification_profile":"go-standard","priority":"P2"}`
	tests := []struct {
		name        string
		method      string
		path        string
		body        string
		host        string
		origin      string
		fetchSite   string
		contentType string
		token       string
		wantStatus  int
	}{
		{name: "foreign host get", method: http.MethodGet, path: "/api/runtime", host: "evil.example", wantStatus: http.StatusForbidden},
		{name: "foreign origin submit", method: http.MethodPost, path: "/api/tasks", body: validSubmit, host: "127.0.0.1:8787", origin: "https://evil.example", contentType: "application/json", token: "test-owner-token", wantStatus: http.StatusForbidden},
		{name: "cross site submit", method: http.MethodPost, path: "/api/tasks", body: validSubmit, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", fetchSite: "cross-site", contentType: "application/json", token: "test-owner-token", wantStatus: http.StatusForbidden},
		{name: "plain text submit", method: http.MethodPost, path: "/api/tasks", body: validSubmit, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "text/plain", token: "test-owner-token", wantStatus: http.StatusUnsupportedMediaType},
		{name: "missing token cancel", method: http.MethodPost, path: "/api/tasks/task-ui-test/cancel", body: `{}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "invalid token input", method: http.MethodPost, path: "/api/tasks/task-ui-test/input", body: `{"message":"continue"}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", token: "wrong-token", wantStatus: http.StatusForbidden},
		{name: "missing token project add", method: http.MethodPost, path: "/api/projects", body: `{"root":"D:\\\\MAR"}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "missing token policy", method: http.MethodPost, path: "/api/projects/mar/policy", body: `{"local_file_write":true,"local_git_write":true}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "missing token feedback", method: http.MethodPost, path: "/api/tasks/task-ui-test/feedback", body: `{"verdict":"COMMENT","message":"note"}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "missing token sandbox prepare", method: http.MethodPost, path: "/api/runtime/sandbox/prepare", body: `{}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "missing token web bridge start", method: http.MethodPost, path: "/api/connections/web-bridge/start", body: `{}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "missing token web bridge restart", method: http.MethodPost, path: "/api/connections/web-bridge/restart", body: `{}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "missing token Claude diagnose", method: http.MethodPost, path: "/api/connections/claude-web/diagnose", body: `{}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "missing token connector config", method: http.MethodPost, path: "/api/connections/claude-web/config", body: `{"stable_base_url":"https://mar.example.com","preferred_mode":"stable"}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "missing token connector rotate", method: http.MethodPost, path: "/api/connections/claude-web/rotate", body: `{}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "missing token OpenAI tunnel config", method: http.MethodPost, path: "/api/connections/openai-tunnel/config", body: `{"tunnel_id":"tunnel_0123456789"}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "missing token OpenAI tunnel start", method: http.MethodPost, path: "/api/connections/openai-tunnel/start", body: `{}`, host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", contentType: "application/json", wantStatus: http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeOwnerMCP{}
			backend := &ownerUIBackend{session: fake, sessionToken: "test-owner-token"}
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			if tc.token != "" {
				req.Header.Set(ownerSessionHeader, tc.token)
			}
			rec := httptest.NewRecorder()
			backend.routes().ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if fake.name != "" {
				t.Fatalf("unauthorized request reached MCP tool %q", fake.name)
			}
		})
	}
}

func TestOwnerUIOpenAITunnelConfigLifecyclePersistsDesiredStateWithoutSecret(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := newOpenAITunnelManager(ctx, service.NewTaskService(db), t.TempDir())
	defer manager.Close()
	process := newFakeTunnelProcess()
	manager.findClient = func(store.OpenAITunnelConfig, string) (string, error) { return `C:\fake\tunnel-client.exe`, nil }
	manager.runCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "doctor" {
			return "doctor ready", nil
		}
		return "", nil
	}
	manager.startProcess = func(string, []string, func(string)) (tunnelClientProcess, error) { return process, nil }
	config, err := db.EnsureOpenAITunnelConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Configure(config); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAR_UI_TUNNEL_KEY", "unit-test-ui-credential")
	backend := &ownerUIBackend{db: db, svc: service.NewTaskService(db), openAITunnel: manager, sessionToken: "test-owner-token"}
	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Host = "127.0.0.1:8787"
		req.Header.Set("Origin", "http://127.0.0.1:8787")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(ownerSessionHeader, "test-owner-token")
		rec := httptest.NewRecorder()
		backend.routes().ServeHTTP(rec, req)
		return rec
	}
	configured := post("/api/connections/openai-tunnel/config", `{"tunnel_id":"tunnel_0123456789abcdef","api_key_env":"MAR_UI_TUNNEL_KEY"}`)
	if configured.Code != http.StatusOK || !strings.Contains(configured.Body.String(), `"identifier":"tunnel_0123456789abcdef"`) {
		t.Fatalf("config failed: %d %s", configured.Code, configured.Body.String())
	}
	started := post("/api/connections/openai-tunnel/start", `{}`)
	if started.Code != http.StatusOK || strings.Contains(started.Body.String(), "unit-test-ui-credential") {
		t.Fatalf("start failed or leaked secret: %d %s", started.Code, started.Body.String())
	}
	persisted, err := db.GetOpenAITunnelConfig(ctx)
	if err != nil || !persisted.DesiredRunning {
		t.Fatalf("desired-running was not persisted: config=%+v err=%v", persisted, err)
	}
	stopped := post("/api/connections/openai-tunnel/stop", `{}`)
	if stopped.Code != http.StatusOK {
		t.Fatalf("stop failed: %d %s", stopped.Code, stopped.Body.String())
	}
	persisted, err = db.GetOpenAITunnelConfig(ctx)
	if err != nil || persisted.DesiredRunning {
		t.Fatalf("stopped desired state was not persisted: config=%+v err=%v", persisted, err)
	}
	manager.runCommand = func(_ context.Context, _ string, _ ...string) (string, error) {
		return "diagnostic exposed unit-test-ui-credential", errors.New("command exposed unit-test-ui-credential")
	}
	failed := post("/api/connections/openai-tunnel/start", `{}`)
	if failed.Code != http.StatusServiceUnavailable || strings.Contains(failed.Body.String(), "unit-test-ui-credential") {
		t.Fatalf("failed start leaked secret through owner API: status=%d body=%s", failed.Code, failed.Body.String())
	}
}

func TestOwnerUIConnectionHubShowsIndependentGPTAndClaudeMCPLinks(t *testing.T) {
	for _, required := range []string{"data-openai-tunnel", "Secure MCP Tunnel · outbound-only", "data-web-connector", "MCP Link · HTTPS", "chatgpt-web", "claude-web", "data-bridge-diagnose", "data-copy-url", "provider-details", "overflow-wrap:anywhere", ".identifier-row button { width:144px", "DISCONNECTING:'Đang ngắt…'", "setInterval(async()=>", "await loadRuntime()"} {
		if !strings.Contains(ownerUIHTML, required) {
			t.Fatalf("Connection Hub is missing %q", required)
		}
	}
}

func TestOwnerUIWebBridgeStartStopUsesSameOriginSessionBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager := newRemoteBridgeManager(ctx, service.NewTaskService(db), t.TempDir())
	manager.findTunnel = func() (string, error) { return `C:\\fake\\cloudflared.exe`, nil }
	var stopped atomic.Bool
	fakeDone := make(chan error)
	manager.startTunnel = func(_ context.Context, _ string, localURL string) (string, func() error, <-chan error, error) {
		return localURL, func() error { stopped.Store(true); return nil }, fakeDone, nil
	}
	if err := manager.ConfigureProfiles(testRemoteProfiles(t)); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	backend := &ownerUIBackend{db: db, svc: service.NewTaskService(db), bridge: manager, brainMode: "web", sessionToken: "test-owner-token"}

	post := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Host = "127.0.0.1:8787"
		req.Header.Set("Origin", "http://127.0.0.1:8787")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(ownerSessionHeader, "test-owner-token")
		rec := httptest.NewRecorder()
		backend.routes().ServeHTTP(rec, req)
		return rec
	}
	start := post("/api/connections/web-bridge/start")
	if start.Code != http.StatusOK || !strings.Contains(start.Body.String(), "/mcp/") || !strings.Contains(start.Body.String(), "LINK_READY") {
		t.Fatalf("start bridge failed: status=%d body=%s", start.Code, start.Body.String())
	}
	runtimeReq := httptest.NewRequest(http.MethodGet, "/api/runtime", nil)
	runtimeReq.Host = "127.0.0.1:8787"
	runtimeRec := httptest.NewRecorder()
	backend.routes().ServeHTTP(runtimeRec, runtimeReq)
	if runtimeRec.Code != http.StatusOK || !strings.Contains(runtimeRec.Body.String(), "chatgpt-web") || !strings.Contains(runtimeRec.Body.String(), "claude-web") || !strings.Contains(runtimeRec.Body.String(), "LINK_READY") || !strings.Contains(runtimeRec.Body.String(), "/mcp/") {
		t.Fatalf("active bridge missing from runtime metadata: status=%d body=%s", runtimeRec.Code, runtimeRec.Body.String())
	}
	gptDiagnose := post("/api/connections/chatgpt-web/diagnose")
	if gptDiagnose.Code != http.StatusOK {
		t.Fatalf("GPT diagnostics failed: status=%d body=%s", gptDiagnose.Code, gptDiagnose.Body.String())
	}
	claudeDiagnose := post("/api/connections/claude-web/diagnose")
	if claudeDiagnose.Code != http.StatusOK {
		t.Fatalf("Claude diagnostics failed: status=%d body=%s", claudeDiagnose.Code, claudeDiagnose.Body.String())
	}
	restart := post("/api/connections/web-bridge/restart")
	if restart.Code != http.StatusOK || !strings.Contains(restart.Body.String(), "LINK_READY") {
		t.Fatalf("Claude bridge restart failed: status=%d body=%s", restart.Code, restart.Body.String())
	}
	stop := post("/api/connections/web-bridge/stop")
	if stop.Code != http.StatusOK || !stopped.Load() || strings.Contains(stop.Body.String(), "public_url\":\"http") {
		t.Fatalf("stop bridge did not revoke public URL: status=%d stopped=%v body=%s", stop.Code, stopped.Load(), stop.Body.String())
	}
}

func TestOwnerUIStableConnectorConfigAndRotationAreIndependent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	profiles, err := ensureRemoteConnectorProfiles(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	manager := newRemoteBridgeManager(ctx, service.NewTaskService(db), t.TempDir())
	if err := manager.ConfigureProfiles(profiles); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	backend := &ownerUIBackend{db: db, svc: service.NewTaskService(db), bridge: manager, sessionToken: "test-owner-token"}

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Host = "127.0.0.1:8787"
		req.Header.Set("Origin", "http://127.0.0.1:8787")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(ownerSessionHeader, "test-owner-token")
		rec := httptest.NewRecorder()
		backend.routes().ServeHTTP(rec, req)
		return rec
	}
	beforeGPT, _ := db.GetRemoteConnectorProfile(ctx, store.RemoteConnectorChatGPTWeb)
	beforeClaude, _ := db.GetRemoteConnectorProfile(ctx, store.RemoteConnectorClaudeWeb)
	config := post("/api/connections/claude-web/config", `{"stable_base_url":"https://mar.example.com/base","preferred_mode":"stable"}`)
	if config.Code != http.StatusOK {
		t.Fatalf("stable config failed: %d %s", config.Code, config.Body.String())
	}
	afterClaude, _ := db.GetRemoteConnectorProfile(ctx, store.RemoteConnectorClaudeWeb)
	afterGPT, _ := db.GetRemoteConnectorProfile(ctx, store.RemoteConnectorChatGPTWeb)
	if afterClaude.StableBaseURL != "https://mar.example.com/base" || afterClaude.PreferredMode != store.RemoteConnectorModeStable || afterClaude.PathToken != beforeClaude.PathToken {
		t.Fatalf("Claude config mismatch: %+v", afterClaude)
	}
	if afterGPT != beforeGPT {
		t.Fatalf("Claude config contaminated GPT profile: before=%+v after=%+v", beforeGPT, afterGPT)
	}

	rotate := post("/api/connections/claude-web/rotate", `{}`)
	if rotate.Code != http.StatusOK {
		t.Fatalf("rotate failed: %d %s", rotate.Code, rotate.Body.String())
	}
	rotatedClaude, _ := db.GetRemoteConnectorProfile(ctx, store.RemoteConnectorClaudeWeb)
	rotatedGPT, _ := db.GetRemoteConnectorProfile(ctx, store.RemoteConnectorChatGPTWeb)
	if rotatedClaude.PathToken == afterClaude.PathToken {
		t.Fatal("Claude rotate kept old capability token")
	}
	if rotatedGPT != beforeGPT {
		t.Fatalf("Claude rotate contaminated GPT profile: before=%+v after=%+v", beforeGPT, rotatedGPT)
	}

	gptConfig := post("/api/connections/chatgpt-web/config", `{"stable_base_url":"https://gpt.mar.example.com","preferred_mode":"stable"}`)
	if gptConfig.Code != http.StatusOK {
		t.Fatalf("GPT stable config failed: %d %s", gptConfig.Code, gptConfig.Body.String())
	}
	configuredGPT, _ := db.GetRemoteConnectorProfile(ctx, store.RemoteConnectorChatGPTWeb)
	claudeAfterGPTConfig, _ := db.GetRemoteConnectorProfile(ctx, store.RemoteConnectorClaudeWeb)
	if configuredGPT.StableBaseURL != "https://gpt.mar.example.com" || configuredGPT.PreferredMode != store.RemoteConnectorModeStable || configuredGPT.PathToken != beforeGPT.PathToken {
		t.Fatalf("GPT config mismatch: %+v", configuredGPT)
	}
	if claudeAfterGPTConfig != rotatedClaude {
		t.Fatalf("GPT config contaminated Claude profile: before=%+v after=%+v", rotatedClaude, claudeAfterGPTConfig)
	}
	gptRotate := post("/api/connections/chatgpt-web/rotate", `{}`)
	if gptRotate.Code != http.StatusOK {
		t.Fatalf("GPT rotate failed: %d %s", gptRotate.Code, gptRotate.Body.String())
	}
	finalGPT, _ := db.GetRemoteConnectorProfile(ctx, store.RemoteConnectorChatGPTWeb)
	finalClaude, _ := db.GetRemoteConnectorProfile(ctx, store.RemoteConnectorClaudeWeb)
	if finalGPT.PathToken == configuredGPT.PathToken {
		t.Fatal("GPT rotate kept old capability token")
	}
	if finalClaude != rotatedClaude {
		t.Fatalf("GPT rotate contaminated Claude profile: before=%+v after=%+v", rotatedClaude, finalClaude)
	}

	bad := post("/api/connections/claude-web/config", `{"stable_base_url":"https://random.trycloudflare.com","preferred_mode":"stable"}`)
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "temporary") {
		t.Fatalf("Quick Tunnel hostname was accepted as stable: %d %s", bad.Code, bad.Body.String())
	}
}

func TestOwnerUISandboxPreparationUsesUACHelperAndRechecksReadiness(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "mar.exe")
	called := false
	checked := false
	backend := &ownerUIBackend{
		executable:   exe,
		dataRoot:     filepath.Join(root, "data"),
		sessionToken: "test-owner-token",
		sandboxPrepare: func(_ context.Context, gotExe, workspace string) error {
			called = true
			if gotExe != exe || workspace != filepath.Join(root, "data", "sandbox-host-probe") {
				t.Fatalf("unexpected preparation target exe=%q workspace=%q", gotExe, workspace)
			}
			return nil
		},
		sandboxCheck: func(_ context.Context, gotExe, workspace string) (bool, string) {
			checked = true
			if gotExe != exe || workspace != filepath.Join(root, "data", "sandbox-host-probe") {
				t.Fatalf("unexpected readiness target exe=%q workspace=%q", gotExe, workspace)
			}
			return true, "prepared"
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/runtime/sandbox/prepare", strings.NewReader(`{}`))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ownerSessionHeader, "test-owner-token")
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if !called || !checked {
		t.Fatalf("sandbox preparation lifecycle incomplete: called=%v checked=%v", called, checked)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"sandbox_host_ready":true`) {
		t.Fatalf("unexpected sandbox prepare response %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOwnerUISessionTokenIsEmbeddedForSameOriginClient(t *testing.T) {
	backend := &ownerUIBackend{sessionToken: "test-owner-token"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "test-owner-token") || strings.Contains(rec.Body.String(), "__MAR_OWNER_SESSION_TOKEN__") {
		t.Fatal("owner UI did not embed the current startup session token")
	}
}

func TestOwnerUIAddProjectAndPolicyAreDurable(t *testing.T) {
	root := t.TempDir()
	runOwnerGit(t, root, "init")
	runOwnerGit(t, root, "config", "user.email", "mar-owner-ui@example.invalid")
	runOwnerGit(t, root, "config", "user.name", "MAR Owner UI Test")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/owner-ui-add\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runOwnerGit(t, root, "add", "go.mod")
	runOwnerGit(t, root, "commit", "-m", "base")

	dbPath := filepath.Join(t.TempDir(), "mar.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewTaskService(db)
	backend := &ownerUIBackend{db: db, svc: svc, sessionToken: "test-owner-token"}

	body, _ := json.Marshal(ownerProjectRequest{Root: root})
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ownerSessionHeader, "test-owner-token")
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add project status=%d body=%s", rec.Code, rec.Body.String())
	}
	var added struct {
		Project ownerProjectView `json:"project"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &added); err != nil {
		t.Fatal(err)
	}
	if added.Project.ID == "" || !added.Project.Supported || !added.Project.Policy.LocalFileWrite || !added.Project.Policy.LocalGitWrite {
		t.Fatalf("unexpected added project: %+v", added.Project)
	}

	policyBody := []byte(`{"local_file_write":true,"local_git_write":false}`)
	policyReq := httptest.NewRequest(http.MethodPost, "/api/projects/"+added.Project.ID+"/policy", bytes.NewReader(policyBody))
	policyReq.Host = "127.0.0.1:8787"
	policyReq.Header.Set("Origin", "http://127.0.0.1:8787")
	policyReq.Header.Set("Content-Type", "application/json")
	policyReq.Header.Set(ownerSessionHeader, "test-owner-token")
	policyRec := httptest.NewRecorder()
	backend.routes().ServeHTTP(policyRec, policyReq)
	if policyRec.Code != http.StatusOK {
		t.Fatalf("update policy status=%d body=%s", policyRec.Code, policyRec.Body.String())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	policy, err := reopened.GetProjectPolicy(context.Background(), added.Project.ID)
	if err != nil || !policy.LocalFileWrite || policy.LocalGitWrite || policy.NetworkAllowed || policy.RemoteGitWrite || policy.DeployAllowed {
		t.Fatalf("policy did not survive restart or widened unsupported authority: %+v err=%v", policy, err)
	}
}

func TestOwnerUIRecentWorkRestoresWithoutTaskIDAndPrematureAcceptanceFails(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.NewTaskService(db)
	root := t.TempDir()
	if _, _, err := svc.RegisterProject(context.Background(), "project-1", root); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "Fix the owner journey",
		Acceptance:          []string{"Owner can reopen current work"},
		Boundaries:          []string{"local project only"},
		NonGoals:            []string{"no deployment"},
		ProjectID:           "project-1",
		BaseRevision:        "base",
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
		VerificationProfile: "go-standard",
		Priority:            "P2",
	}
	task, _, err := svc.Submit(context.Background(), "owner-ui-recent", contract)
	if err != nil {
		t.Fatal(err)
	}
	backend := &ownerUIBackend{db: db, svc: svc, sessionToken: "test-owner-token"}
	listReq := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	listReq.Host = "127.0.0.1:8787"
	listRec := httptest.NewRecorder()
	backend.routes().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), task.ID) || !strings.Contains(listRec.Body.String(), "Fix the owner journey") {
		t.Fatalf("recent work continuity failed: status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	feedbackReq := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID+"/feedback", strings.NewReader(`{"verdict":"ACCEPTED"}`))
	feedbackReq.Host = "127.0.0.1:8787"
	feedbackReq.Header.Set("Origin", "http://127.0.0.1:8787")
	feedbackReq.Header.Set("Content-Type", "application/json")
	feedbackReq.Header.Set(ownerSessionHeader, "test-owner-token")
	feedbackRec := httptest.NewRecorder()
	backend.routes().ServeHTTP(feedbackRec, feedbackReq)
	if feedbackRec.Code != http.StatusBadRequest || !strings.Contains(feedbackRec.Body.String(), "durable task result") {
		t.Fatalf("owner acceptance was allowed before a durable result: status=%d body=%s", feedbackRec.Code, feedbackRec.Body.String())
	}
}

func runOwnerGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}
