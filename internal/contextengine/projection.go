package contextengine

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"mar/internal/domain"
)

const DecisionProjectionVersion = 1

type DecisionProjectionConfig struct {
	MaxBytes             int
	MaxRecentItems       int
	MaxRecentBytes       int
	MaxArtifacts         int
	MaxRepositoryEntries int
}

type DecisionProjectionEvent struct {
	Role       string `json:"role"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Content    string `json:"content"`
}

type DecisionProjectionResult struct {
	ID                   string               `json:"id"`
	Version              int64                `json:"version"`
	FinalRevision        string               `json:"final_revision"`
	EvidenceID           string               `json:"evidence_id"`
	IntegrationStatus    string               `json:"integration_status"`
	WorkspaceDisposition string               `json:"workspace_disposition"`
	Verdict              domain.ResultVerdict `json:"verdict"`
	IntegrityHash        string               `json:"integrity_hash"`
	CreatedAt            time.Time            `json:"created_at"`
}
type DecisionProjectionCommandFailure struct {
	Index        int      `json:"index"`
	Name         string   `json:"name"`
	Args         []string `json:"args,omitempty"`
	Cwd          string   `json:"cwd,omitempty"`
	ExitCode     int      `json:"exit_code"`
	DurationMS   int64    `json:"duration_ms"`
	OutputSHA256 string   `json:"output_sha256"`
	OutputPrefix string   `json:"output_prefix,omitempty"`
}

type DecisionProjectionEvidence struct {
	ID                 string                            `json:"id"`
	AttemptID          string                            `json:"attempt_id"`
	RunEpoch           int64                             `json:"run_epoch"`
	CandidateRevision  string                            `json:"candidate_revision"`
	ProfileID          string                            `json:"profile_id"`
	ProfileHash        string                            `json:"profile_hash"`
	EnvironmentHash    string                            `json:"environment_hash"`
	Verdict            domain.VerificationVerdict        `json:"verdict"`
	FirstFailedCommand *DecisionProjectionCommandFailure `json:"first_failed_command,omitempty"`
	IntegrityHash      string                            `json:"integrity_hash"`
	CreatedAt          time.Time                         `json:"created_at"`
}

// DecisionProjectionState is an ephemeral snapshot assembled from MAR durable truth, never a second authority source.
type DecisionProjectionState struct {
	TaskID               string
	GoalHash             string
	TaskState            domain.TaskState
	ProjectID            string
	WorkspaceID          string
	WorkspaceState       domain.WorkspaceState
	BaseRevision         string
	CurrentRevision      string
	AttemptID            string
	RunEpoch             int64
	AttemptAuthority     domain.AttemptAuthorityState
	LatestControlVersion int64
	Controls             []domain.TaskControl
	Checkpoint           *domain.SemanticCheckpoint
	Result               *DecisionProjectionResult
	Evidence             *DecisionProjectionEvidence
	Artifacts            []domain.ObservationArtifact
	CreatedAt            time.Time
}

type DecisionProjectionInput struct {
	Contract   domain.GoalContract
	State      DecisionProjectionState
	Repository Pack
	Recent     []DecisionProjectionEvent
}

// DecisionProjection is bounded cognition input containing current identities and recoverability references, not an append-only transcript.
type DecisionProjection struct {
	Version              int                          `json:"version"`
	CreatedAt            time.Time                    `json:"created_at"`
	TaskID               string                       `json:"task_id"`
	GoalHash             string                       `json:"goal_hash"`
	Contract             domain.GoalContract          `json:"goal_contract"`
	TaskState            domain.TaskState             `json:"task_state"`
	ProjectID            string                       `json:"project_id"`
	WorkspaceID          string                       `json:"workspace_id"`
	WorkspaceState       domain.WorkspaceState        `json:"workspace_state"`
	BaseRevision         string                       `json:"base_revision"`
	CurrentRevision      string                       `json:"current_revision"`
	AttemptID            string                       `json:"attempt_id"`
	RunEpoch             int64                        `json:"run_epoch"`
	AttemptAuthority     domain.AttemptAuthorityState `json:"attempt_authority"`
	LatestControlVersion int64                        `json:"latest_control_version"`
	Controls             []domain.TaskControl         `json:"latest_controls"`
	Checkpoint           *domain.SemanticCheckpoint   `json:"latest_checkpoint,omitempty"`
	Result               *DecisionProjectionResult    `json:"result_identity,omitempty"`
	Evidence             *DecisionProjectionEvidence  `json:"verification_identity,omitempty"`
	Artifacts            []domain.ObservationArtifact `json:"observation_artifacts,omitempty"`
	Repository           Pack                         `json:"untrusted_repository_context_json"`
	Recent               []DecisionProjectionEvent    `json:"recent_protocol_evidence,omitempty"`
	Truncated            bool                         `json:"truncated"`
	Bytes                int                          `json:"bytes"`
}

func BuildDecisionProjection(in DecisionProjectionInput, cfg DecisionProjectionConfig) (DecisionProjection, error) {
	cfg = decisionProjectionDefaults(cfg)
	if cfg.MaxBytes < 2048 {
		return DecisionProjection{}, errors.New("decision projection byte bound is too small")
	}
	hash, err := in.Contract.Hash()
	if err != nil {
		return DecisionProjection{}, err
	}
	s := in.State
	if s.TaskID == "" || s.AttemptID == "" || s.RunEpoch <= 0 || s.CreatedAt.IsZero() || s.WorkspaceID == "" || s.CurrentRevision == "" {
		return DecisionProjection{}, errors.New("decision projection authoritative identity is incomplete")
	}
	if !strings.EqualFold(s.GoalHash, hash) || !strings.EqualFold(s.BaseRevision, in.Contract.BaseRevision) || s.ProjectID != in.Contract.ProjectID || s.AttemptAuthority != domain.AttemptActive {
		return DecisionProjection{}, errors.New("decision projection authoritative identity mismatch")
	}
	if !strings.EqualFold(in.Repository.Revision, s.CurrentRevision) || !strings.EqualFold(in.Repository.GoalHash, hash) {
		return DecisionProjection{}, errors.New("decision projection repository identity mismatch")
	}
	controls := append([]domain.TaskControl(nil), s.Controls...)
	sort.Slice(controls, func(i, j int) bool { return controls[i].Version < controls[j].Version })
	last := int64(0)
	for _, c := range controls {
		if c.TaskID != s.TaskID || !c.IntegrityValid() || c.Version <= last || c.Version > s.LatestControlVersion {
			return DecisionProjection{}, errors.New("decision projection control identity/integrity mismatch")
		}
		last = c.Version
	}
	if s.LatestControlVersion > 0 && (len(controls) == 0 || last != s.LatestControlVersion) {
		return DecisionProjection{}, errors.New("decision projection cannot omit latest durable control")
	}
	if s.LatestControlVersion == 0 && len(controls) != 0 {
		return DecisionProjection{}, errors.New("decision projection control version is inconsistent")
	}
	if s.Checkpoint != nil {
		c := s.Checkpoint
		if c.TaskID != s.TaskID || c.GoalHash != hash || c.BaseRevision != in.Contract.BaseRevision || c.RunEpoch > s.RunEpoch || !c.IntegrityValid() {
			return DecisionProjection{}, errors.New("decision projection checkpoint mismatch")
		}
	}
	if s.Result != nil && (s.Result.ID == "" || s.Result.FinalRevision == "" || s.Result.EvidenceID == "" || s.Result.IntegrityHash == "") {
		return DecisionProjection{}, errors.New("decision projection result identity incomplete")
	}
	if s.Evidence != nil && (s.Result == nil || s.Evidence.ID != s.Result.EvidenceID || s.Evidence.CandidateRevision == "" || s.Evidence.IntegrityHash == "") {
		return DecisionProjection{}, errors.New("decision projection evidence mismatch")
	}
	if s.Evidence != nil && s.Evidence.FirstFailedCommand != nil {
		failure := s.Evidence.FirstFailedCommand
		if s.Evidence.Verdict != domain.VerificationFail || failure.Index <= 0 || strings.TrimSpace(failure.Name) == "" || strings.TrimSpace(failure.OutputSHA256) == "" || failure.DurationMS < 0 {
			return DecisionProjection{}, errors.New("decision projection first failed command is invalid")
		}
	}
	repo := cloneDecisionPack(in.Repository)
	truncated := repo.Truncated
	if len(repo.Entries) > cfg.MaxRepositoryEntries {
		repo.Entries = repo.Entries[:cfg.MaxRepositoryEntries]
		repo.Truncated = true
		truncated = true
	}
	repo.Bytes = len(repo.Render())
	criticalArtifactHandles := map[string]struct{}{}
	if s.Checkpoint != nil {
		for _, ref := range s.Checkpoint.Payload.CriticalEvidenceRefs {
			ref = strings.TrimSpace(ref)
			if strings.HasPrefix(ref, "obs-") {
				criticalArtifactHandles[ref] = struct{}{}
			}
		}
	}
	criticalHandles := make([]string, 0, len(criticalArtifactHandles))
	for handle := range criticalArtifactHandles {
		criticalHandles = append(criticalHandles, handle)
	}
	sort.Strings(criticalHandles)
	if len(criticalHandles) > cfg.MaxArtifacts {
		return DecisionProjection{}, errors.New("authority-critical observation artifacts exceed decision projection item bound")
	}
	artifactByHandle := make(map[string]domain.ObservationArtifact, len(s.Artifacts))
	for _, a := range s.Artifacts {
		if a.TaskID != s.TaskID || a.RunEpoch <= 0 || a.RunEpoch > s.RunEpoch || !validDecisionArtifact(a) {
			return DecisionProjection{}, errors.New("decision projection artifact mismatch")
		}
		if a.RunEpoch == s.RunEpoch {
			if a.AttemptID != s.AttemptID {
				return DecisionProjection{}, errors.New("decision projection current artifact attempt mismatch")
			}
		} else if _, critical := criticalArtifactHandles[a.Handle]; !critical {
			return DecisionProjection{}, errors.New("decision projection historical artifact is not checkpoint-referenced")
		}
		if _, duplicate := artifactByHandle[a.Handle]; duplicate {
			return DecisionProjection{}, errors.New("decision projection contains duplicate observation artifact handle")
		}
		artifactByHandle[a.Handle] = a
	}
	arts := make([]domain.ObservationArtifact, 0, min(cfg.MaxArtifacts, len(artifactByHandle)))
	for _, handle := range criticalHandles {
		a, ok := artifactByHandle[handle]
		if !ok {
			return DecisionProjection{}, fmt.Errorf("authority-critical observation artifact %s is unavailable", handle)
		}
		arts = append(arts, a)
	}
	optionalHandles := make([]string, 0, len(artifactByHandle)-len(criticalHandles))
	for handle := range artifactByHandle {
		if _, critical := criticalArtifactHandles[handle]; !critical {
			optionalHandles = append(optionalHandles, handle)
		}
	}
	sort.Strings(optionalHandles)
	for _, handle := range optionalHandles {
		if len(arts) >= cfg.MaxArtifacts {
			truncated = true
			break
		}
		arts = append(arts, artifactByHandle[handle])
	}
	recent := boundDecisionRecent(in.Recent, cfg.MaxRecentItems, cfg.MaxRecentBytes)
	if len(recent) < len(in.Recent) {
		truncated = true
	}
	p := DecisionProjection{Version: DecisionProjectionVersion, CreatedAt: s.CreatedAt.UTC(), TaskID: s.TaskID, GoalHash: hash, Contract: in.Contract, TaskState: s.TaskState, ProjectID: s.ProjectID, WorkspaceID: s.WorkspaceID, WorkspaceState: s.WorkspaceState, BaseRevision: s.BaseRevision, CurrentRevision: s.CurrentRevision, AttemptID: s.AttemptID, RunEpoch: s.RunEpoch, AttemptAuthority: s.AttemptAuthority, LatestControlVersion: s.LatestControlVersion, Controls: controls, Checkpoint: s.Checkpoint, Result: s.Result, Evidence: s.Evidence, Artifacts: arts, Repository: repo, Recent: recent, Truncated: truncated}
	for projectionSize(p) > cfg.MaxBytes && len(p.Recent) > 0 {
		p.Recent = p.Recent[1:]
		p.Truncated = true
	}
	for projectionSize(p) > cfg.MaxBytes && len(p.Repository.Entries) > 0 {
		p.Repository.Entries = p.Repository.Entries[:len(p.Repository.Entries)-1]
		p.Repository.Truncated = true
		p.Repository.Bytes = len(p.Repository.Render())
		p.Truncated = true
	}
	for projectionSize(p) > cfg.MaxBytes && len(p.Repository.Terms) > 0 {
		p.Repository.Terms = p.Repository.Terms[:len(p.Repository.Terms)-1]
		p.Repository.Truncated = true
		p.Repository.Bytes = len(p.Repository.Render())
		p.Truncated = true
	}
	for projectionSize(p) > cfg.MaxBytes && len(p.Artifacts) > len(criticalHandles) {
		p.Artifacts = p.Artifacts[:len(p.Artifacts)-1]
		p.Truncated = true
	}
	for i := 0; i < 4; i++ {
		n := projectionSize(p)
		if p.Bytes == n {
			break
		}
		p.Bytes = n
	}
	if n := projectionSize(p); n > cfg.MaxBytes {
		return DecisionProjection{}, fmt.Errorf("authority-critical decision projection exceeds bound: %d > %d bytes", n, cfg.MaxBytes)
	} else {
		p.Bytes = n
	}
	return p, nil
}

func decisionProjectionDefaults(c DecisionProjectionConfig) DecisionProjectionConfig {
	if c.MaxBytes <= 0 {
		c.MaxBytes = 192 << 10
	}
	if c.MaxRecentItems <= 0 {
		c.MaxRecentItems = 8
	}
	if c.MaxRecentBytes <= 0 {
		c.MaxRecentBytes = 24 << 10
	}
	if c.MaxArtifacts <= 0 {
		c.MaxArtifacts = 16
	}
	if c.MaxRepositoryEntries <= 0 {
		c.MaxRepositoryEntries = 12
	}
	return c
}
func cloneDecisionPack(in Pack) Pack {
	out := in
	out.Terms = append([]string(nil), in.Terms...)
	out.Entries = append([]Entry(nil), in.Entries...)
	for i := range out.Entries {
		out.Entries[i].Reasons = append([]string(nil), out.Entries[i].Reasons...)
	}
	return out
}
func boundDecisionRecent(in []DecisionProjectionEvent, maxItems, maxBytes int) []DecisionProjectionEvent {
	if maxItems <= 0 || maxBytes <= 0 || len(in) == 0 {
		return nil
	}
	start := 0
	if len(in) > maxItems {
		start = len(in) - maxItems
	}
	out := make([]DecisionProjectionEvent, 0, len(in)-start)
	remaining := maxBytes
	for _, e := range in[start:] {
		if remaining <= 0 {
			break
		}
		c := e
		if len(c.Content) > remaining {
			c.Content = c.Content[:remaining]
		}
		remaining -= len(c.Content)
		out = append(out, c)
	}
	return out
}
func validDecisionArtifact(a domain.ObservationArtifact) bool {
	return strings.HasPrefix(a.Handle, "obs-") && len(a.Handle) == 68 && len(strings.TrimSpace(a.SHA256)) == 64 && a.CapturedBytes >= 0 && a.SourceBytes >= a.CapturedBytes && a.Complete != a.Truncated
}
func projectionSize(p DecisionProjection) int {
	raw, err := json.Marshal(p)
	if err != nil {
		return int(^uint(0) >> 1)
	}
	return len(raw)
}
