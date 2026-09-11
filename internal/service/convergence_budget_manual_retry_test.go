package service

import (
	"encoding/json"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestManualAttemptOverrideRequiresFreshBlockedChoiceAfterPhysicalTermination(t *testing.T) {
	terminated := time.Now().UTC().Add(-time.Minute)
	attempt := domain.ExecutionAttempt{TaskID: "task-manual-retry", ID: "attempt-3", RunEpoch: 3, AuthorityState: domain.AttemptPhysicallyTerminated, TerminatedAt: &terminated}

	fresh := testTaskControl(t, domain.SteerPayload{Kind: domain.SteerBlockedChoice, Message: "Owner approves one manual continuation after automatic attempt cap."}, terminated.Add(time.Second))
	if !manualAttemptOverrideAllowed(fresh, true, attempt, true) {
		t.Fatal("fresh blocked_choice after physical termination must allow exactly one manual retry")
	}

	stale := testTaskControl(t, domain.SteerPayload{Kind: domain.SteerBlockedChoice, Message: "old choice"}, terminated)
	if manualAttemptOverrideAllowed(stale, true, attempt, true) {
		t.Fatal("control at or before prior termination must not reopen the attempt budget")
	}

	contextOnly := testTaskControl(t, domain.SteerPayload{Kind: domain.SteerContext, Message: "context only"}, terminated.Add(time.Second))
	if manualAttemptOverrideAllowed(contextOnly, true, attempt, true) {
		t.Fatal("non-blocked-choice steering must not override the attempt limit")
	}

	active := attempt
	active.AuthorityState = domain.AttemptActive
	active.TerminatedAt = nil
	if manualAttemptOverrideAllowed(fresh, true, active, true) {
		t.Fatal("manual retry must require physical termination proof")
	}
}

func testTaskControl(t *testing.T, payload domain.SteerPayload, created time.Time) domain.TaskControl {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	control := domain.TaskControl{
		ID: "control-1", TaskID: "task-manual-retry", Version: 1, IdempotencyKey: "manual-retry-control",
		Kind: domain.ControlSteer, Payload: raw, CreatedAt: created.UTC(),
	}
	digest, err := control.IntegrityDigest()
	if err != nil {
		t.Fatal(err)
	}
	control.IntegrityHash = digest
	return control
}
