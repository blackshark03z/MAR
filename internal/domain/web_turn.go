package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type WebTurn struct {
	ID            string          `json:"turn_id"`
	TaskID        string          `json:"task_id"`
	AttemptID     string          `json:"attempt_id"`
	RunEpoch      int64           `json:"run_epoch"`
	RequestID     string          `json:"request_id"`
	Request       json.RawMessage `json:"request"`
	Response      json.RawMessage `json:"response,omitempty"`
	RequestHash   string          `json:"request_hash"`
	ResponseHash  string          `json:"response_hash,omitempty"`
	IntegrityHash string          `json:"integrity_hash"`
	CreatedAt     time.Time       `json:"created_at"`
	RespondedAt   *time.Time      `json:"responded_at,omitempty"`
}

func HashWebTurnJSON(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return "", errors.New("web turn JSON must be valid and non-empty")
	}
	// Web turns cross MCP structured-content boundaries, which may legally
	// reserialize an equivalent JSON object with different field ordering or
	// whitespace. Bind integrity to canonical JSON semantics rather than the
	// incidental wire encoding so transport round-trips do not create false
	// tamper failures.
	var value any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func (t WebTurn) ValidateIdentity() error {
	if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.TaskID) == "" || strings.TrimSpace(t.AttemptID) == "" || strings.TrimSpace(t.RequestID) == "" {
		return errors.New("web turn id, task_id, attempt_id and request_id are required")
	}
	if t.RunEpoch <= 0 || t.CreatedAt.IsZero() {
		return errors.New("web turn run_epoch and created_at are required")
	}
	requestHash, err := HashWebTurnJSON(t.Request)
	if err != nil || !strings.EqualFold(requestHash, strings.TrimSpace(t.RequestHash)) {
		return errors.New("web turn request hash is invalid")
	}
	if len(t.Response) == 0 {
		if t.ResponseHash != "" || t.RespondedAt != nil {
			return errors.New("pending web turn cannot contain response metadata")
		}
	} else {
		responseHash, err := HashWebTurnJSON(t.Response)
		if err != nil || !strings.EqualFold(responseHash, strings.TrimSpace(t.ResponseHash)) || t.RespondedAt == nil || t.RespondedAt.IsZero() {
			return errors.New("web turn response metadata is invalid")
		}
	}
	return nil
}

func (t WebTurn) IntegrityDigest() (string, error) {
	if err := t.ValidateIdentity(); err != nil {
		return "", err
	}
	responded := ""
	if t.RespondedAt != nil {
		responded = t.RespondedAt.UTC().Format(time.RFC3339Nano)
	}
	canonical := struct {
		ID           string `json:"turn_id"`
		TaskID       string `json:"task_id"`
		AttemptID    string `json:"attempt_id"`
		RunEpoch     int64  `json:"run_epoch"`
		RequestID    string `json:"request_id"`
		RequestHash  string `json:"request_hash"`
		ResponseHash string `json:"response_hash"`
		CreatedAt    string `json:"created_at"`
		RespondedAt  string `json:"responded_at"`
	}{
		ID: t.ID, TaskID: t.TaskID, AttemptID: t.AttemptID, RunEpoch: t.RunEpoch,
		RequestID: t.RequestID, RequestHash: t.RequestHash, ResponseHash: t.ResponseHash,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339Nano), RespondedAt: responded,
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (t WebTurn) IntegrityValid() bool {
	want, err := t.IntegrityDigest()
	return err == nil && strings.EqualFold(want, strings.TrimSpace(t.IntegrityHash))
}
