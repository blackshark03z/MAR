package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/model"
)

func TestWebTurnDurablyPausesAndResumesActiveAttempt(t *testing.T) {
	_, svc, _ := newHarness(t)
	ctx := context.Background()
	task, _, err := svc.Submit(ctx, "web-turn-task", contract("use GPT Web as coding brain"))
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
		RequestID: "task-turn-001",
		Model:     "gpt-5.6-sol",
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "bounded coding worker"},
			{Role: model.RoleUser, Content: "inspect the requested file"},
		},
		Tools:           []model.ToolDefinition{{Name: "read_file", Parameters: json.RawMessage(`{"type":"object"}`), Strict: true}},
		MaxOutputTokens: 1024,
	}
	turn, created, err := svc.RequestWebTurnForAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, req)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !turn.IntegrityValid() {
		t.Fatalf("web turn was not created with valid integrity: %+v", turn)
	}
	status, err := svc.Status(ctx, task.ID)
	if err != nil || status.State != domain.TaskInputRequired {
		t.Fatalf("web turn did not pause task in INPUT_REQUIRED: state=%s err=%v", status.State, err)
	}
	snapshot, err := svc.StatusSnapshot(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.BrainTurnAvailable || snapshot.NextAction == "" || snapshot.Detail == "" {
		t.Fatalf("web brain wait is not actionable from status: %+v", snapshot)
	}
	pending, available, err := svc.PendingWebTurn(ctx, task.ID)
	if err != nil || !available || pending.ID != turn.ID {
		t.Fatalf("pending web turn unavailable: turn=%+v available=%v err=%v", pending, available, err)
	}

	message := model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"README.md"}`}}}
	completed, responded, err := svc.RespondWebTurn(ctx, task.ID, turn.ID, message, "tool_calls")
	if err != nil {
		t.Fatal(err)
	}
	if !responded || !completed.IntegrityValid() || completed.RespondedAt == nil {
		t.Fatalf("web turn response was not durably completed: %+v", completed)
	}
	status, err = svc.Status(ctx, task.ID)
	if err != nil || status.State != domain.TaskRunning {
		t.Fatalf("web turn response did not resume RUNNING: state=%s err=%v", status.State, err)
	}
	snapshot, err = svc.StatusSnapshot(ctx, task.ID)
	if err != nil || snapshot.BrainTurnAvailable || snapshot.NextAction == "" {
		t.Fatalf("running status retained stale brain-turn UX: %+v err=%v", snapshot, err)
	}
	response, available, err := svc.WebTurnResponse(ctx, turn.ID)
	if err != nil || !available {
		t.Fatalf("web turn response unavailable: %+v available=%v err=%v", response, available, err)
	}
	if response.Model != "gpt-5.6-sol" || response.Message.Role != model.RoleAssistant || len(response.Message.ToolCalls) != 1 || response.Usage.TotalTokens <= 0 || !response.Usage.Estimated {
		t.Fatalf("unexpected web brain response: %+v", response)
	}
	if _, available, err := svc.PendingWebTurn(ctx, task.ID); err != nil || available {
		t.Fatalf("responded web turn remained pending: available=%v err=%v", available, err)
	}
}
