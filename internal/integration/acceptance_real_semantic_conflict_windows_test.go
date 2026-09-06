//go:build windows

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/domain"
	"mar/internal/service"
	"mar/internal/store"
)

type t13StoreFreshGate struct{ store *store.SQLite }

func (g t13StoreFreshGate) LatestFreshResult(ctx context.Context, taskID string) (domain.TaskResult, bool, error) {
	return g.store.LatestTaskResult(ctx, taskID)
}

func TestAcceptanceT13RealSemanticConflictNeverSilentlyIntegratesSecondGoal(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.go"), []byte("package conflict\n\nconst Mode = \"v1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "api.go"), []byte("package conflict\n\nfunc APIExpectedMode() string { return Mode }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t13Git(t, root, "init", "-b", "main")
	t13Git(t, root, "config", "user.name", "MAR T13")
	t13Git(t, root, "config", "user.email", "mar-t13@local.invalid")
	t13Git(t, root, "config", "core.autocrlf", "false")
	t13Git(t, root, "add", "-A")
	t13Git(t, root, "commit", "-m", "baseline")
	base := strings.TrimSpace(t13GitOut(t, root, "rev-parse", "HEAD"))

	worktreeA := filepath.Join(t.TempDir(), "task-a")
	worktreeB := filepath.Join(t.TempDir(), "task-b")
	t13Git(t, root, "worktree", "add", "--detach", worktreeA, base)
	t13Git(t, root, "worktree", "add", "--detach", worktreeB, base)
	defer func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", worktreeA).Run()
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", worktreeB).Run()
	}()

	if err := os.WriteFile(filepath.Join(worktreeA, "config.go"), []byte("package conflict\n\nconst Mode = \"v2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t13Git(t, worktreeA, "add", "config.go")
	t13Git(t, worktreeA, "commit", "-m", "migrate mode to v2")
	candidateA := strings.TrimSpace(t13GitOut(t, worktreeA, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(worktreeB, "api.go"), []byte("package conflict\n\nfunc APIExpectedMode() string { return \"v1\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t13Git(t, worktreeB, "add", "api.go")
	t13Git(t, worktreeB, "commit", "-m", "freeze API assumption at v1")
	candidateB := strings.TrimSpace(t13GitOut(t, worktreeB, "rev-parse", "HEAD"))

	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.NewTaskService(db)
	if _, _, err := svc.RegisterProject(ctx, "t13-project", root); err != nil {
		t.Fatal(err)
	}
	taskA := prepareT13VerifiedCandidate(t, db, svc, "t13-project", "t13-a", worktreeA, base, candidateA, "Migrate the architecture mode to v2.", "Mode is v2", "config.go")
	taskB := prepareT13VerifiedCandidate(t, db, svc, "t13-project", "t13-b", worktreeB, base, candidateB, "Keep API architecture semantics fixed at v1.", "API remains v1", "api.go")

	manager, err := NewManager(db, t13StoreFreshGate{store: db})
	if err != nil {
		t.Fatal(err)
	}
	_, integratedA, err := manager.Integrate(ctx, taskA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if integratedA.IntegrationStatus != "INTEGRATED" {
		t.Fatalf("first semantic candidate did not integrate: %+v", integratedA)
	}
	if got := strings.TrimSpace(t13GitOut(t, root, "rev-parse", "HEAD")); got != candidateA {
		t.Fatalf("authoritative head did not advance to first candidate: got=%s want=%s", got, candidateA)
	}

	_, blockedB, err := manager.Integrate(ctx, taskB.ID)
	if !errors.Is(err, ErrHeadDrift) {
		t.Fatalf("second incompatible goal was not surfaced as authoritative drift: %v", err)
	}
	if blockedB.IntegrationStatus != "BLOCKED" || len(blockedB.UnresolvedRisks) == 0 {
		t.Fatalf("second incompatible goal did not publish explicit blocked evidence: %+v", blockedB)
	}
	stateA, err := svc.Status(ctx, taskA.ID)
	if err != nil {
		t.Fatal(err)
	}
	stateB, err := svc.Status(ctx, taskB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stateA.State != domain.TaskComplete || stateB.State != domain.TaskBlocked {
		t.Fatalf("semantic conflict states are incoherent: A=%s B=%s", stateA.State, stateB.State)
	}
	configBytes, err := os.ReadFile(filepath.Join(root, "config.go"))
	if err != nil {
		t.Fatal(err)
	}
	apiBytes, err := os.ReadFile(filepath.Join(root, "api.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configBytes), `Mode = "v2"`) {
		t.Fatalf("first candidate was not authoritative: %s", configBytes)
	}
	if strings.Contains(string(apiBytes), `return "v1"`) {
		t.Fatalf("second conflicting candidate silently reached authoritative project: %s", apiBytes)
	}
}

func prepareT13VerifiedCandidate(t *testing.T, db *store.SQLite, svc *service.TaskService, projectID, key, workspacePath, base, candidate, goal, criterion, changedArea string) domain.Task {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	contract := domain.GoalContract{Goal: goal, Acceptance: []string{criterion}, ProjectID: projectID, BaseRevision: base, Authority: domain.Authority{LocalFileWrite: true, LocalGitWrite: true}, VerificationProfile: "t13-profile", Priority: "P2"}
	task, _, err := svc.Submit(ctx, key, contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	workspace := domain.Workspace{ID: "ws-" + key, TaskID: task.ID, ProjectID: projectID, Path: workspacePath, BaseRevision: base, CreatedAt: now, UpdatedAt: now}
	workspace, _, err = db.BeginWorkspace(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkWorkspaceReady(ctx, workspace.ID, task.ID, base, now); err != nil {
		t.Fatal(err)
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "t13-worker", "t13-daemon", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RecordWorkspaceHeadForAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, base, candidate, now); err != nil {
		t.Fatal(err)
	}
	if err := svc.TransitionForAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch, domain.TaskVerifying); err != nil {
		t.Fatal(err)
	}

	environment := json.RawMessage(`{"schema":1,"scenario":"t13-real-semantic-conflict"}`)
	environmentDigest := sha256.Sum256(environment)
	outputDigest := sha256.Sum256([]byte("PASS"))
	evidence := domain.VerificationEvidence{
		ID:                "evidence-" + key,
		TaskID:            task.ID,
		AttemptID:         attempt.ID,
		RunEpoch:          attempt.RunEpoch,
		GoalHash:          task.ContractHash,
		BaseRevision:      base,
		CandidateRevision: candidate,
		ProfileID:         contract.VerificationProfile,
		ProfileHash:       "t13-profile-hash",
		EnvironmentJSON:   environment,
		EnvironmentHash:   hex.EncodeToString(environmentDigest[:]),
		Commands:          []domain.VerificationCommandEvidence{{Name: "go", Args: []string{"test", "./..."}, ExitCode: 0, Passed: true, DurationMS: 1, OutputSHA256: hex.EncodeToString(outputDigest[:])}},
		Acceptance:        []domain.AcceptanceEvidence{{Criterion: criterion, Passed: true, EvidenceRefs: []string{"command:1"}}},
		Verdict:           domain.VerificationPass,
		CreatedAt:         now,
	}
	evidence.IntegrityHash, err = evidence.IntegrityDigest()
	if err != nil {
		t.Fatal(err)
	}
	result := domain.TaskResult{
		ID:                   "result-" + key,
		TaskID:               task.ID,
		GoalHash:             task.ContractHash,
		BaseRevision:         base,
		FinalRevision:        candidate,
		ChangedAreas:         []string{changedArea},
		EvidenceID:           evidence.ID,
		VerificationExecuted: []string{"go test ./..."},
		PassFailEvidence:     []string{"command:1:PASS"},
		UnresolvedRisks:      []string{},
		IntegrationStatus:    "NOT_INTEGRATED",
		WorkspaceDisposition: "RETAINED",
		Verdict:              domain.ResultVerified,
		CreatedAt:            now,
	}
	if _, err := db.PersistVerificationOutcome(ctx, evidence, result, now); err != nil {
		t.Fatal(err)
	}
	if err := db.ConfirmAttemptTerminated(ctx, task.ID, attempt.ID, attempt.RunEpoch, "t13-worker-exited", now); err != nil {
		t.Fatal(err)
	}
	return task
}

func t13Git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func t13GitOut(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
