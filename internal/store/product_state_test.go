package store

import (
	"context"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestProjectPolicyRoundTripAndRecentTaskContinuity(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir() + `/mar.db`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	project := domain.Project{ID: "p", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	policy := domain.ProjectPolicy{ProjectID: project.ID, LocalFileWrite: true, LocalGitWrite: false, UpdatedAt: now}
	if err := s.PutProjectPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProjectPolicy(ctx, project.ID)
	if err != nil || !got.LocalFileWrite || got.LocalGitWrite {
		t.Fatalf("project policy round trip failed: %+v err=%v", got, err)
	}

	contract := domain.GoalContract{Goal: "g", Acceptance: []string{"a"}, ProjectID: project.ID, BaseRevision: "base", VerificationProfile: "go-standard", Priority: "P2"}
	task := domain.Task{ID: "task-1", IdempotencyKey: "key-1", Contract: contract, ContractHash: "hash", State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
	if _, created, err := s.SubmitTask(ctx, task); err != nil || !created {
		t.Fatalf("submit fixture: created=%v err=%v", created, err)
	}
	recent, err := s.ListRecentTasks(ctx, 10)
	if err != nil || len(recent) != 1 || recent[0].ID != task.ID {
		t.Fatalf("recent tasks lost durable continuity: %+v err=%v", recent, err)
	}
}

func TestOwnerFeedbackIsCandidateBoundAndIdempotent(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir() + `/mar.db`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	project := domain.Project{ID: "p", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{Goal: "g", Acceptance: []string{"a"}, ProjectID: project.ID, BaseRevision: "base", VerificationProfile: "go-standard", Priority: "P2"}
	task := domain.Task{ID: "task-1", IdempotencyKey: "key-1", Contract: contract, ContractHash: "hash", State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
	if _, _, err := s.SubmitTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	// Feedback FK is bound to a durable result. This store-level fixture inserts
	// the minimum referenced rows to test idempotency/integrity independently of verifier behavior.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO execution_attempts(attempt_id,task_id,run_epoch,worker_id,supervisor_id,authority_state,started_at,heartbeat_at,lease_deadline,terminal_status) VALUES('a','task-1',1,'w','s','PHYSICALLY_TERMINATED',?,?,?,'')`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO verification_evidence(evidence_id,task_id,attempt_id,run_epoch,goal_hash,base_revision,candidate_revision,profile_id,profile_hash,environment_json,environment_hash,commands_json,acceptance_json,verdict,integrity_hash,created_at) VALUES('e','task-1','a',1,'hash','base','candidate','go-standard','ph','{}','eh','[]','[]','PASS','ih',?)`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO task_results(result_id,task_id,version,goal_hash,base_revision,final_revision,changed_areas_json,evidence_id,verification_executed_json,pass_fail_evidence_json,unresolved_risks_json,integration_status,workspace_disposition,resource_summary_json,verdict,integrity_hash,created_at) VALUES('r','task-1',1,'hash','base','candidate','[]','e','[]','[]','[]','INTEGRATED','REMOVED','{}','VERIFIED','rih',?)`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	feedback := domain.OwnerFeedback{ID: "f", IdempotencyKey: "fk", TaskID: task.ID, ResultID: "r", CandidateRevision: "candidate", Verdict: domain.OwnerFeedbackAccepted, CreatedAt: now}
	feedback.IntegrityHash, _ = feedback.IntegrityDigest()
	first, created, err := s.RecordOwnerFeedback(ctx, feedback)
	if err != nil || !created || !first.IntegrityValid() {
		t.Fatalf("record feedback: created=%v feedback=%+v err=%v", created, first, err)
	}
	second, created, err := s.RecordOwnerFeedback(ctx, feedback)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("idempotent feedback replay failed: created=%v second=%+v err=%v", created, second, err)
	}
	listed, err := s.ListOwnerFeedbackByTask(ctx, task.ID, 10)
	if err != nil || len(listed) != 1 || listed[0].CandidateRevision != "candidate" {
		t.Fatalf("candidate-bound feedback listing failed: %+v err=%v", listed, err)
	}
}
