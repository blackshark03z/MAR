//go:build windows

package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"mar/internal/domain"
	"mar/internal/effects"
	"mar/internal/processctl"
	"mar/internal/store"
)

var ErrSealReconciledNotApplied = errors.New("candidate seal was reconciled as not applied; explicit retry is required")

type SealedCandidate struct {
	Revision     string   `json:"revision"`
	BaseRevision string   `json:"base_revision"`
	ChangedPaths []string `json:"changed_paths"`
}

type CandidateSealer struct {
	store   *store.SQLite
	effects *effects.Manager
	gitPath string
	now     func() time.Time
}

func NewCandidateSealer(s *store.SQLite, effectsManager *effects.Manager) (*CandidateSealer, error) {
	if s == nil || effectsManager == nil {
		return nil, errors.New("candidate sealer requires store and effect manager")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("Git executable is required for candidate sealing: %w", err)
	}
	return &CandidateSealer{store: s, effects: effectsManager, gitPath: gitPath, now: time.Now}, nil
}

type sealIntentPayload struct {
	WorkspacePath      string   `json:"workspace_path"`
	ExpectedHead       string   `json:"expected_head"`
	ChangedPaths       []string `json:"changed_paths"`
	WorkspaceStateHash string   `json:"workspace_state_hash"`
	CommitMessage      string   `json:"commit_message"`
}

type sealObserved struct {
	CandidateRevision string   `json:"candidate_revision"`
	ChangedPaths      []string `json:"changed_paths"`
}

func (s *CandidateSealer) Seal(ctx context.Context, taskID, attemptID string, epoch int64) (SealedCandidate, error) {
	if err := s.store.ValidateAttemptAuthority(ctx, taskID, attemptID, epoch); err != nil {
		return SealedCandidate{}, err
	}
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return SealedCandidate{}, err
	}
	if task.State != domain.TaskRunning {
		return SealedCandidate{}, store.ErrStateConflict
	}
	workspace, err := s.store.GetWorkspaceByTask(ctx, taskID)
	if err != nil {
		return SealedCandidate{}, err
	}
	if workspace.State != domain.WorkspaceReady {
		return SealedCandidate{}, store.ErrStateConflict
	}
	operationID := sealOperationID(taskID, attemptID, epoch)
	if existing, getErr := s.store.GetEffect(ctx, operationID); getErr == nil {
		return s.resumeExistingSeal(ctx, taskID, attemptID, epoch, workspace.BaseRevision, workspace.Path, existing)
	} else if !errors.Is(getErr, store.ErrNotFound) {
		return SealedCandidate{}, getErr
	}
	head, err := s.gitText(ctx, taskID, workspace.Path, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return SealedCandidate{}, err
	}
	if workspace.HeadRevision != "" && head != workspace.HeadRevision {
		return SealedCandidate{}, fmt.Errorf("workspace HEAD drift: durable=%s actual=%s", workspace.HeadRevision, head)
	}
	paths, err := s.changedPaths(ctx, taskID, workspace.Path)
	if err != nil {
		return SealedCandidate{}, err
	}
	if len(paths) == 0 {
		return SealedCandidate{Revision: head, BaseRevision: workspace.BaseRevision}, nil
	}
	if !task.Contract.Authority.LocalGitWrite {
		return SealedCandidate{}, errors.New("Goal Contract does not grant local Git write authority required to seal a changed candidate")
	}
	stateHash, err := workspaceStateHash(workspace.Path, paths)
	if err != nil {
		return SealedCandidate{}, err
	}
	message := fmt.Sprintf("MAR candidate task=%s attempt=%s epoch=%d", taskID, attemptID, epoch)
	payload := sealIntentPayload{WorkspacePath: filepath.Clean(workspace.Path), ExpectedHead: head, ChangedPaths: paths, WorkspaceStateHash: stateHash, CommitMessage: message}
	payloadJSON, _ := json.Marshal(payload)
	intent := domain.EffectIntent{
		OperationID:          sealOperationID(taskID, attemptID, epoch),
		TaskID:               taskID,
		AttemptID:            attemptID,
		RunEpoch:             epoch,
		Type:                 domain.EffectLocalObservable,
		ExpectedPrecondition: "workspace-head=" + head + ";state=" + stateHash,
		Payload:              payloadJSON,
	}
	_, decision, planErr := s.effects.Plan(ctx, intent)
	switch decision {
	case effects.DecisionAlreadyApplied:
		return s.reconcileApplied(ctx, taskID, attemptID, epoch, workspace.BaseRevision, payload, intent.OperationID)
	case effects.DecisionReconcile:
		return s.reconcileUncertain(ctx, taskID, attemptID, epoch, workspace.BaseRevision, payload, intent.OperationID)
	case effects.DecisionObservedNotApplied:
		if _, err := s.effects.RearmAfterNotApplied(ctx, intent.OperationID); err != nil {
			return SealedCandidate{}, err
		}
		_, decision, planErr = s.effects.Plan(ctx, intent)
		if planErr != nil || decision != effects.DecisionDispatch {
			return SealedCandidate{}, fmt.Errorf("rearmed candidate seal is not dispatchable: decision=%s err=%v", decision, planErr)
		}
	case effects.DecisionDispatch:
		if planErr != nil {
			return SealedCandidate{}, planErr
		}
	default:
		if planErr != nil {
			return SealedCandidate{}, planErr
		}
		return SealedCandidate{}, fmt.Errorf("unexpected candidate seal decision %q", decision)
	}

	return s.dispatchSeal(ctx, taskID, attemptID, epoch, workspace.BaseRevision, payload, intent.OperationID)
}

func (s *CandidateSealer) resumeExistingSeal(ctx context.Context, taskID, attemptID string, epoch int64, base, workspacePath string, record domain.EffectRecord) (SealedCandidate, error) {
	if record.Intent.TaskID != taskID || record.Intent.AttemptID != attemptID || record.Intent.RunEpoch != epoch || record.Intent.Type != domain.EffectLocalObservable {
		return SealedCandidate{}, errors.New("existing candidate-seal effect identity mismatch")
	}
	var payload sealIntentPayload
	if err := json.Unmarshal(record.Intent.Payload, &payload); err != nil {
		return SealedCandidate{}, fmt.Errorf("decode existing candidate-seal payload: %w", err)
	}
	if filepath.Clean(payload.WorkspacePath) != filepath.Clean(workspacePath) {
		return SealedCandidate{}, errors.New("existing candidate-seal workspace identity mismatch")
	}
	_, decision, planErr := s.effects.Plan(ctx, record.Intent)
	switch decision {
	case effects.DecisionAlreadyApplied:
		return s.reconcileApplied(ctx, taskID, attemptID, epoch, base, payload, record.Intent.OperationID)
	case effects.DecisionReconcile:
		return s.reconcileUncertain(ctx, taskID, attemptID, epoch, base, payload, record.Intent.OperationID)
	case effects.DecisionObservedNotApplied:
		if _, err := s.effects.RearmAfterNotApplied(ctx, record.Intent.OperationID); err != nil {
			return SealedCandidate{}, err
		}
		_, decision, planErr = s.effects.Plan(ctx, record.Intent)
		if planErr != nil || decision != effects.DecisionDispatch {
			return SealedCandidate{}, fmt.Errorf("rearmed existing candidate seal is not dispatchable: decision=%s err=%v", decision, planErr)
		}
	case effects.DecisionDispatch:
		if planErr != nil {
			return SealedCandidate{}, planErr
		}
	default:
		if planErr != nil {
			return SealedCandidate{}, planErr
		}
		return SealedCandidate{}, fmt.Errorf("unexpected existing candidate seal decision %q", decision)
	}
	return s.dispatchSeal(ctx, taskID, attemptID, epoch, base, payload, record.Intent.OperationID)
}

func (s *CandidateSealer) dispatchSeal(ctx context.Context, taskID, attemptID string, epoch int64, base string, payload sealIntentPayload, operationID string) (SealedCandidate, error) {
	if _, err := s.effects.AuthorizeDispatch(ctx, operationID, taskID, attemptID, epoch); err != nil {
		return SealedCandidate{}, err
	}
	currentHash, err := workspaceStateHash(payload.WorkspacePath, payload.ChangedPaths)
	if err != nil || currentHash != payload.WorkspaceStateHash {
		observation, _ := json.Marshal(map[string]any{"reason": "workspace changed after seal authorization", "actual_state_hash": currentHash})
		_, _ = s.effects.ObserveNotApplied(ctx, operationID, observation)
		if err != nil {
			return SealedCandidate{}, err
		}
		return SealedCandidate{}, ErrSealReconciledNotApplied
	}
	if _, err := s.git(ctx, taskID, payload.WorkspacePath, append([]string{"add", "-A", "--"}, payload.ChangedPaths...)...); err != nil {
		return s.reconcileAfterCommandError(ctx, taskID, attemptID, epoch, base, payload, operationID, err)
	}
	if _, err := s.git(ctx, taskID, payload.WorkspacePath,
		"-c", "user.name=MAR",
		"-c", "user.email=mar@local.invalid",
		"-c", "commit.gpgsign=false",
		"-c", "core.hooksPath=NUL",
		"commit", "--no-verify", "--no-gpg-sign", "-m", payload.CommitMessage,
	); err != nil {
		return s.reconcileAfterCommandError(ctx, taskID, attemptID, epoch, base, payload, operationID, err)
	}
	return s.reconcileApplied(ctx, taskID, attemptID, epoch, base, payload, operationID)
}

func (s *CandidateSealer) reconcileAfterCommandError(ctx context.Context, taskID, attemptID string, epoch int64, base string, payload sealIntentPayload, operationID string, commandErr error) (SealedCandidate, error) {
	candidate, applied, inspectErr := s.inspectSeal(ctx, taskID, payload)
	if inspectErr != nil {
		return SealedCandidate{}, fmt.Errorf("candidate seal command failed (%v) and reconciliation was inconclusive: %w", commandErr, inspectErr)
	}
	if applied {
		observed, _ := json.Marshal(sealObserved{CandidateRevision: candidate, ChangedPaths: payload.ChangedPaths})
		if _, err := s.effects.ObserveApplied(ctx, operationID, observed); err != nil {
			return SealedCandidate{}, err
		}
		if err := s.store.RecordWorkspaceHeadForAttempt(ctx, taskID, attemptID, epoch, payload.ExpectedHead, candidate, s.now().UTC()); err != nil {
			return SealedCandidate{}, err
		}
		return SealedCandidate{Revision: candidate, BaseRevision: base, ChangedPaths: append([]string(nil), payload.ChangedPaths...)}, nil
	}
	observation, _ := json.Marshal(map[string]any{"reason": "candidate commit not observed", "command_error": commandErr.Error()})
	if _, err := s.effects.ObserveNotApplied(ctx, operationID, observation); err != nil {
		return SealedCandidate{}, err
	}
	return SealedCandidate{}, fmt.Errorf("%w: %v", ErrSealReconciledNotApplied, commandErr)
}

func (s *CandidateSealer) reconcileUncertain(ctx context.Context, taskID, attemptID string, epoch int64, base string, payload sealIntentPayload, operationID string) (SealedCandidate, error) {
	candidate, applied, err := s.inspectSeal(ctx, taskID, payload)
	if err != nil {
		return SealedCandidate{}, err
	}
	if !applied {
		observation, _ := json.Marshal(map[string]any{"reason": "HEAD remains expected precondition"})
		if _, err := s.effects.ObserveNotApplied(ctx, operationID, observation); err != nil {
			return SealedCandidate{}, err
		}
		return SealedCandidate{}, ErrSealReconciledNotApplied
	}
	observed, _ := json.Marshal(sealObserved{CandidateRevision: candidate, ChangedPaths: payload.ChangedPaths})
	if _, err := s.effects.ObserveApplied(ctx, operationID, observed); err != nil {
		return SealedCandidate{}, err
	}
	if err := s.store.RecordWorkspaceHeadForAttempt(ctx, taskID, attemptID, epoch, payload.ExpectedHead, candidate, s.now().UTC()); err != nil {
		return SealedCandidate{}, err
	}
	return SealedCandidate{Revision: candidate, BaseRevision: base, ChangedPaths: append([]string(nil), payload.ChangedPaths...)}, nil
}

func (s *CandidateSealer) reconcileApplied(ctx context.Context, taskID, attemptID string, epoch int64, base string, payload sealIntentPayload, operationID string) (SealedCandidate, error) {
	candidate, applied, err := s.inspectSeal(ctx, taskID, payload)
	if err != nil {
		return SealedCandidate{}, err
	}
	if !applied {
		return SealedCandidate{}, errors.New("effect ledger says candidate seal applied but Git truth does not match")
	}
	record, err := s.store.GetEffect(ctx, operationID)
	if err != nil {
		return SealedCandidate{}, err
	}
	switch record.State {
	case domain.EffectDispatched:
		observed, _ := json.Marshal(sealObserved{CandidateRevision: candidate, ChangedPaths: payload.ChangedPaths})
		if _, err := s.effects.ObserveApplied(ctx, operationID, observed); err != nil {
			return SealedCandidate{}, err
		}
	case domain.EffectObserved:
		if record.ObservationOutcome != domain.OutcomeApplied {
			return SealedCandidate{}, errors.New("candidate seal is observed with a non-applied outcome")
		}
	default:
		return SealedCandidate{}, fmt.Errorf("candidate seal is not durably dispatched/applied: state=%s", record.State)
	}
	if err := s.store.RecordWorkspaceHeadForAttempt(ctx, taskID, attemptID, epoch, payload.ExpectedHead, candidate, s.now().UTC()); err != nil {
		return SealedCandidate{}, err
	}
	return SealedCandidate{Revision: candidate, BaseRevision: base, ChangedPaths: append([]string(nil), payload.ChangedPaths...)}, nil
}

func (s *CandidateSealer) inspectSeal(ctx context.Context, taskID string, payload sealIntentPayload) (string, bool, error) {
	head, err := s.gitText(ctx, taskID, payload.WorkspacePath, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", false, err
	}
	if head == payload.ExpectedHead {
		return "", false, nil
	}
	parent, err := s.gitText(ctx, taskID, payload.WorkspacePath, "rev-parse", "--verify", "HEAD^")
	if err != nil || parent != payload.ExpectedHead {
		return "", false, errors.New("workspace HEAD moved to an unexpected commit during candidate reconciliation")
	}
	message, err := s.gitText(ctx, taskID, payload.WorkspacePath, "log", "-1", "--format=%B")
	if err != nil || strings.TrimSpace(message) != payload.CommitMessage {
		return "", false, errors.New("candidate reconciliation found unexpected commit message")
	}
	changed, err := s.gitPaths(ctx, taskID, payload.WorkspacePath, "diff", "--name-only", "-z", payload.ExpectedHead+".."+head, "--")
	if err != nil {
		return "", false, err
	}
	if !equalStrings(changed, payload.ChangedPaths) {
		return "", false, errors.New("candidate reconciliation changed-path identity mismatch")
	}
	currentStateHash, err := workspaceStateHash(payload.WorkspacePath, payload.ChangedPaths)
	if err != nil {
		return "", false, err
	}
	if currentStateHash != payload.WorkspaceStateHash {
		return "", false, errors.New("candidate reconciliation workspace-state identity mismatch")
	}
	diffArgs := append([]string{"diff", "--name-only", "-z", head, "--"}, payload.ChangedPaths...)
	dirty, err := s.gitPaths(ctx, taskID, payload.WorkspacePath, diffArgs...)
	if err != nil {
		return "", false, err
	}
	if len(dirty) != 0 {
		return "", false, errors.New("candidate reconciliation commit does not match authorized workspace state")
	}
	return head, true, nil
}

func (s *CandidateSealer) changedPaths(ctx context.Context, taskID, root string) ([]string, error) {
	tracked, err := s.gitPaths(ctx, taskID, root, "diff", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	untracked, err := s.gitPaths(ctx, taskID, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(tracked)+len(untracked))
	out := make([]string, 0, len(tracked)+len(untracked))
	for _, path := range append(tracked, untracked...) {
		path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if path == "." || path == ".." || strings.HasPrefix(path, "../") || filepath.IsAbs(path) {
			return nil, fmt.Errorf("Git returned unsafe candidate path %q", path)
		}
		lower := strings.ToLower(path)
		if lower == ".mar" || strings.HasPrefix(lower, ".mar/") || lower == ".git" || strings.HasPrefix(lower, ".git/") {
			continue
		}
		for _, r := range path {
			if unicode.IsControl(r) {
				return nil, fmt.Errorf("candidate path contains control character: %q", path)
			}
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func workspaceStateHash(root string, paths []string) (string, error) {
	h := sha256.New()
	for _, rel := range paths {
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			_, _ = h.Write([]byte("DELETED\x00"))
			continue
		}
		if err != nil {
			return "", err
		}
		_, _ = h.Write([]byte(info.Mode().String()))
		_, _ = h.Write([]byte{0})
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return "", err
			}
			_, _ = h.Write([]byte("SYMLINK:" + target))
			continue
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("candidate path is not a regular file: %s", rel)
		}
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		fileHash := sha256.New()
		_, copyErr := io.Copy(fileHash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		_, _ = h.Write([]byte(hex.EncodeToString(fileHash.Sum(nil))))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *CandidateSealer) gitText(ctx context.Context, taskID, root string, args ...string) (string, error) {
	output, err := s.git(ctx, taskID, root, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (s *CandidateSealer) gitPaths(ctx context.Context, taskID, root string, args ...string) ([]string, error) {
	output, err := s.git(ctx, taskID, root, args...)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var paths []string
	for _, path := range strings.Split(output, "\x00") {
		if path == "" {
			continue
		}
		path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *CandidateSealer) git(ctx context.Context, taskID, root string, args ...string) (string, error) {
	env, err := candidateGitEnvironment(root, s.gitPath)
	if err != nil {
		return "", err
	}
	baseArgs := []string{
		"-c", "core.autocrlf=false",
		"-c", "core.eol=lf",
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=NUL",
		"-c", "core.excludesFile=NUL",
		"-c", "credential.helper=",
		"-c", "core.pager=cat",
		"-C", root,
	}
	baseArgs = append(baseArgs, args...)
	output, runErr := processctl.RunContainedCommand(ctx, processctl.CommandSpec{
		TaskID:         taskID,
		OperationID:    "candidate-git-" + shortSealHash(strings.Join(args, "\x00")),
		Path:           s.gitPath,
		Args:           baseArgs,
		Dir:            root,
		Env:            env,
		MaxOutputBytes: 256 << 10,
	})
	if runErr != nil {
		return output, fmt.Errorf("candidate Git %s failed: %w: %s", firstOr(args, "operation"), runErr, strings.TrimSpace(output))
	}
	return output, nil
}

func candidateGitEnvironment(root, gitPath string) ([]string, error) {
	systemRoot := strings.TrimSpace(os.Getenv("SystemRoot"))
	if systemRoot == "" {
		systemRoot = strings.TrimSpace(os.Getenv("WINDIR"))
	}
	if systemRoot == "" {
		return nil, errors.New("Windows SystemRoot is required for candidate Git")
	}
	profileRoot := filepath.Join(root, ".mar", "runtime", "candidate-git-profile")
	tempRoot := filepath.Join(root, ".mar", "runtime", "candidate-git-tmp")
	for _, dir := range []string{profileRoot, tempRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return []string{
		"SystemRoot=" + systemRoot,
		"WINDIR=" + systemRoot,
		"ComSpec=" + filepath.Join(systemRoot, "System32", "cmd.exe"),
		"PATH=" + filepath.Dir(gitPath) + string(os.PathListSeparator) + filepath.Join(systemRoot, "System32") + string(os.PathListSeparator) + systemRoot,
		"PATHEXT=.COM;.EXE;.BAT;.CMD",
		"USERPROFILE=" + profileRoot,
		"HOME=" + profileRoot,
		"TEMP=" + tempRoot,
		"TMP=" + tempRoot,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_PAGER=cat",
		"PAGER=cat",
	}, nil
}

func sealOperationID(taskID, attemptID string, epoch int64) string {
	return "candidate-seal-" + shortSealHash(taskID+"\x00"+attemptID+"\x00"+strconv.FormatInt(epoch, 10))
}

func shortSealHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func firstOr(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return values[0]
}
