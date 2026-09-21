package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"mar/internal/contextengine"
	"mar/internal/domain"
	"mar/internal/model"
	"mar/internal/store"
)

const CognitionDeltaVersion = 1

type CognitionEventKind string

const (
	CognitionEventDecisionRequired                 CognitionEventKind = "DECISION_REQUIRED"
	CognitionEventAuthorityRequired                CognitionEventKind = "AUTHORITY_REQUIRED"
	CognitionEventRecoverableFailureNeedsReasoning CognitionEventKind = "RECOVERABLE_FAILURE_NEEDS_REASONING"
	CognitionEventCandidateReady                   CognitionEventKind = "CANDIDATE_READY"
	CognitionEventTerminal                         CognitionEventKind = "TERMINAL"
)

type CognitionEvent struct {
	Kind            CognitionEventKind `json:"kind"`
	Sequence        int64              `json:"sequence"`
	TaskID          string             `json:"task_id"`
	RunEpoch        int64              `json:"run_epoch"`
	TaskState       domain.TaskState   `json:"task_state"`
	CurrentRevision string             `json:"current_revision"`
}

type CognitionRequestDelta struct {
	RequestID         string                     `json:"request_id"`
	Model             string                     `json:"model,omitempty"`
	System            string                     `json:"system,omitempty"`
	Projection        map[string]json.RawMessage `json:"projection_changed,omitempty"`
	ProjectionRemoved []string                   `json:"projection_removed,omitempty"`
	ProtocolTail      []model.Message            `json:"protocol_tail,omitempty"`
	Tools             []model.ToolDefinition     `json:"tools,omitempty"`
	ReasoningEffort   string                     `json:"reasoning_effort,omitempty"`
	MaxOutputTokens   int64                      `json:"max_output_tokens,omitempty"`
}

type CognitionDelta struct {
	Version      int                    `json:"version"`
	Mode         string                 `json:"mode"`
	Event        CognitionEvent         `json:"event"`
	BaseCursor   string                 `json:"base_cursor,omitempty"`
	Cursor       string                 `json:"cursor"`
	FullRequest  *model.TurnRequest     `json:"full_request,omitempty"`
	Delta        *CognitionRequestDelta `json:"delta,omitempty"`
	FullBytes    int                    `json:"full_bytes"`
	PayloadBytes int                    `json:"payload_bytes"`
}

type cognitionCursor struct {
	Version     int    `json:"version"`
	TaskID      string `json:"task_id"`
	TurnID      string `json:"turn_id"`
	AttemptID   string `json:"attempt_id"`
	RunEpoch    int64  `json:"run_epoch"`
	RequestHash string `json:"request_hash"`
}

func (s *TaskService) CognitionDelta(ctx context.Context, current domain.WebTurn, cursorToken string) (CognitionDelta, error) {
	if !current.IntegrityValid() || len(current.Response) != 0 {
		return CognitionDelta{}, errors.New("cognition delta requires an intact pending web turn")
	}
	var previous *domain.WebTurn
	if cursor, ok := decodeCognitionCursor(cursorToken); ok &&
		cursor.TaskID == current.TaskID &&
		cursor.AttemptID == current.AttemptID &&
		cursor.RunEpoch == current.RunEpoch {
		prior, err := s.store.GetWebTurn(ctx, cursor.TurnID)
		if err == nil &&
			prior.IntegrityValid() &&
			prior.TaskID == current.TaskID &&
			prior.AttemptID == current.AttemptID &&
			prior.RunEpoch == current.RunEpoch &&
			strings.EqualFold(prior.RequestHash, cursor.RequestHash) &&
			len(prior.Response) != 0 &&
			prior.CreatedAt.Before(current.CreatedAt) {
			previous = &prior
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return CognitionDelta{}, err
		}
	}
	return BuildCognitionDelta(current, previous)
}

// AutomaticCognitionDelta derives a safe delta base from durable turns without
// requiring the external cognition client to manage a cursor. It only reuses a
// responded predecessor from the same task, attempt, and run epoch. If the
// bounded history cannot prove the correct predecessor, it falls back to full.
func (s *TaskService) AutomaticCognitionDelta(ctx context.Context, current domain.WebTurn) (CognitionDelta, error) {
	if !current.IntegrityValid() || len(current.Response) != 0 {
		return CognitionDelta{}, errors.New("automatic cognition delta requires an intact pending web turn")
	}
	turns, err := s.store.ListWebTurnsByTaskEpoch(ctx, current.TaskID, current.RunEpoch, 128)
	if err != nil {
		return CognitionDelta{}, err
	}
	return BuildCognitionDelta(current, latestAutomaticCognitionBase(current, turns))
}

func latestAutomaticCognitionBase(current domain.WebTurn, turns []domain.WebTurn) *domain.WebTurn {
	currentSeen := false
	for i := range turns {
		if turns[i].ID == current.ID {
			currentSeen = true
			break
		}
	}
	if !currentSeen {
		return nil
	}
	var previous *domain.WebTurn
	for i := range turns {
		candidate := turns[i]
		if candidate.ID == current.ID ||
			candidate.TaskID != current.TaskID ||
			candidate.AttemptID != current.AttemptID ||
			candidate.RunEpoch != current.RunEpoch ||
			!candidate.CreatedAt.Before(current.CreatedAt) ||
			len(candidate.Response) == 0 ||
			candidate.RespondedAt == nil ||
			!candidate.IntegrityValid() {
			continue
		}
		if previous == nil || candidate.CreatedAt.After(previous.CreatedAt) ||
			(candidate.CreatedAt.Equal(previous.CreatedAt) && candidate.ID > previous.ID) {
			copy := candidate
			previous = &copy
		}
	}
	return previous
}

func BuildCognitionDelta(current domain.WebTurn, previous *domain.WebTurn) (CognitionDelta, error) {
	if !current.IntegrityValid() {
		return CognitionDelta{}, errors.New("current web turn integrity is invalid")
	}
	currentRequest, currentProjection, currentSystem, currentTail, err := decodeCognitionTurn(current)
	if err != nil {
		return CognitionDelta{}, err
	}
	fullBytes, err := json.Marshal(currentRequest)
	if err != nil {
		return CognitionDelta{}, err
	}
	cursor, err := encodeCognitionCursor(cognitionCursor{
		Version: CognitionDeltaVersion, TaskID: current.TaskID, TurnID: current.ID,
		AttemptID: current.AttemptID, RunEpoch: current.RunEpoch, RequestHash: current.RequestHash,
	})
	if err != nil {
		return CognitionDelta{}, err
	}
	result := CognitionDelta{
		Version: CognitionDeltaVersion,
		Event: CognitionEvent{
			Kind: cognitionEventKind(currentProjection.TaskState), Sequence: current.CreatedAt.UTC().UnixNano(),
			TaskID: current.TaskID, RunEpoch: current.RunEpoch, TaskState: currentProjection.TaskState,
			CurrentRevision: currentProjection.CurrentRevision,
		},
		Cursor: cursor, FullBytes: len(fullBytes),
	}
	if previous == nil {
		result.Mode = "full"
		copy := currentRequest
		result.FullRequest = &copy
		return finalizeCognitionDelta(result)
	}
	if !previous.IntegrityValid() ||
		previous.TaskID != current.TaskID ||
		previous.AttemptID != current.AttemptID ||
		previous.RunEpoch != current.RunEpoch ||
		len(previous.Response) == 0 ||
		!previous.CreatedAt.Before(current.CreatedAt) {
		result.Mode = "full"
		copy := currentRequest
		result.FullRequest = &copy
		return finalizeCognitionDelta(result)
	}
	previousRequest, previousProjection, previousSystem, _, err := decodeCognitionTurn(*previous)
	if err != nil {
		result.Mode = "full"
		copy := currentRequest
		result.FullRequest = &copy
		return finalizeCognitionDelta(result)
	}
	baseCursor, err := encodeCognitionCursor(cognitionCursor{
		Version: CognitionDeltaVersion, TaskID: previous.TaskID, TurnID: previous.ID,
		AttemptID: previous.AttemptID, RunEpoch: previous.RunEpoch, RequestHash: previous.RequestHash,
	})
	if err != nil {
		return CognitionDelta{}, err
	}
	changed, removed, err := diffProjectionSections(previousProjection, currentProjection)
	if err != nil {
		return CognitionDelta{}, err
	}
	delta := &CognitionRequestDelta{
		RequestID:         currentRequest.RequestID,
		Projection:        changed,
		ProjectionRemoved: removed,
		ProtocolTail:      currentTail,
	}
	if currentRequest.Model != previousRequest.Model {
		delta.Model = currentRequest.Model
	}
	if currentSystem != previousSystem {
		delta.System = currentSystem
	}
	if !toolDefinitionsEqual(currentRequest.Tools, previousRequest.Tools) {
		delta.Tools = currentRequest.Tools
	}
	if currentRequest.ReasoningEffort != previousRequest.ReasoningEffort {
		delta.ReasoningEffort = currentRequest.ReasoningEffort
	}
	if currentRequest.MaxOutputTokens != previousRequest.MaxOutputTokens {
		delta.MaxOutputTokens = currentRequest.MaxOutputTokens
	}
	result.Mode = "delta"
	result.BaseCursor = baseCursor
	result.Delta = delta
	result, err = finalizeCognitionDelta(result)
	if err != nil {
		return CognitionDelta{}, err
	}
	if result.PayloadBytes >= result.FullBytes {
		result.Mode = "full"
		result.BaseCursor = ""
		result.Delta = nil
		copy := currentRequest
		result.FullRequest = &copy
		return finalizeCognitionDelta(result)
	}
	return result, nil
}

func decodeCognitionTurn(turn domain.WebTurn) (model.TurnRequest, contextengine.DecisionProjection, string, []model.Message, error) {
	var req model.TurnRequest
	if err := json.Unmarshal(turn.Request, &req); err != nil {
		return model.TurnRequest{}, contextengine.DecisionProjection{}, "", nil, err
	}
	const prefix = "MAR_DECISION_PROJECTION_JSON:\n"
	const suffix = "\n\nThis is MAR's bounded derived current decision state."
	projectionIndex := -1
	var projection contextengine.DecisionProjection
	for i, message := range req.Messages {
		if message.Role != model.RoleUser || !strings.HasPrefix(message.Content, prefix) {
			continue
		}
		raw := strings.TrimPrefix(message.Content, prefix)
		if at := strings.Index(raw, suffix); at >= 0 {
			raw = raw[:at]
		}
		if err := json.Unmarshal([]byte(raw), &projection); err != nil {
			return model.TurnRequest{}, contextengine.DecisionProjection{}, "", nil, err
		}
		projectionIndex = i
		break
	}
	if projectionIndex < 0 {
		return model.TurnRequest{}, contextengine.DecisionProjection{}, "", nil, errors.New("web turn request has no DecisionProjection message")
	}
	system := ""
	for _, message := range req.Messages[:projectionIndex] {
		if message.Role == model.RoleSystem {
			system = message.Content
			break
		}
	}
	tail := append([]model.Message(nil), req.Messages[projectionIndex+1:]...)
	return req, projection, system, tail, nil
}

func diffProjectionSections(previous, current contextengine.DecisionProjection) (map[string]json.RawMessage, []string, error) {
	previousFields, err := projectionSectionMap(previous)
	if err != nil {
		return nil, nil, err
	}
	currentFields, err := projectionSectionMap(current)
	if err != nil {
		return nil, nil, err
	}
	changed := map[string]json.RawMessage{}
	for key, raw := range currentFields {
		if old, ok := previousFields[key]; !ok || !jsonRawEqual(old, raw) {
			changed[key] = append(json.RawMessage(nil), raw...)
		}
	}
	removed := make([]string, 0)
	for key := range previousFields {
		if _, ok := currentFields[key]; !ok {
			removed = append(removed, key)
		}
	}
	sort.Strings(removed)
	return changed, removed, nil
}

func projectionSectionMap(projection contextengine.DecisionProjection) (map[string]json.RawMessage, error) {
	raw, err := json.Marshal(projection)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	delete(fields, "bytes")
	return fields, nil
}

func cognitionEventKind(state domain.TaskState) CognitionEventKind {
	switch state {
	case domain.TaskInputRequired:
		return CognitionEventAuthorityRequired
	case domain.TaskBlocked, domain.TaskRetryWait, domain.TaskFailed:
		return CognitionEventRecoverableFailureNeedsReasoning
	case domain.TaskVerifying, domain.TaskReviewing, domain.TaskReadyToIntegrate, domain.TaskIntegrating, domain.TaskVerified:
		return CognitionEventCandidateReady
	case domain.TaskComplete, domain.TaskCancelled:
		return CognitionEventTerminal
	default:
		return CognitionEventDecisionRequired
	}
}

func finalizeCognitionDelta(result CognitionDelta) (CognitionDelta, error) {
	wire, err := json.Marshal(result)
	if err != nil {
		return CognitionDelta{}, err
	}
	result.PayloadBytes = len(wire)
	wire, err = json.Marshal(result)
	if err != nil {
		return CognitionDelta{}, err
	}
	result.PayloadBytes = len(wire)
	return result, nil
}

func encodeCognitionCursor(cursor cognitionCursor) (string, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCognitionCursor(token string) (cognitionCursor, bool) {
	if strings.TrimSpace(token) == "" {
		return cognitionCursor{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return cognitionCursor{}, false
	}
	var cursor cognitionCursor
	if err := json.Unmarshal(raw, &cursor); err != nil ||
		cursor.Version != CognitionDeltaVersion ||
		cursor.TaskID == "" ||
		cursor.TurnID == "" ||
		cursor.AttemptID == "" ||
		cursor.RunEpoch <= 0 ||
		cursor.RequestHash == "" {
		return cognitionCursor{}, false
	}
	return cursor, true
}

func jsonRawEqual(a, b json.RawMessage) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	l, _ := json.Marshal(left)
	r, _ := json.Marshal(right)
	return string(l) == string(r)
}

func toolDefinitionsEqual(a, b []model.ToolDefinition) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
