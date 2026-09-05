package openaichat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"mar/internal/model"
)

const defaultMaxResponseBytes int64 = 8 << 20

type Config struct {
	BaseURL          string
	APIKeyEnv        string
	RequestTimeout   time.Duration
	MaxResponseBytes int64
	HTTPClient       *http.Client
}

type Client struct {
	endpoint         string
	apiKeyEnv        string
	httpClient       *http.Client
	requestTimeout   time.Duration
	maxResponseBytes int64
}

type HTTPError struct {
	StatusCode int
	RequestID  string
	RetryAfter string
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("model provider HTTP %d: %s", e.StatusCode, e.Body)
}

func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("model provider base URL is required")
	}
	if strings.TrimSpace(cfg.APIKeyEnv) == "" {
		return nil, errors.New("API key environment variable name is required")
	}
	maxBytes := cfg.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxResponseBytes
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &Client{
		endpoint:         base + "/chat/completions",
		apiKeyEnv:        cfg.APIKeyEnv,
		httpClient:       client,
		requestTimeout:   timeout,
		maxResponseBytes: maxBytes,
	}, nil
}

type chatRequest struct {
	Model           string        `json:"model"`
	Messages        []chatMessage `json:"messages"`
	Tools           []chatTool    `json:"tools,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatFunctionCall `json:"function"`
}

type chatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict,omitempty"`
}

type chatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
	} `json:"usage"`
}

func (c *Client) Turn(ctx context.Context, req model.TurnRequest) (model.TurnResponse, error) {
	// Enforce MAR's request bound even when a caller injects a custom HTTP
	// client with no timeout. A shorter caller deadline still wins.
	turnCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	apiKey := strings.TrimSpace(os.Getenv(c.apiKeyEnv))
	if apiKey == "" {
		return model.TurnResponse{}, fmt.Errorf("model API key environment variable %s is empty", c.apiKeyEnv)
	}
	wire := chatRequest{Model: req.Model, ReasoningEffort: req.ReasoningEffort}
	wire.Messages = make([]chatMessage, 0, len(req.Messages))
	for _, msg := range req.Messages {
		wm := chatMessage{Role: string(msg.Role), Content: msg.Content, ToolCallID: msg.ToolCallID}
		for _, call := range msg.ToolCalls {
			wm.ToolCalls = append(wm.ToolCalls, chatToolCall{ID: call.ID, Type: "function", Function: chatFunctionCall{Name: call.Name, Arguments: call.Arguments}})
		}
		wire.Messages = append(wire.Messages, wm)
	}
	for _, tool := range req.Tools {
		wire.Tools = append(wire.Tools, chatTool{Type: "function", Function: chatFunction{Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters, Strict: tool.Strict}})
	}
	payload, err := json.Marshal(wire)
	if err != nil {
		return model.TurnResponse{}, fmt.Errorf("encode model request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(turnCtx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return model.TurnResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-Client-Request-Id", req.RequestID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return model.TurnResponse{}, err
	}
	defer resp.Body.Close()
	body, tooLarge, err := readBounded(resp.Body, c.maxResponseBytes)
	if err != nil {
		return model.TurnResponse{}, err
	}
	if tooLarge {
		return model.TurnResponse{}, fmt.Errorf("model provider response exceeds %d bytes", c.maxResponseBytes)
	}
	providerRequestID := resp.Header.Get("X-Request-Id")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.TurnResponse{}, &HTTPError{
			StatusCode: resp.StatusCode,
			RequestID:  providerRequestID,
			RetryAfter: resp.Header.Get("Retry-After"),
			Body:       boundedErrorText(body, 4096),
		}
	}
	var decoded chatResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return model.TurnResponse{}, fmt.Errorf("decode model response: %w", err)
	}
	if len(decoded.Choices) != 1 {
		return model.TurnResponse{}, fmt.Errorf("expected exactly one model choice, got %d", len(decoded.Choices))
	}
	choice := decoded.Choices[0]
	if choice.Message.Role != "" && choice.Message.Role != string(model.RoleAssistant) {
		return model.TurnResponse{}, fmt.Errorf("unexpected provider response role %q", choice.Message.Role)
	}
	out := model.Message{Role: model.RoleAssistant, Content: choice.Message.Content}
	for _, call := range choice.Message.ToolCalls {
		if call.Type != "" && call.Type != "function" {
			return model.TurnResponse{}, fmt.Errorf("unsupported model tool call type %q", call.Type)
		}
		if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Function.Name) == "" {
			return model.TurnResponse{}, errors.New("provider returned malformed function call")
		}
		out.ToolCalls = append(out.ToolCalls, model.ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	if strings.TrimSpace(out.Content) == "" && len(out.ToolCalls) == 0 {
		return model.TurnResponse{}, errors.New("provider returned empty assistant message")
	}
	if decoded.Usage.PromptTokens < 0 || decoded.Usage.CompletionTokens < 0 || decoded.Usage.TotalTokens < 0 {
		return model.TurnResponse{}, errors.New("provider returned negative token usage")
	}
	return model.TurnResponse{
		ProviderResponseID: decoded.ID,
		Model:              decoded.Model,
		Message:            out,
		FinishReason:       choice.FinishReason,
		Usage: model.Usage{
			InputTokens:  decoded.Usage.PromptTokens,
			OutputTokens: decoded.Usage.CompletionTokens,
			TotalTokens:  decoded.Usage.TotalTokens,
		},
		ProviderRequestID: providerRequestID,
	}, nil
}

func readBounded(r io.Reader, limit int64) ([]byte, bool, error) {
	lr := io.LimitReader(r, limit+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, false, err
	}
	return body, int64(len(body)) > limit, nil
}

func boundedErrorText(body []byte, max int) string {
	text := strings.TrimSpace(string(body))
	if len(text) > max {
		text = text[:max] + "..."
	}
	return text
}

func (c *Client) Endpoint() string { return c.endpoint }

func ParseRetryAfterSeconds(v string) (time.Duration, bool) {
	seconds, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}
