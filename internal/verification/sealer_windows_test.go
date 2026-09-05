//go:build windows

package verification

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/effects"
	"mar/internal/service"
	"mar/internal/store"
)

type sealerHarness struct {
	store   *store.SQLite
	service *service.TaskService
	sealer  *CandidateSealer
	task    domain.Task
	attempt domain.ExecutionAttempt
	root    string
	base    string
	dbPath  string
}

func newSealerHarness(t *testing.T, localGitWrite bool) sealerHarness {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	runVerificationGit(t, root, "init")
	runVerificationGit(t, root, "config", "user.name", "test")
	runVerificationGit(t, root, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runVerificationGit(t, root, "add", "main.go")
	runVerificationGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runVerificationGitOutput(t, root, "rev-parse", "HEAD"))

	dbPath := filepath.Join(t.TempDir(), "mar.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	svc := service.NewTaskService(s)
	if _, _, err := svc.RegisterProject(ctx, "seal-project", root); err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "seal candidate",
		Acceptance:          []string{"candidate is revision-bound"},
		ProjectID:           "seal-project",
		BaseRevision:        base,
		Authority:           domain.Authority{LocalFileWrite: true, LocalGitWrite: localGitWrite},
		VerificationProfile: "test",
		Priority:            "P2",
	}
	task, _, err := svc.Submit(ctx, "seal-task-"+base[:8], contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	workspace := domain.Workspace{ID: "workspace-seal", TaskID: task.ID, ProjectID: contract.ProjectID, Path: root, BaseRevision: base, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	workspace, _, err = s.BeginWorkspace(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWorkspaceReady(ctx, workspace.ID, task.ID, base, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-seal", "supervisor-seal", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sealer, err := NewCandidateSealer(s, effects.New(s))
	if err != nil {
		t.Fatal(err)
	}
	return sealerHarness{s, svc, sealer, task, attempt, root, base, dbPath}
}

func TestCandidateSealerCreatesDetachedRevisionAndExcludesMARArtifacts(t *testing.T) {
	h := newSealerHarness(t, true)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(h.root, "main.go"), []byte("package main\nfunc main() { println(\"ok\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.root, ".mar", "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.root, ".mar", "runtime", "secret.tmp"), []byte("runtime-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	candidate, err := h.sealer.Seal(ctx, h.task.ID, h.attempt.ID, h.attempt.RunEpoch)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Revision == h.base || len(candidate.ChangedPaths) != 1 || candidate.ChangedPaths[0] != "main.go" {
		t.Fatalf("unexpected candidate: %+v", candidate)
	}
	parent := strings.TrimSpace(runVerificationGitOutput(t, h.root, "rev-parse", "HEAD^"))
	if parent != h.base {
		t.Fatalf("candidate parent mismatch: %s != %s", parent, h.base)
	}
	tracked := runVerificationGitOutput(t, h.root, "ls-files")
	if strings.Contains(tracked, ".mar/") {
		t.Fatalf("task-owned .mar artifact was committed: %s", tracked)
	}
	workspace, err := h.store.GetWorkspaceByTask(ctx, h.task.ID)
	if err != nil || workspace.HeadRevision != candidate.Revision {
		t.Fatalf("durable workspace head not advanced: %+v err=%v", workspace, err)
	}
	record, err := h.store.GetEffect(ctx, sealOperationID(h.task.ID, h.attempt.ID, h.attempt.RunEpoch))
	if err != nil || record.State != domain.EffectObserved || record.ObservationOutcome != domain.OutcomeApplied {
		t.Fatalf("candidate effect not observed applied: %+v err=%v", record, err)
	}
}

func TestCandidateSealerResumeAfterAppliedReturnsSameRevision(t *testing.T) {
	h := newSealerHarness(t, true)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(h.root, "main.go"), []byte("package main\nfunc main() { println(\"resume\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := h.sealer.Seal(ctx, h.task.ID, h.attempt.ID, h.attempt.RunEpoch)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.sealer.Seal(ctx, h.task.ID, h.attempt.ID, h.attempt.RunEpoch)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != second.Revision || len(second.ChangedPaths) != 1 || second.ChangedPaths[0] != "main.go" {
		t.Fatalf("applied candidate seal was not resumed idempotently: first=%+v second=%+v", first, second)
	}
	if got := strings.TrimSpace(runVerificationGitOutput(t, h.root, "rev-parse", "HEAD")); got != first.Revision {
		t.Fatalf("resume created an unexpected second candidate commit: got=%s want=%s", got, first.Revision)
	}
}

func TestCandidateSealerRejectsChangedWorkspaceWithoutLocalGitWrite(t *testing.T) {
	h := newSealerHarness(t, false)
	if err := os.WriteFile(filepath.Join(h.root, "main.go"), []byte("package main\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := h.sealer.Seal(context.Background(), h.task.ID, h.attempt.ID, h.attempt.RunEpoch)
	if err == nil || !strings.Contains(err.Error(), "local Git write authority") {
		t.Fatalf("missing LocalGitWrite did not block candidate seal: %v", err)
	}
	if got := strings.TrimSpace(runVerificationGitOutput(t, h.root, "rev-parse", "HEAD")); got != h.base {
		t.Fatalf("candidate commit happened despite denied authority: %s", got)
	}
}

func TestCandidateSealerReconciliationRejectsCommitWithWrongAuthorizedState(t *testing.T) {
	h := newSealerHarness(t, true)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(h.root, "main.go"), []byte("package main\nfunc main() { println(\"authorized\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := h.sealer.changedPaths(ctx, h.task.ID, h.root)
	if err != nil {
		t.Fatal(err)
	}
	stateHash, err := workspaceStateHash(h.root, paths)
	if err != nil {
		t.Fatal(err)
	}
	message := "MAR candidate task=" + h.task.ID + " attempt=" + h.attempt.ID + " epoch=1"
	payload := sealIntentPayload{WorkspacePath: filepath.Clean(h.root), ExpectedHead: h.base, ChangedPaths: paths, WorkspaceStateHash: stateHash, CommitMessage: message}
	payloadJSON, _ := json.Marshal(payload)
	intent := domain.EffectIntent{OperationID: sealOperationID(h.task.ID, h.attempt.ID, h.attempt.RunEpoch), TaskID: h.task.ID, AttemptID: h.attempt.ID, RunEpoch: h.attempt.RunEpoch, Type: domain.EffectLocalObservable, ExpectedPrecondition: "workspace-head=" + h.base + ";state=" + stateHash, Payload: payloadJSON}
	mgr := effects.New(h.store)
	if _, decision, err := mgr.Plan(ctx, intent); err != nil || decision != effects.DecisionDispatch {
		t.Fatalf("pre-dispatch plan: %s %v", decision, err)
	}
	if _, err := mgr.AuthorizeDispatch(ctx, intent.OperationID, h.task.ID, h.attempt.ID, h.attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(h.root, "main.go"), []byte("package main\nfunc main() { println(\"wrong-state\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runVerificationGit(t, h.root, "add", "main.go")
	runVerificationGit(t, h.root, "commit", "-m", message)

	if _, err := h.sealer.Seal(ctx, h.task.ID, h.attempt.ID, h.attempt.RunEpoch); err == nil || !strings.Contains(err.Error(), "workspace-state identity mismatch") {
		t.Fatalf("reconciliation accepted commit bytes outside authorized state: %v", err)
	}
	record, err := h.store.GetEffect(ctx, intent.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != domain.EffectDispatched || record.ObservationOutcome != "" {
		t.Fatalf("inconclusive mismatched commit was falsely observed: %+v", record)
	}
	workspace, err := h.store.GetWorkspaceByTask(ctx, h.task.ID)
	if err != nil || workspace.HeadRevision != h.base {
		t.Fatalf("mismatched candidate advanced durable workspace head: workspace=%+v err=%v", workspace, err)
	}
}

func TestCandidateSealerReconcilesDispatchedNotAppliedBeforeExplicitRetry(t *testing.T) {
	h := newSealerHarness(t, true)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(h.root, "main.go"), []byte("package main\nfunc main() { println(2) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := h.sealer.changedPaths(ctx, h.task.ID, h.root)
	if err != nil {
		t.Fatal(err)
	}
	stateHash, err := workspaceStateHash(h.root, paths)
	if err != nil {
		t.Fatal(err)
	}
	message := "MAR candidate task=" + h.task.ID + " attempt=" + h.attempt.ID + " epoch=1"
	payload := sealIntentPayload{WorkspacePath: filepath.Clean(h.root), ExpectedHead: h.base, ChangedPaths: paths, WorkspaceStateHash: stateHash, CommitMessage: message}
	payloadJSON, _ := json.Marshal(payload)
	intent := domain.EffectIntent{OperationID: sealOperationID(h.task.ID, h.attempt.ID, h.attempt.RunEpoch), TaskID: h.task.ID, AttemptID: h.attempt.ID, RunEpoch: h.attempt.RunEpoch, Type: domain.EffectLocalObservable, ExpectedPrecondition: "workspace-head=" + h.base + ";state=" + stateHash, Payload: payloadJSON}
	mgr := effects.New(h.store)
	if _, decision, err := mgr.Plan(ctx, intent); err != nil || decision != effects.DecisionDispatch {
		t.Fatalf("pre-dispatch plan: %s %v", decision, err)
	}
	if _, err := mgr.AuthorizeDispatch(ctx, intent.OperationID, h.task.ID, h.attempt.ID, h.attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sealer.Seal(ctx, h.task.ID, h.attempt.ID, h.attempt.RunEpoch); !errors.Is(err, ErrSealReconciledNotApplied) {
		t.Fatalf("uncertain non-applied seal did not require explicit retry: %v", err)
	}
	if got := strings.TrimSpace(runVerificationGitOutput(t, h.root, "rev-parse", "HEAD")); got != h.base {
		t.Fatalf("reconciliation blindly dispatched commit: %s", got)
	}
	candidate, err := h.sealer.Seal(ctx, h.task.ID, h.attempt.ID, h.attempt.RunEpoch)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Revision == h.base {
		t.Fatal("explicit retry did not create candidate commit")
	}
}

func runVerificationGit(t *testing.T, root string, args ...string) {
	t.Helper()
	_ = runVerificationGitOutput(t, root, args...)
}

func runVerificationGitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}
