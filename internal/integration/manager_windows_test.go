//go:build windows

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/service"
	"mar/internal/store"
	"mar/internal/testsupport"
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
	ref                     string
	head                    string
	clean                   bool
	descendant              bool
	updateCalls             int
	resetCalls              int
	staged                  bool
	unstaged                bool
	untracked               bool
	injectStagedAfterCAS    bool
	injectUnstagedAfterCAS  bool
	injectUntrackedAfterCAS bool
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
	case "diff-index":
		if g.staged {
			return "owner-staged.txt\x00", nil
		}
		return "", nil
	case "diff-files":
		if g.unstaged {
			return "owner-unstaged.txt\x00", nil
		}
		return "", nil
	case "ls-files":
		if g.untracked {
			return "owner-untracked.txt\x00", nil
		}
		return "", nil
	case "update-ref":
		if len(args) != 4 {
			return "", fmt.Errorf("unexpected update-ref args: %v", args)
		}
		if args[1] != g.ref || g.head != args[3] {
			return "", errors.New("compare-and-advance precondition failed")
		}
		g.updateCalls++
		g.head = args[2]
		if g.injectStagedAfterCAS {
			g.staged = true
			g.clean = false
		}
		if g.injectUnstagedAfterCAS {
			g.unstaged = true
			g.clean = false
		}
		if g.injectUntrackedAfterCAS {
			g.untracked = true
			g.clean = false
		}
		return "", nil
	case "read-tree":
		if len(args) != 5 || args[1] != "-m" || args[2] != "-u" || args[4] != g.head {
			return "", fmt.Errorf("unexpected read-tree args: %v", args)
		}
		g.resetCalls++
		g.clean = true
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
		ResourceSummary:      domain.ResourceSummary{AgentTurns: 3, AgentToolCalls: 4, ModelInputTokens: 100, ModelOutputTokens: 50, ModelTotalTokens: 150},
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

func TestIntegrationStopsBeforeCheckoutMutationWhenOwnerWorkAppearsAfterCAS(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fakeIntegrationGit)
	}{
		{name: "staged", configure: func(g *fakeIntegrationGit) { g.injectStagedAfterCAS = true }},
		{name: "unstaged", configure: func(g *fakeIntegrationGit) { g.injectUnstagedAfterCAS = true }},
		{name: "untracked", configure: func(g *fakeIntegrationGit) { g.injectUntrackedAfterCAS = true }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newIntegrationHarness(t)
			defer h.store.Close()
			attempt := prepareDispatchedAttempt(t, h)
			git := &fakeIntegrationGit{ref: attempt.ExpectedRef, head: h.base, clean: true, descendant: true}
			tc.configure(git)
			gate := &fakeFreshResultGate{result: h.result, fresh: true}
			manager, err := newManagerWithGit(h.store, gate, git)
			if err != nil {
				t.Fatal(err)
			}
			observedAttempt, _, err := manager.RecoverAttempt(context.Background(), attempt.ID)
			if !errors.Is(err, ErrWorktreeSyncRequired) {
				t.Fatalf("Owner %s work did not stop synchronization: %v", tc.name, err)
			}
			if git.updateCalls != 1 || git.resetCalls != 0 {
				t.Fatalf("unexpected checkout mutation after Owner %s work: update=%d sync=%d", tc.name, git.updateCalls, git.resetCalls)
			}
			if observedAttempt.Status != domain.IntegrationDispatched {
				t.Fatalf("integration should remain recoverable/dispatched, got %s", observedAttempt.Status)
			}
		})
	}
}

func TestTwoTreeSyncPreservesOrRefusesOwnerWork(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, root string)
		wantExitOK bool
		wantA      string
		wantB      string
		wantC      string
	}{
		{name: "staged same file", prepare: func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, "a.txt"), []byte("OWNER-STAGED"), 0o644)
			runIntegrationGit(t, root, "add", "a.txt")
		}, wantA: "OWNER-STAGED", wantB: "", wantC: "base-c"},
		{name: "unstaged same file", prepare: func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("OWNER-UNSTAGED"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, wantA: "OWNER-UNSTAGED", wantB: "", wantC: "base-c"},
		{name: "untracked collision", prepare: func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("OWNER-UNTRACKED"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, wantA: "base", wantB: "OWNER-UNTRACKED", wantC: "base-c"},
		{name: "staged different file", prepare: func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "c.txt"), []byte("OWNER-STAGED-OTHER"), 0o644); err != nil {
				t.Fatal(err)
			}
			runIntegrationGit(t, root, "add", "c.txt")
		}, wantExitOK: true, wantA: "candidate", wantB: "candidate-b", wantC: "OWNER-STAGED-OTHER"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			runIntegrationGit(t, root, "init", "-b", "main")
			runIntegrationGit(t, root, "config", "user.email", "integration-race@example.invalid")
			runIntegrationGit(t, root, "config", "user.name", "Integration Race")
			if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("base"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "c.txt"), []byte("base-c"), 0o644); err != nil {
				t.Fatal(err)
			}
			runIntegrationGit(t, root, "add", "a.txt", "c.txt")
			runIntegrationGit(t, root, "commit", "-m", "base")
			base := strings.TrimSpace(runIntegrationGit(t, root, "rev-parse", "HEAD"))
			if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("candidate"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("candidate-b"), 0o644); err != nil {
				t.Fatal(err)
			}
			runIntegrationGit(t, root, "add", "a.txt", "b.txt")
			runIntegrationGit(t, root, "commit", "-m", "candidate")
			candidate := strings.TrimSpace(runIntegrationGit(t, root, "rev-parse", "HEAD"))
			runIntegrationGit(t, root, "reset", "--hard", base)
			runIntegrationGit(t, root, "update-ref", "refs/heads/main", candidate, base)
			tc.prepare(t, root)

			cmd := exec.Command("git", "-C", root, "read-tree", "-m", "-u", base, candidate)
			err := cmd.Run()
			if tc.wantExitOK && err != nil {
				t.Fatalf("safe different-file sync failed: %v", err)
			}
			if !tc.wantExitOK && err == nil {
				t.Fatal("conflicting Owner work was unexpectedly overwritten")
			}
			read := func(name string) string {
				data, readErr := os.ReadFile(filepath.Join(root, name))
				if errors.Is(readErr, os.ErrNotExist) {
					return ""
				}
				if readErr != nil {
					t.Fatal(readErr)
				}
				return string(data)
			}
			if got := read("a.txt"); got != tc.wantA {
				t.Fatalf("a.txt=%q want=%q", got, tc.wantA)
			}
			if got := read("b.txt"); got != tc.wantB {
				t.Fatalf("b.txt=%q want=%q", got, tc.wantB)
			}
			if got := read("c.txt"); got != tc.wantC {
				t.Fatalf("c.txt=%q want=%q", got, tc.wantC)
			}
		})
	}
}

func runIntegrationGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	git := testsupport.RequireExecutable(t, "git")
	cmd := exec.Command(git, append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
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

func TestRetryBlockedVerifiedIntegrationReusesCandidateWithoutNewWorker(t *testing.T) {
	h := newIntegrationHarness(t)
	defer h.store.Close()
	ctx := context.Background()
	git := &fakeIntegrationGit{ref: "refs/heads/main", head: h.base, clean: false, descendant: true}
	gate := &fakeFreshResultGate{result: h.result, fresh: true}
	manager, err := newManagerWithGit(h.store, gate, git)
	if err != nil {
		t.Fatal(err)
	}
	_, blocked, err := manager.Integrate(ctx, h.task.ID)
	if !errors.Is(err, ErrIntegrationBlocked) {
		t.Fatalf("dirty authoritative worktree did not block initial integration: %v", err)
	}
	if blocked.IntegrationStatus != "BLOCKED" || blocked.FinalRevision != h.candidate || blocked.EvidenceID != h.result.EvidenceID {
		t.Fatalf("blocked result lost verified identity: %+v", blocked)
	}
	if blocked.ResourceSummary != h.result.ResourceSummary {
		t.Fatalf("blocked result changed resource summary: got=%+v want=%+v", blocked.ResourceSummary, h.result.ResourceSummary)
	}
	beforeTask, err := h.store.GetTask(ctx, h.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeAttempt, ok, err := h.store.CurrentAttemptByTask(ctx, h.task.ID)
	if err != nil || !ok {
		t.Fatalf("verified attempt unavailable before retry: ok=%v err=%v", ok, err)
	}
	if beforeTask.State != domain.TaskBlocked || beforeAttempt.AuthorityState != domain.AttemptPhysicallyTerminated {
		t.Fatalf("unexpected pre-retry authority state: task=%s attempt=%s", beforeTask.State, beforeAttempt.AuthorityState)
	}
	gate.result = blocked
	git.clean = true
	handled, err := manager.RetryBlockedVerifiedIntegration(ctx, h.task.ID)
	if err != nil || !handled {
		t.Fatalf("verified integration retry failed: handled=%v err=%v", handled, err)
	}
	afterTask, err := h.store.GetTask(ctx, h.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterAttempt, ok, err := h.store.CurrentAttemptByTask(ctx, h.task.ID)
	if err != nil || !ok {
		t.Fatalf("verified attempt unavailable after retry: ok=%v err=%v", ok, err)
	}
	if afterTask.State != domain.TaskComplete || afterTask.RunEpoch != beforeTask.RunEpoch {
		t.Fatalf("retry changed coding epoch or failed to complete: before_epoch=%d after_epoch=%d state=%s", beforeTask.RunEpoch, afterTask.RunEpoch, afterTask.State)
	}
	if afterAttempt.ID != beforeAttempt.ID || afterAttempt.RunEpoch != beforeAttempt.RunEpoch || afterAttempt.AuthorityState != domain.AttemptPhysicallyTerminated {
		t.Fatalf("retry created or changed coding attempt: before=%+v after=%+v", beforeAttempt, afterAttempt)
	}
	final, ok, err := h.store.LatestTaskResult(ctx, h.task.ID)
	if err != nil || !ok {
		t.Fatalf("final integrated result unavailable: ok=%v err=%v", ok, err)
	}
	if final.IntegrationStatus != "INTEGRATED" || final.FinalRevision != blocked.FinalRevision || final.EvidenceID != blocked.EvidenceID || final.ResourceSummary != blocked.ResourceSummary {
		t.Fatalf("integration retry did not preserve verified result identity/resource summary: %+v", final)
	}
	if git.updateCalls != 1 || git.head != h.candidate {
		t.Fatalf("retry did not apply candidate exactly once: calls=%d head=%s", git.updateCalls, git.head)
	}
	latestIntegration, ok, err := h.store.LatestIntegrationAttempt(ctx, h.task.ID)
	if err != nil || !ok {
		t.Fatalf("retry integration attempt unavailable: ok=%v err=%v", ok, err)
	}
	if latestIntegration.Status != domain.IntegrationComplete || latestIntegration.TaskResultID != blocked.ID || latestIntegration.CandidateRevision != blocked.FinalRevision || latestIntegration.EvidenceID != blocked.EvidenceID {
		t.Fatalf("retry integration attempt identity mismatch: %+v", latestIntegration)
	}
}

func TestRetryBlockedVerifiedIntegrationRejectsStaleEvidenceWithoutReplacement(t *testing.T) {
	h := newIntegrationHarness(t)
	defer h.store.Close()
	ctx := context.Background()
	git := &fakeIntegrationGit{ref: "refs/heads/main", head: h.base, clean: false, descendant: true}
	gate := &fakeFreshResultGate{result: h.result, fresh: true}
	manager, err := newManagerWithGit(h.store, gate, git)
	if err != nil {
		t.Fatal(err)
	}
	_, blocked, err := manager.Integrate(ctx, h.task.ID)
	if !errors.Is(err, ErrIntegrationBlocked) {
		t.Fatalf("initial dirty worktree did not block: %v", err)
	}
	beforeTask, err := h.store.GetTask(ctx, h.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeAttempt, ok, err := h.store.CurrentAttemptByTask(ctx, h.task.ID)
	if err != nil || !ok {
		t.Fatalf("verified attempt unavailable: ok=%v err=%v", ok, err)
	}
	gate.result = blocked
	gate.fresh = false
	git.clean = true
	handled, err := manager.RetryBlockedVerifiedIntegration(ctx, h.task.ID)
	if !handled || !errors.Is(err, ErrNotReady) {
		t.Fatalf("stale evidence was not fail-closed: handled=%v err=%v", handled, err)
	}
	afterTask, err := h.store.GetTask(ctx, h.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterAttempt, ok, err := h.store.CurrentAttemptByTask(ctx, h.task.ID)
	if err != nil || !ok {
		t.Fatalf("attempt unavailable after stale retry: ok=%v err=%v", ok, err)
	}
	if afterTask.State != domain.TaskBlocked || afterTask.RunEpoch != beforeTask.RunEpoch || afterAttempt.ID != beforeAttempt.ID || afterAttempt.RunEpoch != beforeAttempt.RunEpoch {
		t.Fatalf("stale retry escaped into replacement coding path")
	}
	if _, ok, err := h.store.LatestIntegrationAttempt(ctx, h.task.ID); err != nil || ok {
		t.Fatalf("stale retry created integration attempt: ok=%v err=%v", ok, err)
	}
	latest, ok, err := h.store.LatestTaskResult(ctx, h.task.ID)
	if err != nil || !ok || latest.ID != blocked.ID || latest.EvidenceID != blocked.EvidenceID || latest.ResourceSummary != blocked.ResourceSummary {
		t.Fatalf("stale retry mutated verified result: latest=%+v ok=%v err=%v", latest, ok, err)
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
