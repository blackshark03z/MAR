package service

import (
	"context"
	"errors"
	"strings"

	"mar/internal/domain"
)

func (s *TaskService) RecordOwnerFeedback(ctx context.Context, taskID, idempotencyKey string, verdict domain.OwnerFeedbackVerdict, message string) (domain.OwnerFeedback, bool, error) {
	taskID = strings.TrimSpace(taskID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if taskID == "" || idempotencyKey == "" {
		return domain.OwnerFeedback{}, false, errors.New("task id and idempotency key are required")
	}
	result, available, err := s.Result(ctx, taskID)
	if err != nil {
		return domain.OwnerFeedback{}, false, err
	}
	if !available {
		return domain.OwnerFeedback{}, false, errors.New("owner feedback requires a durable task result")
	}
	if verdict == domain.OwnerFeedbackAccepted && (result.Verdict != domain.ResultVerified || result.IntegrationStatus != "INTEGRATED") {
		return domain.OwnerFeedback{}, false, errors.New("owner acceptance requires a verified integrated result")
	}
	feedback := domain.OwnerFeedback{
		ID:                newID("feedback"),
		IdempotencyKey:    idempotencyKey,
		TaskID:            taskID,
		ResultID:          result.ID,
		CandidateRevision: result.FinalRevision,
		Verdict:           verdict,
		Message:           strings.TrimSpace(message),
		CreatedAt:         s.now().UTC(),
	}
	if err := feedback.ValidateIdentity(); err != nil {
		return domain.OwnerFeedback{}, false, err
	}
	feedback.IntegrityHash, err = feedback.IntegrityDigest()
	if err != nil {
		return domain.OwnerFeedback{}, false, err
	}
	return s.store.RecordOwnerFeedback(ctx, feedback)
}
