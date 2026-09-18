package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type VerificationVerdict string

const (
	VerificationPass       VerificationVerdict = "PASS"
	VerificationFail       VerificationVerdict = "FAIL"
	VerificationUnverified VerificationVerdict = "UNVERIFIED"
)

type AcceptanceStatus string

const (
	AcceptancePass       AcceptanceStatus = "PASS"
	AcceptanceFail       AcceptanceStatus = "FAIL"
	AcceptanceUnverified AcceptanceStatus = "UNVERIFIED"
)

type VerificationCommandEvidence struct {
	Name         string   `json:"name"`
	Args         []string `json:"args"`
	Cwd          string   `json:"cwd,omitempty"`
	ExitCode     int      `json:"exit_code"`
	Passed       bool     `json:"passed"`
	DurationMS   int64    `json:"duration_ms"`
	OutputSHA256 string   `json:"output_sha256"`
	OutputPrefix string   `json:"output_prefix,omitempty"`
}

type AcceptanceEvidence struct {
	Criterion    string           `json:"criterion"`
	Passed       bool             `json:"passed"` // legacy compatibility; Status is authoritative when present.
	Status       AcceptanceStatus `json:"status,omitempty"`
	Scenario     string           `json:"scenario,omitempty"`
	Oracle       string           `json:"oracle,omitempty"`
	Observation  string           `json:"observation,omitempty"`
	EvidenceRefs []string         `json:"evidence_refs"`
}

func (e AcceptanceEvidence) EffectiveStatus() AcceptanceStatus {
	if e.Status != "" {
		return e.Status
	}
	if e.Passed {
		return AcceptancePass
	}
	return AcceptanceFail
}

type VerificationEvidence struct {
	ID                string                        `json:"id"`
	TaskID            string                        `json:"task_id"`
	AttemptID         string                        `json:"attempt_id"`
	RunEpoch          int64                         `json:"run_epoch"`
	GoalHash          string                        `json:"goal_hash"`
	BaseRevision      string                        `json:"base_revision"`
	CandidateRevision string                        `json:"candidate_revision"`
	ProfileID         string                        `json:"profile_id"`
	ProfileHash       string                        `json:"profile_hash"`
	EnvironmentJSON   json.RawMessage               `json:"environment_json"`
	EnvironmentHash   string                        `json:"environment_hash"`
	Commands          []VerificationCommandEvidence `json:"commands"`
	Acceptance        []AcceptanceEvidence          `json:"acceptance"`
	Verdict           VerificationVerdict           `json:"verdict"`
	IntegrityHash     string                        `json:"integrity_hash"`
	CreatedAt         time.Time                     `json:"created_at"`
}

func (e VerificationEvidence) ValidateIdentity() error {
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.TaskID) == "" || strings.TrimSpace(e.AttemptID) == "" {
		return errors.New("verification evidence id, task_id and attempt_id are required")
	}
	if e.RunEpoch <= 0 {
		return errors.New("verification evidence run_epoch must be positive")
	}
	for _, value := range []string{e.GoalHash, e.BaseRevision, e.CandidateRevision, e.ProfileID, e.ProfileHash, e.EnvironmentHash} {
		if strings.TrimSpace(value) == "" {
			return errors.New("verification evidence identity fields are required")
		}
	}
	if len(e.EnvironmentJSON) == 0 || !json.Valid(e.EnvironmentJSON) {
		return errors.New("verification environment_json must be valid JSON")
	}
	environmentDigest := sha256.Sum256(e.EnvironmentJSON)
	if !strings.EqualFold(e.EnvironmentHash, hex.EncodeToString(environmentDigest[:])) {
		return errors.New("verification environment hash does not match environment_json")
	}
	if len(e.Acceptance) == 0 {
		return errors.New("verification evidence requires acceptance evaluation")
	}
	allCommandsPassed := true
	allAcceptancePassed := true
	acceptanceUnverified := false
	acceptanceFailed := false
	for _, command := range e.Commands {
		if strings.TrimSpace(command.Name) == "" || command.DurationMS < 0 {
			return errors.New("verification command evidence is incomplete")
		}
		decoded, err := hex.DecodeString(strings.TrimSpace(command.OutputSHA256))
		if err != nil || len(decoded) != sha256.Size {
			return errors.New("verification command output_sha256 is invalid")
		}
		if command.Passed != (command.ExitCode == 0) {
			return errors.New("verification command pass flag and exit code disagree")
		}
		allCommandsPassed = allCommandsPassed && command.Passed
	}
	for _, criterion := range e.Acceptance {
		if strings.TrimSpace(criterion.Criterion) == "" {
			return errors.New("acceptance evidence criterion is required")
		}
		status := criterion.EffectiveStatus()
		if status != AcceptancePass && status != AcceptanceFail && status != AcceptanceUnverified {
			return errors.New("acceptance evidence status is invalid")
		}
		if status == AcceptanceUnverified {
			acceptanceUnverified = true
			allAcceptancePassed = false
			if len(criterion.EvidenceRefs) != 0 {
				return errors.New("unverified acceptance evidence must not claim evidence_refs")
			}
			continue
		}
		if status == AcceptanceFail {
			acceptanceFailed = true
		}
		if len(criterion.EvidenceRefs) == 0 {
			return errors.New("verified acceptance evidence requires evidence_refs")
		}
		if criterion.Status != "" && (strings.TrimSpace(criterion.Scenario) == "" || strings.TrimSpace(criterion.Oracle) == "" || strings.TrimSpace(criterion.Observation) == "") {
			return errors.New("criterion-specific acceptance evidence requires scenario, oracle and observation")
		}
		allAcceptancePassed = allAcceptancePassed && status == AcceptancePass
	}
	if e.Verdict != VerificationPass && e.Verdict != VerificationFail && e.Verdict != VerificationUnverified {
		return errors.New("verification evidence verdict must be PASS, FAIL, or UNVERIFIED")
	}
	switch e.Verdict {
	case VerificationPass:
		if !allCommandsPassed || !allAcceptancePassed {
			return errors.New("PASS verification verdict requires all command and acceptance evidence to pass")
		}
	case VerificationUnverified:
		if !allCommandsPassed || acceptanceFailed || !acceptanceUnverified {
			return errors.New("UNVERIFIED verdict requires passing commands and at least one unverified criterion without acceptance failure")
		}
	case VerificationFail:
		if allCommandsPassed && !acceptanceFailed {
			return errors.New("FAIL verification verdict requires at least one failed command or acceptance observation")
		}
	}
	if e.CreatedAt.IsZero() {
		return errors.New("verification evidence created_at is required")
	}
	return nil
}

func (e VerificationEvidence) IntegrityDigest() (string, error) {
	canonical := struct {
		ID                string                        `json:"id"`
		TaskID            string                        `json:"task_id"`
		AttemptID         string                        `json:"attempt_id"`
		RunEpoch          int64                         `json:"run_epoch"`
		GoalHash          string                        `json:"goal_hash"`
		BaseRevision      string                        `json:"base_revision"`
		CandidateRevision string                        `json:"candidate_revision"`
		ProfileID         string                        `json:"profile_id"`
		ProfileHash       string                        `json:"profile_hash"`
		EnvironmentJSON   json.RawMessage               `json:"environment_json"`
		EnvironmentHash   string                        `json:"environment_hash"`
		Commands          []VerificationCommandEvidence `json:"commands"`
		Acceptance        []AcceptanceEvidence          `json:"acceptance"`
		Verdict           VerificationVerdict           `json:"verdict"`
		CreatedAt         string                        `json:"created_at"`
	}{e.ID, e.TaskID, e.AttemptID, e.RunEpoch, e.GoalHash, e.BaseRevision, e.CandidateRevision, e.ProfileID, e.ProfileHash, e.EnvironmentJSON, e.EnvironmentHash, e.Commands, e.Acceptance, e.Verdict, e.CreatedAt.UTC().Format(time.RFC3339Nano)}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (e VerificationEvidence) IntegrityValid() bool {
	if err := e.ValidateIdentity(); err != nil {
		return false
	}
	want, err := e.IntegrityDigest()
	return err == nil && strings.EqualFold(want, strings.TrimSpace(e.IntegrityHash))
}

type ResultVerdict string

const (
	ResultVerified           ResultVerdict = "VERIFIED"
	ResultVerificationFailed ResultVerdict = "VERIFICATION_FAILED"
	ResultUnverified         ResultVerdict = "UNVERIFIED"
)

type ResourceSummary struct {
	AgentTurns        int   `json:"agent_turns,omitempty"`
	AgentToolCalls    int   `json:"agent_tool_calls,omitempty"`
	ModelInputTokens  int64 `json:"model_input_tokens,omitempty"`
	ModelOutputTokens int64 `json:"model_output_tokens,omitempty"`
	ModelTotalTokens  int64 `json:"model_total_tokens,omitempty"`
}

type TaskResult struct {
	ID                   string          `json:"id"`
	TaskID               string          `json:"task_id"`
	Version              int64           `json:"version"`
	GoalHash             string          `json:"goal_hash"`
	BaseRevision         string          `json:"base_revision"`
	FinalRevision        string          `json:"final_revision"`
	ChangedAreas         []string        `json:"changed_areas"`
	EvidenceID           string          `json:"evidence_id"`
	VerificationExecuted []string        `json:"verification_executed"`
	PassFailEvidence     []string        `json:"pass_fail_evidence"`
	UnresolvedRisks      []string        `json:"unresolved_risks"`
	IntegrationStatus    string          `json:"integration_status"`
	WorkspaceDisposition string          `json:"workspace_disposition"`
	ResourceSummary      ResourceSummary `json:"resource_summary"`
	Verdict              ResultVerdict   `json:"verdict"`
	IntegrityHash        string          `json:"integrity_hash"`
	CreatedAt            time.Time       `json:"created_at"`
}

func (r TaskResult) ValidateIdentity() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.TaskID) == "" || r.Version <= 0 {
		return errors.New("task result id, task_id and positive version are required")
	}
	for _, value := range []string{r.GoalHash, r.BaseRevision, r.FinalRevision, r.EvidenceID, r.IntegrationStatus, r.WorkspaceDisposition} {
		if strings.TrimSpace(value) == "" {
			return errors.New("task result identity/status fields are required")
		}
	}
	if r.ChangedAreas == nil || r.UnresolvedRisks == nil {
		return errors.New("task result changed_areas and unresolved_risks must be explicit arrays")
	}
	if len(r.VerificationExecuted) == 0 || len(r.PassFailEvidence) == 0 {
		return errors.New("task result requires verification and pass/fail evidence")
	}
	if r.ResourceSummary.AgentTurns < 0 || r.ResourceSummary.AgentToolCalls < 0 || r.ResourceSummary.ModelInputTokens < 0 || r.ResourceSummary.ModelOutputTokens < 0 || r.ResourceSummary.ModelTotalTokens < 0 {
		return errors.New("task result resource summary cannot be negative")
	}
	if r.Verdict != ResultVerified && r.Verdict != ResultVerificationFailed && r.Verdict != ResultUnverified {
		return errors.New("task result verdict is invalid")
	}
	if r.CreatedAt.IsZero() {
		return errors.New("task result created_at is required")
	}
	return nil
}

func (r TaskResult) IntegrityDigest() (string, error) {
	canonical := struct {
		ID                   string          `json:"id"`
		TaskID               string          `json:"task_id"`
		Version              int64           `json:"version"`
		GoalHash             string          `json:"goal_hash"`
		BaseRevision         string          `json:"base_revision"`
		FinalRevision        string          `json:"final_revision"`
		ChangedAreas         []string        `json:"changed_areas"`
		EvidenceID           string          `json:"evidence_id"`
		VerificationExecuted []string        `json:"verification_executed"`
		PassFailEvidence     []string        `json:"pass_fail_evidence"`
		UnresolvedRisks      []string        `json:"unresolved_risks"`
		IntegrationStatus    string          `json:"integration_status"`
		WorkspaceDisposition string          `json:"workspace_disposition"`
		ResourceSummary      ResourceSummary `json:"resource_summary"`
		Verdict              ResultVerdict   `json:"verdict"`
		CreatedAt            string          `json:"created_at"`
	}{r.ID, r.TaskID, r.Version, r.GoalHash, r.BaseRevision, r.FinalRevision, r.ChangedAreas, r.EvidenceID, r.VerificationExecuted, r.PassFailEvidence, r.UnresolvedRisks, r.IntegrationStatus, r.WorkspaceDisposition, r.ResourceSummary, r.Verdict, r.CreatedAt.UTC().Format(time.RFC3339Nano)}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (r TaskResult) IntegrityValid() bool {
	if err := r.ValidateIdentity(); err != nil {
		return false
	}
	want, err := r.IntegrityDigest()
	return err == nil && strings.EqualFold(want, strings.TrimSpace(r.IntegrityHash))
}
