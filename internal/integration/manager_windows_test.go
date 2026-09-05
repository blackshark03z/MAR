//go:build windows

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/service"
	"mar/internal/store"
)

type integrationHarness struct {
	store     *store.SQLite
	service   *service.TaskService
	project   domain.Project
	task      domain.Task
	attempt   domain.ExecutionAttempt
	result    domain.TaskResult
	dbPath    string
	base      string
	candidate string
	now       time.Time
}

type fakeFreshResultGate struct {
	result domain.TaskResult
	fresh  bool
	err    error
}

func (g *fakeFreshResultGate) LatestFreshResult(context.Context, string) (domain.TaskResult, bool, error) {
	return g.result, g.fresh, g.err
}

type fakeIntegrationGit struct {
	ref         string
	head        string
	clean       bool
	descendant  bool
	updateCalls int
	resetCalls  int
}

func (g *fakeIntegrationGit) Run(_ context.Context, _ string, _ string, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("missing git operation")
	}
	switch args[0] {
	case "symbolic-ref":
		return g.ref, nil
	case "rev-parse":
		return g.head, nil
	case "status":
		if g.clean {
			return "", nil
		}
		return " M owner-change.txt", nil
	case "merge-base":
		if g.descendant {
			return "", nil
		}
		return "", errors.New("candidate is not a descendant")
	case "update-ref":
		if len(args) != 4 {
			return "", fmt.Errorf("unexpected update-ref args: %v", args)
		}
		if args[1] != g.ref || g.head != args[3] {
			return "", errors.New("compare-and-advance precondition failed")
		}
		g.updateCalls++
		g.head = args[2]
		return "", nil
	case "reset":
		if len(args) != 3 || args[1] != "--merge" || args[2] != g.head {
			return "", fmt.Errorf("unexpected reset args: %v", args)
		}
		g.resetCalls++
		return "", nil
	default:
		return "", fmt.Errorf("unexpected git operation: %v", args)
	}
}

func newIntegrationHarness(t *testing.T) integrationHarness {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	base := "base-revision"
	candidate := "candidate-revision"
	dbPath := filepath.Join(t.TempDir(), "mar.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewTaskService(s)
	project, _, err := svc.RegisterProject(ctx, "integration-project", t.TempDir())
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "prove crash-safe authoritative integration",
		Acceptance:          []string{"candidate integrates exactly once"},
		ProjectID:           project.ID,
		BaseRevision:        base,
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: true},
		VerificationProfile: "integration-test",
		Priority:            "P2",
	}
	task, _, err := svc.Submit(ctx, "integration-key", contract)
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			_ = s.Close()
			t.Fatal(err)
		}
	}
	workspace := domain.Workspace{
		ID:           "integration-workspace",
		TaskID:       task.ID,
		ProjectID:    project.ID,
		Path:         t.TempDir(),
		BaseRevision: base,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	workspace, _, err = s.BeginWorkspace(ctx, workspace)
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	if err := s.MarkWorkspaceReady(ctx, workspace.ID, task.ID, base, now); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "integration-worker", "integration-supervisor", time.Minute)
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	if err := s.RecordWorkspaceHeadForAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, base, candidate, now); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	if err := svc.TransitionForAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, domain.TaskVerifying); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}

	environment := json.RawMessage(`{"schema":1,"toolchain":"integration-test"}`)
	environmentDigest := sha256.Sum256(environment)
	outputDigest := sha256.Sum256([]byte("PASS"))
	evidence := domain.VerificationEvidence{
		ID:                "integration-evidence",
		TaskID:            task.ID,
		AttemptID:         attempt.ID,
		RunEpoch:          attempt.RunEpoch,
		GoalHash:          task.ContractHash,
		BaseRevision:      base,
		CandidateRevision: candidate,
		ProfileID:         contract.VerificationProfile,
		ProfileHash:       "integration-profile-hash",
		EnvironmentJSON:   environment,
		EnvironmentHash:   hex.EncodeToString(environmentDigest[:]),
		Commands: []domain.VerificationCommandEvidence{{
			Name:         "go",
			Args:         []string{"test", "./..."},
			ExitCode:     0,
			Passed:       true,
			DurationMS:   1,
			OutputSHA256: hex.EncodeToString(outputDigest[:]),
		}},
		Acceptance: []domain.AcceptanceEvidence{{
			Criterion:    contract.Acceptance[0],
			Passed:       true,
			EvidenceRefs: []string{"command:1"},
		}},
		Verdict:   domain.VerificationPass,
		CreatedAt: now,
	}
	evidence.IntegrityHash, err = evidence.IntegrityDigest()
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	result := domain.TaskResult{
		ID:                   "integration-result-verified",
		TaskID:               task.ID,
		GoalHash:             task.ContractHash,
		BaseRevision:         base,
		FinalRevision:        candidate,
		ChangedAreas:         []string{"internal/example.go"},
		EvidenceID:           evidence.ID,
		VerificationExecuted: []string{"go test ./..."},
		PassFailEvidence:     []string{"command:1:PASS"},
		UnresolvedRisks:      []string{},
		IntegrationStatus:    "NOT_INTEGRATED",
		WorkspaceDisposition: "RETAINED",
		Verdict:              domain.ResultVerified,
		CreatedAt:            now,
	}
	result, err = s.PersistVerificationOutcome(ctx, evidence, result, now)
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	if err := s.ConfirmAttemptTerminated(ctx, task.ID, attempt.ID, attempt.RunEpoch, "verified", now); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	return integrationHarness{store: s, service: svc, project: project, task: task, attempt: attempt, result: result, dbPath: dbPath, base: base, candidate: candidate, now: now}
}

func prepareDispatchedAttempt(t *testing.T, h integrationHarness) domain.IntegrationAttempt {
	t.Helper()
	ctx := context.Background()
	attempt, err := h.store.PrepareIntegrationAttempt(ctx, domain.IntegrationAttempt{
		ID:                 "integration-attempt",
		TaskID:             h.task.ID,
		ProjectID:          h.project.ID,
		ExpectedRef:        "refs/heads/main",
		ExpectedHead:       h.base,
		TaskResultID:       h.result.ID,
		TaskResultVersion:  h.result.Version,
		TaskResultRevision: h.result.FinalRevision,
		CandidateRevision:  h.result.FinalRevision,
		EvidenceID:         h.result.EvidenceID,
	}, h.now)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err = h.store.MarkIntegrationDispatched(ctx, attempt.ID, h.now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func TestRecoverDispatchedBeforeCASAdvancesOnceAndFinalizes(t *testing.T) {
	h := newIntegrationHarness(t)
	defer h.store.Close()
	attempt := prepareDispatchedAttempt(t, h)
	git := &fakeIntegrationGit{ref: attempt.ExpectedRef, head: h.base, clean: true, descendant: true}
	gate := &fakeFreshResultGate{result: h.result, fresh: true}
	manager, err := newManagerWithGit(h.store, gate, git)
	if err != nil {
		t.Fatal(err)
	}

	completed, result, err := manager.RecoverAttempt(context.Background(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != domain.IntegrationComplete || result.IntegrationStatus != "INTEGRATED" || result.Version != h.result.Version+1 {
		t.Fatalf("unexpected recovered integration: attempt=%+v result=%+v", completed, result)
	}
	if git.updateCalls != 1 || git.head != h.candidate {
		t.Fatalf("CAS advancement count/head mismatch: calls=%d head=%s", git.updateCalls, git.head)
	}
	task, err := h.store.GetTask(context.Background(), h.task.ID)
	if err != nil || task.State != domain.TaskComplete {
		t.Fatalf("task did not finalize COMPLETE: task=%+v err=%v", task, err)
	}

	completedAgain, resultAgain, err := manager.RecoverAttempt(context.Background(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completedAgain.Status != domain.IntegrationComplete || resultAgain.ID != result.ID || git.updateCalls != 1 {
		t.Fatalf("recovery was not idempotent: attempt=%+v result=%+v updateCalls=%d", completedAgain, resultAgain, git.updateCalls)
	}
}

func TestRecoverAfterCASBeforeFinalizeDoesNotDoubleAdvance(t *testing.T) {
	h := newIntegrationHarness(t)
	defer h.store.Close()
	attempt := prepareDispatchedAttempt(t, h)

	if err := h.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	h.store = reopened
	defer h.store.Close()

	git := &fakeIntegrationGit{ref: attempt.ExpectedRef, head: h.candidate, clean: true, descendant: true}
	gate := &fakeFreshResultGate{result: h.result, fresh: true}
	manager, err := newManagerWithGit(h.store, gate, git)
	if err != nil {
		t.Fatal(err)
	}
	completed, result, err := manager.RecoverAttempt(context.Background(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != domain.IntegrationComplete || result.IntegrationStatus != "INTEGRATED" {
		t.Fatalf("post-CAS recovery did not finalize: attempt=%+v result=%+v", completed, result)
	}
	if git.updateCalls != 0 {
		t.Fatalf("post-CAS recovery repeated authoritative update-ref: %d", git.updateCalls)
	}
}

func TestIntegrateRejectsAuthoritativeBaseDrift(t *testing.T) {
	h := newIntegrationHarness(t)
	defer h.store.Close()
	git := &fakeIntegrationGit{ref: "refs/heads/main", head: "new-authoritative-head", clean: true, descendant: true}
	gate := &fakeFreshResultGate{result: h.result, fresh: true}
	manager, err := newManagerWithGit(h.store, gate, git)
	if err != nil {
		t.Fatal(err)
	}

	_, blocked, err := manager.Integrate(context.Background(), h.task.ID)
	if !errors.Is(err, ErrHeadDrift) {
		t.Fatalf("base drift was not rejected: %v", err)
	}
	if blocked.IntegrationStatus != "BLOCKED" || len(blocked.UnresolvedRisks) == 0 {
		t.Fatalf("base drift did not publish explicit blocked result: %+v", blocked)
	}
	task, loadErr := h.store.GetTask(context.Background(), h.task.ID)
	if loadErr != nil || task.State != domain.TaskBlocked {
		t.Fatalf("base drift did not block task: task=%+v err=%v", task, loadErr)
	}
	if _, ok, loadErr := h.store.LatestIntegrationAttempt(context.Background(), h.task.ID); loadErr != nil || ok {
		t.Fatalf("base drift created an integration attempt: ok=%v err=%v", ok, loadErr)
	}
}

func TestPreparedAttemptRejectsEvidenceThatBecomesStaleBeforeDispatch(t *testing.T) {
	h := newIntegrationHarness(t)
	defer h.store.Close()
	ctx := context.Background()
	attempt, err := h.store.PrepareIntegrationAttempt(ctx, domain.IntegrationAttempt{
		ID:                 "integration-attempt-stale",
		TaskID:             h.task.ID,
		ProjectID:          h.project.ID,
		ExpectedRef:        "refs/heads/main",
		ExpectedHead:       h.base,
		TaskResultID:       h.result.ID,
		TaskResultVersion:  h.result.Version,
		TaskResultRevision: h.result.FinalRevision,
		CandidateRevision:  h.result.FinalRevision,
		EvidenceID:         h.result.EvidenceID,
	}, h.now)
	if err != nil {
		t.Fatal(err)
	}
	git := &fakeIntegrationGit{ref: attempt.ExpectedRef, head: h.base, clean: true, descendant: true}
	gate := &fakeFreshResultGate{result: h.result, fresh: false}
	manager, err := newManagerWithGit(h.store, gate, git)
	if err != nil {
		t.Fatal(err)
	}

	blockedAttempt, blockedResult, err := manager.RecoverAttempt(ctx, attempt.ID)
	if !errors.Is(err, ErrIntegrationBlocked) {
		t.Fatalf("stale evidence did not block prepared integration: %v", err)
	}
	if blockedAttempt.Status != domain.IntegrationBlocked || blockedResult.IntegrationStatus != "BLOCKED" || git.updateCalls != 0 {
		t.Fatalf("stale evidence reached integration side effect: attempt=%+v result=%+v calls=%d", blockedAttempt, blockedResult, git.updateCalls)
	}
}
