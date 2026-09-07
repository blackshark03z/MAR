package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if !strings.Contains(fmt.Sprint(payload["brain_next_action"]), "MCP-capable client") || !strings.Contains(rec.Body.String(), "REMOTE_BRIDGE_REQUIRED") {
		t.Fatalf("missing actionable brain guidance: %#v", payload)
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
