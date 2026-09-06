package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"mar/internal/domain"
	"mar/internal/model"
	"mar/internal/store"
)

func (s *TaskService) RequestWebTurnForAttempt(ctx context.Context, taskID, attemptID string, epoch int64, req model.TurnRequest) (domain.WebTurn, bool, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(attemptID) == "" || epoch <= 0 {
		return domain.WebTurn{}, false, errors.New("web turn requires task id, attempt id and positive epoch")
	}
	if err := model.ValidateTurnRequest(req); err != nil {
		return domain.WebTurn{}, false, fmt.Errorf("invalid web turn request: %w", err)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	requestHash, err := domain.HashWebTurnJSON(raw)
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	now := s.now().UTC()
	turn := domain.WebTurn{
		ID:          newID("turn"),
		TaskID:      taskID,
		AttemptID:   attemptID,
		RunEpoch:    epoch,
		RequestID:   req.RequestID,
		Request:     append(json.RawMessage(nil), raw...),
		RequestHash: requestHash,
		CreatedAt:   now,
	}
	turn.IntegrityHash, err = turn.IntegrityDigest()
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	return s.store.PublishWebTurn(ctx, turn)
}

func (s *TaskService) PendingWebTurn(ctx context.Context, taskID string) (domain.WebTurn, bool, error) {
	if strings.TrimSpace(taskID) == "" {
		return domain.WebTurn{}, false, errors.New("task id is required")
	}
	return s.store.PendingWebTurn(ctx, taskID)
}

func (s *TaskService) RespondWebTurn(ctx context.Context, taskID, turnID string, message model.Message, finishReason string) (domain.WebTurn, bool, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(turnID) == "" {
		return domain.WebTurn{}, false, errors.New("task id and turn id are required")
	}
	turn, err := s.store.GetWebTurn(ctx, turnID)
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	if turn.TaskID != taskID {
		return domain.WebTurn{}, false, store.ErrStateConflict
	}
	if len(turn.Response) != 0 {
		var existing model.TurnResponse
		if err := json.Unmarshal(turn.Response, &existing); err != nil {
			return domain.WebTurn{}, false, err
		}
		candidate, err := buildWebTurnResponse(turn, message, finishReason)
		if err != nil {
			return domain.WebTurn{}, false, err
		}
		existingRaw, _ := json.Marshal(existing)
		candidateRaw, _ := json.Marshal(candidate)
		if string(existingRaw) == string(candidateRaw) {
			return turn, false, nil
		}
		return domain.WebTurn{}, false, store.ErrWebTurnConflict
	}
	response, err := buildWebTurnResponse(turn, message, finishReason)
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	responseHash, err := domain.HashWebTurnJSON(raw)
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	respondedAt := s.now().UTC()
	turn.Response = append(json.RawMessage(nil), raw...)
	turn.ResponseHash = responseHash
	turn.RespondedAt = &respondedAt
	turn.IntegrityHash, err = turn.IntegrityDigest()
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	return s.store.RespondWebTurn(ctx, turn)
}

func (s *TaskService) WebTurnResponse(ctx context.Context, turnID string) (model.TurnResponse, bool, error) {
	turn, err := s.store.GetWebTurn(ctx, turnID)
	if err != nil {
		return model.TurnResponse{}, false, err
	}
	if len(turn.Response) == 0 {
		return model.TurnResponse{}, false, nil
	}
	var response model.TurnResponse
	if err := json.Unmarshal(turn.Response, &response); err != nil {
		return model.TurnResponse{}, false, fmt.Errorf("decode web turn response: %w", err)
	}
	return response, true, nil
}

func buildWebTurnResponse(turn domain.WebTurn, message model.Message, finishReason string) (model.TurnResponse, error) {
	var req model.TurnRequest
	if err := json.Unmarshal(turn.Request, &req); err != nil {
		return model.TurnResponse{}, fmt.Errorf("decode web turn request: %w", err)
	}
	message.Role = model.RoleAssistant
	message.ToolCallID = ""
	if strings.TrimSpace(message.Content) == "" && len(message.ToolCalls) == 0 {
		return model.TurnResponse{}, errors.New("web brain response requires assistant content or tool calls")
	}
	allowed := make(map[string]struct{}, len(req.Tools))
	for _, tool := range req.Tools {
		allowed[tool.Name] = struct{}{}
	}
	for _, call := range message.ToolCalls {
		if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
			return model.TurnResponse{}, errors.New("web brain tool call requires id and name")
		}
		if _, ok := allowed[call.Name]; !ok {
			return model.TurnResponse{}, fmt.Errorf("web brain tool call %q is not offered by this turn", call.Name)
		}
	}
	finishReason = strings.TrimSpace(finishReason)
	if finishReason == "" {
		if len(message.ToolCalls) != 0 {
			finishReason = "tool_calls"
		} else {
			finishReason = "stop"
		}
	}
	switch finishReason {
	case "stop", "tool_calls", "function_call":
	default:
		return model.TurnResponse{}, fmt.Errorf("unsupported web brain finish_reason %q", finishReason)
	}
	inputBytes, _ := json.Marshal(req)
	outputBytes, _ := json.Marshal(message)
	inputTokens := estimateWebTokens(len(inputBytes))
	outputTokens := estimateWebTokens(len(outputBytes))
	return model.TurnResponse{
		ProviderResponseID: "web:" + turn.ID,
		Model:              req.Model,
		Message:            message,
		FinishReason:       finishReason,
		Usage: model.Usage{
			InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: inputTokens + outputTokens, Estimated: true,
		},
	}, nil
}

func estimateWebTokens(byteCount int) int64 {
	if byteCount <= 0 {
		return 1
	}
	// This is only a conservative runtime budget estimate for Web Chat turns;
	// it is not provider billing/accounting data.
	return int64((byteCount + 2) / 3)
}
