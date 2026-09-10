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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mar/internal/domain"

	"mar/internal/mcpedge"
	"mar/internal/model"

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
	if !strings.Contains(fmt.Sprint(payload["brain_next_action"]), "Tunnel") || !strings.Contains(rec.Body.String(), "chatgpt-web") || !strings.Contains(rec.Body.String(), "claude-web") || !strings.Contains(rec.Body.String(), "openai-tunnel") {
		t.Fatalf("missing actionable Web MCP guidance: %#v", payload)
	}
	if strings.Contains(rec.Body.String(), "super-secret-value") {
		t.Fatal("runtime response leaked provider API key value")
	}
}

func TestOwnerUIUsageAggregationSeparatesTimeWindowsAndCoverage(t *testing.T) {
	loc := time.FixedZone("owner-local", 7*60*60)
	now := time.Date(2026, 9, 9, 18, 0, 0, 0, loc)
	results := []domain.TaskResult{
		{CreatedAt: now.Add(-2 * time.Hour), ResourceSummary: domain.ResourceSummary{ModelInputTokens: 100, ModelOutputTokens: 40, ModelTotalTokens: 140}},
		{CreatedAt: now.AddDate(0, 0, -3), ResourceSummary: domain.ResourceSummary{ModelInputTokens: 200, ModelOutputTokens: 80, ModelTotalTokens: 280}},
		{CreatedAt: now.AddDate(0, 0, -10), ResourceSummary: domain.ResourceSummary{}},
	}
	view := buildOwnerUsage(results, now)
	if view.Today.TotalTokens != 140 || view.Today.ResultsWithTokenData != 1 {
		t.Fatalf("unexpected today usage: %+v", view.Today)
	}
	if view.Week.TotalTokens != 140 || view.Week.Results != 1 {
		t.Fatalf("unexpected calendar-week usage: %+v", view.Week)
	}
	if view.AllTime.TotalTokens != 420 || view.AllTime.ResultsWithoutTokenData != 1 || view.AllTime.Results != 3 {
		t.Fatalf("unexpected all-time usage: %+v", view.AllTime)
	}
	if len(view.Daily) != 30 || view.Daily[len(view.Daily)-1].Date != "2026-09-09" {
		t.Fatalf("unexpected daily window: %+v", view.Daily)
	}
	if view.MeasurementScope != "MAR_OBSERVED_DURABLE_RESULT_USAGE" || view.BucketBasis != "RESULT_CREATED_AT_LOCAL" || view.ProviderAttribution != "UNAVAILABLE" {
		t.Fatalf("usage provenance must make measurement limits explicit: %+v", view)
	}
}

func TestOwnerUISystemAttentionUsesRealStateAndDeduplicates(t *testing.T) {
	connections := []ownerConnectionView{
		{ID: "openai-tunnel", Status: "CONNECTING", DesiredRunning: true, Connected: false, NextAction: "Reconnect GPT"},
		{ID: "openai-tunnel", Status: "ERROR", DesiredRunning: true, Connected: false, LastError: "duplicate GPT observation"},
		{ID: store.RemoteConnectorClaudeWeb, Status: "ROUTE_OFFLINE", LastError: "route health failed"},
	}
	items := buildOwnerSystemAttention(false, "sandbox probe failed", connections)
	if len(items) != 3 {
		t.Fatalf("expected sandbox + deduplicated GPT + Claude attention, got %+v", items)
	}
	seen := map[string]bool{}
	for _, item := range items {
		if seen[item.ID] {
			t.Fatalf("duplicate attention id %q: %+v", item.ID, items)
		}
		seen[item.ID] = true
		if item.View != "connections" || item.NextAction == "" {
			t.Fatalf("attention item is not actionable: %+v", item)
		}
	}
	for _, id := range []string{"sandbox", "gpt-connection", "claude-connection"} {
		if !seen[id] {
			t.Fatalf("missing attention item %q: %+v", id, items)
		}
	}
}

func TestOwnerUIUsageEndpointReturnsThirtyDayWindowOnEmptyStore(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	backend := &ownerUIBackend{db: db}
	req := httptest.NewRequest(http.MethodGet, "/api/usage", nil)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	var payload ownerUsageView
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Daily) != 30 || payload.AllTime.TotalTokens != 0 || payload.AllTime.Results != 0 {
		t.Fatalf("unexpected empty usage payload: %+v", payload)
	}
}

func TestOwnerUITaskAttentionExplainsStateResultAndNextAction(t *testing.T) {
	needed, severity, reason, next := ownerTaskAttentionFromState(domain.TaskInputRequired)
	if !needed || severity != "high" || !strings.Contains(reason, "input") || next == "" {
		t.Fatalf("input-required attention is not actionable: needed=%v severity=%q reason=%q next=%q", needed, severity, reason, next)
	}
	needed, severity, reason, next = ownerTaskAttentionFromResult(domain.TaskComplete, domain.TaskResult{Verdict: domain.ResultUnverified, IntegrationStatus: "NOT_APPLIED"})
	if !needed || severity != "high" || !strings.Contains(reason, "evidence") || !strings.Contains(next, "verification") {
		t.Fatalf("unverified-result attention is not actionable: needed=%v severity=%q reason=%q next=%q", needed, severity, reason, next)
	}
	needed, severity, reason, next = ownerTaskAttentionFromResult(domain.TaskComplete, domain.TaskResult{Verdict: domain.ResultVerified, IntegrationStatus: "INTEGRATED", UnresolvedRisks: []string{"owner review"}})
	if !needed || severity != "medium" || !strings.Contains(reason, "1 unresolved") || next == "" {
		t.Fatalf("risk attention is not actionable: needed=%v severity=%q reason=%q next=%q", needed, severity, reason, next)
	}
}

func TestOwnerUIV6ConvergenceInformationArchitecture(t *testing.T) {
	for _, marker := range []string{`data-view="overview" class="active" aria-current="page">Overview`, `data-view="live">Live Operations`, `data-view="work">Tasks`, `data-view="projects">Workspaces`, `data-view="connections">Connections`, `data-view="usage">Usage`, `data-view="advanced">Diagnostics`, `id="workspace-filter"`, `id="operational-answer"`, "Right now", `id="overview-connections"`, "card.className='overview-provider-card'", "Chi tiết bên dưới", "provider-logo claude", "provider-logo openai", "function providerMark(", `id="live-page-flow-count"`, `id="live-page-token-total"`, `id="live-page-token-input"`, `id="live-page-token-output"`, `class="panel realtime-chart-panel"`, `id="live-active-flow-list"`, `class="live-provider-details"`, `id="live-zone-gpt"`, `id="live-zone-claude"`, "Chưa có đủ dữ liệu realtime.", "input:next.live.input", "output:next.live.output", "liveTokenSamples.length>40", "renderActiveFlowList();", `id="task-status-filter"`, `id="task-search"`, `id="task-worker-selector"`, "Tự động · MAR scheduler", `class="tasks-layout"`, `class="task-list compact"`, "function filteredTasks()", "html,body { overflow-x:hidden", `id="usage-daily-rows"`, `id="usage-coverage"`, `id="pick-project-root"`, "Mở chọn thư mục", "mar.owner.workspace.v2", "Session count unavailable", "Telemetry: stale / unavailable", "operationalPollInFlight", "usagePollInFlight", "function operationalHealth()", "function sessionSummary()", "function routeSummary()", "function liveFlowRows()", "function renderProviderZones()", "waiting_for_ai_turn", "captureRealtimeObservation", "taskStageLabel"} {
		if !strings.Contains(ownerUIHTML, marker) {
			t.Fatalf("owner console v6 contract missing %q", marker)
		}
	}
	for _, forbidden := range []string{"worker-win-01", "codex-worker"} {
		if strings.Contains(ownerUIHTML, forbidden) {
			t.Fatalf("owner console v6 invented unsupported worker identity %q", forbidden)
		}
	}
}

func TestOwnerUITaskFlowIdentityIncludesRunEpoch(t *testing.T) {
	payload, err := json.Marshal(ownerTaskView{ID: "task-1", ProjectID: "mar", Goal: "observe flow", State: domain.TaskRunning, RunEpoch: 7})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"run_epoch":7`) {
		t.Fatalf("owner task flow identity lost run epoch: %s", payload)
	}
}

func TestOwnerUILiveUsageUsesDurableWebTurnsAndBrainWaitIsNotOwnerAttention(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.NewTaskService(db)
	if _, _, err := svc.RegisterProject(ctx, "live-project", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "Observe live model progress",
		Acceptance:          []string{"Live model usage is visible without fake billing data"},
		Boundaries:          []string{"local project only"},
		NonGoals:            []string{"no deployment"},
		ProjectID:           "live-project",
		BaseRevision:        "base-revision",
		Authority:           domain.Authority{LocalFileWrite: true},
		VerificationProfile: "go-standard",
		Priority:            "P2",
	}
	task, _, err := svc.Submit(ctx, "live-ui-task", contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker", "daemon", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := model.TurnRequest{
		RequestID:       "live-turn-001",
		Model:           "gpt-5.6-sol",
		Messages:        []model.Message{{Role: model.RoleSystem, Content: "bounded worker"}, {Role: model.RoleUser, Content: "inspect README"}},
		Tools:           []model.ToolDefinition{{Name: "read_file", Parameters: json.RawMessage(`{"type":"object"}`), Strict: true}},
		MaxOutputTokens: 1024,
	}
	turn, _, err := svc.RequestWebTurnForAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, req)
	if err != nil {
		t.Fatal(err)
	}
	backend := &ownerUIBackend{db: db, svc: svc, brainMode: "web"}
	waitingTask, err := svc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	live, err := backend.liveUsageForTask(ctx, waitingTask)
	if err != nil {
		t.Fatal(err)
	}
	if live == nil || !live.Available || live.TokensAvailable || !live.PendingTurn || live.Source != "WEB_TURN_DURABLE_ESTIMATE" {
		t.Fatalf("pending live turn truth is wrong: %+v", live)
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	httpReq.Host = "127.0.0.1:8787"
	backend.routes().ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("tasks status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Tasks []ownerTaskView `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || len(payload.Tasks) != 1 {
		t.Fatalf("decode tasks: %+v err=%v body=%s", payload, err, rec.Body.String())
	}
	if !payload.Tasks[0].WaitingForAITurn || payload.Tasks[0].NeedsAttention {
		t.Fatalf("Web Brain wait was misclassified as Owner attention: %+v", payload.Tasks[0])
	}
	message := model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "call-live-1", Name: "read_file", Arguments: `{"path":"README.md"}`}}}
	if _, _, err := svc.RespondWebTurn(ctx, task.ID, turn.ID, message, "tool_calls"); err != nil {
		t.Fatal(err)
	}
	runningTask, err := svc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	live, err = backend.liveUsageForTask(ctx, runningTask)
	if err != nil {
		t.Fatal(err)
	}
	if live == nil || !live.Available || !live.TokensAvailable || live.PendingTurn || !live.Estimated || live.Turns != 1 || live.TotalTokens <= 0 || live.InputTokens <= 0 || live.OutputTokens <= 0 {
		t.Fatalf("completed live turn usage is wrong: %+v", live)
	}
}

func TestOwnerUIProjectPickerUsesBoundedHostPicker(t *testing.T) {
	backend := &ownerUIBackend{
		sessionToken: "test-owner-token",
		projectPicker: func(context.Context) (string, error) {
			return `D:\\Selected\\Workspace`, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/projects/pick", strings.NewReader(`{}`))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ownerSessionHeader, "test-owner-token")
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `D:\\\\Selected\\\\Workspace`) {
		t.Fatalf("unexpected picker response %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOwnerUIProjectPickerCancellationMutatesNothing(t *testing.T) {
	backend := &ownerUIBackend{
		sessionToken: "test-owner-token",
		projectPicker: func(context.Context) (string, error) {
			return "", nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/projects/pick", strings.NewReader(`{}`))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ownerSessionHeader, "test-owner-token")
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"cancelled":true`) || !strings.Contains(rec.Body.String(), `"path":""`) {
		t.Fatalf("unexpected cancelled picker response %d: %s", rec.Code, rec.Body.String())
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
	makeOpenAITunnelPassiveForTest(manager)
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
	for _, required := range []string{"data-openai-tunnel", "Secure MCP Tunnel · ChatGPT primary · outbound-only", "data-web-connector", "MCP Link · HTTPS", "chatgpt-web", "claude-web", "data-bridge-diagnose", "data-copy-url", "provider-details", "overflow-wrap:anywhere", ".identifier-row button { width:144px", "DISCONNECTING:'Đang ngắt…'", "operationalPollInFlight", "usagePollInFlight", "void pollOperational()", "void pollUsage()", "await loadRuntime()", "card.className='card provider-card'"} {
		if !strings.Contains(ownerUIHTML, required) {
			t.Fatalf("Connection Hub is missing %q", required)
		}
	}
	if strings.Contains(ownerUIHTML, "card.className=card provider-card") {
		t.Fatal("Connection Hub contains malformed JavaScript for provider card className")
	}
}

func TestShouldAutoStartRemoteBridgeOnlyForTemporaryWebProfiles(t *testing.T) {
	profiles := testRemoteProfiles(t)
	if !shouldAutoStartRemoteBridge(profiles) {
		t.Fatal("temporary GPT/Claude profiles should auto-start the remote bridge")
	}
	for i := range profiles {
		profiles[i].PreferredMode = store.RemoteConnectorModeStable
		profiles[i].StableBaseURL = "https://mar.example.com"
	}
	if shouldAutoStartRemoteBridge(profiles) {
		t.Fatal("all-stable connector profiles should not start a Quick Tunnel")
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
	makeRemoteBridgePassiveForTest(manager)
	manager.waitReady = func(context.Context, string, string) error { return nil }
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
	makeRemoteBridgePassiveForTest(manager)
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

func ownerRuntimeConnectionContract(t *testing.T) string {
	t.Helper()
	backend := &ownerUIBackend{brainMode: "web", executable: "mar.exe", dataRoot: t.TempDir(), sessionToken: "test-owner-token", sandboxCheck: func(context.Context, string, string) (bool, string) { return true, "prepared" }}
	req := httptest.NewRequest(http.MethodGet, "/api/runtime", nil)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestOwnerUIStatefulSessionTelemetryUsesObservedSessionIDs(t *testing.T) {
	manager := &remoteBridgeManager{quickBaseURL: "https://route.example.invalid", telemetry: map[string]*remoteConnectorTelemetry{store.RemoteConnectorClaudeWeb: {}}}
	now := time.Now().UTC()
	manager.observe(store.RemoteConnectorClaudeWeb, mcpedge.RemoteHTTPEvent{At: now, HTTPMethod: http.MethodPost, JSONRPCMethod: "initialize"})
	manager.observe(store.RemoteConnectorClaudeWeb, mcpedge.RemoteHTTPEvent{At: now, HTTPMethod: http.MethodPost, JSONRPCMethod: "tools/list", SessionID: "session-real-1"})
	manager.mu.Lock()
	state := manager.connectorStateLocked(store.RemoteConnectorProfile{ID: store.RemoteConnectorClaudeWeb, PreferredMode: store.RemoteConnectorModeTemporary}, manager.telemetry[store.RemoteConnectorClaudeWeb])
	manager.mu.Unlock()
	if !state.ActiveSessionsAvailable || state.ActiveSessions != 1 {
		t.Fatalf("expected one authoritative active session, got %+v", state)
	}

	manager.observe(store.RemoteConnectorClaudeWeb, mcpedge.RemoteHTTPEvent{At: now.Add(time.Second), HTTPMethod: http.MethodDelete, SessionID: "session-real-1"})
	manager.mu.Lock()
	state = manager.connectorStateLocked(store.RemoteConnectorProfile{ID: store.RemoteConnectorClaudeWeb, PreferredMode: store.RemoteConnectorModeTemporary}, manager.telemetry[store.RemoteConnectorClaudeWeb])
	manager.mu.Unlock()
	if !state.ActiveSessionsAvailable || state.ActiveSessions != 0 || state.Status != "IDLE" {
		t.Fatalf("explicit session close should reconcile immediately to zero/idle: %+v", state)
	}

	manager.observe(store.RemoteConnectorClaudeWeb, mcpedge.RemoteHTTPEvent{At: now.Add(2 * time.Second), JSONRPCMethod: "tools/list", SessionID: "session-real-2"})
	manager.mu.Lock()
	manager.telemetry[store.RemoteConnectorClaudeWeb].sessions["session-real-2"] = now.Add(-mcpedge.RemoteMCPSessionTimeout - time.Second)
	state = manager.connectorStateLocked(store.RemoteConnectorProfile{ID: store.RemoteConnectorClaudeWeb, PreferredMode: store.RemoteConnectorModeTemporary}, manager.telemetry[store.RemoteConnectorClaudeWeb])
	manager.mu.Unlock()
	if !state.ActiveSessionsAvailable || state.ActiveSessions != 0 {
		t.Fatalf("expired session should reconcile to zero without losing measurement semantics: %+v", state)
	}
}

func TestOwnerUIStatefulSessionTelemetryFailsClosedOnCardinalityOverflow(t *testing.T) {
	manager := &remoteBridgeManager{telemetry: map[string]*remoteConnectorTelemetry{store.RemoteConnectorClaudeWeb: {}}}
	now := time.Now().UTC()
	for i := 0; i <= remoteSessionTelemetryLimit; i++ {
		manager.observe(store.RemoteConnectorClaudeWeb, mcpedge.RemoteHTTPEvent{At: now, JSONRPCMethod: "tools/list", SessionID: fmt.Sprintf("session-%03d", i)})
	}
	manager.mu.Lock()
	state := manager.connectorStateLocked(store.RemoteConnectorProfile{ID: store.RemoteConnectorClaudeWeb, PreferredMode: store.RemoteConnectorModeTemporary}, manager.telemetry[store.RemoteConnectorClaudeWeb])
	manager.mu.Unlock()
	if state.ActiveSessionsAvailable || state.ActiveSessions != 0 || state.ActiveSessionsReason != "CARDINALITY_LIMIT" {
		t.Fatalf("saturated session telemetry must fail closed instead of exposing a partial count: %+v", state)
	}
}

func TestOwnerUIStatelessGPTDoesNotInferActiveSessions(t *testing.T) {
	body := ownerRuntimeConnectionContract(t)
	var payload struct {
		Connections []ownerConnectionView `json:"connections"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	for _, connection := range payload.Connections {
		if connection.ID != "openai-tunnel" {
			continue
		}
		if connection.ActiveSessionsAvailable || connection.ActiveSessions != nil || !strings.Contains(connection.SessionCountDetail, "không suy đoán") {
			t.Fatalf("stateless GPT session count was fabricated: %+v", connection)
		}
		return
	}
	t.Fatal("OpenAI Secure Tunnel connection missing")
}

func TestOwnerUIGPTSecureTunnelPrimary(t *testing.T) {
	body := ownerRuntimeConnectionContract(t)
	primary, fallback := strings.Index(body, `"id":"openai-tunnel"`), strings.Index(body, `"id":"chatgpt-web"`)
	if primary < 0 || fallback < 0 || primary >= fallback || !strings.Contains(body, `"name":"GPT · OpenAI Secure Tunnel"`) {
		t.Fatalf("Secure Tunnel is not primary: %s", body)
	}
	for _, marker := range []string{"OpenAI Secure MCP Tunnel là đường ChatGPT chính", "connection-primary", "Secure MCP Tunnel · ChatGPT primary", "Khuyến nghị", "Link ready không đồng nghĩa Connected", "connection-state-grid"} {
		if !strings.Contains(ownerUIHTML, marker) {
			t.Fatalf("missing primary UX %q", marker)
		}
	}
}

func TestOwnerUIGPTQuickTunnelIsFallback(t *testing.T) {
	body := ownerRuntimeConnectionContract(t)
	for _, marker := range []string{"GPT Server URL fallback", "Fallback/debug cho ChatGPT", "hostname có thể đổi"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("missing fallback semantics %q: %s", marker, body)
		}
	}
	for _, marker := range []string{"Server URL / Quick Tunnel chỉ là fallback tạm thời", "connection-fallback"} {
		if !strings.Contains(ownerUIHTML, marker) {
			t.Fatalf("missing fallback UX %q", marker)
		}
	}
}

func TestOwnerUIOpenAITunnelSaveArmsAutoResume(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := newOpenAITunnelManager(ctx, service.NewTaskService(db), t.TempDir())
	makeOpenAITunnelPassiveForTest(manager)
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
	t.Setenv("MAR_UI_AUTOSTART_KEY", "unit-test-autostart-secret")
	backend := &ownerUIBackend{db: db, svc: service.NewTaskService(db), openAITunnel: manager, sessionToken: "test-owner-token"}
	req := httptest.NewRequest(http.MethodPost, "/api/connections/openai-tunnel/config", strings.NewReader(`{"tunnel_id":"tunnel_0123456789abcdef","api_key_env":"MAR_UI_AUTOSTART_KEY"}`))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ownerSessionHeader, "test-owner-token")
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "unit-test-autostart-secret") {
		t.Fatalf("save/autostart failed or leaked secret: %d %s", rec.Code, rec.Body.String())
	}
	persisted, err := db.GetOpenAITunnelConfig(ctx)
	if err != nil || !persisted.DesiredRunning || persisted.TunnelID != "tunnel_0123456789abcdef" {
		t.Fatalf("auto-resume not armed: %+v err=%v", persisted, err)
	}
	state := manager.State()
	if !state.Running || state.Identifier != persisted.TunnelID {
		t.Fatalf("save did not start tunnel: %+v", state)
	}
}

func TestOwnerUIOpenAITunnelIdentitySurvivesRestart(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config, err := db.EnsureOpenAITunnelConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config.TunnelID = "tunnel_feedfacecafebeef"
	config.DesiredRunning = true
	config.UpdatedAt = time.Now().UTC()
	if err := db.UpsertOpenAITunnelConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	manager := newOpenAITunnelManager(ctx, service.NewTaskService(db), t.TempDir())
	makeOpenAITunnelPassiveForTest(manager)
	defer manager.Close()
	persisted, err := db.GetOpenAITunnelConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Configure(persisted); err != nil {
		t.Fatal(err)
	}
	state := manager.State()
	if state.Identifier != "tunnel_feedfacecafebeef" || !state.DesiredRunning {
		t.Fatalf("restart lost identity: %+v", state)
	}
}
func TestOwnerUIProviderOverviewDoesNotDoubleCountGPTFallback(t *testing.T) {
	for _, marker := range []string{"activeConnector(tunnel)?tunnel:(activeConnector(fallback)?fallback", "c.active_sessions_available===true&&Number(c.active_sessions||0)>0", "IDLE:'Idle · không có session/activity live'"} {
		if !strings.Contains(ownerUIHTML, marker) {
			t.Fatalf("provider aggregation truth contract missing %q", marker)
		}
	}
}

func TestOwnerUIConnectionUXAccessibilityContract(t *testing.T) {
	for _, marker := range []string{":focus-visible", "min-width: 44px", "min-height: 44px", "scroll-padding-top: 84px", "summary { cursor:pointer; font-weight:650; min-height:44px", ".actions a { min-height:44px", `aria-current="page"`, `role="status" aria-live="polite" aria-atomic="true"`, `role="alert"`, `aria-label="Thiết lập ChatGPT một lần"`} {
		if !strings.Contains(ownerUIHTML, marker) {
			t.Fatalf("accessibility contract missing %q", marker)
		}
	}
}

func TestOwnerUIUsageDailyBreakdownIncludesMeasuredInputOutputTotal(t *testing.T) {
	for _, marker := range []string{`id="usage-daily-rows"`, "Input / Output theo ngày", "usageMeasuredField(day,'input_tokens')", "usageMeasuredField(day,'output_tokens')", "usageMeasuredField(day,'total_tokens')", "bucket theo durable result created_at local"} {
		if !strings.Contains(ownerUIHTML, marker) {
			t.Fatalf("daily usage breakdown missing %q", marker)
		}
	}
}

func TestOwnerUIUsageChartsDoNotCreateDecorativeKeyboardStops(t *testing.T) {
	if strings.Contains(ownerUIHTML, "bar.tabIndex=0") {
		t.Fatal("non-interactive usage bars must not create dozens of keyboard stops")
	}
	if !strings.Contains(ownerUIHTML, "bar.setAttribute('role','img')") || !strings.Contains(ownerUIHTML, "bar.setAttribute('aria-label',bar.title)") {
		t.Fatal("usage bars must retain accessible data semantics without becoming controls")
	}
}

func TestOwnerUIConnectionUXResponsivePrimaryFlow(t *testing.T) {
	for _, marker := range []string{".connection-primary { grid-column:auto; border-width:1px; }", "@media(max-width:360px)", ".identifier-row{grid-template-columns:1fr;}", ".actions>button{width:100%;}", ".connection-state-grid{grid-template-columns:1fr;}", "Lưu &amp; kết nối"} {
		if !strings.Contains(ownerUIHTML, marker) {
			t.Fatalf("responsive primary flow missing %q", marker)
		}
	}
}

func TestOwnerUIClaudeConnectionRemainsIndependent(t *testing.T) {
	body := ownerRuntimeConnectionContract(t)
	for _, marker := range []string{`"id":"claude-web"`, `"name":"Claude Web"`, "Kết nối Claude độc lập với GPT Secure Tunnel"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("Claude independence missing %q: %s", marker, body)
		}
	}
	if strings.Index(body, `"id":"claude-web"`) == strings.Index(body, `"id":"chatgpt-web"`) {
		t.Fatal("Claude and GPT fallback collapsed into one connector")
	}
}
func runOwnerGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	git := requireGitTool(t)
	cmdArgs := append([]string{"-C", root}, args...)
	cmd := exec.Command(git, cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}
