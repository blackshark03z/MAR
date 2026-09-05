package model

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}

type TurnRequest struct {
	RequestID       string           `json:"request_id"`
	Model           string           `json:"model"`
	Messages        []Message        `json:"messages"`
	Tools           []ToolDefinition `json:"tools,omitempty"`
	ReasoningEffort string           `json:"reasoning_effort,omitempty"`
}

type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type TurnResponse struct {
	ProviderResponseID string  `json:"provider_response_id"`
	Model              string  `json:"model"`
	Message            Message `json:"message"`
	FinishReason       string  `json:"finish_reason"`
	Usage              Usage   `json:"usage"`
	ProviderRequestID  string  `json:"provider_request_id,omitempty"`
}

type Provider interface {
	Turn(context.Context, TurnRequest) (TurnResponse, error)
}

type Gateway struct {
	provider Provider
}

func NewGateway(provider Provider) (*Gateway, error) {
	if provider == nil {
		return nil, errors.New("model provider is required")
	}
	return &Gateway{provider: provider}, nil
}

func (g *Gateway) Turn(ctx context.Context, req TurnRequest) (TurnResponse, error) {
	if err := ValidateTurnRequest(req); err != nil {
		return TurnResponse{}, err
	}
	return g.provider.Turn(ctx, req)
}

func ValidateTurnRequest(req TurnRequest) error {
	if strings.TrimSpace(req.RequestID) == "" {
		return errors.New("request_id is required")
	}
	if strings.TrimSpace(req.Model) == "" {
		return errors.New("model is required")
	}
	if len(req.Messages) == 0 {
		return errors.New("at least one message is required")
	}
	for i, msg := range req.Messages {
		switch msg.Role {
		case RoleSystem, RoleUser:
			if strings.TrimSpace(msg.Content) == "" {
				return errors.New("system/user message content is required")
			}
			if len(msg.ToolCalls) != 0 || msg.ToolCallID != "" {
				return errors.New("system/user message cannot contain tool-call fields")
			}
		case RoleAssistant:
			if strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
				return errors.New("assistant message requires content or tool calls")
			}
			if msg.ToolCallID != "" {
				return errors.New("assistant message cannot have tool_call_id")
			}
			for _, call := range msg.ToolCalls {
				if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
					return errors.New("assistant tool call requires id and name")
				}
			}
		case RoleTool:
			if strings.TrimSpace(msg.ToolCallID) == "" {
				return errors.New("tool message requires tool_call_id")
			}
			if len(msg.ToolCalls) != 0 {
				return errors.New("tool message cannot contain tool calls")
			}
		default:
			return errors.New("invalid message role at index " + itoa(i))
		}
	}
	seen := make(map[string]struct{}, len(req.Tools))
	for _, tool := range req.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return errors.New("tool name is required")
		}
		if _, ok := seen[name]; ok {
			return errors.New("duplicate tool name: " + name)
		}
		seen[name] = struct{}{}
		if len(tool.Parameters) == 0 || !json.Valid(tool.Parameters) {
			return errors.New("tool parameters must be valid JSON")
		}
	}
	return nil
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
