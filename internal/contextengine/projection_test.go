package contextengine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mar/internal/domain"
)

func TestDecisionProjectionBindsAuthoritativeIdentityWithoutCreatingSecondAuthority(t *testing.T) {
	in := projectionFixture(t)
	p, err := BuildDecisionProjection(in, DecisionProjectionConfig{MaxBytes: 32 << 10})
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != DecisionProjectionVersion || p.TaskID != in.State.TaskID || p.GoalHash != in.State.GoalHash || p.AttemptID != in.State.AttemptID || p.RunEpoch != in.State.RunEpoch || p.CurrentRevision != in.State.CurrentRevision || !p.CreatedAt.Equal(in.State.CreatedAt.UTC()) {
		t.Fatalf("projection identity mismatch: %+v", p)
	}
	if p.AttemptAuthority != domain.AttemptActive || p.Contract.Goal != in.Contract.Goal {
		t.Fatalf("authority/contract truth missing: %+v", p)
	}
}

func TestDecisionProjectionBoundsAdversarialHistory(t *testing.T) {
	in := projectionFixture(t)
	for i := 0; i < 2000; i++ {
		in.Recent = append(in.Recent, DecisionProjectionEvent{Role: "tool", ToolCallID: "old", Kind: "read_file", Content: strings.Repeat("stale-history-", 2000)})
	}
	for i := 0; i < 50; i++ {
		in.Repository.Entries = append(in.Repository.Entries, Entry{Path: "huge.go", SHA256: strings.Repeat("a", 64), Text: strings.Repeat("raw-diff-", 4000)})
	}
	p, err := BuildDecisionProjection(in, DecisionProjectionConfig{MaxBytes: 12 << 10, MaxRecentItems: 4, MaxRecentBytes: 2048, MaxArtifacts: 4, MaxRepositoryEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 12<<10 || len(p.Recent) > 4 || len(p.Repository.Entries) > 2 || !p.Truncated {
		t.Fatalf("projection budgets not enforced: bytes=%d recent=%d entries=%d truncated=%v", len(raw), len(p.Recent), len(p.Repository.Entries), p.Truncated)
	}
	if strings.Contains(string(raw), strings.Repeat("stale-history-", 100)) || strings.Contains(string(raw), strings.Repeat("raw-diff-", 100)) {
		t.Fatal("stale transcript/repository payload leaked into bounded projection")
	}
}

func TestDecisionProjectionPreservesAuthorityCriticalTruth(t *testing.T) {
	in := projectionFixture(t)
	cp := validProjectionCheckpoint(t, in.State.TaskID, "old-attempt", 1, in.State.GoalHash, in.Contract.BaseRevision, in.State.CurrentRevision)
	cp.Payload.Blockers = []string{"owner decision pending"}
	cp.Payload.NextAction = "wait for bounded owner choice"
	cp.IntegrityHash, _ = cp.IntegrityDigest()
	in.State.Checkpoint = &cp
	control := validProjectionControl(t, in.State.TaskID, 7, domain.ControlInput, domain.InputPayload{Message: "use retained workspace"})
	in.State.Controls = []domain.TaskControl{control}
	in.State.LatestControlVersion = 7
	in.State.Result = &DecisionProjectionResult{ID: "result-1", Version: 1, FinalRevision: "candidate", EvidenceID: "evidence-1", IntegrationStatus: "PENDING", WorkspaceDisposition: "RETAINED", IntegrityHash: strings.Repeat("c", 64), CreatedAt: time.Now().UTC()}
	in.State.Evidence = &DecisionProjectionEvidence{ID: "evidence-1", AttemptID: "old-attempt", RunEpoch: 1, CandidateRevision: "candidate", ProfileID: "go-standard", ProfileHash: strings.Repeat("d", 64), EnvironmentHash: strings.Repeat("e", 64), Verdict: domain.VerificationPass, IntegrityHash: strings.Repeat("f", 64), CreatedAt: time.Now().UTC()}
	p, err := BuildDecisionProjection(in, DecisionProjectionConfig{MaxBytes: 32 << 10})
	if err != nil {
		t.Fatal(err)
	}
	if p.Checkpoint == nil || p.Checkpoint.Payload.NextAction != "wait for bounded owner choice" || len(p.Controls) != 1 || p.Controls[0].Version != 7 || p.Result == nil || p.Evidence == nil {
		t.Fatalf("authority-critical truth was dropped: %+v", p)
	}
	in.State.Checkpoint.Payload.CurrentHypothesis = strings.Repeat("mandatory-current-truth-", 400)
	in.State.Checkpoint.IntegrityHash, _ = in.State.Checkpoint.IntegrityDigest()
	if _, err := BuildDecisionProjection(in, DecisionProjectionConfig{MaxBytes: 2048}); err == nil {
		t.Fatal("expected fail-closed projection when mandatory current truth cannot fit")
	}
}

func TestDecisionProjectionUsesObservationArtifactReferences(t *testing.T) {
	in := projectionFixture(t)
	in.State.Artifacts = []domain.ObservationArtifact{{Handle: "obs-" + strings.Repeat("a", 64), TaskID: in.State.TaskID, AttemptID: in.State.AttemptID, RunEpoch: in.State.RunEpoch, ToolCallID: "call-big", Kind: "read_file", SHA256: strings.Repeat("b", 64), CapturedBytes: 8 << 20, SourceBytes: 9 << 20, Complete: false, Truncated: true, CreatedAt: time.Now().UTC()}}
	p, err := BuildDecisionProjection(in, DecisionProjectionConfig{MaxBytes: 32 << 10})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "obs-"+strings.Repeat("a", 64)) || !strings.Contains(text, strings.Repeat("b", 64)) {
		t.Fatalf("artifact recoverability metadata missing: %s", text)
	}
	if strings.Contains(text, strings.Repeat("artifact-raw-payload", 20)) {
		t.Fatal("projection unexpectedly contains raw artifact bytes")
	}
}

func TestDecisionProjectionCarriesFirstVerificationFailure(t *testing.T) {
	in := projectionFixture(t)
	in.State.Result = &DecisionProjectionResult{
		ID: "result-failed", Version: 1, FinalRevision: "candidate", EvidenceID: "evidence-failed",
		IntegrationStatus: "NOT_INTEGRATED", WorkspaceDisposition: "RETAINED",
		Verdict: domain.ResultVerificationFailed, IntegrityHash: strings.Repeat("c", 64), CreatedAt: time.Now().UTC(),
	}
	in.State.Evidence = &DecisionProjectionEvidence{
		ID: "evidence-failed", AttemptID: "old-attempt", RunEpoch: 1, CandidateRevision: "candidate",
		ProfileID: "go-standard", ProfileHash: strings.Repeat("d", 64), EnvironmentHash: strings.Repeat("e", 64),
		Verdict: domain.VerificationFail,
		FirstFailedCommand: &DecisionProjectionCommandFailure{
			Index: 1, Name: "go", Args: []string{"test", "./..."}, ExitCode: 1, DurationMS: 120,
			OutputSHA256: strings.Repeat("a", 64), OutputPrefix: "FAIL repair-me-now",
		},
		IntegrityHash: strings.Repeat("f", 64), CreatedAt: time.Now().UTC(),
	}
	p, err := BuildDecisionProjection(in, DecisionProjectionConfig{MaxBytes: 32 << 10})
	if err != nil {
		t.Fatal(err)
	}
	if p.Evidence == nil || p.Evidence.FirstFailedCommand == nil || p.Evidence.FirstFailedCommand.Name != "go" || !strings.Contains(p.Evidence.FirstFailedCommand.OutputPrefix, "repair-me-now") {
		t.Fatalf("first verification failure was not preserved in projection: %+v", p.Evidence)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "first_failed_command") || !strings.Contains(string(raw), "repair-me-now") {
		t.Fatalf("bounded first failure missing from projection JSON: %s", raw)
	}
}

func projectionFixture(t *testing.T) DecisionProjectionInput {
	t.Helper()
	contract := domain.GoalContract{Goal: "bounded decisions", Acceptance: []string{"bounded"}, ProjectID: "mar", BaseRevision: "base", VerificationProfile: "go-standard", Priority: "high"}
	hash, err := contract.Hash()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	return DecisionProjectionInput{Contract: contract, State: DecisionProjectionState{TaskID: "task-1", GoalHash: hash, TaskState: domain.TaskRunning, ProjectID: "mar", WorkspaceID: "workspace-1", WorkspaceState: domain.WorkspaceState("READY"), BaseRevision: "base", CurrentRevision: "base", AttemptID: "attempt-2", RunEpoch: 2, AttemptAuthority: domain.AttemptActive, CreatedAt: now}, Repository: Pack{Revision: "base", GoalHash: hash, Terms: []string{"bounded"}, Entries: []Entry{{Path: "worker.go", SHA256: strings.Repeat("1", 64), Text: "package worker\n"}}}}
}

func validProjectionCheckpoint(t *testing.T, taskID, attemptID string, epoch int64, goalHash, base, current string) domain.SemanticCheckpoint {
	t.Helper()
	cp := domain.SemanticCheckpoint{ID: "checkpoint-1", TaskID: taskID, AttemptID: attemptID, RunEpoch: epoch, Version: 1, GoalHash: goalHash, BaseRevision: base, CurrentRevision: current, Payload: domain.SemanticCheckpointPayload{CurrentHypothesis: "current", VerificationStatus: "pending", NextAction: "continue"}, CreatedAt: time.Now().UTC()}
	var err error
	cp.IntegrityHash, err = cp.IntegrityDigest()
	if err != nil {
		t.Fatal(err)
	}
	return cp
}

func validProjectionControl(t *testing.T, taskID string, version int64, kind domain.ControlKind, payload any) domain.TaskControl {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	c := domain.TaskControl{ID: "control-1", TaskID: taskID, Version: version, IdempotencyKey: "control-key", Kind: kind, Payload: raw, CreatedAt: time.Now().UTC()}
	c.IntegrityHash, err = c.IntegrityDigest()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
