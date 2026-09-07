//go:build windows

package verification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"mar/internal/aci"
	"mar/internal/domain"
	"mar/internal/store"
)

var ErrVerificationStale = errors.New("verification identity became stale")

type CommandRuntime interface {
	Root() string
	RunCommand(context.Context, aci.Command) (aci.ExecResult, error)
	SelfHostingSafe() bool
}

type VerifyRequest struct {
	TaskID          string
	AttemptID       string
	RunEpoch        int64
	Runtime         CommandRuntime
	ResourceSummary domain.ResourceSummary
}

type environmentSnapshotFunc func(Profile) (json.RawMessage, string, error)

type Verifier struct {
	store       *store.SQLite
	sealer      *CandidateSealer
	profiles    *Registry
	now         func() time.Time
	environment environmentSnapshotFunc
}

func NewVerifier(s *store.SQLite, sealer *CandidateSealer, profiles *Registry) (*Verifier, error) {
	if s == nil || sealer == nil || profiles == nil {
		return nil, errors.New("verifier requires store, candidate sealer and profile registry")
	}
	return &Verifier{store: s, sealer: sealer, profiles: profiles, now: time.Now, environment: hostEnvironmentSnapshot}, nil
}

func (v *Verifier) Verify(ctx context.Context, req VerifyRequest) (domain.TaskResult, error) {
	if strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.AttemptID) == "" || req.RunEpoch <= 0 {
		return domain.TaskResult{}, errors.New("verification task_id, attempt_id and positive run_epoch are required")
	}
	if req.Runtime == nil || !req.Runtime.SelfHostingSafe() {
		return domain.TaskResult{}, errors.New("verification requires the self-hosting-safe Coding ACI runtime")
	}
	if err := v.store.ValidateAttemptAuthority(ctx, req.TaskID, req.AttemptID, req.RunEpoch); err != nil {
		return domain.TaskResult{}, err
	}
	task, err := v.store.GetTask(ctx, req.TaskID)
	if err != nil {
		return domain.TaskResult{}, err
	}
	if task.State != domain.TaskRunning {
		return domain.TaskResult{}, store.ErrStateConflict
	}
	goalHash, err := task.Contract.Hash()
	if err != nil {
		return domain.TaskResult{}, err
	}
	if goalHash != task.ContractHash {
		return domain.TaskResult{}, errors.New("durable Goal Contract hash mismatch")
	}
	profile, ok := v.profiles.Get(task.Contract.VerificationProfile)
	if !ok {
		return domain.TaskResult{}, fmt.Errorf("verification profile %q is not registered", task.Contract.VerificationProfile)
	}
	profileHash, err := profile.Hash()
	if err != nil {
		return domain.TaskResult{}, err
	}

	candidate, err := v.sealer.Seal(ctx, req.TaskID, req.AttemptID, req.RunEpoch)
	if err != nil {
		return domain.TaskResult{}, err
	}
	if err := profile.ValidateChangedPaths(candidate.ChangedPaths); err != nil {
		return domain.TaskResult{}, fmt.Errorf("verification profile admission rejected candidate: %w", err)
	}
	workspace, err := v.store.GetWorkspaceByTask(ctx, req.TaskID)
	if err != nil {
		return domain.TaskResult{}, err
	}
	if !sameVerificationPath(workspace.Path, req.Runtime.Root()) {
		return domain.TaskResult{}, errors.New("verification runtime root does not match durable task workspace")
	}
	if err := v.assertCandidateHead(ctx, req.TaskID, candidate.Revision, true); err != nil {
		return domain.TaskResult{}, err
	}
	if err := v.store.TransitionTaskForAttempt(ctx, req.TaskID, req.AttemptID, req.RunEpoch, domain.TaskRunning, domain.TaskVerifying, v.now().UTC()); err != nil {
		return domain.TaskResult{}, err
	}

	startEnvironmentJSON, startEnvironmentHash, err := v.environment(profile)
	if err != nil {
		_ = v.store.TransitionTaskForAttempt(ctx, req.TaskID, req.AttemptID, req.RunEpoch, domain.TaskVerifying, domain.TaskBlocked, v.now().UTC())
		return domain.TaskResult{}, fmt.Errorf("capture verification environment: %w", err)
	}

	commandEvidence := make([]domain.VerificationCommandEvidence, 0, len(profile.Commands))
	commandOutputs := make([]string, 0, len(profile.Commands))
	allCommandsPassed := true
	for _, command := range profile.Commands {
		if err := v.store.ValidateAttemptAuthority(ctx, req.TaskID, req.AttemptID, req.RunEpoch); err != nil {
			return domain.TaskResult{}, err
		}
		started := v.now()
		result, runErr := req.Runtime.RunCommand(ctx, aci.Command{Name: command.Name, Args: append([]string(nil), command.Args...), Cwd: command.Cwd})
		if runErr != nil && result.ExitCode == 0 {
			result.ExitCode = -1
		}
		duration := v.now().Sub(started)
		if duration < 0 {
			duration = 0
		}
		observed := result.Output
		if runErr != nil {
			if observed != "" && !strings.HasSuffix(observed, "\n") {
				observed += "\n"
			}
			observed += "[error] " + runErr.Error()
		}
		sum := sha256.Sum256([]byte(observed))
		passed := runErr == nil && result.ExitCode == 0
		if !passed {
			allCommandsPassed = false
		}
		commandOutputs = append(commandOutputs, observed)
		commandEvidence = append(commandEvidence, domain.VerificationCommandEvidence{
			Name:         strings.TrimSpace(command.Name),
			Args:         append([]string(nil), command.Args...),
			Cwd:          filepath.ToSlash(filepath.Clean(command.Cwd)),
			ExitCode:     result.ExitCode,
			Passed:       passed,
			DurationMS:   duration.Milliseconds(),
			OutputSHA256: hex.EncodeToString(sum[:]),
			OutputPrefix: boundVerificationText(observed, 4096),
		})
	}
	if err := v.store.ValidateAttemptAuthority(ctx, req.TaskID, req.AttemptID, req.RunEpoch); err != nil {
		return domain.TaskResult{}, err
	}

	endEnvironmentJSON, endEnvironmentHash, envErr := v.environment(profile)
	environmentStable := envErr == nil && startEnvironmentHash == endEnvironmentHash
	candidateStable := v.assertCandidateHead(ctx, req.TaskID, candidate.Revision, true) == nil

	checks := make(map[int]domain.AcceptanceCheck, len(task.Contract.AcceptanceChecks))
	for _, check := range task.Contract.AcceptanceChecks {
		checks[check.CriterionIndex] = check
	}
	acceptance := make([]domain.AcceptanceEvidence, 0, len(task.Contract.Acceptance))
	acceptanceUnverified := false
	acceptanceFailed := false
	for i, criterion := range task.Contract.Acceptance {
		criterion = strings.TrimSpace(criterion)
		check, ok := checks[i+1]
		if !ok {
			acceptanceUnverified = true
			acceptance = append(acceptance, domain.AcceptanceEvidence{Criterion: criterion, Status: domain.AcceptanceUnverified, EvidenceRefs: []string{}})
			continue
		}
		status, observation, refs := v.observeAcceptanceOracle(req.Runtime.Root(), check, commandEvidence, commandOutputs)
		switch status {
		case domain.AcceptanceFail:
			acceptanceFailed = true
		case domain.AcceptanceUnverified:
			acceptanceUnverified = true
		}
		acceptance = append(acceptance, domain.AcceptanceEvidence{
			Criterion:    criterion,
			Passed:       status == domain.AcceptancePass,
			Status:       status,
			Scenario:     strings.TrimSpace(check.Scenario),
			Oracle:       strings.TrimSpace(check.Oracle),
			Observation:  observation,
			EvidenceRefs: refs,
		})
	}

	environmentJSON := startEnvironmentJSON
	environmentHash := startEnvironmentHash
	risks := make([]string, 0, 3)
	if envErr != nil {
		risks = append(risks, "verification environment could not be re-identified after command execution: "+boundVerificationText(envErr.Error(), 1024))
	} else if !environmentStable {
		driftPayload, marshalErr := json.Marshal(struct {
			Stable bool            `json:"stable"`
			Start  json.RawMessage `json:"start"`
			End    json.RawMessage `json:"end"`
		}{Stable: false, Start: startEnvironmentJSON, End: endEnvironmentJSON})
		if marshalErr == nil {
			environmentJSON = driftPayload
			sum := sha256.Sum256(driftPayload)
			environmentHash = hex.EncodeToString(sum[:])
		}
		risks = append(risks, "verification environment/toolchain changed during verification")
	}
	if !candidateStable {
		risks = append(risks, "candidate workspace revision or tracked content drifted during verification")
	}
	if !allCommandsPassed {
		risks = append(risks, "one or more required verification commands failed")
	}
	if acceptanceUnverified {
		risks = append(risks, "one or more acceptance criteria lack a valid criterion-specific scenario/oracle observation")
	}
	if acceptanceFailed {
		risks = append(risks, "one or more criterion-specific acceptance observations failed")
	}
	if allCommandsPassed && !acceptanceFailed && (!environmentStable || !candidateStable) {
		for i := range acceptance {
			if acceptance[i].EffectiveStatus() == domain.AcceptancePass {
				acceptance[i].Passed = false
				acceptance[i].Status = domain.AcceptanceUnverified
				acceptance[i].Observation = "candidate or verification-environment identity drift invalidated the prior observation"
				acceptance[i].EvidenceRefs = []string{}
				acceptanceUnverified = true
			}
		}
	}

	verdict := domain.VerificationFail
	resultVerdict := domain.ResultVerificationFailed
	switch {
	case !allCommandsPassed || acceptanceFailed:
		// A concrete command/oracle observation failed.
	case !environmentStable || !candidateStable || acceptanceUnverified:
		verdict = domain.VerificationUnverified
		resultVerdict = domain.ResultUnverified
	default:
		verdict = domain.VerificationPass
		resultVerdict = domain.ResultVerified
	}
	createdAt := v.now().UTC()
	evidence := domain.VerificationEvidence{
		ID:                newVerificationID("evidence"),
		TaskID:            task.ID,
		AttemptID:         req.AttemptID,
		RunEpoch:          req.RunEpoch,
		GoalHash:          task.ContractHash,
		BaseRevision:      task.Contract.BaseRevision,
		CandidateRevision: candidate.Revision,
		ProfileID:         profile.ID,
		ProfileHash:       profileHash,
		EnvironmentJSON:   append(json.RawMessage(nil), environmentJSON...),
		EnvironmentHash:   environmentHash,
		Commands:          commandEvidence,
		Acceptance:        acceptance,
		Verdict:           verdict,
		CreatedAt:         createdAt,
	}
	evidence.IntegrityHash, err = evidence.IntegrityDigest()
	if err != nil {
		return domain.TaskResult{}, err
	}

	verificationExecuted := make([]string, 0, len(profile.Commands))
	passFailEvidence := make([]string, 0, len(commandEvidence)+len(acceptance)+1)
	passFailEvidence = append(passFailEvidence, "verification_evidence:"+evidence.ID)
	for i, command := range commandEvidence {
		status := "FAIL"
		if command.Passed {
			status = "PASS"
		}
		verificationExecuted = append(verificationExecuted, formatVerificationCommand(command.Name, command.Args))
		passFailEvidence = append(passFailEvidence, fmt.Sprintf("command:%d:%s:%s", i+1, status, command.OutputSHA256))
	}
	for i, criterion := range acceptance {
		status := string(criterion.EffectiveStatus())
		passFailEvidence = append(passFailEvidence, fmt.Sprintf("acceptance:%d:%s", i+1, status))
	}
	changed := append([]string{}, candidate.ChangedPaths...)
	result := domain.TaskResult{
		ID:                   newVerificationID("result"),
		TaskID:               task.ID,
		GoalHash:             task.ContractHash,
		BaseRevision:         task.Contract.BaseRevision,
		FinalRevision:        candidate.Revision,
		ChangedAreas:         changed,
		EvidenceID:           evidence.ID,
		VerificationExecuted: verificationExecuted,
		PassFailEvidence:     passFailEvidence,
		UnresolvedRisks:      append([]string{}, risks...),
		IntegrationStatus:    "NOT_INTEGRATED",
		WorkspaceDisposition: "RETAINED",
		ResourceSummary:      req.ResourceSummary,
		Verdict:              resultVerdict,
		CreatedAt:            createdAt,
	}
	persisted, err := v.store.PersistVerificationOutcome(ctx, evidence, result, v.now().UTC())
	if err != nil {
		return domain.TaskResult{}, err
	}
	return persisted, nil
}

// EvidenceFresh is the integration/reuse gate. Historical evidence remains
// durable, but it is not eligible after candidate/profile/environment drift.
func (v *Verifier) EvidenceFresh(ctx context.Context, evidenceID string) (bool, error) {
	evidence, err := v.store.GetVerificationEvidence(ctx, evidenceID)
	if err != nil {
		return false, err
	}
	if evidence.Verdict != domain.VerificationPass {
		return false, nil
	}
	task, err := v.store.GetTask(ctx, evidence.TaskID)
	if err != nil {
		return false, err
	}
	if task.ContractHash != evidence.GoalHash || task.Contract.BaseRevision != evidence.BaseRevision || task.Contract.VerificationProfile != evidence.ProfileID {
		return false, nil
	}
	profile, ok := v.profiles.Get(evidence.ProfileID)
	if !ok {
		return false, nil
	}
	profileHash, err := profile.Hash()
	if err != nil || profileHash != evidence.ProfileHash {
		return false, err
	}
	workspace, err := v.store.GetWorkspaceByTask(ctx, evidence.TaskID)
	if err != nil {
		return false, err
	}
	if workspace.HeadRevision != evidence.CandidateRevision {
		return false, nil
	}
	if err := v.assertCandidateHead(ctx, evidence.TaskID, evidence.CandidateRevision, true); err != nil {
		if errors.Is(err, ErrVerificationStale) {
			return false, nil
		}
		return false, err
	}
	_, environmentHash, err := v.environment(profile)
	if err != nil {
		return false, err
	}
	return environmentHash == evidence.EnvironmentHash, nil
}

func (v *Verifier) LatestFreshResult(ctx context.Context, taskID string) (domain.TaskResult, bool, error) {
	result, ok, err := v.store.LatestTaskResult(ctx, taskID)
	if err != nil || !ok {
		return domain.TaskResult{}, false, err
	}
	fresh, err := v.EvidenceFresh(ctx, result.EvidenceID)
	if err != nil || !fresh || result.Verdict != domain.ResultVerified {
		return domain.TaskResult{}, false, err
	}
	return result, true, nil
}

func (v *Verifier) assertCandidateHead(ctx context.Context, taskID, candidate string, requireClean bool) error {
	workspace, err := v.store.GetWorkspaceByTask(ctx, taskID)
	if err != nil {
		return err
	}
	if workspace.State != domain.WorkspaceReady || workspace.HeadRevision != candidate {
		return ErrVerificationStale
	}
	head, err := v.sealer.gitText(ctx, taskID, workspace.Path, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	if head != candidate {
		return ErrVerificationStale
	}
	tracked, err := v.sealer.gitPaths(ctx, taskID, workspace.Path, "diff", "--name-only", "-z", candidate, "--")
	if err != nil {
		return err
	}
	if len(tracked) != 0 {
		return ErrVerificationStale
	}
	if requireClean {
		changed, err := v.sealer.changedPaths(ctx, taskID, workspace.Path)
		if err != nil {
			return err
		}
		if len(changed) != 0 {
			return ErrVerificationStale
		}
	}
	return nil
}

type environmentCommandIdentity struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type environmentIdentity struct {
	Schema  int                          `json:"schema"`
	GOOS    string                       `json:"goos"`
	GOARCH  string                       `json:"goarch"`
	Sandbox string                       `json:"sandbox"`
	Tools   []environmentCommandIdentity `json:"tools"`
}

func hostEnvironmentSnapshot(profile Profile) (json.RawMessage, string, error) {
	seen := make(map[string]struct{})
	tools := make([]environmentCommandIdentity, 0, len(profile.Commands))
	for _, command := range profile.Commands {
		path, err := exec.LookPath(command.Name)
		if err != nil {
			return nil, "", fmt.Errorf("resolve verification tool %q: %w", command.Name, err)
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return nil, "", err
		}
		path = filepath.Clean(path)
		key := strings.ToLower(path)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		file, err := os.Open(path)
		if err != nil {
			return nil, "", err
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, file)
		closeErr := file.Close()
		if copyErr != nil {
			return nil, "", copyErr
		}
		if closeErr != nil {
			return nil, "", closeErr
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, "", err
		}
		tools = append(tools, environmentCommandIdentity{Name: filepath.Base(path), Path: filepath.ToSlash(path), SHA256: hex.EncodeToString(h.Sum(nil)), Size: info.Size()})
	}
	sort.Slice(tools, func(i, j int) bool { return strings.ToLower(tools[i].Path) < strings.ToLower(tools[j].Path) })
	identity := environmentIdentity{Schema: 1, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Sandbox: string(aci.IsolationEnforcedSandbox), Tools: tools}
	payload, err := json.Marshal(identity)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(payload)
	return payload, hex.EncodeToString(sum[:]), nil
}

func newVerificationID(prefix string) string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return prefix + "-" + hex.EncodeToString(buf)
}

func (v *Verifier) observeAcceptanceOracle(root string, check domain.AcceptanceCheck, commands []domain.VerificationCommandEvidence, outputs []string) (domain.AcceptanceStatus, string, []string) {
	oracle := strings.TrimSpace(check.Oracle)
	if strings.HasPrefix(oracle, "output_contains:") {
		literal := strings.TrimSpace(strings.TrimPrefix(oracle, "output_contains:"))
		if literal == "" || len(check.CommandIndexes) == 0 {
			return domain.AcceptanceUnverified, "output_contains oracle is incomplete", []string{}
		}
		matched := 0
		for _, commandIndex := range check.CommandIndexes {
			if commandIndex < 1 || commandIndex > len(commands) || commandIndex > len(outputs) {
				return domain.AcceptanceUnverified, "referenced verification command index is unavailable", []string{}
			}
			command := commands[commandIndex-1]
			if !command.Passed {
				return domain.AcceptanceFail, fmt.Sprintf("referenced command %d failed before the required output observation could be established", commandIndex), []string{fmt.Sprintf("command:%d:%s", commandIndex, command.OutputSHA256)}
			}
			if strings.Contains(outputs[commandIndex-1], literal) {
				matched = commandIndex
				break
			}
		}
		if matched == 0 {
			return domain.AcceptanceUnverified, "referenced commands passed but did not emit the required output observation", []string{}
		}
		return domain.AcceptancePass, fmt.Sprintf("required literal observed in command %d output", matched), []string{fmt.Sprintf("command:%d:%s", matched, commands[matched-1].OutputSHA256)}
	}
	if strings.HasPrefix(oracle, "file_contains:") {
		rest := strings.TrimPrefix(oracle, "file_contains:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return domain.AcceptanceUnverified, "file_contains oracle is incomplete", []string{}
		}
		rel := filepath.Clean(strings.TrimSpace(parts[0]))
		literal := strings.TrimSpace(parts[1])
		if rel == "." || rel == ".." || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || literal == "" {
			return domain.AcceptanceUnverified, "file_contains oracle path/literal is invalid", []string{}
		}
		full := filepath.Join(root, rel)
		relCheck, err := filepath.Rel(root, full)
		if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
			return domain.AcceptanceUnverified, "file_contains oracle escaped the verification root", []string{}
		}
		content, err := os.ReadFile(full)
		if err != nil {
			if os.IsNotExist(err) {
				return domain.AcceptanceFail, fmt.Sprintf("candidate file %q does not exist", filepath.ToSlash(rel)), []string{"file:" + filepath.ToSlash(rel) + ":missing"}
			}
			return domain.AcceptanceUnverified, "candidate file could not be observed: " + boundVerificationText(err.Error(), 512), []string{}
		}
		sum := sha256.Sum256(content)
		ref := "file:" + filepath.ToSlash(rel) + ":" + hex.EncodeToString(sum[:])
		if !strings.Contains(string(content), literal) {
			return domain.AcceptanceFail, fmt.Sprintf("candidate file %q does not contain the required literal", filepath.ToSlash(rel)), []string{ref}
		}
		return domain.AcceptancePass, fmt.Sprintf("required literal observed in candidate file %q", filepath.ToSlash(rel)), []string{ref}
	}
	return domain.AcceptanceUnverified, "oracle is not machine-verifiable by MAR V1", []string{}
}

func formatVerificationCommand(name string, args []string) string {
	parts := append([]string{strings.TrimSpace(name)}, args...)
	return strings.Join(parts, " ")
}

func boundVerificationText(value string, maxBytes int) string {
	if maxBytes <= 0 || value == "" {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes]
}

func sameVerificationPath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(absA), filepath.Clean(absB))
}
