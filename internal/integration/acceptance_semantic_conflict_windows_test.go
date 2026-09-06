//go:build windows

package integration

import (
	"context"
	"errors"
	"testing"

	"mar/internal/domain"
)

func TestAcceptanceT13DisjointFileIncompatibleGoalsNeverSilentlyIntegrate(t *testing.T) {
	h := newIntegrationHarness(t)
	defer h.store.Close()
	// Task B changed a different file from Task A, but both candidates were
	// derived from the same architectural base. Task A has already advanced the
	// authoritative branch, so B's assumptions are stale even though the textual
	// file sets are disjoint. Frozen V1 requires explicit reconciliation rather
	// than silently treating disjoint paths as semantic compatibility.
	h.result.ChangedAreas = []string{"config/task-b.go"}
	git := &fakeIntegrationGit{ref: "refs/heads/main", head: "task-a-api-revision", clean: true, descendant: true}
	gate := &fakeFreshResultGate{result: h.result, fresh: true}
	manager, err := newManagerWithGit(h.store, gate, git)
	if err != nil {
		t.Fatal(err)
	}
	_, blocked, err := manager.Integrate(context.Background(), h.task.ID)
	if !errors.Is(err, ErrHeadDrift) {
		t.Fatalf("semantic-conflict scenario did not surface stale assumptions: %v", err)
	}
	if blocked.IntegrationStatus != "BLOCKED" || len(blocked.UnresolvedRisks) == 0 || git.updateCalls != 0 {
		t.Fatalf("incompatible disjoint-file candidate silently integrated: result=%+v update_calls=%d", blocked, git.updateCalls)
	}
	task, loadErr := h.store.GetTask(context.Background(), h.task.ID)
	if loadErr != nil || task.State != domain.TaskBlocked {
		t.Fatalf("semantic conflict did not durably block for reconciliation: task=%+v err=%v", task, loadErr)
	}
}
