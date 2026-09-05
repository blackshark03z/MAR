package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type IntegrationStatus string

const (
	IntegrationPrepared   IntegrationStatus = "PREPARED"
	IntegrationDispatched IntegrationStatus = "DISPATCHED"
	IntegrationComplete   IntegrationStatus = "COMPLETE"
	IntegrationBlocked    IntegrationStatus = "BLOCKED"
)

type IntegrationAttempt struct {
	ID                 string            `json:"integration_attempt_id"`
	TaskID             string            `json:"task_id"`
	Version            int64             `json:"version"`
	ProjectID          string            `json:"project_id"`
	ExpectedRef        string            `json:"expected_ref"`
	ExpectedHead       string            `json:"expected_head"`
	TaskResultID       string            `json:"task_result_id"`
	TaskResultVersion  int64             `json:"task_result_version"`
	TaskResultRevision string            `json:"task_result_revision"`
	CandidateRevision  string            `json:"candidate_revision"`
	EvidenceID         string            `json:"evidence_id"`
	Status             IntegrationStatus `json:"status"`
	ObservedHead       string            `json:"observed_head,omitempty"`
	Failure            string            `json:"failure,omitempty"`
	IntegrityHash      string            `json:"integrity_hash"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

func (a IntegrationAttempt) ValidateIdentity() error {
	for _, value := range []string{a.ID, a.TaskID, a.ProjectID, a.ExpectedRef, a.ExpectedHead, a.TaskResultID, a.TaskResultRevision, a.CandidateRevision, a.EvidenceID} {
		if strings.TrimSpace(value) == "" {
			return errors.New("integration attempt identity fields are required")
		}
	}
	if a.Version <= 0 || a.TaskResultVersion <= 0 {
		return errors.New("integration attempt versions must be positive")
	}
	switch a.Status {
	case IntegrationPrepared, IntegrationDispatched, IntegrationComplete, IntegrationBlocked:
	default:
		return errors.New("integration attempt status is invalid")
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		return errors.New("integration attempt timestamps are required")
	}
	return nil
}

func (a IntegrationAttempt) IntegrityDigest() (string, error) {
	if err := a.ValidateIdentity(); err != nil {
		return "", err
	}
	canonical := struct {
		ID                 string            `json:"integration_attempt_id"`
		TaskID             string            `json:"task_id"`
		Version            int64             `json:"version"`
		ProjectID          string            `json:"project_id"`
		ExpectedRef        string            `json:"expected_ref"`
		ExpectedHead       string            `json:"expected_head"`
		TaskResultID       string            `json:"task_result_id"`
		TaskResultVersion  int64             `json:"task_result_version"`
		TaskResultRevision string            `json:"task_result_revision"`
		CandidateRevision  string            `json:"candidate_revision"`
		EvidenceID         string            `json:"evidence_id"`
		Status             IntegrationStatus `json:"status"`
		ObservedHead       string            `json:"observed_head"`
		Failure            string            `json:"failure"`
		CreatedAt          string            `json:"created_at"`
		UpdatedAt          string            `json:"updated_at"`
	}{a.ID, a.TaskID, a.Version, a.ProjectID, a.ExpectedRef, a.ExpectedHead, a.TaskResultID, a.TaskResultVersion, a.TaskResultRevision, a.CandidateRevision, a.EvidenceID, a.Status, a.ObservedHead, a.Failure, a.CreatedAt.UTC().Format(time.RFC3339Nano), a.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (a IntegrationAttempt) IntegrityValid() bool {
	want, err := a.IntegrityDigest()
	return err == nil && strings.EqualFold(strings.TrimSpace(a.IntegrityHash), want)
}
