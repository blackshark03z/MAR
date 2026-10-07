package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestCompactTerminalWebTurnRequestsPreservesIntegrityAndBlockedRecovery(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO projects(id, root, created_at) VALUES ('p', 'D:/p', ?)`, now.Add(-48*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		taskID  string
		state   domain.TaskState
		turnID  string
		request string
	}{
		{taskID: "complete-task", state: domain.TaskComplete, turnID: "complete-turn", request: `{"request_id":"complete","payload":"large"}`},
		{taskID: "blocked-task", state: domain.TaskBlocked, turnID: "blocked-turn", request: `{"request_id":"blocked","payload":"resume-me"}`},
	} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO tasks(id,idempotency_key,project_id,contract_json,contract_hash,state,created_at,updated_at) VALUES(?,?,'p','{}','hash',?,?,?)`, tc.taskID, "key-"+tc.taskID, string(tc.state), now.Add(-48*time.Hour).Format(time.RFC3339Nano), now.Add(-48*time.Hour).Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		attemptID := "attempt-" + tc.taskID
		if _, err := s.db.ExecContext(ctx, `INSERT INTO execution_attempts(attempt_id,task_id,run_epoch,worker_id,supervisor_id,authority_state,started_at,heartbeat_at,lease_deadline,terminated_at,terminal_status) VALUES(?,?,1,'worker','supervisor','PHYSICALLY_TERMINATED',?,?,?,?,'done')`, attemptID, tc.taskID, now.Add(-47*time.Hour).Format(time.RFC3339Nano), now.Add(-47*time.Hour).Format(time.RFC3339Nano), now.Add(-46*time.Hour).Format(time.RFC3339Nano), now.Add(-46*time.Hour).Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		request := json.RawMessage(tc.request)
		response := json.RawMessage(`{"model":"gpt","usage":{"total_tokens":12},"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}`)
		requestHash, err := domain.HashWebTurnJSON(request)
		if err != nil {
			t.Fatal(err)
		}
		responseHash, err := domain.HashWebTurnJSON(response)
		if err != nil {
			t.Fatal(err)
		}
		responded := now.Add(-46 * time.Hour)
		turn := domain.WebTurn{ID: tc.turnID, TaskID: tc.taskID, AttemptID: attemptID, RunEpoch: 1, RequestID: "request-" + tc.turnID, Request: request, Response: response, RequestHash: requestHash, ResponseHash: responseHash, CreatedAt: now.Add(-47 * time.Hour), RespondedAt: &responded}
		turn.IntegrityHash, err = turn.IntegrityDigest()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO web_turns(turn_id,task_id,attempt_id,run_epoch,request_id,request_json,response_json,request_hash,response_hash,integrity_hash,created_at,responded_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, turn.ID, turn.TaskID, turn.AttemptID, turn.RunEpoch, turn.RequestID, []byte(turn.Request), []byte(turn.Response), turn.RequestHash, turn.ResponseHash, turn.IntegrityHash, turn.CreatedAt.Format(time.RFC3339Nano), turn.RespondedAt.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}

	cutoff := now.Add(-24 * time.Hour)
	plan, err := s.TerminalWebTurnCompactionPlan(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Rows != 1 || plan.RequestBytes == 0 {
		t.Fatalf("unexpected compaction plan: %+v", plan)
	}
	applied, err := s.CompactTerminalWebTurnRequests(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if applied != plan {
		t.Fatalf("applied compaction=%+v plan=%+v", applied, plan)
	}

	complete, err := s.GetWebTurn(ctx, "complete-turn")
	if err != nil {
		t.Fatal(err)
	}
	if !complete.RequestCompacted || len(complete.Request) != 0 || !complete.IntegrityValid() {
		t.Fatalf("complete turn not compacted safely: %+v", complete)
	}
	blocked, err := s.GetWebTurn(ctx, "blocked-turn")
	if err != nil {
		t.Fatal(err)
	}
	if blocked.RequestCompacted || len(blocked.Request) == 0 || !blocked.IntegrityValid() {
		t.Fatalf("blocked recovery turn was compacted: %+v", blocked)
	}
	usage, err := s.ListWebTurnUsageObservationsByTaskEpoch(ctx, "complete-task", 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 || len(usage[0].Response) == 0 {
		t.Fatalf("usage observation lost after compaction: %+v", usage)
	}
}
