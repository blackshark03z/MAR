package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"mar/internal/contextengine"
	"mar/internal/domain"
)

const maxDecisionProjectionArtifacts = 16

// DecisionProjectionState derives the current cognition state from MAR durable
// truth. The returned value is ephemeral and read-only; SQLite remains the sole
// coordination authority.
func (s *TaskService) DecisionProjectionState(ctx context.Context, taskID, attemptID string, epoch int64) (contextengine.DecisionProjectionState, error) {
	if s == nil || s.store == nil || strings.TrimSpace(taskID) == "" || strings.TrimSpace(attemptID) == "" || epoch <= 0 {
		return contextengine.DecisionProjectionState{}, fmt.Errorf("decision projection identity is incomplete")
	}
	if err := s.store.ValidateAttemptAuthority(ctx, taskID, attemptID, epoch); err != nil {
		return contextengine.DecisionProjectionState{}, err
	}

	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return contextengine.DecisionProjectionState{}, err
	}
	goalHash, err := task.Contract.Hash()
	if err != nil {
		return contextengine.DecisionProjectionState{}, err
	}
	if task.ID != taskID || task.RunEpoch != epoch || !strings.EqualFold(task.ContractHash, goalHash) {
		return contextengine.DecisionProjectionState{}, fmt.Errorf("decision projection task identity is stale or inconsistent")
	}
	workspace, err := s.store.GetWorkspaceByTask(ctx, taskID)
	if err != nil {
		return contextengine.DecisionProjectionState{}, err
	}
	if workspace.TaskID != taskID || workspace.ProjectID != task.Contract.ProjectID || workspace.State != domain.WorkspaceReady || !strings.EqualFold(workspace.BaseRevision, task.Contract.BaseRevision) || strings.TrimSpace(workspace.HeadRevision) == "" {
		return contextengine.DecisionProjectionState{}, fmt.Errorf("decision projection workspace identity is stale or inconsistent")
	}
	attempt, ok, err := s.store.CurrentAttemptByTask(ctx, taskID)
	if err != nil {
		return contextengine.DecisionProjectionState{}, err
	}
	if !ok || attempt.ID != attemptID || attempt.RunEpoch != epoch || attempt.AuthorityState != domain.AttemptActive {
		return contextengine.DecisionProjectionState{}, fmt.Errorf("decision projection attempt is not current active authority")
	}

	state := contextengine.DecisionProjectionState{
		TaskID: task.ID, GoalHash: task.ContractHash, TaskState: task.State, ProjectID: task.Contract.ProjectID,
		WorkspaceID: workspace.ID, WorkspaceState: workspace.State, BaseRevision: task.Contract.BaseRevision, CurrentRevision: workspace.HeadRevision,
		AttemptID: attempt.ID, RunEpoch: attempt.RunEpoch, AttemptAuthority: attempt.AuthorityState, CreatedAt: task.UpdatedAt.UTC(),
	}
	advance := func(v time.Time) {
		if v = v.UTC(); v.After(state.CreatedAt) {
			state.CreatedAt = v
		}
	}
	advance(workspace.UpdatedAt)
	advance(attempt.StartedAt)

	controls := map[string]domain.TaskControl{}
	latest, hasLatest, err := s.store.LatestTaskControl(ctx, taskID)
	if err != nil {
		return contextengine.DecisionProjectionState{}, err
	}
	if hasLatest {
		if !latest.IntegrityValid() || latest.TaskID != taskID {
			return contextengine.DecisionProjectionState{}, fmt.Errorf("latest decision projection control is invalid")
		}
		state.LatestControlVersion = latest.Version
		controls[latest.ID] = latest
	}
	for _, kind := range []domain.ControlKind{domain.ControlSteer, domain.ControlInput, domain.ControlCancel} {
		c, available, getErr := s.store.LatestTaskControlByKind(ctx, taskID, kind)
		if getErr != nil {
			return contextengine.DecisionProjectionState{}, getErr
		}
		if !available {
			continue
		}
		if !c.IntegrityValid() || c.TaskID != taskID {
			return contextengine.DecisionProjectionState{}, fmt.Errorf("decision projection control is invalid")
		}
		controls[c.ID] = c
	}
	state.Controls = make([]domain.TaskControl, 0, len(controls))
	for _, c := range controls {
		state.Controls = append(state.Controls, c)
		advance(c.CreatedAt)
	}
	sort.Slice(state.Controls, func(i, j int) bool { return state.Controls[i].Version < state.Controls[j].Version })

	cp, hasCheckpoint, err := s.store.LatestValidCheckpoint(ctx, taskID)
	if err != nil {
		return contextengine.DecisionProjectionState{}, err
	}
	if hasCheckpoint {
		state.Checkpoint = &cp
		advance(cp.CreatedAt)
	}

	artifactByHandle := map[string]domain.ObservationArtifact{}
	if hasCheckpoint {
		for _, ref := range cp.Payload.CriticalEvidenceRefs {
			handle := strings.TrimSpace(ref)
			if !strings.HasPrefix(handle, "obs-") {
				continue
			}
			if _, seen := artifactByHandle[handle]; seen {
				continue
			}
			if len(artifactByHandle) >= maxDecisionProjectionArtifacts {
				return contextengine.DecisionProjectionState{}, fmt.Errorf("authority-critical observation artifact references exceed decision projection item bound")
			}
			a, getErr := s.store.GetObservationArtifact(ctx, handle)
			if getErr != nil {
				return contextengine.DecisionProjectionState{}, fmt.Errorf("load checkpoint observation artifact %s: %w", handle, getErr)
			}
			if a.TaskID != taskID || a.RunEpoch <= 0 || a.RunEpoch > epoch || (a.RunEpoch == epoch && a.AttemptID != attemptID) {
				return contextengine.DecisionProjectionState{}, fmt.Errorf("checkpoint observation artifact is stale or outside task authority")
			}
			artifactByHandle[handle] = a
			advance(a.CreatedAt)
		}
	}
	currentArtifacts, err := s.store.ListObservationArtifacts(ctx, taskID, attemptID, epoch, maxDecisionProjectionArtifacts)
	if err != nil {
		return contextengine.DecisionProjectionState{}, err
	}
	for _, a := range currentArtifacts {
		if len(artifactByHandle) >= maxDecisionProjectionArtifacts {
			break
		}
		if _, seen := artifactByHandle[a.Handle]; seen {
			continue
		}
		artifactByHandle[a.Handle] = a
		advance(a.CreatedAt)
	}
	state.Artifacts = make([]domain.ObservationArtifact, 0, len(artifactByHandle))
	for _, a := range artifactByHandle {
		state.Artifacts = append(state.Artifacts, a)
	}
	sort.Slice(state.Artifacts, func(i, j int) bool {
		if state.Artifacts[i].RunEpoch != state.Artifacts[j].RunEpoch {
			return state.Artifacts[i].RunEpoch < state.Artifacts[j].RunEpoch
		}
		return state.Artifacts[i].Handle < state.Artifacts[j].Handle
	})

	result, hasResult, err := s.store.LatestTaskResult(ctx, taskID)
	if err != nil {
		return contextengine.DecisionProjectionState{}, err
	}
	if hasResult {
		if result.TaskID != taskID || !strings.EqualFold(result.GoalHash, task.ContractHash) || !strings.EqualFold(result.BaseRevision, task.Contract.BaseRevision) || !result.IntegrityValid() {
			return contextengine.DecisionProjectionState{}, fmt.Errorf("decision projection task result identity is inconsistent")
		}
		state.Result = &contextengine.DecisionProjectionResult{ID: result.ID, Version: result.Version, FinalRevision: result.FinalRevision, EvidenceID: result.EvidenceID, IntegrationStatus: result.IntegrationStatus, WorkspaceDisposition: result.WorkspaceDisposition, Verdict: result.Verdict, IntegrityHash: result.IntegrityHash, CreatedAt: result.CreatedAt}
		advance(result.CreatedAt)
		evidence, getErr := s.store.GetVerificationEvidence(ctx, result.EvidenceID)
		if getErr != nil {
			return contextengine.DecisionProjectionState{}, getErr
		}
		if evidence.TaskID != taskID || !strings.EqualFold(evidence.GoalHash, task.ContractHash) || !strings.EqualFold(evidence.BaseRevision, task.Contract.BaseRevision) || !evidence.IntegrityValid() {
			return contextengine.DecisionProjectionState{}, fmt.Errorf("decision projection verification evidence identity is inconsistent")
		}
		state.Evidence = &contextengine.DecisionProjectionEvidence{
			ID: evidence.ID, AttemptID: evidence.AttemptID, RunEpoch: evidence.RunEpoch,
			CandidateRevision: evidence.CandidateRevision, ProfileID: evidence.ProfileID, ProfileHash: evidence.ProfileHash,
			EnvironmentHash: evidence.EnvironmentHash, Verdict: evidence.Verdict,
			FirstFailedCommand: firstFailedVerificationCommand(evidence),
			IntegrityHash:      evidence.IntegrityHash, CreatedAt: evidence.CreatedAt,
		}
		advance(evidence.CreatedAt)
	}
	if state.CreatedAt.IsZero() {
		return contextengine.DecisionProjectionState{}, fmt.Errorf("decision projection has no durable timestamp")
	}
	return state, nil
}

func firstFailedVerificationCommand(evidence domain.VerificationEvidence) *contextengine.DecisionProjectionCommandFailure {
	for i, command := range evidence.Commands {
		if command.Passed {
			continue
		}
		return &contextengine.DecisionProjectionCommandFailure{
			Index: i + 1, Name: command.Name, Args: append([]string(nil), command.Args...), Cwd: command.Cwd,
			ExitCode: command.ExitCode, DurationMS: command.DurationMS,
			OutputSHA256: command.OutputSHA256, OutputPrefix: command.OutputPrefix,
		}
	}
	return nil
}
