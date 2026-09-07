package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type OwnerFeedbackVerdict string

const (
	OwnerFeedbackAccepted OwnerFeedbackVerdict = "ACCEPTED"
	OwnerFeedbackRejected OwnerFeedbackVerdict = "REJECTED"
	OwnerFeedbackComment  OwnerFeedbackVerdict = "COMMENT"
)

// OwnerFeedback binds owner judgment to the exact durable result/candidate that
// was reviewed. It is product evidence, not execution authority.
type OwnerFeedback struct {
	ID                string               `json:"id"`
	IdempotencyKey    string               `json:"idempotency_key"`
	TaskID            string               `json:"task_id"`
	ResultID          string               `json:"result_id"`
	CandidateRevision string               `json:"candidate_revision"`
	Verdict           OwnerFeedbackVerdict `json:"verdict"`
	Message           string               `json:"message,omitempty"`
	IntegrityHash     string               `json:"integrity_hash"`
	CreatedAt         time.Time            `json:"created_at"`
}

func (f OwnerFeedback) ValidateIdentity() error {
	for _, value := range []string{f.ID, f.IdempotencyKey, f.TaskID, f.ResultID, f.CandidateRevision} {
		if strings.TrimSpace(value) == "" {
			return errors.New("owner feedback identity fields are required")
		}
	}
	switch f.Verdict {
	case OwnerFeedbackAccepted, OwnerFeedbackRejected, OwnerFeedbackComment:
	default:
		return errors.New("owner feedback verdict is invalid")
	}
	if f.Verdict == OwnerFeedbackComment && strings.TrimSpace(f.Message) == "" {
		return errors.New("owner feedback comment requires a message")
	}
	if f.CreatedAt.IsZero() {
		return errors.New("owner feedback created_at is required")
	}
	return nil
}

func (f OwnerFeedback) IntegrityDigest() (string, error) {
	payload, err := json.Marshal(struct {
		ID                string               `json:"id"`
		IdempotencyKey    string               `json:"idempotency_key"`
		TaskID            string               `json:"task_id"`
		ResultID          string               `json:"result_id"`
		CandidateRevision string               `json:"candidate_revision"`
		Verdict           OwnerFeedbackVerdict `json:"verdict"`
		Message           string               `json:"message"`
		CreatedAt         string               `json:"created_at"`
	}{f.ID, f.IdempotencyKey, f.TaskID, f.ResultID, f.CandidateRevision, f.Verdict, strings.TrimSpace(f.Message), f.CreatedAt.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (f OwnerFeedback) IntegrityValid() bool {
	if err := f.ValidateIdentity(); err != nil {
		return false
	}
	want, err := f.IntegrityDigest()
	return err == nil && strings.EqualFold(want, strings.TrimSpace(f.IntegrityHash))
}
