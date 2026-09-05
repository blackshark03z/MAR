//go:build windows

package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mar/internal/aci"
	"mar/internal/domain"
	"mar/internal/store"
)

type fakeVerificationRuntime struct {
	root    string
	results []aci.ExecResult
	errs    []error
	hook    func(int)
	calls   []aci.Command
}

func (f *fakeVerificationRuntime) Root() string          { return f.root }
func (f *fakeVerificationRuntime) SelfHostingSafe() bool { return true }
func (f *fakeVerificationRuntime) RunCommand(_ context.Context, command aci.Command) (aci.ExecResult, error) {
	index := len(f.calls)
	f.calls = append(f.calls, command)
	if f.hook != nil {
		f.hook(index)
	}
	var result aci.ExecResult
	if index < len(f.results) {
		result = f.results[index]
	}
	var err error
	if index < len(f.errs) {
		err = f.errs[index]
	}
	return result, err
}

func fixedVerificationEnvironment(payload string) environmentSnapshotFunc {
	return func(Profile) (json.RawMessage, string, error) {
		raw := json.RawMessage(payload)
		sum := sha256.Sum256(raw)
		return append(json.RawMessage(nil), raw...), hex.EncodeToString(sum[:]), nil
	}
}

func verifierForHarness(t *testing.T, h sealerHarness, profile Profile) *Verifier {
	t.Helper()
	registry, err := NewRegistry(profile)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(h.store, h.sealer, registry)
	if err != nil {
		t.Fatal(err)
	}
	verifier.environment = fixedVerificationEnvironment(`{"schema":1,"toolchain":"fake-stable"}`)
	return verifier
}

func verifierProfile() Profile {
	return Profile{ID: "test", Commands: []Command{
		{Name: "go", Args: []string{"test", "./..."}},
		{Name: "go", Args: []string{"vet", "./..."}},
	}}
}

func prepareVerifierCandidate(t *testing.T, h sealerHarness) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.root, "main.go"), []byte("package main\nfunc main() { println(\"verified\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVerifierPassPersistsRevisionBoundEvidenceAndVerifiedState(t *testing.T) {
	h := newSealerHarness(t, true)
	prepareVerifierCandidate(t, h)
	verifier := verifierForHarness(t, h, verifierProfile())
	runtime := &fakeVerificationRuntime{root: h.root, results: []aci.ExecResult{
		{Output: "ok test", ExitCode: 0},
		{Output: "ok vet", ExitCode: 0},
	}}

	result, err := verifier.Verify(context.Background(), VerifyRequest{
		TaskID: h.task.ID, AttemptID: h.attempt.ID, RunEpoch: h.attempt.RunEpoch, Runtime: runtime,
		ResourceSummary: domain.ResourceSummary{AgentTurns: 4, AgentToolCalls: 7, ModelTotalTokens: 1234},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != domain.ResultVerified || result.Version != 1 || len(result.ChangedAreas) != 1 || result.ChangedAreas[0] != "main.go" {
		t.Fatalf("unexpected verified result: %+v", result)
	}
	if result.UnresolvedRisks == nil || len(result.UnresolvedRisks) != 0 || !result.IntegrityValid() {
		t.Fatalf("verified result risks/integrity invalid: %+v", result)
	}
	task, err := h.store.GetTask(context.Background(), h.task.ID)
	if err != nil || task.State != domain.TaskVerified {
		t.Fatalf("task did not become VERIFIED from authoritative evidence: task=%+v err=%v", task, err)
	}
	evidence, err := h.store.GetVerificationEvidence(context.Background(), result.EvidenceID)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Verdict != domain.VerificationPass || evidence.CandidateRevision != result.FinalRevision || evidence.ProfileHash == "" || evidence.EnvironmentHash == "" || !evidence.IntegrityValid() {
		t.Fatalf("unexpected verification evidence: %+v", evidence)
	}
	if len(evidence.Acceptance) != len(h.task.Contract.Acceptance) || !evidence.Acceptance[0].Passed {
		t.Fatalf("acceptance was not authoritatively evaluated: %+v", evidence.Acceptance)
	}
	fresh, err := verifier.EvidenceFresh(context.Background(), evidence.ID)
	if err != nil || !fresh {
		t.Fatalf("newly verified evidence should be fresh: fresh=%v err=%v", fresh, err)
	}
}

func TestVerifierFailurePersistsEvidenceAndCannotBecomeVerified(t *testing.T) {
	h := newSealerHarness(t, true)
	prepareVerifierCandidate(t, h)
	verifier := verifierForHarness(t, h, verifierProfile())
	runtime := &fakeVerificationRuntime{
		root:    h.root,
		results: []aci.ExecResult{{Output: "FAIL", ExitCode: 1}, {Output: "ok vet", ExitCode: 0}},
		errs:    []error{errors.New("exit status 1"), nil},
	}

	result, err := verifier.Verify(context.Background(), VerifyRequest{TaskID: h.task.ID, AttemptID: h.attempt.ID, RunEpoch: h.attempt.RunEpoch, Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != domain.ResultVerificationFailed || len(result.UnresolvedRisks) == 0 {
		t.Fatalf("failed verification did not produce explicit failed result/risk: %+v", result)
	}
	task, err := h.store.GetTask(context.Background(), h.task.ID)
	if err != nil || task.State != domain.TaskBlocked {
		t.Fatalf("failed verification reached invalid state: task=%+v err=%v", task, err)
	}
	evidence, err := h.store.GetVerificationEvidence(context.Background(), result.EvidenceID)
	if err != nil || evidence.Verdict != domain.VerificationFail || evidence.Acceptance[0].Passed {
		t.Fatalf("failed evidence invalid: evidence=%+v err=%v", evidence, err)
	}
	fresh, err := verifier.EvidenceFresh(context.Background(), evidence.ID)
	if err != nil || fresh {
		t.Fatalf("failed evidence must never be integration-eligible: fresh=%v err=%v", fresh, err)
	}
}

func TestVerifierRejectsStaleAttemptBeforeCandidatePublication(t *testing.T) {
	h := newSealerHarness(t, true)
	prepareVerifierCandidate(t, h)
	verifier := verifierForHarness(t, h, verifierProfile())
	if err := h.service.LogicalFenceAttempt(context.Background(), h.task.ID, h.attempt.ID, h.attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	_, err := verifier.Verify(context.Background(), VerifyRequest{TaskID: h.task.ID, AttemptID: h.attempt.ID, RunEpoch: h.attempt.RunEpoch, Runtime: &fakeVerificationRuntime{root: h.root}})
	if !errors.Is(err, store.ErrStaleAttempt) {
		t.Fatalf("stale attempt was not fenced: %v", err)
	}
	if _, ok, loadErr := h.store.LatestTaskResult(context.Background(), h.task.ID); loadErr != nil || ok {
		t.Fatalf("stale attempt published a result: ok=%v err=%v", ok, loadErr)
	}
	task, err := h.store.GetTask(context.Background(), h.task.ID)
	if err != nil || task.State != domain.TaskRunning {
		t.Fatalf("stale attempt unexpectedly changed task state: task=%+v err=%v", task, err)
	}
}

func TestVerifierEnvironmentDriftProducesFailedEvidence(t *testing.T) {
	h := newSealerHarness(t, true)
	prepareVerifierCandidate(t, h)
	verifier := verifierForHarness(t, h, verifierProfile())
	calls := 0
	verifier.environment = func(Profile) (json.RawMessage, string, error) {
		calls++
		payload := json.RawMessage(`{"schema":1,"toolchain":"A"}`)
		if calls > 1 {
			payload = json.RawMessage(`{"schema":1,"toolchain":"B"}`)
		}
		sum := sha256.Sum256(payload)
		return payload, hex.EncodeToString(sum[:]), nil
	}
	runtime := &fakeVerificationRuntime{root: h.root, results: []aci.ExecResult{{ExitCode: 0}, {ExitCode: 0}}}
	result, err := verifier.Verify(context.Background(), VerifyRequest{TaskID: h.task.ID, AttemptID: h.attempt.ID, RunEpoch: h.attempt.RunEpoch, Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != domain.ResultVerificationFailed || len(result.UnresolvedRisks) == 0 {
		t.Fatalf("environment drift did not fail verification explicitly: %+v", result)
	}
	task, _ := h.store.GetTask(context.Background(), h.task.ID)
	if task.State != domain.TaskBlocked {
		t.Fatalf("environment drift task state=%s", task.State)
	}
}

func TestVerifierCandidateDriftProducesFailedEvidence(t *testing.T) {
	h := newSealerHarness(t, true)
	prepareVerifierCandidate(t, h)
	verifier := verifierForHarness(t, h, verifierProfile())
	runtime := &fakeVerificationRuntime{
		root:    h.root,
		results: []aci.ExecResult{{ExitCode: 0}, {ExitCode: 0}},
		hook: func(index int) {
			if index == 0 {
				if err := os.WriteFile(filepath.Join(h.root, "main.go"), []byte("package main\nfunc main() { println(\"drift\") }\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		},
	}
	result, err := verifier.Verify(context.Background(), VerifyRequest{TaskID: h.task.ID, AttemptID: h.attempt.ID, RunEpoch: h.attempt.RunEpoch, Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != domain.ResultVerificationFailed || len(result.UnresolvedRisks) == 0 {
		t.Fatalf("candidate drift did not fail verification: %+v", result)
	}
}

func TestVerifierFreshnessRejectsProfileAndRevisionDrift(t *testing.T) {
	h := newSealerHarness(t, true)
	prepareVerifierCandidate(t, h)
	verifier := verifierForHarness(t, h, verifierProfile())
	result, err := verifier.Verify(context.Background(), VerifyRequest{
		TaskID: h.task.ID, AttemptID: h.attempt.ID, RunEpoch: h.attempt.RunEpoch,
		Runtime: &fakeVerificationRuntime{root: h.root, results: []aci.ExecResult{{ExitCode: 0}, {ExitCode: 0}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	driftedRegistry, err := NewRegistry(Profile{ID: "test", Commands: []Command{{Name: "go", Args: []string{"build", "./..."}}}})
	if err != nil {
		t.Fatal(err)
	}
	driftedVerifier, err := NewVerifier(h.store, h.sealer, driftedRegistry)
	if err != nil {
		t.Fatal(err)
	}
	driftedVerifier.environment = verifier.environment
	fresh, err := driftedVerifier.EvidenceFresh(context.Background(), result.EvidenceID)
	if err != nil || fresh {
		t.Fatalf("profile drift did not stale prior evidence: fresh=%v err=%v", fresh, err)
	}

	runVerificationGit(t, h.root, "config", "user.name", "test")
	runVerificationGit(t, h.root, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(h.root, "after.txt"), []byte("new revision\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runVerificationGit(t, h.root, "add", "after.txt")
	runVerificationGit(t, h.root, "commit", "-m", "revision drift")
	fresh, err = verifier.EvidenceFresh(context.Background(), result.EvidenceID)
	if err != nil || fresh {
		t.Fatalf("revision drift did not stale prior evidence: fresh=%v err=%v", fresh, err)
	}
}

func TestVerifierUntrackedSourceDriftProducesFailedEvidence(t *testing.T) {
	h := newSealerHarness(t, true)
	prepareVerifierCandidate(t, h)
	verifier := verifierForHarness(t, h, verifierProfile())
	runtime := &fakeVerificationRuntime{
		root:    h.root,
		results: []aci.ExecResult{{ExitCode: 0}, {ExitCode: 0}},
		hook: func(index int) {
			if index == 0 {
				if err := os.WriteFile(filepath.Join(h.root, "untracked.go"), []byte("package main\nvar untracked = true\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		},
	}
	result, err := verifier.Verify(context.Background(), VerifyRequest{TaskID: h.task.ID, AttemptID: h.attempt.ID, RunEpoch: h.attempt.RunEpoch, Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != domain.ResultVerificationFailed || len(result.UnresolvedRisks) == 0 {
		t.Fatalf("untracked source drift did not fail verification: %+v", result)
	}
}

func TestVerifierFreshnessRejectsEnvironmentAndUntrackedWorkspaceDrift(t *testing.T) {
	h := newSealerHarness(t, true)
	prepareVerifierCandidate(t, h)
	verifier := verifierForHarness(t, h, verifierProfile())
	stableEnvironment := verifier.environment
	result, err := verifier.Verify(context.Background(), VerifyRequest{
		TaskID: h.task.ID, AttemptID: h.attempt.ID, RunEpoch: h.attempt.RunEpoch,
		Runtime: &fakeVerificationRuntime{root: h.root, results: []aci.ExecResult{{ExitCode: 0}, {ExitCode: 0}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier.environment = fixedVerificationEnvironment(`{"schema":1,"toolchain":"fake-drifted"}`)
	fresh, err := verifier.EvidenceFresh(context.Background(), result.EvidenceID)
	if err != nil || fresh {
		t.Fatalf("environment drift did not stale prior evidence: fresh=%v err=%v", fresh, err)
	}

	verifier.environment = stableEnvironment
	if err := os.WriteFile(filepath.Join(h.root, "untracked.go"), []byte("package main\nvar drifted = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh, err = verifier.EvidenceFresh(context.Background(), result.EvidenceID)
	if err != nil || fresh {
		t.Fatalf("untracked workspace drift did not stale prior evidence: fresh=%v err=%v", fresh, err)
	}
}

func TestVerificationResultPersistsAcrossSQLiteReopen(t *testing.T) {
	h := newSealerHarness(t, true)
	prepareVerifierCandidate(t, h)
	verifier := verifierForHarness(t, h, verifierProfile())
	result, err := verifier.Verify(context.Background(), VerifyRequest{
		TaskID: h.task.ID, AttemptID: h.attempt.ID, RunEpoch: h.attempt.RunEpoch,
		Runtime: &fakeVerificationRuntime{root: h.root, results: []aci.ExecResult{{ExitCode: 0}, {ExitCode: 0}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, ok, err := reopened.LatestTaskResult(context.Background(), h.task.ID)
	if err != nil || !ok || persisted.ID != result.ID || !persisted.IntegrityValid() {
		t.Fatalf("task result did not survive reopen: ok=%v persisted=%+v err=%v", ok, persisted, err)
	}
	evidence, err := reopened.GetVerificationEvidence(context.Background(), persisted.EvidenceID)
	if err != nil || !evidence.IntegrityValid() || evidence.CandidateRevision != persisted.FinalRevision {
		t.Fatalf("verification evidence did not survive reopen: evidence=%+v err=%v", evidence, err)
	}
}
