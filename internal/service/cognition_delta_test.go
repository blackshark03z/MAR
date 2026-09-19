package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mar/internal/contextengine"
	"mar/internal/domain"
	"mar/internal/model"
)

func TestCognitionDeltaRecoveryFromCurrentState(t *testing.T) {
	previous := testCognitionTurn(t, "turn-prev", "request-prev", time.Unix(1700000000, 0).UTC(), "first")
	completeCognitionTurn(t, &previous)

	current := testCognitionTurn(t, "turn-current", "request-current", time.Unix(1700000010, 0).UTC(), "second")
	view, err := BuildCognitionDelta(current, &previous)
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != "delta" || view.BaseCursor == "" || view.Delta == nil {
		t.Fatalf("compatible persisted base did not produce delta: %+v", view)
	}
	if _, ok := view.Delta.Projection["recent_protocol_evidence"]; !ok {
		t.Fatalf("cumulative delta omitted changed current projection: %+v", view.Delta)
	}

	restarted := current
	restarted.AttemptID = "attempt-new"
	restarted.RunEpoch = 2
	restarted.Request = testCognitionRequestJSON(t, restarted.TaskID, restarted.AttemptID, restarted.RunEpoch, "third")
	restarted.RequestHash, _ = domain.HashWebTurnJSON(restarted.Request)
	restarted.IntegrityHash, _ = restarted.IntegrityDigest()
	recovered, err := BuildCognitionDelta(restarted, &previous)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Mode != "full" || recovered.FullRequest == nil {
		t.Fatalf("cross-attempt stale base must recover from full current state: %+v", recovered)
	}

	first, err := BuildCognitionDelta(current, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Mode != "full" || first.FullRequest == nil || first.Cursor == "" {
		t.Fatalf("missing cursor must recover from full current state: %+v", first)
	}
	if _, ok := decodeCognitionCursor("definitely-not-a-cursor"); ok {
		t.Fatal("malformed cognition cursor was accepted")
	}
}

func TestCognitionDeltaReducesProjectionCost(t *testing.T) {
	previous := testCognitionTurn(t, "turn-prev", "request-prev", time.Unix(1700000000, 0).UTC(), "first")
	completeCognitionTurn(t, &previous)
	current := testCognitionTurn(t, "turn-current", "request-current", time.Unix(1700000010, 0).UTC(), "small material delta")

	view, err := BuildCognitionDelta(current, &previous)
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != "delta" || view.Delta == nil {
		t.Fatalf("small compatible change unexpectedly fell back to full: %+v", view)
	}
	if view.PayloadBytes*100 > view.FullBytes*70 {
		t.Fatalf("delta did not materially reduce projection cost: payload=%d full=%d ratio=%.3f", view.PayloadBytes, view.FullBytes, float64(view.PayloadBytes)/float64(view.FullBytes))
	}
	t.Logf("Slice B projection bytes: full=%d delta=%d ratio=%.4f", view.FullBytes, view.PayloadBytes, float64(view.PayloadBytes)/float64(view.FullBytes))
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "small material delta") {
		t.Fatal("delta omitted changed projection evidence")
	}
}

func TestCognitionEventKindsAreBounded(t *testing.T) {
	cases := map[domain.TaskState]CognitionEventKind{
		domain.TaskRunning:       CognitionEventDecisionRequired,
		domain.TaskInputRequired: CognitionEventAuthorityRequired,
		domain.TaskBlocked:       CognitionEventRecoverableFailureNeedsReasoning,
		domain.TaskVerifying:     CognitionEventCandidateReady,
		domain.TaskComplete:      CognitionEventTerminal,
	}
	for state, want := range cases {
		if got := cognitionEventKind(state); got != want {
			t.Fatalf("state=%s kind=%s want=%s", state, got, want)
		}
	}
}

func completeCognitionTurn(t *testing.T, turn *domain.WebTurn) {
	t.Helper()
	turn.Response = json.RawMessage(`{"provider_response_id":"web:` + turn.ID + `","model":"test","message":{"role":"assistant","content":"done"},"finish_reason":"stop","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	turn.ResponseHash, _ = domain.HashWebTurnJSON(turn.Response)
	responded := turn.CreatedAt.Add(time.Second)
	turn.RespondedAt = &responded
	turn.IntegrityHash, _ = turn.IntegrityDigest()
}

func testCognitionTurn(t *testing.T, turnID, requestID string, created time.Time, recent string) domain.WebTurn {
	t.Helper()
	turn := domain.WebTurn{
		ID: turnID, TaskID: "task-delta", AttemptID: "attempt-delta", RunEpoch: 1,
		RequestID: requestID, CreatedAt: created,
	}
	turn.Request = testCognitionRequestJSON(t, turn.TaskID, turn.AttemptID, turn.RunEpoch, recent)
	turn.RequestHash, _ = domain.HashWebTurnJSON(turn.Request)
	turn.IntegrityHash, _ = turn.IntegrityDigest()
	return turn
}

func testCognitionRequestJSON(t *testing.T, taskID, attemptID string, epoch int64, recent string) json.RawMessage {
	t.Helper()
	goalHash := strings.Repeat("a", 64)
	entries := make([]contextengine.Entry, 0, 18)
	for i := 0; i < 18; i++ {
		entries = append(entries, contextengine.Entry{
			Path: "pkg/file.go", SHA256: strings.Repeat("b", 64), Score: 10,
			StartLine: 1, EndLine: 20, Reasons: []string{"symbol:Worker"},
			Text: strings.Repeat("package worker\nfunc Worker() {}\n", 8),
		})
	}
	projection := contextengine.DecisionProjection{
		Version: 1, CreatedAt: time.Unix(1700000000, 0).UTC(),
		TaskID: taskID, GoalHash: goalHash,
		Contract: domain.GoalContract{
			Goal: strings.Repeat("bounded cognition goal ", 80), Acceptance: []string{"verified"},
			Boundaries: []string{"same authority"}, ProjectID: "mar", BaseRevision: "rev-a",
			VerificationProfile: "go-standard", Priority: "P1",
		},
		TaskState: domain.TaskRunning, ProjectID: "mar",
		WorkspaceID: "workspace-delta", WorkspaceState: domain.WorkspaceReady,
		BaseRevision: "rev-a", CurrentRevision: "rev-a",
		AttemptID: attemptID, RunEpoch: epoch, AttemptAuthority: domain.AttemptActive,
		Repository: contextengine.Pack{Revision: "rev-a", GoalHash: goalHash, Entries: entries, Bytes: 10000},
		Recent:     []contextengine.DecisionProjectionEvent{{Role: "tool", Kind: "read", Content: recent}},
	}
	projectionJSON, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	req := model.TurnRequest{
		RequestID: "request-wire-" + recent, Model: "test",
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: strings.Repeat("system instructions ", 50)},
			{Role: model.RoleUser, Content: "MAR_DECISION_PROJECTION_JSON:\n" + string(projectionJSON) + "\n\nThis is MAR's bounded derived current decision state. Goal/authority/task/attempt/revision/control/checkpoint/result identities are authoritative as supplied by MAR."},
			{Role: model.RoleAssistant, Content: "previous assistant"},
			{Role: model.RoleTool, ToolCallID: "call-prev", Content: `{"ok":true}`},
		},
		Tools:           []model.ToolDefinition{{Name: "read_file", Description: "read", Parameters: json.RawMessage(`{"type":"object"}`), Strict: true}},
		ReasoningEffort: "high", MaxOutputTokens: 1024,
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
