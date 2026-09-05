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
