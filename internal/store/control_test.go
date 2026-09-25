package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestTaskCancellationLinearizesBeforeIntegrationDispatch(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	project := domain.Project{ID: "cancel-linearization-project", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	newTask := func(id string, state domain.TaskState) domain.Task {
		contract := domain.GoalContract{Goal: "cancel linearization", Acceptance: []string{"bounded"}, ProjectID: project.ID, BaseRevision: "base", VerificationProfile: "test", Priority: "P2"}
		hash, err := contract.Hash()
		if err != nil {
			t.Fatal(err)
		}
		task := domain.Task{ID: id, IdempotencyKey: id + "-submit", Contract: contract, ContractHash: hash, State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
		if _, _, err := s.SubmitTask(ctx, task); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET state = ? WHERE id = ?`, string(state), id); err != nil {
			t.Fatal(err)
		}
		task.State = state
		return task
	}

	before := newTask("cancel-before-dispatch", domain.TaskReadyToIntegrate)
	payload, _ := json.Marshal(domain.CancelPayload{Reason: "owner cancelled before publication"})
	control, created, err := s.RequestTaskCancellation(ctx, "cancel-control-before", before.ID, "cancel-before-key", payload, now.Add(time.Second))
	if err != nil || !created || control.Kind != domain.ControlCancel {
		t.Fatalf("pre-dispatch cancellation failed: created=%v control=%+v err=%v", created, control, err)
	}
	cancelled, err := s.GetTask(ctx, before.ID)
	if err != nil || cancelled.State != domain.TaskCancelled {
		t.Fatalf("pre-dispatch cancellation did not win: task=%+v err=%v", cancelled, err)
	}

	after := newTask("cancel-after-dispatch", domain.TaskIntegrating)
	if _, created, err := s.RequestTaskCancellation(ctx, "cancel-control-after", after.ID, "cancel-after-key", payload, now.Add(2*time.Second)); !errors.Is(err, ErrStateConflict) || created {
		t.Fatalf("post-dispatch cancellation was accepted: created=%v err=%v", created, err)
	}
	var cancelControls int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_controls WHERE task_id = ? AND kind = ?`, after.ID, string(domain.ControlCancel)).Scan(&cancelControls); err != nil {
		t.Fatal(err)
	}
	if cancelControls != 0 {
		t.Fatalf("post-dispatch cancellation was durably recorded: %d", cancelControls)
	}
	integrating, err := s.GetTask(ctx, after.ID)
	if err != nil || integrating.State != domain.TaskIntegrating {
		t.Fatalf("publication state changed after rejected cancellation: task=%+v err=%v", integrating, err)
	}
}

func TestListBlockedTasksWithFreshSteerPreservesControlSemantics(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	project := domain.Project{ID: "blocked-steer-project", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	newBlocked := func(id string) domain.Task {
		contract := domain.GoalContract{Goal: id, Acceptance: []string{"bounded"}, ProjectID: project.ID, BaseRevision: "base", VerificationProfile: "test", Priority: "P2"}
		hash, err := contract.Hash()
		if err != nil {
			t.Fatal(err)
		}
		task := domain.Task{ID: id, IdempotencyKey: id + "-submit", Contract: contract, ContractHash: hash, State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
		if _, _, err := s.SubmitTask(ctx, task); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, "UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?", string(domain.TaskBlocked), now.Format(time.RFC3339Nano), id); err != nil {
			t.Fatal(err)
		}
		task.State = domain.TaskBlocked
		return task
	}
	steerPayload, _ := json.Marshal(domain.SteerPayload{Kind: domain.SteerContext, Message: "continue"})
	cancelPayload, _ := json.Marshal(domain.CancelPayload{Reason: "later control"})

	fresh := newBlocked("blocked-fresh")
	if _, created, err := s.PublishTaskControl(ctx, "fresh-steer", fresh.ID, "fresh-steer-key", domain.ControlSteer, steerPayload, []domain.TaskState{domain.TaskBlocked}, now.Add(time.Second)); err != nil || !created {
		t.Fatalf("publish fresh steer: created=%v err=%v", created, err)
	}

	stale := newBlocked("blocked-stale")
	if _, created, err := s.PublishTaskControl(ctx, "stale-steer", stale.ID, "stale-steer-key", domain.ControlSteer, steerPayload, []domain.TaskState{domain.TaskBlocked}, now.Add(-time.Second)); err != nil || !created {
		t.Fatalf("publish stale steer: created=%v err=%v", created, err)
	}

	superseded := newBlocked("blocked-superseded")
	if _, created, err := s.PublishTaskControl(ctx, "superseded-steer", superseded.ID, "superseded-steer-key", domain.ControlSteer, steerPayload, []domain.TaskState{domain.TaskBlocked}, now.Add(time.Second)); err != nil || !created {
		t.Fatalf("publish superseded steer: created=%v err=%v", created, err)
	}
	if _, created, err := s.PublishTaskControl(ctx, "superseded-cancel", superseded.ID, "superseded-cancel-key", domain.ControlCancel, cancelPayload, []domain.TaskState{domain.TaskBlocked}, now.Add(2*time.Second)); err != nil || !created {
		t.Fatalf("publish superseding control: created=%v err=%v", created, err)
	}

	got, err := s.ListBlockedTasksWithFreshSteer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != fresh.ID {
		t.Fatalf("fresh blocked steer filter mismatch: %+v", got)
	}
}

func TestTaskControlPersistsAcrossReopenAndRejectsTamper(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mar.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	project := domain.Project{ID: "control-store-project", Root: t.TempDir(), CreatedAt: now}
	if _, _, err := s.RegisterProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{Goal: "durable controls", Acceptance: []string{"control survives reopen"}, ProjectID: project.ID, BaseRevision: "base", VerificationProfile: "test", Priority: "P2"}
	hash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: "control-store-task", IdempotencyKey: "control-store-submit", Contract: contract, ContractHash: hash, State: domain.TaskSubmitted, CreatedAt: now, UpdatedAt: now}
	if _, _, err := s.SubmitTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(domain.SteerPayload{Kind: domain.SteerContext, Message: "durable fact"})
	control, created, err := s.PublishTaskControl(ctx, "control-store-1", task.ID, "control-key-1", domain.ControlSteer, payload, []domain.TaskState{domain.TaskSubmitted}, now)
	if err != nil || !created || !control.IntegrityValid() {
		t.Fatalf("publish control failed: created=%v control=%+v err=%v", created, control, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	latest, ok, err := reopened.LatestTaskControl(ctx, task.ID)
	if err != nil || !ok || latest.ID != control.ID || !latest.IntegrityValid() {
		t.Fatalf("control did not survive reopen: ok=%v latest=%+v err=%v", ok, latest, err)
	}
	if _, err := reopened.db.ExecContext(ctx, `UPDATE task_controls SET payload_json = ? WHERE control_id = ?`, []byte(`{"kind":"context","message":"tampered"}`), control.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.LatestTaskControl(ctx, task.ID); !errors.Is(err, ErrControlIntegrity) {
		t.Fatalf("tampered control was accepted: %v", err)
	}
}
