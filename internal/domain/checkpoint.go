package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type SemanticCheckpointPayload struct {
	CompletedWork        []string `json:"completed_work"`
	CurrentHypothesis    string   `json:"current_hypothesis"`
	ChangedAreas         []string `json:"changed_areas"`
	VerificationStatus   string   `json:"verification_status"`
	Blockers             []string `json:"blockers"`
	RemainingWork        []string `json:"remaining_work"`
	NextAction           string   `json:"next_action"`
	CriticalEvidenceRefs []string `json:"critical_evidence_refs"`
}

func (p SemanticCheckpointPayload) Validate() error {
	if strings.TrimSpace(p.CurrentHypothesis) == "" {
		return errors.New("checkpoint current_hypothesis is required")
	}
	if strings.TrimSpace(p.VerificationStatus) == "" {
		return errors.New("checkpoint verification_status is required")
	}
	if strings.TrimSpace(p.NextAction) == "" {
		return errors.New("checkpoint next_action is required")
	}
	return nil
}

type SemanticCheckpoint struct {
	ID              string                    `json:"id"`
	TaskID          string                    `json:"task_id"`
	AttemptID       string                    `json:"attempt_id"`
	RunEpoch        int64                     `json:"run_epoch"`
	Version         int64                     `json:"version"`
	GoalHash        string                    `json:"goal_hash"`
	BaseRevision    string                    `json:"base_revision"`
	CurrentRevision string                    `json:"current_revision"`
	Payload         SemanticCheckpointPayload `json:"payload"`
	IntegrityHash   string                    `json:"integrity_hash"`
	CreatedAt       time.Time                 `json:"created_at"`
}

func (c SemanticCheckpoint) ValidateIdentity() error {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.TaskID) == "" || strings.TrimSpace(c.AttemptID) == "" {
		return errors.New("checkpoint id, task_id and attempt_id are required")
	}
	if c.RunEpoch <= 0 || c.Version <= 0 {
		return errors.New("checkpoint run_epoch and version must be positive")
	}
	if strings.TrimSpace(c.GoalHash) == "" || strings.TrimSpace(c.BaseRevision) == "" || strings.TrimSpace(c.CurrentRevision) == "" {
		return errors.New("checkpoint goal_hash, base_revision and current_revision are required")
	}
	if c.CreatedAt.IsZero() {
		return errors.New("checkpoint created_at is required")
	}
	return c.Payload.Validate()
}

func (c SemanticCheckpoint) IntegrityDigest() (string, error) {
	canonical := struct {
		ID              string                    `json:"id"`
		TaskID          string                    `json:"task_id"`
		AttemptID       string                    `json:"attempt_id"`
		RunEpoch        int64                     `json:"run_epoch"`
		Version         int64                     `json:"version"`
		GoalHash        string                    `json:"goal_hash"`
		BaseRevision    string                    `json:"base_revision"`
		CurrentRevision string                    `json:"current_revision"`
		Payload         SemanticCheckpointPayload `json:"payload"`
		CreatedAt       string                    `json:"created_at"`
	}{
		ID:              c.ID,
		TaskID:          c.TaskID,
		AttemptID:       c.AttemptID,
		RunEpoch:        c.RunEpoch,
		Version:         c.Version,
		GoalHash:        c.GoalHash,
		BaseRevision:    c.BaseRevision,
		CurrentRevision: c.CurrentRevision,
		Payload:         c.Payload,
		CreatedAt:       c.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (c SemanticCheckpoint) IntegrityValid() bool {
	if err := c.ValidateIdentity(); err != nil {
		return false
	}
	want, err := c.IntegrityDigest()
	return err == nil && strings.EqualFold(want, strings.TrimSpace(c.IntegrityHash))
}
