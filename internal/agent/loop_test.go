package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/contextengine"
	"mar/internal/domain"
	"mar/internal/model"
	"mar/internal/service"
	"mar/internal/store"
)

type scriptedGateway struct {
	responses []model.TurnResponse
	errs      []error
	requests  []model.TurnRequest
	hook      func(int, model.TurnRequest)
}

func (g *scriptedGateway) Turn(_ context.Context, req model.TurnRequest) (model.TurnResponse, error) {
	g.requests = append(g.requests, req)
	index := len(g.requests) - 1
	if g.hook != nil {
		g.hook(index, req)
	}
	if index < len(g.errs) && g.errs[index] != nil {
		return model.TurnResponse{}, g.errs[index]
	}
	if index >= len(g.responses) {
		return model.TurnResponse{}, errors.New("scripted gateway exhausted")
	}
	return g.responses[index], nil
}

type blockingGateway struct{}

func (blockingGateway) Turn(ctx context.Context, _ model.TurnRequest) (model.TurnResponse, error) {
	<-ctx.Done()
	return model.TurnResponse{}, ctx.Err()
}

type fakeTools struct {
	safe    bool
	defs    []model.ToolDefinition
	calls   []model.ToolCall
	outputs map[string]string
	errs    map[string]error
}

func (f *fakeTools) ToolDefinitions() []model.ToolDefinition {
	return append([]model.ToolDefinition(nil), f.defs...)
}

func (f *fakeTools) ExecuteTool(_ context.Context, call model.ToolCall) (string, error) {
	f.calls = append(f.calls, call)
	if err := f.errs[call.Name]; err != nil {
		return "", err
	}
	if output, ok := f.outputs[call.Name]; ok {
		return output, nil
	}
	return `{"ok":true}`, nil
}

func (f *fakeTools) SelfHostingSafe() bool { return f.safe }

type fakeContextBuilder struct {
	pack contextengine.Pack
	err  error
}

type fakeAttemptAuthority struct {
	calls int
	fn    func(int, string, string, int64) (bool, error)
}

func (f *fakeAttemptAuthority) AttemptAuthoritative(_ context.Context, taskID, attemptID string, epoch int64) (bool, error) {
	f.calls++
	if f.fn != nil {
		return f.fn(f.calls, taskID, attemptID, epoch)
	}
	return true, nil
}

type fakeCheckpointStore struct {
	latest     domain.SemanticCheckpoint
	hasLatest  bool
	latestErr  error
	publishErr error
	published  []domain.SemanticCheckpointPayload
}

func (f *fakeCheckpointStore) LatestValidCheckpoint(context.Context, string) (domain.SemanticCheckpoint, bool, error) {
	return f.latest, f.hasLatest, f.latestErr
}

func (f *fakeCheckpointStore) PublishCheckpoint(_ context.Context, taskID, attemptID string, epoch int64, currentRevision string, payload domain.SemanticCheckpointPayload) (domain.SemanticCheckpoint, error) {
	if f.publishErr != nil {
		return domain.SemanticCheckpoint{}, f.publishErr
	}
	f.published = append(f.published, payload)
	return domain.SemanticCheckpoint{ID: "checkpoint-fake", TaskID: taskID, AttemptID: attemptID, RunEpoch: epoch, Version: int64(len(f.published)), CurrentRevision: currentRevision, IntegrityHash: strings.Repeat("a", 64)}, nil
}

func (f fakeContextBuilder) Build(ctx context.Context, req contextengine.Request) (contextengine.Pack, error) {
	if err := ctx.Err(); err != nil {
		return contextengine.Pack{}, err
	}
	if f.err != nil {
		return contextengine.Pack{}, f.err
	}
	pack := f.pack
	if pack.Revision == "" {
		pack.Revision = req.ExpectedRevision
	}
	if pack.GoalHash == "" {
		hash, err := req.Contract.Hash()
		if err != nil {
			return contextengine.Pack{}, err
		}
		pack.GoalHash = hash
	}
	return pack, nil
}

func TestLoopExecutesToolTurnsAndRequiresExplicitFinish(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(70, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "call-write", Name: "write_file", Arguments: `{"path":"a.txt","expected_sha256":"ABSENT","content":"hello"}`}}}),
		assistantResponse(60, model.Message{Role: model.RoleAssistant, Content: "ready", ToolCalls: []model.ToolCall{{ID: "call-finish", Name: finishToolName, Arguments: `{"status":"completed_candidate","summary":"implementation ready for verification"}`}}}),
	}}
	tools := newFakeTools(true, "read_file", "write_file", "run_command")
	loop := newTestLoop(t, gateway, tools, testConfig())
	result, err := loop.Run(context.Background(), testRunRequest(true))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCompletedCandidate || result.Turns != 2 || result.ToolCalls != 2 || result.Usage.TotalTokens != 130 {
		t.Fatalf("unexpected loop result: %+v", result)
	}
	if len(tools.calls) != 1 || tools.calls[0].Name != "write_file" {
		t.Fatalf("coding tool execution mismatch: %+v", tools.calls)
	}
	if len(gateway.requests) != 2 || gateway.requests[0].MaxOutputTokens != testConfig().MaxOutputTokensPerTurn {
		t.Fatalf("unexpected model requests: %+v", gateway.requests)
	}
	if !hasTool(gateway.requests[0].Tools, finishToolName) || !hasTool(gateway.requests[0].Tools, "write_file") {
		t.Fatalf("pinned tools missing expected definitions: %+v", gateway.requests[0].Tools)
	}
	secondMessages := gateway.requests[1].Messages
	if len(secondMessages) < 4 || secondMessages[len(secondMessages)-1].Role != model.RoleTool || secondMessages[len(secondMessages)-1].ToolCallID != "call-write" {
		t.Fatalf("tool observation was not fed back into next model turn: %+v", secondMessages)
	}
	if !strings.Contains(gateway.requests[0].Messages[0].Content, "UNTRUSTED EVIDENCE") || !strings.Contains(gateway.requests[0].Messages[1].Content, "AUTHORITATIVE_GOAL_CONTRACT_JSON") {
		t.Fatalf("trust boundary missing from initial prompt: %+v", gateway.requests[0].Messages)
	}
}

func TestLoopFiltersMutationToolsForReadOnlyGoalAndRejectsHallucinatedWrite(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(40, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "write-denied", Name: "write_file", Arguments: `{}`}}}),
		assistantResponse(40, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "finish", Name: finishToolName, Arguments: `{"status":"completed_candidate","summary":"read-only analysis complete"}`}}}),
	}}
	tools := newFakeTools(true, "read_file", "search_text", "write_file", "replace_exact", "run_command", "git_status", "git_diff")
	loop := newTestLoop(t, gateway, tools, testConfig())
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCompletedCandidate || len(tools.calls) != 0 {
		t.Fatalf("read-only authority was widened: result=%+v calls=%+v", result, tools.calls)
	}
	for _, forbidden := range []string{"write_file", "replace_exact", "run_command"} {
		if hasTool(gateway.requests[0].Tools, forbidden) {
			t.Fatalf("read-only turn exposed forbidden tool %s", forbidden)
		}
	}
	last := gateway.requests[1].Messages[len(gateway.requests[1].Messages)-1]
	if last.Role != model.RoleTool || !strings.Contains(last.Content, "not available under this Goal Contract authority") {
		t.Fatalf("hallucinated forbidden tool did not become bounded error observation: %+v", last)
	}
}

func TestLoopFailsClosedOnUnclassifiedRuntimeTool(t *testing.T) {
	gateway := &scriptedGateway{}
	tools := newFakeTools(true, "read_file", "shell_anything")
	loop := newTestLoop(t, gateway, tools, testConfig())
	_, err := loop.Run(context.Background(), testRunRequest(true))
	if err == nil || !strings.Contains(err.Error(), "unclassified coding tool") {
		t.Fatalf("expected unclassified-tool failure, got %v", err)
	}
	if len(gateway.requests) != 0 {
		t.Fatal("model was called after authority classification failed")
	}
}

func TestLoopRejectsStaleAttemptBeforeModelTurn(t *testing.T) {
	gateway := &scriptedGateway{}
	authority := &fakeAttemptAuthority{fn: func(_ int, _, _ string, _ int64) (bool, error) { return false, nil }}
	loop, err := New(gateway, newFakeTools(true, "read_file"), fakeContextBuilder{}, authority, &fakeCheckpointStore{}, Profile{Model: "test-model", BaseInstructions: "trusted"}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCancelled || len(gateway.requests) != 0 || authority.calls != 1 {
		t.Fatalf("stale attempt reached model: result=%+v requests=%d checks=%d", result, len(gateway.requests), authority.calls)
	}
}

func TestLoopFencesAttemptAfterModelBeforeMutationDispatch(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "write-after-fence", Name: "write_file", Arguments: `{}`}}}),
	}}
	tools := newFakeTools(true, "write_file")
	authority := &fakeAttemptAuthority{fn: func(call int, _, _ string, _ int64) (bool, error) {
		return call < 3, nil
	}}
	loop, err := New(gateway, tools, fakeContextBuilder{}, authority, &fakeCheckpointStore{}, Profile{Model: "test-model", BaseInstructions: "trusted"}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := loop.Run(context.Background(), testRunRequest(true))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCancelled || len(tools.calls) != 0 || len(gateway.requests) != 1 || authority.calls != 3 {
		t.Fatalf("fenced attempt mutated after model turn: result=%+v tools=%+v requests=%d checks=%d", result, tools.calls, len(gateway.requests), authority.calls)
	}
}

func TestLoopAttemptAuthorityInfrastructureErrorBlocksBeforeModel(t *testing.T) {
	gateway := &scriptedGateway{}
	authority := &fakeAttemptAuthority{fn: func(_ int, _, _ string, _ int64) (bool, error) { return false, errors.New("db unavailable") }}
	loop, err := New(gateway, newFakeTools(true, "read_file"), fakeContextBuilder{}, authority, &fakeCheckpointStore{}, Profile{Model: "test-model", BaseInstructions: "trusted"}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBlocked || !strings.Contains(result.Blocker, "db unavailable") || len(gateway.requests) != 0 {
		t.Fatalf("authority infrastructure failure was not fail-closed: %+v", result)
	}
}

func TestLoopRejectsUnsafeCodingRuntimeBeforeModelTurn(t *testing.T) {
	gateway := &scriptedGateway{}
	tools := newFakeTools(false, "read_file")
	loop := newTestLoop(t, gateway, tools, testConfig())
	_, err := loop.Run(context.Background(), testRunRequest(false))
	if err == nil || !strings.Contains(err.Error(), "self-hosting-safe") {
		t.Fatalf("expected unsafe-runtime rejection, got %v", err)
	}
	if len(gateway.requests) != 0 {
		t.Fatal("model was called with unsafe coding runtime")
	}
}

func TestLoopDoesNotExecuteMixedFinishAndMutationBatch(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(50, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{
			{ID: "write", Name: "write_file", Arguments: `{}`},
			{ID: "finish", Name: finishToolName, Arguments: `{"status":"completed_candidate","summary":"done"}`},
		}}),
	}}
	tools := newFakeTools(true, "write_file")
	loop := newTestLoop(t, gateway, tools, testConfig())
	result, err := loop.Run(context.Background(), testRunRequest(true))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBlocked || !strings.Contains(result.Blocker, "finish_task must be the sole") || len(tools.calls) != 0 {
		t.Fatalf("mixed finish batch was not fail-closed: result=%+v calls=%+v", result, tools.calls)
	}
}

func TestLoopToolBudgetStopsWholeBatchBeforeSideEffects(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(30, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{
			{ID: "one", Name: "read_file", Arguments: `{"path":"a"}`},
			{ID: "two", Name: "read_file", Arguments: `{"path":"b"}`},
		}}),
	}}
	tools := newFakeTools(true, "read_file")
	cfg := testConfig()
	cfg.MaxToolCalls = 1
	cfg.MaxToolCallsPerTurn = 1
	loop := newTestLoop(t, gateway, tools, cfg)
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBudgetExhausted || len(tools.calls) != 0 {
		t.Fatalf("tool budget allowed partial batch execution: result=%+v calls=%+v", result, tools.calls)
	}
}

func TestLoopTokenBudgetStopsTurnBeforeToolSideEffects(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(120, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "write", Name: "write_file", Arguments: `{}`}}}),
	}}
	tools := newFakeTools(true, "write_file")
	cfg := testConfig()
	cfg.MaxTotalTokens = 100
	cfg.MaxOutputTokensPerTurn = 50
	loop := newTestLoop(t, gateway, tools, cfg)
	result, err := loop.Run(context.Background(), testRunRequest(true))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBudgetExhausted || len(tools.calls) != 0 || result.Usage.TotalTokens != 120 {
		t.Fatalf("token-overrun turn executed side effects or under-reported consumed usage: result=%+v calls=%+v", result, tools.calls)
	}
}

func TestLoopRequiresProviderTokenAccounting(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "finish", Name: finishToolName, Arguments: `{"status":"completed_candidate","summary":"done"}`}}}}}}
	loop := newTestLoop(t, gateway, newFakeTools(true, "read_file"), testConfig())
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBlocked || !strings.Contains(result.Blocker, "token accounting") {
		t.Fatalf("missing usage was not fail-closed: %+v", result)
	}
}

func TestLoopMalformedFinishReturnsToolErrorAndCanRecover(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(25, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "bad-finish", Name: finishToolName, Arguments: `{"status":"wrong","summary":"x"}`}}}),
		assistantResponse(25, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "good-finish", Name: finishToolName, Arguments: `{"status":"blocked","summary":"need owner choice","blocker":"ambiguous API contract"}`}}}),
	}}
	loop := newTestLoop(t, gateway, newFakeTools(true, "read_file"), testConfig())
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBlocked || result.ToolCalls != 2 || result.Blocker != "ambiguous API contract" {
		t.Fatalf("malformed finish recovery failed: %+v", result)
	}
	last := gateway.requests[1].Messages[len(gateway.requests[1].Messages)-1]
	if last.Role != model.RoleTool || !strings.Contains(last.Content, "unsupported finish_task status") {
		t.Fatalf("malformed finish error was not fed back: %+v", last)
	}
}

func TestLoopRejectsDuplicateToolCallIDBeforeSecondExecution(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "same", Name: "read_file", Arguments: `{"path":"a"}`}}}),
		assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "same", Name: "read_file", Arguments: `{"path":"b"}`}}}),
	}}
	tools := newFakeTools(true, "read_file")
	loop := newTestLoop(t, gateway, tools, testConfig())
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBlocked || len(tools.calls) != 1 || !strings.Contains(result.Blocker, "duplicate tool call id") {
		t.Fatalf("duplicate call id was not fenced: result=%+v calls=%+v", result, tools.calls)
	}
}

func TestLoopRejectsDuplicateToolCallIDWithinBatchBeforeAnyExecution(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{
			{ID: "same-batch", Name: "read_file", Arguments: `{"path":"a"}`},
			{ID: "same-batch", Name: "read_file", Arguments: `{"path":"b"}`},
		}}),
	}}
	tools := newFakeTools(true, "read_file")
	loop := newTestLoop(t, gateway, tools, testConfig())
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBlocked || len(tools.calls) != 0 || !strings.Contains(result.Blocker, "within batch") {
		t.Fatalf("same-batch duplicate call id was not fenced before side effects: result=%+v calls=%+v", result, tools.calls)
	}
}

func TestLoopProviderLengthFinishReasonStopsTools(t *testing.T) {
	response := assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "partial", Name: "write_file", Arguments: `{"path":"a.txt"}`}}})
	response.FinishReason = "length"
	gateway := &scriptedGateway{responses: []model.TurnResponse{response}}
	tools := newFakeTools(true, "write_file")
	loop := newTestLoop(t, gateway, tools, testConfig())
	result, err := loop.Run(context.Background(), testRunRequest(true))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBudgetExhausted || len(tools.calls) != 0 || result.Usage.TotalTokens != 20 || !strings.Contains(result.Blocker, "output-length") {
		t.Fatalf("truncated provider turn reached tools: result=%+v calls=%+v", result, tools.calls)
	}
}

func TestLoopProviderOutputTokenCapViolationStopsToolsAndAccountsUsage(t *testing.T) {
	response := model.TurnResponse{
		Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "too-long", Name: "write_file", Arguments: `{}`}}},
		Usage:   model.Usage{InputTokens: 10, OutputTokens: 250, TotalTokens: 260},
	}
	gateway := &scriptedGateway{responses: []model.TurnResponse{response}}
	tools := newFakeTools(true, "write_file")
	cfg := testConfig()
	cfg.MaxOutputTokensPerTurn = 200
	loop := newTestLoop(t, gateway, tools, cfg)
	result, err := loop.Run(context.Background(), testRunRequest(true))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBudgetExhausted || len(tools.calls) != 0 || result.Usage.TotalTokens != 260 || !strings.Contains(result.Blocker, "output token cap") {
		t.Fatalf("provider output-cap violation was not fenced/accounted: result=%+v calls=%+v", result, tools.calls)
	}
}

func TestLoopRejectsMismatchedContextIdentityBeforeModelTurn(t *testing.T) {
	for _, tc := range []struct {
		name string
		pack contextengine.Pack
	}{
		{name: "revision", pack: contextengine.Pack{Revision: "wrong-revision"}},
		{name: "goal-hash", pack: contextengine.Pack{Revision: "rev-test", GoalHash: "wrong-hash"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := &scriptedGateway{}
			loop, err := New(gateway, newFakeTools(true, "read_file"), fakeContextBuilder{pack: tc.pack}, &fakeAttemptAuthority{}, &fakeCheckpointStore{}, Profile{Model: "test-model", BaseInstructions: "trusted"}, testConfig())
			if err != nil {
				t.Fatal(err)
			}
			_, err = loop.Run(context.Background(), testRunRequest(false))
			if err == nil || !strings.Contains(err.Error(), "context builder returned unexpected") {
				t.Fatalf("expected context identity rejection, got %v", err)
			}
			if len(gateway.requests) != 0 {
				t.Fatal("model was called after context identity mismatch")
			}
		})
	}
}

func TestLoopAssistantWithoutFinishOrToolsIsBlocked(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{assistantResponse(20, model.Message{Role: model.RoleAssistant, Content: "I am done"})}}
	loop := newTestLoop(t, gateway, newFakeTools(true, "read_file"), testConfig())
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBlocked || !strings.Contains(result.Blocker, "finish_task is required") {
		t.Fatalf("implicit model completion was accepted: %+v", result)
	}
}

func TestLoopAssistantByteBoundStopsOversizedToolArguments(t *testing.T) {
	huge := strings.Repeat("x", 1000)
	gateway := &scriptedGateway{responses: []model.TurnResponse{assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "huge", Name: "write_file", Arguments: huge}}})}}
	tools := newFakeTools(true, "write_file")
	cfg := testConfig()
	cfg.MaxAssistantBytes = 256
	loop := newTestLoop(t, gateway, tools, cfg)
	result, err := loop.Run(context.Background(), testRunRequest(true))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBudgetExhausted || len(tools.calls) != 0 {
		t.Fatalf("oversized assistant response reached tools: result=%+v calls=%+v", result, tools.calls)
	}
}

func TestLoopInternalDeadlineIsBudgetExhausted(t *testing.T) {
	cfg := testConfig()
	cfg.MaxDuration = 20 * time.Millisecond
	loop := newTestLoop(t, blockingGateway{}, newFakeTools(true, "read_file"), cfg)
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBudgetExhausted || !strings.Contains(result.Blocker, "wall-clock") {
		t.Fatalf("internal deadline terminal mismatch: %+v", result)
	}
}

func TestLoopParentCancellationIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loop := newTestLoop(t, &scriptedGateway{}, newFakeTools(true, "read_file"), testConfig())
	result, err := loop.Run(ctx, testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCancelled {
		t.Fatalf("parent cancellation terminal mismatch: %+v", result)
	}
}

func TestLoopRequestByteBudgetStopsBeforeProviderCall(t *testing.T) {
	gateway := &scriptedGateway{}
	cfg := testConfig()
	cfg.MaxRequestBytes = 1024
	loop := newTestLoop(t, gateway, newFakeTools(true, "read_file"), cfg)
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBudgetExhausted || len(gateway.requests) != 0 {
		t.Fatalf("oversized request reached provider: result=%+v requests=%d", result, len(gateway.requests))
	}
}

func TestLoopPublishesSemanticCheckpointAndContinues(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(30, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "checkpoint-1", Name: checkpointToolName, Arguments: `{"completed_work":["implemented parser"],"current_hypothesis":"parser is correct","changed_areas":["parser.go"],"verification_status":"unit tests pending","blockers":[],"remaining_work":["run tests"],"next_action":"run targeted tests","critical_evidence_refs":["parser.go:10"]}`}}}),
		assistantResponse(30, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "finish", Name: finishToolName, Arguments: `{"status":"completed_candidate","summary":"ready for verification"}`}}}),
	}}
	checkpoints := &fakeCheckpointStore{}
	loop := newTestLoopWithCheckpoints(t, gateway, newFakeTools(true, "read_file"), checkpoints, testConfig())
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCompletedCandidate || len(checkpoints.published) != 1 || checkpoints.published[0].NextAction != "run targeted tests" {
		t.Fatalf("semantic checkpoint was not published: result=%+v published=%+v", result, checkpoints.published)
	}
	if !hasTool(gateway.requests[0].Tools, checkpointToolName) {
		t.Fatal("checkpoint_task was not exposed to the model")
	}
	last := gateway.requests[1].Messages[len(gateway.requests[1].Messages)-1]
	if last.Role != model.RoleTool || last.ToolCallID != "checkpoint-1" || !strings.Contains(last.Content, "checkpoint-fake") {
		t.Fatalf("checkpoint observation was not returned to model: %+v", last)
	}
}

func TestLoopResumesFromLatestValidSemanticCheckpointWithoutTranscriptReplay(t *testing.T) {
	req := testRunRequest(false)
	hash, err := req.Contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := domain.SemanticCheckpoint{
		ID: "checkpoint-resume", TaskID: req.TaskID, AttemptID: "attempt-old", RunEpoch: 1, Version: 3,
		GoalHash: hash, BaseRevision: req.Contract.BaseRevision, CurrentRevision: "rev-prior",
		Payload:   domain.SemanticCheckpointPayload{CompletedWork: []string{"fixed parser"}, CurrentHypothesis: "remaining failure is serializer", ChangedAreas: []string{"parser.go"}, VerificationStatus: "parser tests pass", RemainingWork: []string{"fix serializer"}, NextAction: "inspect serializer", CriticalEvidenceRefs: []string{"test:parser"}},
		CreatedAt: time.Unix(1234, 0).UTC(),
	}
	checkpoint.IntegrityHash, err = checkpoint.IntegrityDigest()
	if err != nil {
		t.Fatal(err)
	}
	checkpoints := &fakeCheckpointStore{latest: checkpoint, hasLatest: true}
	gateway := &scriptedGateway{responses: []model.TurnResponse{assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "finish", Name: finishToolName, Arguments: `{"status":"completed_candidate","summary":"resume seed consumed"}`}}})}}
	loop := newTestLoopWithCheckpoints(t, gateway, newFakeTools(true, "read_file"), checkpoints, testConfig())
	result, err := loop.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResumeCheckpointID != checkpoint.ID || result.ResumeCheckpointVersion != 3 {
		t.Fatalf("resume checkpoint identity missing: %+v", result)
	}
	if len(gateway.requests) != 1 || len(gateway.requests[0].Messages) != 2 {
		t.Fatalf("resume unexpectedly replayed transcript: %+v", gateway.requests)
	}
	initial := gateway.requests[0].Messages[1].Content
	if !strings.Contains(initial, "UNTRUSTED_DURABLE_SEMANTIC_CHECKPOINT_JSON") || !strings.Contains(initial, "inspect serializer") || !strings.Contains(initial, "UNTRUSTED_REPOSITORY_CONTEXT_JSON") {
		t.Fatalf("bounded resume seed missing checkpoint/current evidence: %s", initial)
	}
}

func TestLoopHardBoundsSemanticResumeBeforeProviderCall(t *testing.T) {
	req := testRunRequest(false)
	hash, _ := req.Contract.Hash()
	checkpoint := domain.SemanticCheckpoint{ID: "checkpoint-large", TaskID: req.TaskID, AttemptID: "attempt-old", RunEpoch: 1, Version: 1, GoalHash: hash, BaseRevision: req.Contract.BaseRevision, CurrentRevision: "rev-prior", Payload: domain.SemanticCheckpointPayload{CurrentHypothesis: strings.Repeat("x", 2000), VerificationStatus: "pending", NextAction: "continue"}, CreatedAt: time.Unix(1234, 0).UTC()}
	checkpoint.IntegrityHash, _ = checkpoint.IntegrityDigest()
	gateway := &scriptedGateway{}
	cfg := testConfig()
	cfg.MaxResumeBytes = 512
	loop := newTestLoopWithCheckpoints(t, gateway, newFakeTools(true, "read_file"), &fakeCheckpointStore{latest: checkpoint, hasLatest: true}, cfg)
	result, err := loop.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBudgetExhausted || len(gateway.requests) != 0 || !strings.Contains(result.Blocker, "resume bound") {
		t.Fatalf("oversized semantic resume reached provider: result=%+v requests=%d", result, len(gateway.requests))
	}
}

func TestLoopCheckpointTaskMustBeSoleCallBeforeAnyExecution(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{
		{ID: "checkpoint", Name: checkpointToolName, Arguments: `{}`},
		{ID: "read", Name: "read_file", Arguments: `{}`},
	}})}}
	tools := newFakeTools(true, "read_file")
	checkpoints := &fakeCheckpointStore{}
	loop := newTestLoopWithCheckpoints(t, gateway, tools, checkpoints, testConfig())
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBlocked || len(tools.calls) != 0 || len(checkpoints.published) != 0 || !strings.Contains(result.Blocker, "checkpoint_task must be the sole") {
		t.Fatalf("mixed checkpoint batch executed partially: result=%+v tools=%+v checkpoints=%+v", result, tools.calls, checkpoints.published)
	}
}

func TestLoopFencesAttemptImmediatelyBeforeCheckpointPublish(t *testing.T) {
	gateway := &scriptedGateway{responses: []model.TurnResponse{assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "checkpoint", Name: checkpointToolName, Arguments: `{"completed_work":[],"current_hypothesis":"state","changed_areas":[],"verification_status":"pending","blockers":[],"remaining_work":["work"],"next_action":"continue","critical_evidence_refs":[]}`}}})}}
	authority := &fakeAttemptAuthority{fn: func(call int, _, _ string, _ int64) (bool, error) { return call < 3, nil }}
	checkpoints := &fakeCheckpointStore{}
	loop, err := New(gateway, newFakeTools(true, "read_file"), fakeContextBuilder{}, authority, checkpoints, Profile{Model: "test-model", BaseInstructions: "trusted"}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := loop.Run(context.Background(), testRunRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCancelled || len(checkpoints.published) != 0 || authority.calls != 3 {
		t.Fatalf("stale attempt published checkpoint: result=%+v published=%+v checks=%d", result, checkpoints.published, authority.calls)
	}
}

func TestCompletedCandidateDoesNotTransitionDurableTaskToVerified(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.NewTaskService(db)
	project, _, err := svc.RegisterProject(ctx, "project-completed-candidate", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "produce a candidate without asserting verification",
		Acceptance:          []string{"authoritative verification is still required"},
		ProjectID:           project.ID,
		BaseRevision:        "rev-candidate",
		VerificationProfile: "test",
		Priority:            "P2",
	}
	task, _, err := svc.Submit(ctx, "completed-candidate-no-verify", contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker", "supervisor", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	gateway := &scriptedGateway{responses: []model.TurnResponse{assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "finish", Name: finishToolName, Arguments: `{"status":"completed_candidate","summary":"candidate ready for MAR verification"}`}}})}}
	loop, err := New(gateway, newFakeTools(true), fakeContextBuilder{pack: contextengine.Pack{Revision: contract.BaseRevision}}, svc, svc, Profile{Model: "test-model", BaseInstructions: "trusted"}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := loop.Run(ctx, RunRequest{TaskID: task.ID, AttemptID: attempt.ID, RunEpoch: attempt.RunEpoch, Root: project.Root, Contract: contract, ExpectedRevision: contract.BaseRevision})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCompletedCandidate {
		t.Fatalf("unexpected agent terminal status: %+v", result)
	}
	persisted, err := svc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != domain.TaskRunning {
		t.Fatalf("completed_candidate bypassed authoritative verification: state=%s", persisted.State)
	}
}

func newTestLoop(t *testing.T, gateway ModelGateway, tools ToolRuntime, cfg Config) *Loop {
	t.Helper()
	return newTestLoopWithCheckpoints(t, gateway, tools, &fakeCheckpointStore{}, cfg)
}

func newTestLoopWithCheckpoints(t *testing.T, gateway ModelGateway, tools ToolRuntime, checkpoints CheckpointStore, cfg Config) *Loop {
	t.Helper()
	loop, err := New(gateway, tools, fakeContextBuilder{pack: contextengine.Pack{
		Revision: "rev-test",
		Terms:    []string{"repair", "worker"},
		Entries: []contextengine.Entry{{
			Path: "worker.go", SHA256: strings.Repeat("a", 64), Score: 10, StartLine: 1, EndLine: 2, Reasons: []string{"symbol:Worker"}, Text: "package worker\nfunc Worker() {}\n",
		}},
		Bytes: 256,
	}}, &fakeAttemptAuthority{}, checkpoints, Profile{Model: "test-model", ReasoningEffort: "high", BaseInstructions: "You are the MAR coding worker."}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return loop
}

func newFakeTools(safe bool, names ...string) *fakeTools {
	defs := make([]model.ToolDefinition, 0, len(names))
	for _, name := range names {
		defs = append(defs, model.ToolDefinition{Name: name, Parameters: json.RawMessage(`{"type":"object","additionalProperties":true}`), Strict: true})
	}
	return &fakeTools{safe: safe, defs: defs, outputs: map[string]string{"read_file": `{"ok":true,"result":{"text":"x"}}`, "write_file": `{"ok":true,"result":{"created":true}}`}, errs: make(map[string]error)}
}

func testConfig() Config {
	return Config{
		MaxTurns:               6,
		MaxToolCalls:           10,
		MaxToolCallsPerTurn:    4,
		MaxTotalTokens:         1000,
		MaxOutputTokensPerTurn: 200,
		MaxContextBytes:        8 << 10,
		MaxResumeBytes:         8 << 10,
		MaxRequestBytes:        32 << 10,
		MaxAssistantBytes:      8 << 10,
		MaxObservationBytes:    4 << 10,
		MaxDuration:            time.Second,
	}
}

func testRunRequest(write bool) RunRequest {
	return RunRequest{
		TaskID:    "task-agent-test",
		AttemptID: "attempt-agent-test",
		RunEpoch:  1,
		Root:      ".",
		Contract: domain.GoalContract{
			Goal:                "repair worker behavior",
			Acceptance:          []string{"tests pass"},
			Boundaries:          []string{"do not widen authority"},
			ProjectID:           "project-test",
			BaseRevision:        "rev-test",
			Authority:           domain.Authority{LocalFileWrite: write, LocalGitWrite: write},
			VerificationProfile: "test",
			Priority:            "P2",
		},
		ExpectedRevision: "rev-test",
	}
}

func assistantResponse(tokens int64, message model.Message) model.TurnResponse {
	return model.TurnResponse{Message: message, Usage: model.Usage{InputTokens: tokens / 2, OutputTokens: tokens - tokens/2, TotalTokens: tokens}}
}

func hasTool(defs []model.ToolDefinition, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}
