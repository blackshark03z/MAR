package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type ControlKind string

const (
	ControlSteer  ControlKind = "STEER"
	ControlInput  ControlKind = "INPUT"
	ControlCancel ControlKind = "CANCEL"
)

type SteerKind string

const (
	SteerContext                SteerKind = "context"
	SteerPriority               SteerKind = "priority"
	SteerBlockedChoice          SteerKind = "blocked_choice"
	SteerAdditionalVerification SteerKind = "additional_verification"
	SteerCancel                 SteerKind = "cancel"
)

type SteerPayload struct {
	Kind    SteerKind `json:"kind"`
	Message string    `json:"message"`
}

func (p SteerPayload) Validate() error {
	switch p.Kind {
	case SteerContext, SteerPriority, SteerBlockedChoice, SteerAdditionalVerification, SteerCancel:
	default:
		return errors.New("steering kind is invalid")
	}
	if strings.TrimSpace(p.Message) == "" {
		return errors.New("steering message is required")
	}
	return nil
}

type InputPayload struct {
	Message string `json:"message"`
}

func (p InputPayload) Validate() error {
	if strings.TrimSpace(p.Message) == "" {
		return errors.New("input message is required")
	}
	return nil
}

type CancelPayload struct {
	Reason string `json:"reason,omitempty"`
}

type TaskControl struct {
	ID             string          `json:"control_id"`
	TaskID         string          `json:"task_id"`
	Version        int64           `json:"version"`
	IdempotencyKey string          `json:"idempotency_key"`
	Kind           ControlKind     `json:"kind"`
	Payload        json.RawMessage `json:"payload"`
	IntegrityHash  string          `json:"integrity_hash"`
	CreatedAt      time.Time       `json:"created_at"`
}

func (c TaskControl) ValidateIdentity() error {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.TaskID) == "" || strings.TrimSpace(c.IdempotencyKey) == "" {
		return errors.New("control id, task_id and idempotency_key are required")
	}
	if c.Version <= 0 {
		return errors.New("control version must be positive")
	}
	switch c.Kind {
	case ControlSteer, ControlInput, ControlCancel:
	default:
		return errors.New("control kind is invalid")
	}
	if len(c.Payload) == 0 || !json.Valid(c.Payload) {
		return errors.New("control payload must be valid JSON")
	}
	if c.CreatedAt.IsZero() {
		return errors.New("control created_at is required")
	}
	return nil
}

func (c TaskControl) IntegrityDigest() (string, error) {
	if err := c.ValidateIdentity(); err != nil {
		return "", err
	}
	canonical := struct {
		ID             string          `json:"control_id"`
		TaskID         string          `json:"task_id"`
		Version        int64           `json:"version"`
		IdempotencyKey string          `json:"idempotency_key"`
		Kind           ControlKind     `json:"kind"`
		Payload        json.RawMessage `json:"payload"`
		CreatedAt      string          `json:"created_at"`
	}{c.ID, c.TaskID, c.Version, c.IdempotencyKey, c.Kind, c.Payload, c.CreatedAt.UTC().Format(time.RFC3339Nano)}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (c TaskControl) IntegrityValid() bool {
	digest, err := c.IntegrityDigest()
	return err == nil && strings.EqualFold(strings.TrimSpace(c.IntegrityHash), digest)
}
