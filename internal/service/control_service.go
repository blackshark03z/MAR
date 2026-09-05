package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"mar/internal/domain"
	"mar/internal/store"
)

const maxControlPayloadBytes = 8 << 10

var steerableStates = []domain.TaskState{
	domain.TaskRunning,
	domain.TaskVerifying,
	domain.TaskReviewing,
	domain.TaskReadyToIntegrate,
	domain.TaskInputRequired,
	domain.TaskBlocked,
	domain.TaskRetryWait,
}

type TaskStatusSnapshot struct {
	Task            domain.Task         `json:"task"`
	LatestControl   *domain.TaskControl `json:"latest_control,omitempty"`
	CancelRequested bool                `json:"cancel_requested"`
}

type TaskInspection struct {
	Task       domain.Task                  `json:"task"`
	Workspace  *domain.Workspace            `json:"workspace,omitempty"`
	Attempt    *domain.ExecutionAttempt     `json:"attempt,omitempty"`
	Checkpoint *domain.SemanticCheckpoint   `json:"checkpoint,omitempty"`
	Result     *domain.TaskResult           `json:"result,omitempty"`
	Evidence   *domain.VerificationEvidence `json:"verification_evidence,omitempty"`
	Controls   []domain.TaskControl         `json:"controls"`
}

func (s *TaskService) StatusSnapshot(ctx context.Context, taskID string) (TaskStatusSnapshot, error) {
	task, err := s.Status(ctx, taskID)
	if err != nil {
		return TaskStatusSnapshot{}, err
	}
	snapshot := TaskStatusSnapshot{Task: task}
	latest, ok, err := s.store.LatestTaskControl(ctx, taskID)
	if err != nil {
		return TaskStatusSnapshot{}, err
	}
	if ok {
		snapshot.LatestControl = &latest
	}
	_, snapshot.CancelRequested, err = s.store.LatestTaskControlByKind(ctx, taskID, domain.ControlCancel)
	if err != nil {
		return TaskStatusSnapshot{}, err
	}
	return snapshot, nil
}

func (s *TaskService) Steer(ctx context.Context, taskID, idempotencyKey string, payload domain.SteerPayload) (domain.TaskControl, bool, error) {
	if err := validateControlKey(taskID, idempotencyKey); err != nil {
		return domain.TaskControl{}, false, err
	}
	if err := payload.Validate(); err != nil {
		return domain.TaskControl{}, false, err
	}
	if payload.Kind == domain.SteerCancel {
		return s.Cancel(ctx, taskID, idempotencyKey, domain.CancelPayload{Reason: strings.TrimSpace(payload.Message)})
	}
	raw, err := boundedControlJSON(payload)
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	return s.store.PublishTaskControl(ctx, newID("control"), taskID, idempotencyKey, domain.ControlSteer, raw, steerableStates, s.now().UTC())
}

func (s *TaskService) Input(ctx context.Context, taskID, idempotencyKey string, payload domain.InputPayload) (domain.TaskControl, bool, error) {
	if err := validateControlKey(taskID, idempotencyKey); err != nil {
		return domain.TaskControl{}, false, err
	}
	if err := payload.Validate(); err != nil {
		return domain.TaskControl{}, false, err
	}
	raw, err := boundedControlJSON(payload)
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	return s.store.PublishTaskInput(ctx, newID("control"), taskID, idempotencyKey, raw, s.now().UTC())
}

func (s *TaskService) Cancel(ctx context.Context, taskID, idempotencyKey string, payload domain.CancelPayload) (domain.TaskControl, bool, error) {
	if err := validateControlKey(taskID, idempotencyKey); err != nil {
		return domain.TaskControl{}, false, err
	}
	payload.Reason = strings.TrimSpace(payload.Reason)
	raw, err := boundedControlJSON(payload)
	if err != nil {
		return domain.TaskControl{}, false, err
	}
	return s.store.RequestTaskCancellation(ctx, newID("control"), taskID, idempotencyKey, raw, s.now().UTC())
}

func (s *TaskService) FinalizeCancellation(ctx context.Context, taskID string) error {
	if strings.TrimSpace(taskID) == "" {
		return errors.New("task id is required")
	}
	return s.store.FinalizeTaskCancellation(ctx, taskID, s.now().UTC())
}

func (s *TaskService) Result(ctx context.Context, taskID string) (domain.TaskResult, bool, error) {
	if strings.TrimSpace(taskID) == "" {
		return domain.TaskResult{}, false, errors.New("task id is required")
	}
	return s.store.LatestTaskResult(ctx, taskID)
}

func (s *TaskService) Inspect(ctx context.Context, taskID string) (TaskInspection, error) {
	if strings.TrimSpace(taskID) == "" {
		return TaskInspection{}, errors.New("task id is required")
	}
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return TaskInspection{}, err
	}
	inspection := TaskInspection{Task: task, Controls: []domain.TaskControl{}}
	if workspace, err := s.store.GetWorkspaceByTask(ctx, taskID); err == nil {
		inspection.Workspace = &workspace
	} else if !errors.Is(err, store.ErrNotFound) {
		return TaskInspection{}, err
	}
	if attempt, ok, err := s.store.CurrentAttemptByTask(ctx, taskID); err != nil {
		return TaskInspection{}, err
	} else if ok {
		inspection.Attempt = &attempt
	}
	if checkpoint, ok, err := s.store.LatestValidCheckpoint(ctx, taskID); err != nil {
		return TaskInspection{}, err
	} else if ok {
		inspection.Checkpoint = &checkpoint
	}
	if result, ok, err := s.store.LatestTaskResult(ctx, taskID); err != nil {
		return TaskInspection{}, err
	} else if ok {
		inspection.Result = &result
		evidence, err := s.store.GetVerificationEvidence(ctx, result.EvidenceID)
		if err != nil {
			return TaskInspection{}, err
		}
		inspection.Evidence = &evidence
	}
	controls, err := s.store.ControlsSince(ctx, taskID, 0, 32)
	if err != nil {
		return TaskInspection{}, err
	}
	inspection.Controls = controls
	return inspection, nil
}

func (s *TaskService) ControlsSince(ctx context.Context, taskID string, afterVersion int64, limit int) ([]domain.TaskControl, error) {
	if strings.TrimSpace(taskID) == "" || afterVersion < 0 {
		return nil, errors.New("task id and non-negative control version are required")
	}
	return s.store.ControlsSince(ctx, taskID, afterVersion, limit)
}

func validateControlKey(taskID, idempotencyKey string) error {
	if strings.TrimSpace(taskID) == "" {
		return errors.New("task id is required")
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return errors.New("idempotency key is required")
	}
	return nil
}

func boundedControlJSON(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxControlPayloadBytes {
		return nil, fmt.Errorf("control payload exceeds %d bytes", maxControlPayloadBytes)
	}
	return raw, nil
}
