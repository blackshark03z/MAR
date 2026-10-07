package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintenanceDBStatsSeparatesCompactableTerminalTurns(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO projects(id, root, created_at) VALUES
('p', 'D:/project', ?)
`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	type taskFixture struct {
		id    string
		state string
	}
	for _, task := range []taskFixture{
		{id: "complete", state: "COMPLETE"},
		{id: "cancelled", state: "CANCELLED"},
		{id: "blocked", state: "BLOCKED"},
	} {
		if _, err := s.db.ExecContext(ctx, `
INSERT INTO tasks(id, idempotency_key, project_id, contract_json, contract_hash, state, created_at, updated_at)
VALUES (?, ?, 'p', '{}', 'hash', ?, ?, ?)
`, task.id, "key-"+task.id, task.state, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}

	attempts := []struct {
		id       string
		taskID   string
		started  time.Time
		terminal string
	}{
		{id: "a-complete", taskID: "complete", started: now.Add(-time.Hour), terminal: "worker-exited-before-verification-finalization"},
		{id: "a-cancelled", taskID: "cancelled", started: now.Add(-2 * time.Hour), terminal: "cancelled"},
		{id: "a-blocked", taskID: "blocked", started: now.Add(-3 * time.Hour), terminal: "worker-budget-exhausted"},
	}
	for _, attempt := range attempts {
		if _, err := s.db.ExecContext(ctx, `
INSERT INTO execution_attempts(
	attempt_id, task_id, run_epoch, worker_id, supervisor_id, authority_state,
	started_at, heartbeat_at, lease_deadline, terminal_status
) VALUES (?, ?, 1, 'worker', 'supervisor', 'TERMINATED', ?, ?, ?, ?)
`, attempt.id, attempt.taskID,
			attempt.started.Format(time.RFC3339Nano),
			attempt.started.Format(time.RFC3339Nano),
			attempt.started.Add(time.Hour).Format(time.RFC3339Nano),
			attempt.terminal,
		); err != nil {
			t.Fatal(err)
		}
	}

	type turnFixture struct {
		id       string
		taskID   string
		attempt  string
		request  string
		response string
	}
	for _, turn := range []turnFixture{
		{id: "t-complete", taskID: "complete", attempt: "a-complete", request: "12345", response: "12"},
		{id: "t-cancelled", taskID: "cancelled", attempt: "a-cancelled", request: "1234", response: "123"},
		{id: "t-blocked", taskID: "blocked", attempt: "a-blocked", request: "123456", response: "1"},
	} {
		if _, err := s.db.ExecContext(ctx, `
INSERT INTO web_turns(
	turn_id, task_id, attempt_id, run_epoch, request_id, request_json, response_json,
	request_hash, response_hash, integrity_hash, created_at, responded_at
) VALUES (?, ?, ?, 1, ?, ?, ?, 'request-hash', 'response-hash', 'integrity-hash', ?, ?)
`, turn.id, turn.taskID, turn.attempt, "request-"+turn.id,
			[]byte(turn.request), []byte(turn.response),
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := s.MaintenanceDBStats(ctx, now.Add(-14*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if stats.WebTurnRequestBytes != 15 || stats.WebTurnResponseBytes != 6 {
		t.Fatalf("unexpected web turn byte totals: %+v", stats)
	}
	if stats.CompactableTerminalTurnBytes != 14 {
		t.Fatalf("compactable terminal bytes = %d, want 14", stats.CompactableTerminalTurnBytes)
	}
	if len(stats.TaskStates) != 3 {
		t.Fatalf("task state counts = %+v", stats.TaskStates)
	}
	if len(stats.TerminalStatuses14d) != 3 {
		t.Fatalf("terminal status counts = %+v", stats.TerminalStatuses14d)
	}

	stats, err = s.MaintenanceDBStats(ctx, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.TerminalStatuses14d) != 0 {
		t.Fatalf("future cutoff unexpectedly included terminal statuses: %+v", stats.TerminalStatuses14d)
	}
}
