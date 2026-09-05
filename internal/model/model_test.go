package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"mar/internal/model"
)

type fakeProvider struct {
	calls int
	resp  model.TurnResponse
	err   error
}

func (f *fakeProvider) Turn(context.Context, model.TurnRequest) (model.TurnResponse, error) {
	f.calls++
	return f.resp, f.err
}

func TestGatewayValidatesBeforeProviderCall(t *testing.T) {
	provider := &fakeProvider{}
	gateway, err := model.NewGateway(provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = gateway.Turn(context.Background(), model.TurnRequest{RequestID: "req-1", Model: "m"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if provider.calls != 0 {
		t.Fatal("provider called for invalid request")
	}
}

func TestValidToolConversationPassesValidation(t *testing.T) {
	req := model.TurnRequest{
		RequestID: "req-2",
		Model:     "m",
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "code safely"},
			{Role: model.RoleUser, Content: "read file"},
			{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
			{Role: model.RoleTool, ToolCallID: "call-1", Content: "package main"},
		},
		Tools: []model.ToolDefinition{{Name: "read_file", Parameters: json.RawMessage(`{"type":"object"}`), Strict: true}},
	}
	if err := model.ValidateTurnRequest(req); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayReturnsProviderErrorWithoutRetry(t *testing.T) {
	want := errors.New("provider failed")
	provider := &fakeProvider{err: want}
	gateway, _ := model.NewGateway(provider)
	_, err := gateway.Turn(context.Background(), model.TurnRequest{RequestID: "req", Model: "m", Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}}})
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("gateway retried provider: %d calls", provider.calls)
	}
}
