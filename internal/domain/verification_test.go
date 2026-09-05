package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestVerificationEvidenceIntegrityBindsIdentityEnvironmentAndVerdict(t *testing.T) {
	environment := json.RawMessage(`{"schema":1,"tool":"go"}`)
	environmentDigest := sha256.Sum256(environment)
	outputDigest := sha256.Sum256([]byte("ok"))
	evidence := VerificationEvidence{
		ID:                "evidence-1",
		TaskID:            "task-1",
		AttemptID:         "attempt-1",
		RunEpoch:          1,
		GoalHash:          "goal-hash",
		BaseRevision:      "base",
		CandidateRevision: "candidate",
		ProfileID:         "profile",
		ProfileHash:       "profile-hash",
		EnvironmentJSON:   environment,
		EnvironmentHash:   hex.EncodeToString(environmentDigest[:]),
		Commands: []VerificationCommandEvidence{{
			Name: "go", Args: []string{"test", "./..."}, ExitCode: 0, Passed: true,
			OutputSHA256: hex.EncodeToString(outputDigest[:]),
		}},
		Acceptance: []AcceptanceEvidence{{Criterion: "tests pass", Passed: true, EvidenceRefs: []string{"command:1"}}},
		Verdict:    VerificationPass,
		CreatedAt:  time.Now().UTC(),
	}
	var err error
	evidence.IntegrityHash, err = evidence.IntegrityDigest()
	if err != nil || !evidence.IntegrityValid() {
		t.Fatalf("valid evidence rejected: err=%v evidence=%+v", err, evidence)
	}

	tampered := evidence
	tampered.CandidateRevision = "different-candidate"
	if tampered.IntegrityValid() {
		t.Fatal("candidate revision tamper did not invalidate evidence integrity")
	}
	badEnvironment := evidence
	badEnvironment.EnvironmentHash = "deadbeef"
	if err := badEnvironment.ValidateIdentity(); err == nil {
		t.Fatal("environment JSON/hash mismatch was accepted")
	}
	badVerdict := evidence
	badVerdict.Verdict = VerificationFail
	if err := badVerdict.ValidateIdentity(); err == nil {
		t.Fatal("PASS command/acceptance evidence with FAIL verdict was accepted")
	}
}

func TestTaskResultIntegrityBindsExplicitRiskAndResultIdentity(t *testing.T) {
	result := TaskResult{
		ID:                   "result-1",
		TaskID:               "task-1",
		Version:              1,
		GoalHash:             "goal-hash",
		BaseRevision:         "base",
		FinalRevision:        "candidate",
		ChangedAreas:         []string{},
		EvidenceID:           "evidence-1",
		VerificationExecuted: []string{"go test ./..."},
		PassFailEvidence:     []string{"command:1:PASS"},
		UnresolvedRisks:      []string{},
		IntegrationStatus:    "NOT_INTEGRATED",
		WorkspaceDisposition: "RETAINED",
		ResourceSummary:      ResourceSummary{AgentTurns: 1, AgentToolCalls: 2, ModelTotalTokens: 3},
		Verdict:              ResultVerified,
		CreatedAt:            time.Now().UTC(),
	}
	var err error
	result.IntegrityHash, err = result.IntegrityDigest()
	if err != nil || !result.IntegrityValid() {
		t.Fatalf("valid result rejected: err=%v result=%+v", err, result)
	}

	tampered := result
	tampered.UnresolvedRisks = []string{"new risk"}
	if tampered.IntegrityValid() {
		t.Fatal("unresolved risk tamper did not invalidate result integrity")
	}
	implicit := result
	implicit.UnresolvedRisks = nil
	if err := implicit.ValidateIdentity(); err == nil {
		t.Fatal("implicit unresolved risk state was accepted")
	}
}
