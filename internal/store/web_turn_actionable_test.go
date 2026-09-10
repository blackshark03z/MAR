package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestPendingWebTurnRequiresCurrentEpochActiveAttemptAndInputState(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().UTC()
	project := domain.Project{ID: "web-current-project", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "current web turn truth",
		Acceptance:          []string{"stale turns are historical only"},
		ProjectID:           project.ID,
		BaseRevision:        "base-web-current",
		VerificationProfile: "test",
		Priority:            "P2",
	}
	hash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: "web-current-task", IdempotencyKey: "web-current-key", Contract: contract, ContractHash: hash, State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
	if _, _, err := s.SubmitTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	for _, tr := range []struct{ from, to domain.TaskState }{
		{domain.TaskSubmitted, domain.TaskPreflight},
		{domain.TaskPreflight, domain.TaskWaitingResource},
		{domain.TaskWaitingResource, domain.TaskWorkspaceReady},
	} {
		if err := s.OrchestratorTransition(ctx, task.ID, tr.from, tr.to, now); err != nil {
			t.Fatal(err)
		}
	}
	attempt1, err := s.BeginAttempt(ctx, task.ID, "attempt-epoch-1", "worker", "supervisor", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	turn1 := makeStoreWebTurn(t, task.ID, attempt1.ID, attempt1.RunEpoch, "turn-epoch-1", "request-epoch-1", now.Add(time.Second))
	if _, created, err := s.PublishWebTurn(ctx, turn1); err != nil || !created {
		t.Fatalf("publish epoch-1 turn: created=%v err=%v", created, err)
	}
	if got, ok, err := s.PendingWebTurn(ctx, task.ID); err != nil || !ok || got.ID != turn1.ID {
		t.Fatalf("current epoch-1 turn should be actionable: got=%+v ok=%v err=%v", got, ok, err)
	}

	stamp := now.Add(2 * time.Second).Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, `UPDATE execution_attempts SET authority_state = ?, terminal_status = ?, heartbeat_at = ? WHERE attempt_id = ?`, string(domain.AttemptPhysicallyTerminated), "superseded", stamp, attempt1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET state = ?, run_epoch = ?, updated_at = ? WHERE id = ?`, string(domain.TaskInputRequired), 4, stamp, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO execution_attempts(attempt_id, task_id, run_epoch, worker_id, supervisor_id, authority_state, started_at, heartbeat_at, lease_deadline, terminal_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '')`, "attempt-epoch-4", task.ID, 4, "worker", "supervisor", string(domain.AttemptActive), stamp, stamp, now.Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.PendingWebTurn(ctx, task.ID); err != nil || ok {
		t.Fatalf("epoch-1 unanswered turn must not surface when authoritative epoch is 4: got=%+v ok=%v err=%v", got, ok, err)
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`, string(domain.TaskRunning), stamp, task.ID); err != nil {
		t.Fatal(err)
	}
	turn4 := makeStoreWebTurn(t, task.ID, "attempt-epoch-4", 4, "turn-epoch-4", "request-epoch-4", now.Add(3*time.Second))
	if _, created, err := s.PublishWebTurn(ctx, turn4); err != nil || !created {
		t.Fatalf("publish epoch-4 turn: created=%v err=%v", created, err)
	}
	if got, ok, err := s.PendingWebTurn(ctx, task.ID); err != nil || !ok || got.ID != turn4.ID || got.RunEpoch != 4 {
		t.Fatalf("current epoch-4 turn should be actionable: got=%+v ok=%v err=%v", got, ok, err)
	}
}

func makeStoreWebTurn(t *testing.T, taskID, attemptID string, epoch int64, turnID, requestID string, createdAt time.Time) domain.WebTurn {
	t.Helper()
	request := []byte(`{"request_id":"` + requestID + `"}`)
	requestHash, err := domain.HashWebTurnJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	turn := domain.WebTurn{ID: turnID, TaskID: taskID, AttemptID: attemptID, RunEpoch: epoch, RequestID: requestID, Request: request, RequestHash: requestHash, CreatedAt: createdAt.UTC()}
	turn.IntegrityHash, err = turn.IntegrityDigest()
	if err != nil {
		t.Fatal(err)
	}
	return turn
}
