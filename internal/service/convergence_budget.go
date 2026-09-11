package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mar/internal/contextengine"
	"mar/internal/domain"
	"mar/internal/store"
)

const (
	DefaultWebEpisodeMaxDecisions    int64 = 12
	DefaultWebEpisodeMaxControlCalls int64 = 40
	DefaultWebEpisodeMaxPayloadBytes int64 = 512 << 10
	DefaultTaskMaxModelDecisions     int64 = 24
	DefaultTaskMaxWorkerToolCalls    int64 = 64
	DefaultTaskMaxModelTotalTokens   int64 = 300000
	DefaultTaskMaxActiveExecution          = 30 * time.Minute
	DefaultTaskMaxAttempts           int64 = 3
	maxWebEpisodeReceiptBytes              = 8 << 10
)

var ErrWebEpisodeBudgetExhausted = errors.New("web cognition episode budget exhausted")

type WebEpisodeContinuationReceipt struct {
	TaskID            string `json:"task_id"`
	AttemptID         string `json:"attempt_id"`
	RunEpoch          int64  `json:"run_epoch"`
	GoalHash          string `json:"goal_hash"`
	CurrentRevision   string `json:"current_revision"`
	ProjectionVersion int    `json:"projection_version"`
	ProjectionBinding string `json:"projection_binding"`
	CheckpointID      string `json:"checkpoint_id,omitempty"`
	CheckpointVersion int64  `json:"checkpoint_version,omitempty"`
	ExhaustedReason   string `json:"exhausted_reason"`
	Decisions         int64  `json:"decisions"`
	ControlCalls      int64  `json:"control_calls"`
	PayloadBytes      int64  `json:"payload_bytes"`
	NextAction        string `json:"next_action"`
}

type WebEpisodeBudgetError struct{ Receipt WebEpisodeContinuationReceipt }

func (e *WebEpisodeBudgetError) Error() string {
	return fmt.Sprintf("%v: %s", ErrWebEpisodeBudgetExhausted, e.Receipt.ExhaustedReason)
}
func (e *WebEpisodeBudgetError) Unwrap() error { return ErrWebEpisodeBudgetExhausted }

type TaskConvergenceBudget struct {
	Usage                    store.TaskConvergenceUsage `json:"usage"`
	RemainingModelDecisions  int64                      `json:"remaining_model_decisions"`
	RemainingWorkerToolCalls int64                      `json:"remaining_worker_tool_calls"`
	RemainingModelTokens     int64                      `json:"remaining_model_total_tokens"`
	RemainingActiveExecution time.Duration              `json:"remaining_active_execution"`
	RemainingAttempts        int64                      `json:"remaining_attempts"`
	ExhaustedReason          string                     `json:"exhausted_reason,omitempty"`
	NoProgress               bool                       `json:"no_progress"`
}

func webEpisodeExhaustionReason(u store.WebEpisodeUsage, proposedCalls, proposedBytes int64) string {
	if u.Decisions >= DefaultWebEpisodeMaxDecisions {
		return "decision_limit"
	}
	if u.ControlCalls+proposedCalls > DefaultWebEpisodeMaxControlCalls {
		return "control_call_limit"
	}
	if u.PayloadBytes+proposedBytes > DefaultWebEpisodeMaxPayloadBytes {
		return "payload_byte_limit"
	}
	return ""
}

func (s *TaskService) CheckWebEpisodeAdmission(ctx context.Context, taskID, attemptID string, epoch, proposedCalls, proposedBytes int64) error {
	usage, err := s.store.WebEpisodeUsage(ctx, taskID, attemptID, epoch, true)
	if err != nil {
		return err
	}
	reason := webEpisodeExhaustionReason(usage, proposedCalls, proposedBytes)
	if reason == "" {
		return nil
	}
	receipt, err := s.webEpisodeContinuationReceipt(ctx, taskID, attemptID, epoch, usage, reason)
	if err != nil {
		return err
	}
	return &WebEpisodeBudgetError{Receipt: receipt}
}

func (s *TaskService) webEpisodeContinuationReceipt(ctx context.Context, taskID, attemptID string, epoch int64, usage store.WebEpisodeUsage, reason string) (WebEpisodeContinuationReceipt, error) {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return WebEpisodeContinuationReceipt{}, err
	}
	if task.RunEpoch != epoch {
		return WebEpisodeContinuationReceipt{}, store.ErrStaleAttempt
	}
	revision := strings.TrimSpace(task.Contract.BaseRevision)
	checkpointID := ""
	var checkpointVersion int64
	if cp, ok, err := s.store.LatestValidCheckpoint(ctx, taskID); err != nil {
		return WebEpisodeContinuationReceipt{}, err
	} else if ok {
		revision = strings.TrimSpace(cp.CurrentRevision)
		checkpointID, checkpointVersion = cp.ID, cp.Version
	}
	if revision == "" {
		return WebEpisodeContinuationReceipt{}, errors.New("episode continuation revision identity is unavailable")
	}
	bindingInput := fmt.Sprintf("%s|%s|%s|%d|%s|%s|%d|%d", task.ID, task.ContractHash, attemptID, epoch, revision, checkpointID, checkpointVersion, contextengine.DecisionProjectionVersion)
	sum := sha256.Sum256([]byte(bindingInput))
	receipt := WebEpisodeContinuationReceipt{
		TaskID: task.ID, AttemptID: attemptID, RunEpoch: epoch, GoalHash: task.ContractHash, CurrentRevision: revision,
		ProjectionVersion: contextengine.DecisionProjectionVersion, ProjectionBinding: hex.EncodeToString(sum[:]),
		CheckpointID: checkpointID, CheckpointVersion: checkpointVersion, ExhaustedReason: reason,
		Decisions: usage.Decisions, ControlCalls: usage.ControlCalls, PayloadBytes: usage.PayloadBytes,
		NextAction: "start a fresh Web cognition episode from MAR durable Decision Projection",
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return WebEpisodeContinuationReceipt{}, err
	}
	if len(raw) > maxWebEpisodeReceiptBytes {
		return WebEpisodeContinuationReceipt{}, errors.New("episode continuation receipt exceeds bounded size")
	}
	return receipt, nil
}

func (s *TaskService) TaskConvergenceBudget(ctx context.Context, taskID string) (TaskConvergenceBudget, error) {
	u, err := s.store.TaskConvergenceUsage(ctx, taskID, s.now().UTC())
	if err != nil {
		return TaskConvergenceBudget{}, err
	}
	b := TaskConvergenceBudget{Usage: u, RemainingModelDecisions: DefaultTaskMaxModelDecisions - u.ModelDecisions, RemainingWorkerToolCalls: DefaultTaskMaxWorkerToolCalls - u.WorkerToolCalls, RemainingModelTokens: DefaultTaskMaxModelTotalTokens - u.ModelTotalTokens, RemainingActiveExecution: DefaultTaskMaxActiveExecution - u.ActiveExecution, RemainingAttempts: DefaultTaskMaxAttempts - u.Attempts, NoProgress: u.NoProgressStreak >= 1}
	if b.RemainingModelDecisions < 0 {
		b.RemainingModelDecisions = 0
	}
	if b.RemainingWorkerToolCalls < 0 {
		b.RemainingWorkerToolCalls = 0
	}
	if b.RemainingModelTokens < 0 {
		b.RemainingModelTokens = 0
	}
	if b.RemainingActiveExecution < 0 {
		b.RemainingActiveExecution = 0
	}
	if b.RemainingAttempts < 0 {
		b.RemainingAttempts = 0
	}
	if b.RemainingAttempts == 0 {
		manualRetry, err := s.manualAttemptOverrideApproved(ctx, taskID)
		if err != nil {
			return TaskConvergenceBudget{}, err
		}
		if manualRetry {
			b.RemainingAttempts = 1
		}
	}
	switch {
	case b.NoProgress:
		b.ExhaustedReason = "no_progress"
	case b.RemainingModelDecisions == 0:
		b.ExhaustedReason = "model_decision_limit"
	case b.RemainingWorkerToolCalls == 0:
		b.ExhaustedReason = "worker_tool_call_limit"
	case b.RemainingModelTokens == 0:
		b.ExhaustedReason = "model_token_limit"
	case b.RemainingActiveExecution == 0:
		b.ExhaustedReason = "active_execution_limit"
	case b.RemainingAttempts == 0:
		b.ExhaustedReason = "attempt_limit"
	}
	return b, nil
}

func (s *TaskService) manualAttemptOverrideApproved(ctx context.Context, taskID string) (bool, error) {
	control, hasControl, err := s.store.LatestTaskControl(ctx, taskID)
	if err != nil {
		return false, err
	}
	attempt, hasAttempt, err := s.store.CurrentAttemptByTask(ctx, taskID)
	if err != nil {
		return false, err
	}
	return manualAttemptOverrideAllowed(control, hasControl, attempt, hasAttempt), nil
}

func manualAttemptOverrideAllowed(control domain.TaskControl, hasControl bool, attempt domain.ExecutionAttempt, hasAttempt bool) bool {
	if !hasControl || !hasAttempt || control.Kind != domain.ControlSteer || !control.IntegrityValid() || attempt.AuthorityState != domain.AttemptPhysicallyTerminated || attempt.TerminatedAt == nil {
		return false
	}
	var payload domain.SteerPayload
	if err := json.Unmarshal(control.Payload, &payload); err != nil || payload.Validate() != nil || payload.Kind != domain.SteerBlockedChoice {
		return false
	}
	return control.CreatedAt.After(attempt.TerminatedAt.UTC())
}

func (s *TaskService) BlockForConvergenceBudget(ctx context.Context, taskID string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	switch task.State {
	case domain.TaskWorkspaceReady:
		return s.store.OrchestratorTransition(ctx, taskID, domain.TaskWorkspaceReady, domain.TaskBlocked, s.now().UTC())
	case domain.TaskRetryWait:
		return s.store.OrchestratorTransition(ctx, taskID, domain.TaskRetryWait, domain.TaskBlocked, s.now().UTC())
	default:
		return store.ErrStateConflict
	}
}
