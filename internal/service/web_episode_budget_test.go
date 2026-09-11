package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/contextengine"
	"mar/internal/domain"
	"mar/internal/store"
)

func TestWebEpisodeBudgetsFailClosedAtDecisionCallAndByteLimits(t *testing.T) {
	tests := []struct {
		name         string
		usage        store.WebEpisodeUsage
		calls, bytes int64
		want         string
	}{
		{"decision boundary", store.WebEpisodeUsage{Decisions: DefaultWebEpisodeMaxDecisions}, 1, 1, "decision_limit"},
		{"calls equality", store.WebEpisodeUsage{ControlCalls: DefaultWebEpisodeMaxControlCalls - 1}, 1, 0, ""},
		{"calls plus one", store.WebEpisodeUsage{ControlCalls: DefaultWebEpisodeMaxControlCalls}, 1, 0, "control_call_limit"},
		{"bytes equality", store.WebEpisodeUsage{PayloadBytes: DefaultWebEpisodeMaxPayloadBytes - 1}, 0, 1, ""},
		{"bytes plus one", store.WebEpisodeUsage{PayloadBytes: DefaultWebEpisodeMaxPayloadBytes}, 0, 1, "payload_byte_limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := webEpisodeExhaustionReason(tt.usage, tt.calls, tt.bytes); got != tt.want {
				t.Fatalf("reason=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestWebEpisodeContinuationReceiptResumesFromDurableDecisionProjection(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewTaskService(db)
	project, _, err := svc.RegisterProject(ctx, "episode-receipt-project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{Goal: "resume from durable truth", Acceptance: []string{"no transcript replay"}, ProjectID: project.ID, BaseRevision: "base-receipt", VerificationProfile: "test", Priority: "P1"}
	task, _, err := svc.Submit(ctx, "episode-receipt-task", contract)
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
	usage := store.WebEpisodeUsage{Decisions: DefaultWebEpisodeMaxDecisions, ControlCalls: 24, PayloadBytes: 4096}
	receipt, err := svc.webEpisodeContinuationReceipt(ctx, task.ID, attempt.ID, attempt.RunEpoch, usage, "decision_limit")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TaskID != task.ID || receipt.AttemptID != attempt.ID || receipt.RunEpoch != attempt.RunEpoch || receipt.GoalHash != task.ContractHash || receipt.CurrentRevision != contract.BaseRevision {
		t.Fatalf("receipt identity mismatch: %+v", receipt)
	}
	if receipt.ProjectionVersion != contextengine.DecisionProjectionVersion || receipt.ProjectionBinding == "" || receipt.NextAction == "" || receipt.Decisions != usage.Decisions {
		t.Fatalf("receipt lacks durable binding: %+v", receipt)
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > maxWebEpisodeReceiptBytes || strings.Contains(string(raw), "messages") || strings.Contains(string(raw), "transcript") {
		t.Fatalf("receipt not compact/transcript-free: %s", raw)
	}
}
