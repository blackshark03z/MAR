package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"mar/internal/domain"
)

func (s *SQLite) RecordOwnerFeedback(ctx context.Context, feedback domain.OwnerFeedback) (domain.OwnerFeedback, bool, error) {
	if err := feedback.ValidateIdentity(); err != nil || !feedback.IntegrityValid() {
		if err != nil {
			return domain.OwnerFeedback{}, false, err
		}
		return domain.OwnerFeedback{}, false, errors.New("owner feedback integrity is invalid")
	}
	res, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO owner_feedback(
 feedback_id, idempotency_key, task_id, result_id, candidate_revision, verdict, message, integrity_hash, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, feedback.ID, feedback.IdempotencyKey, feedback.TaskID, feedback.ResultID,
		feedback.CandidateRevision, string(feedback.Verdict), strings.TrimSpace(feedback.Message), feedback.IntegrityHash, feedback.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return domain.OwnerFeedback{}, false, fmt.Errorf("insert owner feedback: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 1 {
		return feedback, true, nil
	}
	existing, err := s.GetOwnerFeedbackByIdempotencyKey(ctx, feedback.IdempotencyKey)
	if err != nil {
		return domain.OwnerFeedback{}, false, err
	}
	if existing.TaskID != feedback.TaskID || existing.ResultID != feedback.ResultID || existing.CandidateRevision != feedback.CandidateRevision || existing.Verdict != feedback.Verdict || strings.TrimSpace(existing.Message) != strings.TrimSpace(feedback.Message) {
		return domain.OwnerFeedback{}, false, ErrIdempotencyConflict
	}
	return existing, false, nil
}

func (s *SQLite) GetOwnerFeedbackByIdempotencyKey(ctx context.Context, key string) (domain.OwnerFeedback, error) {
	return scanOwnerFeedback(s.db.QueryRowContext(ctx, `
SELECT feedback_id, idempotency_key, task_id, result_id, candidate_revision, verdict, message, integrity_hash, created_at
FROM owner_feedback WHERE idempotency_key = ?`, key))
}

func (s *SQLite) ListOwnerFeedbackByTask(ctx context.Context, taskID string, limit int) ([]domain.OwnerFeedback, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT feedback_id, idempotency_key, task_id, result_id, candidate_revision, verdict, message, integrity_hash, created_at
FROM owner_feedback WHERE task_id = ? ORDER BY created_at DESC, feedback_id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("list owner feedback: %w", err)
	}
	defer rows.Close()
	result := make([]domain.OwnerFeedback, 0)
	for rows.Next() {
		var feedback domain.OwnerFeedback
		var verdict, created string
		if err := rows.Scan(&feedback.ID, &feedback.IdempotencyKey, &feedback.TaskID, &feedback.ResultID, &feedback.CandidateRevision, &verdict, &feedback.Message, &feedback.IntegrityHash, &created); err != nil {
			return nil, err
		}
		feedback.Verdict = domain.OwnerFeedbackVerdict(verdict)
		feedback.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil || !feedback.IntegrityValid() {
			if err != nil {
				return nil, err
			}
			return nil, errors.New("owner feedback integrity is invalid")
		}
		result = append(result, feedback)
	}
	return result, rows.Err()
}

type ownerFeedbackRow interface {
	Scan(...any) error
}

func scanOwnerFeedback(row ownerFeedbackRow) (domain.OwnerFeedback, error) {
	var feedback domain.OwnerFeedback
	var verdict, created string
	if err := row.Scan(&feedback.ID, &feedback.IdempotencyKey, &feedback.TaskID, &feedback.ResultID, &feedback.CandidateRevision, &verdict, &feedback.Message, &feedback.IntegrityHash, &created); errors.Is(err, sql.ErrNoRows) {
		return domain.OwnerFeedback{}, ErrNotFound
	} else if err != nil {
		return domain.OwnerFeedback{}, err
	}
	feedback.Verdict = domain.OwnerFeedbackVerdict(verdict)
	var err error
	feedback.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.OwnerFeedback{}, err
	}
	if !feedback.IntegrityValid() {
		return domain.OwnerFeedback{}, errors.New("owner feedback integrity is invalid")
	}
	return feedback, nil
}
