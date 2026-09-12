package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/model"
)

func TestWebTurnUsageObservationsPreserveResponseIntegrityWithoutRequestHydration(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	project := domain.Project{ID: "usage-observation-project", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{Goal: "observe compact web usage", Acceptance: []string{"usage is visible"}, ProjectID: project.ID, BaseRevision: "base", VerificationProfile: "test", Priority: "P2"}
	hash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: "usage-observation-task", IdempotencyKey: "usage-observation-key", Contract: contract, ContractHash: hash, State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
	if _, _, err := s.SubmitTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	for _, tr := range []struct{ from, to domain.TaskState }{{domain.TaskSubmitted, domain.TaskPreflight}, {domain.TaskPreflight, domain.TaskWaitingResource}, {domain.TaskWaitingResource, domain.TaskWorkspaceReady}} {
		if err := s.OrchestratorTransition(ctx, task.ID, tr.from, tr.to, now); err != nil {
			t.Fatal(err)
		}
	}
	attempt, err := s.BeginAttempt(ctx, task.ID, "usage-observation-attempt", "worker", "supervisor", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	turn := makeStoreWebTurn(t, task.ID, attempt.ID, attempt.RunEpoch, "usage-turn", "usage-request", now.Add(time.Second))
	if _, created, err := s.PublishWebTurn(ctx, turn); err != nil || !created {
		t.Fatalf("publish turn: created=%v err=%v", created, err)
	}
	pending, err := s.ListWebTurnUsageObservationsByTaskEpoch(ctx, task.ID, attempt.RunEpoch, 128)
	if err != nil || len(pending) != 1 || len(pending[0].Response) != 0 || pending[0].RespondedAt != nil {
		t.Fatalf("unexpected pending observation: %+v err=%v", pending, err)
	}
	response := model.TurnResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done"}, FinishReason: "stop", Usage: model.Usage{InputTokens: 21, OutputTokens: 8, TotalTokens: 29}}
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
	observations, err := s.ListWebTurnUsageObservationsByTaskEpoch(ctx, task.ID, attempt.RunEpoch, 128)
	if err != nil || len(observations) != 1 || observations[0].RespondedAt == nil || !json.Valid(observations[0].Response) {
		t.Fatalf("unexpected completed observation: %+v err=%v", observations, err)
	}
	if !strings.Contains(string(observations[0].Response), `"total_tokens":29`) {
		t.Fatalf("usage response missing expected tokens: %s", observations[0].Response)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE web_turns SET response_hash = 'bad' WHERE turn_id = ?`, turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListWebTurnUsageObservationsByTaskEpoch(ctx, task.ID, attempt.RunEpoch, 128); err == nil {
		t.Fatal("compact usage observation accepted a corrupted response hash")
	}
}
