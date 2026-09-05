package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type EffectType string

const (
	EffectLocalObservable EffectType = "LOCAL_OBSERVABLE"
	EffectProcess         EffectType = "PROCESS"
	EffectIntegration     EffectType = "INTEGRATION"
	EffectExternal        EffectType = "EXTERNAL"
)

type EffectState string

const (
	EffectPrepared   EffectState = "PREPARED"
	EffectDispatched EffectState = "DISPATCHED"
	EffectObserved   EffectState = "OBSERVED"
)

type ObservationOutcome string

const (
	OutcomeApplied    ObservationOutcome = "APPLIED"
	OutcomeNotApplied ObservationOutcome = "NOT_APPLIED"
)

// EffectIntent is immutable identity for one attempt-bound mutation or
// side-effect. Payload is opaque to the kernel; callers define its semantics.
type EffectIntent struct {
	OperationID          string          `json:"operation_id"`
	TaskID               string          `json:"task_id"`
	AttemptID            string          `json:"attempt_id"`
	RunEpoch             int64           `json:"run_epoch"`
	Type                 EffectType      `json:"effect_type"`
	ExpectedPrecondition string          `json:"expected_precondition"`
	Payload              json.RawMessage `json:"payload"`
}

func (i EffectIntent) Validate() error {
	if strings.TrimSpace(i.OperationID) == "" || strings.TrimSpace(i.TaskID) == "" || strings.TrimSpace(i.AttemptID) == "" {
		return errors.New("operation_id, task_id, and attempt_id are required")
	}
	if i.RunEpoch <= 0 {
		return errors.New("run_epoch must be positive")
	}
	switch i.Type {
	case EffectLocalObservable, EffectProcess, EffectIntegration, EffectExternal:
	default:
		return errors.New("valid effect_type is required")
	}
	if len(i.Payload) > 0 && !json.Valid(i.Payload) {
		return errors.New("effect payload must be valid JSON")
	}
	return nil
}

func (i EffectIntent) CanonicalJSON() ([]byte, error) {
	return json.Marshal(i)
}

func (i EffectIntent) Hash() (string, error) {
	payload, err := i.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

type EffectRecord struct {
	Intent              EffectIntent       `json:"intent"`
	IntentHash          string             `json:"intent_hash"`
	State               EffectState        `json:"state"`
	ObservationOutcome  ObservationOutcome `json:"observation_outcome,omitempty"`
	ObservedResult      json.RawMessage    `json:"observed_result,omitempty"`
	ReconciliationCount int64              `json:"reconciliation_count"`
	CreatedAt           time.Time          `json:"created_at"`
	UpdatedAt           time.Time          `json:"updated_at"`
	DispatchedAt        *time.Time         `json:"dispatched_at,omitempty"`
	ObservedAt          *time.Time         `json:"observed_at,omitempty"`
}
