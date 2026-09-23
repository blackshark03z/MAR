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
	"mar/internal/model"
	"mar/internal/service"
)

type fakeBackend struct {
	steerTask   string
	steerKey    string
	steer       domain.SteerPayload
	submitCalls int
}

func (f *fakeBackend) Submit(_ context.Context, key string, contract domain.GoalContract) (domain.Task, bool, error) {
	f.submitCalls++
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
func (f *fakeBackend) PendingWebTurn(context.Context, string) (domain.WebTurn, bool, error) {
	return domain.WebTurn{}, false, nil
}
func (f *fakeBackend) RespondWebTurn(context.Context, string, string, model.Message, string) (domain.WebTurn, bool, error) {
	return domain.WebTurn{}, true, nil
}
func (f *fakeBackend) AttachLocalPathWithNetwork(_ context.Context, path string, networkAllowed bool) (service.ProjectAttachResult, error) {
	if strings.TrimSpace(path) == "" {
		return service.ProjectAttachResult{}, errors.New("local path is required")
	}
	return service.ProjectAttachResult{Schema: "mar-project-attach-v1", ProjectID: "attached-project", Root: path, Kind: "directory", Mode: "research_only", Created: true, Policy: domain.ProjectPolicy{ProjectID: "attached-project", LocalFileWrite: true, LocalGitWrite: true, NetworkAllowed: networkAllowed}}, nil
}
func (f *fakeBackend) ListProjectDirectory(_ context.Context, projectID, path string, maxEntries int) (service.ProjectListResult, error) {
	if projectID == "" {
		return service.ProjectListResult{}, errors.New("project_id is required for project list")
	}
	return service.ProjectListResult{ProjectID: projectID, Path: path, Entries: []service.ProjectListEntry{{Name: "README.md", Path: "README.md", Kind: "file", SizeBytes: 10}}}, nil
}
func (f *fakeBackend) ReadProjectFile(_ context.Context, projectID, path string) (service.ProjectReadResult, error) {
	if path == "" {
		return service.ProjectReadResult{}, errors.New("file path is required")
	}
	if projectID == "" {
		projectID = "inferred-project"
	}
	return service.ProjectReadResult{ProjectID: projectID, Path: path, Content: "first\nsecond\nthird\n", SizeBytes: 19}, nil
}
func (f *fakeBackend) ProjectGitStatus(_ context.Context, projectID string) (service.ProjectGitStatusResult, error) {
	if projectID == "" {
		return service.ProjectGitStatusResult{}, errors.New("project_id is required for project git_status")
	}
	return service.ProjectGitStatusResult{ProjectID: projectID, Head: "abc123", Branch: "master", Porcelain: " M README.md\n"}, nil
}
func (f *fakeBackend) ProjectGitDiff(_ context.Context, projectID, path string) (service.ProjectGitDiffResult, error) {
	if projectID == "" {
		return service.ProjectGitDiffResult{}, errors.New("project_id is required for project git_diff")
	}
	return service.ProjectGitDiffResult{ProjectID: projectID, Path: path, Diff: "diff --git a/README.md b/README.md\n"}, nil
}
func (f *fakeBackend) WriteProjectFile(_ context.Context, req service.ProjectWriteRequest) (service.ProjectWriteResult, error) {
	return service.ProjectWriteResult{ProjectID: req.ProjectID, Path: req.Path, AfterSHA256: "after", Created: req.ExpectedSHA256 == "ABSENT", Bytes: len(req.Content)}, nil
}
func (f *fakeBackend) ApplyProjectPatch(_ context.Context, req service.ProjectPatchRequest) (service.ProjectPatchResult, error) {
	return service.ProjectPatchResult{ProjectID: req.ProjectID, Path: req.Path, BeforeSHA256: req.ExpectedSHA256, AfterSHA256: "after", Replacements: req.ExpectedCount}, nil
}
func (f *fakeBackend) RunProjectCommand(_ context.Context, projectID, executable string, args []string, cwd string, timeoutSeconds, maxOutputBytes int) (service.ProjectCommandResult, error) {
	return service.ProjectCommandResult{ProjectID: projectID, Executable: executable, Args: args, Cwd: cwd, Output: "ok", ExitCode: 0}, nil
}
func (f *fakeBackend) ApplyAndVerifyProject(_ context.Context, projectID string, changes []service.ProjectOwnedChange, verification []service.ProjectVerifyCommand) (service.ProjectApplyVerifyResult, error) {
	return service.ProjectApplyVerifyResult{ProjectID: projectID, Changes: []service.ProjectChangeResult{}, Verification: []service.ProjectCommandResult{}, Passed: true}, nil
}
func (f *fakeBackend) ListProjectBranches(_ context.Context, projectID string) (service.ProjectBranchListResult, error) {
	return service.ProjectBranchListResult{ProjectID: projectID, Current: "master", Branches: []string{"master"}}, nil
}
func (f *fakeBackend) CreateProjectBranch(_ context.Context, projectID, name string) (service.ProjectBranchResult, error) {
	return service.ProjectBranchResult{ProjectID: projectID, Name: name, Revision: "abc123"}, nil
}
func (f *fakeBackend) CreateProjectWorktree(_ context.Context, projectID, baseline, purpose string) (service.ProjectWorktreeResult, error) {
	return service.ProjectWorktreeResult{ProjectID: projectID, Path: "C:/tmp/worktree", Baseline: baseline, Head: baseline, Purpose: purpose}, nil
}
func (f *fakeBackend) StageProjectPaths(_ context.Context, projectID string, paths []string) (service.ProjectGitActionResult, error) {
	return service.ProjectGitActionResult{ProjectID: projectID, Operation: "git_stage", Paths: paths}, nil
}
func (f *fakeBackend) CommitProject(_ context.Context, projectID, message string) (service.ProjectGitActionResult, error) {
	return service.ProjectGitActionResult{ProjectID: projectID, Operation: "git_commit", Revision: "abc123", Output: message}, nil
}
func (f *fakeBackend) PushProject(_ context.Context, projectID, remote string) (service.ProjectGitActionResult, error) {
	return service.ProjectGitActionResult{ProjectID: projectID, Operation: "git_push", Remote: remote, Branch: "master"}, nil
}
func (f *fakeBackend) FindProjectFiles(_ context.Context, projectID, query string, maxResults int) (service.ProjectFindResult, error) {
	if projectID == "" {
		return service.ProjectFindResult{}, errors.New("project_id is required for project find")
	}
	return service.ProjectFindResult{ProjectID: projectID, Query: query, Matches: []service.ProjectFindMatch{{Path: "README.md", Kind: "file", Rank: 0}}}, nil
}
func (f *fakeBackend) SearchProjectText(_ context.Context, projectID, path, query string, maxResults int) (service.ProjectSearchResult, error) {
	if projectID == "" {
		return service.ProjectSearchResult{}, errors.New("project_id is required for project search")
	}
	if strings.TrimSpace(query) == "" {
		return service.ProjectSearchResult{}, errors.New("search query is required")
	}
	return service.ProjectSearchResult{ProjectID: projectID, Path: path, Query: query, Matches: []service.ProjectSearchMatch{{Path: "README.md", Line: 2, Text: "needle"}}}, nil
}
func (f *fakeBackend) ProjectContext(_ context.Context, projectID string) ([]service.ProjectContextItem, error) {
	if projectID == "" {
		projectID = "mar"
	}
	return []service.ProjectContextItem{{
		ProjectID: projectID,
		Head:      "abc123",
		Policy:    domain.ProjectPolicy{ProjectID: projectID, LocalFileWrite: true, LocalGitWrite: true},
		Capability: service.ProjectCapability{
			State:                          "supported",
			Ecosystems:                     []string{"go"},
			Languages:                      []string{"go"},
			EvidenceMarkers:                []string{"go.mod"},
			SupportedVerificationProfiles:  []string{"go-standard", "go-docs"},
			RecommendedVerificationProfile: "go-standard",
		},
	}}, nil
}

type advertisedProfileBackend struct {
	fakeBackend
	profiles    []string
	recommended string
}

func (b *advertisedProfileBackend) ProjectContext(_ context.Context, projectID string) ([]service.ProjectContextItem, error) {
	if projectID == "" {
		projectID = "mar"
	}
	return []service.ProjectContextItem{{
		ProjectID: projectID,
		Head:      "abc123",
		Policy:    domain.ProjectPolicy{ProjectID: projectID, LocalFileWrite: true, LocalGitWrite: true},
		Capability: service.ProjectCapability{
			State:                          "supported",
			Ecosystems:                     []string{"artifact"},
			SupportedVerificationProfiles:  append([]string(nil), b.profiles...),
			RecommendedVerificationProfile: b.recommended,
		},
	}}, nil
}

type largeReadBackend struct{ fakeBackend }

type largeReceiptBackend struct{ fakeBackend }

func (b *largeReceiptBackend) Submit(_ context.Context, key string, contract domain.GoalContract) (domain.Task, bool, error) {
	return domain.Task{ID: "task-large-receipt", IdempotencyKey: key, Contract: contract, ContractHash: "hash", State: domain.TaskSubmitted, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}, true, nil
}

func (b *largeReceiptBackend) StatusSnapshot(_ context.Context, taskID string) (service.TaskStatusSnapshot, error) {
	return service.TaskStatusSnapshot{Task: domain.Task{ID: taskID, State: domain.TaskInputRequired, RunEpoch: 4, Contract: domain.GoalContract{Goal: strings.Repeat("status-secret-marker-", 8<<10), ProjectID: "mar"}}, BrainTurnAvailable: true, Detail: "waiting", NextAction: "brain_turn"}, nil
}

func (b *largeReceiptBackend) PendingWebTurn(_ context.Context, taskID string) (domain.WebTurn, bool, error) {
	request := json.RawMessage(`{"messages":[{"role":"user","content":"` + strings.Repeat("brain-turn-request-marker-", 8<<10) + `"}],"tools":[]}`)
	return domain.WebTurn{
		ID: "turn-current", TaskID: taskID, AttemptID: "attempt-current", RunEpoch: 4,
		RequestID: "request-current", Request: request, RequestHash: "request-hash", IntegrityHash: "integrity-hash",
		CreatedAt: time.Unix(2, 0).UTC(),
	}, true, nil
}

func (b *largeReceiptBackend) RespondWebTurn(_ context.Context, taskID, turnID string, _ model.Message, _ string) (domain.WebTurn, bool, error) {
	now := time.Unix(2, 0).UTC()
	return domain.WebTurn{ID: turnID, TaskID: taskID, AttemptID: "attempt-current", RunEpoch: 4, RequestID: "request-current", Request: json.RawMessage(`{"secret":"` + strings.Repeat("brain-secret-marker-", 8<<10) + `"}`), ResponseHash: "response-hash", RespondedAt: &now}, true, nil
}

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

func TestSubmitRejectsUnsupportedVerificationProfileBeforeBackend(t *testing.T) {
	backend := &fakeBackend{}
	session := connectTestMCP(t, backend)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "submit", Arguments: map[string]any{
		"idempotency_key": "invalid-profile", "contract": map[string]any{
			"goal": "bounded change", "acceptance": []string{"done"}, "boundaries": []string{"bounded"}, "non_goals": []string{"none"}, "project_id": "mar", "base_revision": "abc", "verification_profile": "minimal", "priority": "P2", "authority": map[string]any{"local_file_write": false, "local_git_write": false, "network_allowed": false, "remote_git_write": false, "deploy_allowed": false},
		},
	}})
	if err != nil {
		t.Fatalf("invalid profile escaped as protocol error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("unsupported verification profile was accepted: %+v", result.StructuredContent)
	}
	if backend.submitCalls != 0 {
		t.Fatalf("unsupported verification profile reached backend submit: calls=%d", backend.submitCalls)
	}
	text, _ := json.Marshal(result.Content)
	if !strings.Contains(string(text), "go-standard") || !strings.Contains(string(text), "go-docs") {
		t.Fatalf("validation error omitted project-advertised supported profiles: %s", text)
	}

	valid, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "submit", Arguments: map[string]any{
		"idempotency_key": "valid-profile", "contract": map[string]any{
			"goal": "bounded docs change", "acceptance": []string{"done"}, "boundaries": []string{"bounded"}, "non_goals": []string{"none"}, "project_id": "mar", "base_revision": "abc", "verification_profile": "go-docs", "priority": "P2", "authority": map[string]any{"local_file_write": false, "local_git_write": false, "network_allowed": false, "remote_git_write": false, "deploy_allowed": false},
		},
	}})
	if err != nil || valid.IsError {
		t.Fatalf("supported verification profile was rejected: err=%v result=%+v", err, valid)
	}
	if backend.submitCalls != 1 {
		t.Fatalf("supported verification profile did not reach backend exactly once: calls=%d", backend.submitCalls)
	}
}

func TestSubmitUsesProjectAdvertisedProfilesForMarkerlessAndGoRelease(t *testing.T) {
	cases := []struct {
		name     string
		profile  string
		profiles []string
	}{
		{name: "markerless-research", profile: "research-artifacts", profiles: []string{"research-artifacts"}},
		{name: "go-release", profile: "go-release", profiles: []string{"go-standard", "go-docs", "go-release"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := &advertisedProfileBackend{profiles: tc.profiles, recommended: tc.profile}
			session := connectTestMCP(t, backend)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "submit", Arguments: map[string]any{
				"idempotency_key": "advertised-" + tc.name, "contract": map[string]any{
					"goal": "bounded project-advertised profile", "acceptance": []string{"done"}, "boundaries": []string{"bounded"}, "non_goals": []string{"none"}, "project_id": "project", "base_revision": "abc", "verification_profile": tc.profile, "priority": "P2", "authority": map[string]any{"local_file_write": false, "local_git_write": false, "network_allowed": false, "remote_git_write": false, "deploy_allowed": false},
				},
			}})
			if err != nil || result.IsError {
				t.Fatalf("project-advertised profile %q was rejected: err=%v result=%+v", tc.profile, err, result)
			}
			if backend.submitCalls != 1 {
				t.Fatalf("project-advertised profile %q did not reach backend exactly once: calls=%d", tc.profile, backend.submitCalls)
			}
		})
	}
}

func TestPublicToolSurfaceListsTrustedOwnerActionWithCanonicalTools(t *testing.T) {
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
	want := []string{"action", "brain_respond", "brain_turn", "control", "project", "submit", "task"}
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

func TestPublicToolSurfaceAddsFastToolOnlyForAutomaticDeltaBackend(t *testing.T) {
	session := connectTestMCP(t, &deltaReceiptBackend{})
	listed, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{"action", "brain_respond", "brain_turn", "brain_turn_fast", "control", "project", "submit", "task"}
	if len(names) != len(want) {
		t.Fatalf("unexpected capable public tool count: got=%v want=%v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("capable public MCP surface mismatch: got=%v want=%v", names, want)
		}
	}
}

func TestSubmitStatusAndBrainRespondUseCompactNoEchoReceipts(t *testing.T) {
	session := connectTestMCP(t, &largeReceiptBackend{})
	goalMarker := strings.Repeat("submit-secret-marker-", 8<<10)
	submitResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "submit", Arguments: map[string]any{"idempotency_key": "compact-submit", "contract": map[string]any{"goal": goalMarker, "project_id": "mar", "base_revision": "abc", "verification_profile": "go-standard", "priority": "P2", "authority": map[string]any{}}}})
	if err != nil {
		t.Fatal(err)
	}
	for name, result := range map[string]*mcp.CallToolResult{"submit": submitResult} {
		raw, _ := json.Marshal(result.StructuredContent)
		if strings.Contains(string(raw), "submit-secret-marker-") || len(raw) > 4096 {
			t.Fatalf("%s receipt echoed oversized request: len=%d", name, len(raw))
		}
	}
	statusResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "status", Arguments: map[string]any{"task_id": "task-large-receipt"}})
	if err != nil {
		t.Fatal(err)
	}
	statusRaw, _ := json.Marshal(statusResult.StructuredContent)
	if strings.Contains(string(statusRaw), "status-secret-marker-") || len(statusRaw) > 4096 {
		t.Fatalf("status receipt echoed Goal Contract: len=%d", len(statusRaw))
	}
	brainResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "brain_respond", Arguments: map[string]any{"task_id": "task-large-receipt", "turn_id": "turn-current", "content": "done"}})
	if err != nil {
		t.Fatal(err)
	}
	brainRaw, _ := json.Marshal(brainResult.StructuredContent)
	if strings.Contains(string(brainRaw), "brain-secret-marker-") || len(brainRaw) > 4096 {
		t.Fatalf("brain_respond receipt echoed request: len=%d", len(brainRaw))
	}
}

func TestBrainTurnStructuredModePreservesStructuredPayloadAndCutsDuplicateText(t *testing.T) {
	session := connectTestMCP(t, &largeReceiptBackend{})

	compat, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "brain_turn", Arguments: map[string]any{
		"task_id": "task-large-receipt",
	}})
	if err != nil {
		t.Fatal(err)
	}
	structured, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "brain_turn", Arguments: map[string]any{
		"task_id": "task-large-receipt", "response_mode": "structured",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if compat.IsError || structured.IsError {
		t.Fatalf("brain_turn mode call failed: compat=%+v structured=%+v", compat, structured)
	}

	compatStructured, err := json.Marshal(compat.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	structuredStructured, err := json.Marshal(structured.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if string(compatStructured) != string(structuredStructured) {
		t.Fatal("structured response mode changed the authoritative brain_turn payload")
	}
	if !strings.Contains(string(structuredStructured), "brain-turn-request-marker-") {
		t.Fatal("structured response mode lost the exact TurnRequest content")
	}

	structuredText, err := json.Marshal(structured.Content)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(structuredText), "brain-turn-request-marker-") {
		t.Fatal("structured response mode duplicated TurnRequest content into TextContent")
	}
	if !strings.Contains(string(structuredText), "turn-current") || !strings.Contains(string(structuredText), "attempt-current") {
		t.Fatalf("structured response mode omitted stable turn identity from compact receipt: %s", structuredText)
	}

	compatSerialized, err := json.Marshal(compat)
	if err != nil {
		t.Fatal(err)
	}
	structuredSerialized, err := json.Marshal(structured)
	if err != nil {
		t.Fatal(err)
	}
	if len(structuredSerialized)*100 > len(compatSerialized)*60 {
		t.Fatalf("structured response mode did not materially reduce application payload: structured=%d compat=%d ratio=%.3f", len(structuredSerialized), len(compatSerialized), float64(len(structuredSerialized))/float64(len(compatSerialized)))
	}
}

func TestProjectReadDoesNotRequireGoalContractProjectIDOrBaseRevision(t *testing.T) {
	session := connectTestMCP(t, &fakeBackend{})
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project_read", Arguments: map[string]any{"path": "README.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("project_read returned tool error: %+v", result.Content)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "inferred-project") || !strings.Contains(text, "README.md") || strings.Contains(text, "base_revision") {
		t.Fatalf("unexpected lightweight read result: %s", text)
	}
}

func TestProjectContextLetsWebTechLeadResolveTechnicalGoalFieldsWithoutOwnerPrompt(t *testing.T) {
	session := connectTestMCP(t, &fakeBackend{})
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project_context", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("project_context returned tool error: %+v", result.Content)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"project_id":"mar"`) ||
		!strings.Contains(text, `"head":"abc123"`) ||
		!strings.Contains(text, `"local_file_write":true`) ||
		!strings.Contains(text, `"evidence_markers":["go.mod"]`) ||
		!strings.Contains(text, `"supported_verification_profiles":["go-standard","go-docs"]`) ||
		!strings.Contains(text, `"recommended_verification_profile":"go-standard"`) {
		t.Fatalf("project_context omitted technical capability inputs: %s", text)
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

type deltaReceiptBackend struct{ largeReceiptBackend }

func (b *deltaReceiptBackend) CognitionDelta(_ context.Context, turn domain.WebTurn, cursor string) (service.CognitionDelta, error) {
	return service.CognitionDelta{
		Version: 1,
		Mode:    "delta",
		Event: service.CognitionEvent{
			Kind: service.CognitionEventDecisionRequired, Sequence: 2,
			TaskID: turn.TaskID, RunEpoch: turn.RunEpoch, TaskState: domain.TaskRunning, CurrentRevision: "rev-current",
		},
		BaseCursor: cursor,
		Cursor:     "cursor-next",
		Delta: &service.CognitionRequestDelta{
			RequestID: turn.RequestID,
			Projection: map[string]json.RawMessage{
				"recent_protocol_evidence": json.RawMessage("{}"),
			},
		},
		FullBytes:    128 << 10,
		PayloadBytes: 512,
	}, nil
}

func (b *deltaReceiptBackend) AutomaticCognitionDelta(ctx context.Context, turn domain.WebTurn) (service.CognitionDelta, error) {
	return b.CognitionDelta(ctx, turn, "automatic-base")
}

func TestBrainTurnFastUsesAutomaticDeltaWithoutCursor(t *testing.T) {
	session := connectTestMCP(t, &deltaReceiptBackend{})
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "brain_turn_fast", Arguments: map[string]any{
		"task_id": "task-large-receipt",
	}})
	if err != nil || result.IsError {
		t.Fatalf("brain_turn_fast failed: err=%v result=%+v", err, result)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"cognition"`) || !strings.Contains(text, `"mode":"delta"`) || !strings.Contains(text, `"base_cursor":"automatic-base"`) {
		t.Fatalf("brain_turn_fast omitted automatic delta metadata: %s", text)
	}
	if strings.Contains(text, "brain-turn-request-marker-") || strings.Contains(text, `"request":`) {
		t.Fatalf("brain_turn_fast leaked full request in delta mode: %d bytes", len(raw))
	}
	content, _ := json.Marshal(result.Content)
	if !strings.Contains(string(content), "brain_turn_fast") || !strings.Contains(string(content), "mode=delta") {
		t.Fatalf("brain_turn_fast compact receipt is incomplete: %s", content)
	}
}

func TestBrainTurnDeltaModeIsOptInAndPreservesDefaultPayload(t *testing.T) {
	session := connectTestMCP(t, &deltaReceiptBackend{})

	full, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "brain_turn", Arguments: map[string]any{
		"task_id": "task-large-receipt", "response_mode": "structured",
	}})
	if err != nil || full.IsError {
		t.Fatalf("default structured brain_turn failed: err=%v result=%+v", err, full)
	}
	fullRaw, err := json.Marshal(full.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fullRaw), "brain-turn-request-marker-") {
		t.Fatal("default brain_turn no longer returns the exact authoritative full WebTurn")
	}

	delta, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "brain_turn", Arguments: map[string]any{
		"task_id": "task-large-receipt", "response_mode": "structured",
		"context_mode": "delta", "cognition_cursor": "cursor-prev",
	}})
	if err != nil || delta.IsError {
		t.Fatalf("delta brain_turn failed: err=%v result=%+v", err, delta)
	}
	deltaRaw, err := json.Marshal(delta.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	text := string(deltaRaw)
	if !strings.Contains(text, `"cognition"`) || !strings.Contains(text, `"mode":"delta"`) || !strings.Contains(text, `"cursor":"cursor-next"`) {
		t.Fatalf("delta brain_turn omitted bounded cognition metadata: %s", text)
	}
	if strings.Contains(text, "brain-turn-request-marker-") || strings.Contains(text, `"request":`) {
		t.Fatalf("delta brain_turn leaked the full request payload: len=%d", len(deltaRaw))
	}
	if len(deltaRaw)*100 > len(fullRaw)*20 {
		t.Fatalf("delta MCP view did not materially reduce outward bytes: delta=%d full=%d ratio=%.3f", len(deltaRaw), len(fullRaw), float64(len(deltaRaw))/float64(len(fullRaw)))
	}

	invalid, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "brain_turn", Arguments: map[string]any{
		"task_id": "task-large-receipt", "context_mode": "delta",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !invalid.IsError {
		t.Fatal("delta context without structured response mode was accepted")
	}
}

func TestCallActionApplyAndVerify(t *testing.T) {
	content := "created\n"
	value, err := callAction(context.Background(), &fakeBackend{}, actionArgs{
		Operation: "apply_and_verify",
		ProjectID: "mar",
		Changes: []actionChangeArgs{{
			Path: "created.txt", ExpectedSHA256: "ABSENT", Content: &content,
		}},
		Verify: []actionVerifyArgs{{
			Executable: "go", Args: []string{"test", "./..."}, Cwd: ".",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value["apply_and_verify"].(service.ProjectApplyVerifyResult)
	if !ok {
		t.Fatalf("unexpected result type: %T", value["apply_and_verify"])
	}
	if !result.Passed || result.ProjectID != "mar" {
		t.Fatalf("unexpected apply_and_verify result: %+v", result)
	}
}
func TestProjectAttachExplicitNetworkAllowed(t *testing.T) {
	session := connectTestMCP(t, &fakeBackend{})
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "project", Arguments: map[string]any{
		"operation": "attach", "path": "C:\\repo", "network_allowed": true,
	}})
	if err != nil || result.IsError {
		t.Fatalf("project attach failed: err=%v result=%+v", err, result)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\"network_allowed\":true") {
		t.Fatalf("attach did not preserve explicit network authority: %s", raw)
	}
}
