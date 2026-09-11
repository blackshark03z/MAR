package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/model"
)

func TestWebEpisodeBudgetReconstructsAcrossRestartAndIgnoresStaleTurns(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mar.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	project := domain.Project{ID: "budget-restart-project", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{Goal: "reconstruct web budget", Acceptance: []string{"restart safe"}, ProjectID: project.ID, BaseRevision: "base-budget", VerificationProfile: "test", Priority: "P1"}
	hash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: "budget-restart-task", IdempotencyKey: "budget-restart-key", Contract: contract, ContractHash: hash, State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
	if _, _, err := s.SubmitTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	for _, tr := range []struct{ from, to domain.TaskState }{{domain.TaskSubmitted, domain.TaskPreflight}, {domain.TaskPreflight, domain.TaskWaitingResource}, {domain.TaskWaitingResource, domain.TaskWorkspaceReady}} {
		if err := s.OrchestratorTransition(ctx, task.ID, tr.from, tr.to, now); err != nil {
			t.Fatal(err)
		}
	}
	attempt, err := s.BeginAttempt(ctx, task.ID, "budget-attempt-1", "worker", "supervisor", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	turn := makeStoreWebTurn(t, task.ID, attempt.ID, attempt.RunEpoch, "budget-turn-1", "budget-request-1", now.Add(time.Second))
	if _, created, err := s.PublishWebTurn(ctx, turn); err != nil || !created {
		t.Fatalf("publish turn: created=%v err=%v", created, err)
	}
	response := model.TurnResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done", ToolCalls: []model.ToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"x"}`}}}, FinishReason: "tool_calls", Usage: model.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	turn.Response = raw
	turn.ResponseHash, err = domain.HashWebTurnJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	responded := now.Add(2 * time.Second)
	turn.RespondedAt = &responded
	turn.IntegrityHash, err = turn.IntegrityDigest()
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := s.RespondWebTurn(ctx, turn); err != nil || !created {
		t.Fatalf("respond turn: created=%v err=%v", created, err)
	}
	want, err := s.WebEpisodeUsage(ctx, task.ID, attempt.ID, attempt.RunEpoch, true)
	if err != nil {
		t.Fatal(err)
	}
	if want.Decisions != 1 || want.ControlCalls != 2 || want.WorkerToolCalls != 1 || want.ModelTotalTokens != 15 {
		t.Fatalf("unexpected usage before restart: %+v", want)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.WebEpisodeUsage(ctx, task.ID, attempt.ID, attempt.RunEpoch, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("restart reconstruction mismatch: got=%+v want=%+v", got, want)
	}
	stamp := now.Add(3 * time.Second).Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, `UPDATE execution_attempts SET authority_state = ?, terminal_status = ?, heartbeat_at = ? WHERE attempt_id = ?`, string(domain.AttemptPhysicallyTerminated), "superseded", stamp, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET state = ?, run_epoch = ?, updated_at = ? WHERE id = ?`, string(domain.TaskRunning), attempt.RunEpoch+1, stamp, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO execution_attempts(attempt_id, task_id, run_epoch, worker_id, supervisor_id, authority_state, started_at, heartbeat_at, lease_deadline, terminal_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '')`, "budget-attempt-2", task.ID, attempt.RunEpoch+1, "worker", "supervisor", string(domain.AttemptActive), stamp, stamp, now.Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WebEpisodeUsage(ctx, task.ID, attempt.ID, attempt.RunEpoch, true); !errors.Is(err, ErrStaleAttempt) {
		t.Fatalf("current-authority lookup must reject stale episode: %v", err)
	}
	history, err := s.WebEpisodeUsage(ctx, task.ID, attempt.ID, attempt.RunEpoch, false)
	if err != nil {
		t.Fatal(err)
	}
	if history != want {
		t.Fatalf("historical accounting changed after supersession: got=%+v want=%+v", history, want)
	}
}
