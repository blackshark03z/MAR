package mcpedge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mar/internal/domain"
	"mar/internal/model"
	"mar/internal/service"
)

const serverVersion = "0.1.0"

type Backend interface {
	Submit(context.Context, string, domain.GoalContract) (domain.Task, bool, error)
	StatusSnapshot(context.Context, string) (service.TaskStatusSnapshot, error)
	Steer(context.Context, string, string, domain.SteerPayload) (domain.TaskControl, bool, error)
	Input(context.Context, string, string, domain.InputPayload) (domain.TaskControl, bool, error)
	Cancel(context.Context, string, string, domain.CancelPayload) (domain.TaskControl, bool, error)
	Result(context.Context, string) (domain.TaskResult, bool, error)
	Inspect(context.Context, string) (service.TaskInspection, error)
	PendingWebTurn(context.Context, string) (domain.WebTurn, bool, error)
	RespondWebTurn(context.Context, string, string, model.Message, string) (domain.WebTurn, bool, error)
}

type submitArgs struct {
	IdempotencyKey string              `json:"idempotency_key" jsonschema:"stable idempotency key for this submission"`
	Contract       domain.GoalContract `json:"contract" jsonschema:"immutable MAR Goal Contract"`
}

type taskArgs struct {
	TaskID string `json:"task_id" jsonschema:"MAR durable task id"`
}

type steerArgs struct {
	TaskID         string           `json:"task_id"`
	IdempotencyKey string           `json:"idempotency_key"`
	Kind           domain.SteerKind `json:"kind" jsonschema:"context, priority, blocked_choice, additional_verification, or cancel"`
	Message        string           `json:"message"`
}

type inputArgs struct {
	TaskID         string `json:"task_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Message        string `json:"message"`
}

type cancelArgs struct {
	TaskID         string `json:"task_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Reason         string `json:"reason,omitempty"`
}

type brainRespondArgs struct {
	TaskID       string           `json:"task_id"`
	TurnID       string           `json:"turn_id"`
	Content      string           `json:"content,omitempty"`
	ToolCalls    []model.ToolCall `json:"tool_calls,omitempty"`
	FinishReason string           `json:"finish_reason,omitempty"`
}

func NewServer(backend Backend) (*mcp.Server, error) {
	if backend == nil {
		return nil, errors.New("MCP backend is required")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "mar", Version: serverVersion}, nil)

	mcp.AddTool(server, &mcp.Tool{Name: "submit", Description: "Submit one immutable MAR Goal Contract and return its durable task handle."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args submitArgs) (*mcp.CallToolResult, map[string]any, error) {
			task, created, err := backend.Submit(ctx, args.IdempotencyKey, args.Contract)
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"created": created, "task": task}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "status", Description: "Read the bounded durable status of one MAR task."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args taskArgs) (*mcp.CallToolResult, map[string]any, error) {
			status, err := backend.StatusSnapshot(ctx, args.TaskID)
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"status": status}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "steer", Description: "Add bounded factual context, priority clarification, a blocked choice, request additional verification, or request cancellation without rewriting the Goal Contract."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args steerArgs) (*mcp.CallToolResult, map[string]any, error) {
			control, created, err := backend.Steer(ctx, args.TaskID, args.IdempotencyKey, domain.SteerPayload{Kind: args.Kind, Message: args.Message})
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"created": created, "control": control}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "input", Description: "Provide bounded user input only to a task that is explicitly INPUT_REQUIRED."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args inputArgs) (*mcp.CallToolResult, map[string]any, error) {
			control, created, err := backend.Input(ctx, args.TaskID, args.IdempotencyKey, domain.InputPayload{Message: args.Message})
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"created": created, "control": control}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "cancel", Description: "Durably request task cancellation. Running attempts are logically fenced immediately; physical termination remains supervisor-owned."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args cancelArgs) (*mcp.CallToolResult, map[string]any, error) {
			control, created, err := backend.Cancel(ctx, args.TaskID, args.IdempotencyKey, domain.CancelPayload{Reason: args.Reason})
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"created": created, "control": control}, nil
		})
	addRawTaskReadTool(server, "result", "Read the latest durable revision-bound TaskResult for one MAR task.", func(ctx context.Context, taskID string) (any, error) {
		result, available, err := backend.Result(ctx, taskID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"available": available, "result": result}, nil
	})
	addRawTaskReadTool(server, "inspect", "Inspect a bounded durable task projection: task, workspace, attempt, checkpoint, pending Web brain turn, result/evidence, and recent control commands.", func(ctx context.Context, taskID string) (any, error) {
		inspection, err := backend.Inspect(ctx, taskID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"inspection": inspection}, nil
	})
	addRawTaskReadTool(server, "brain_turn", "Read the pending durable model turn when MAR is using GPT Web as the coding brain. Reason over the returned messages and offered tools, then answer it with brain_respond. This does not execute coding tools in the MCP process.", func(ctx context.Context, taskID string) (any, error) {
		turn, available, err := backend.PendingWebTurn(ctx, taskID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"available": available, "turn": turn}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "brain_respond", Description: "Return one GPT Web assistant/model response for the exact pending MAR brain turn. Tool calls are executed later inside the weaker worker sandbox, never in the MCP/daemon process."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args brainRespondArgs) (*mcp.CallToolResult, map[string]any, error) {
			turn, created, err := backend.RespondWebTurn(ctx, args.TaskID, args.TurnID, model.Message{Role: model.RoleAssistant, Content: args.Content, ToolCalls: args.ToolCalls}, args.FinishReason)
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"created": created, "turn": turn}, nil
		})

	return server, nil
}

func addRawTaskReadTool(server *mcp.Server, name, description string, read func(context.Context, string) (any, error)) {
	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string"},
		},
		"required":             []string{"task_id"},
		"additionalProperties": false,
	}
	server.AddTool(&mcp.Tool{
		Name:         name,
		Description:  description,
		InputSchema:  inputSchema,
		OutputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if req == nil || req.Params == nil {
			return rawToolError(errors.New("tool request parameters are required")), nil
		}
		var args taskArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return rawToolError(fmt.Errorf("decode tool arguments: %w", err)), nil
		}
		args.TaskID = strings.TrimSpace(args.TaskID)
		if args.TaskID == "" {
			return rawToolError(errors.New("task_id is required")), nil
		}
		value, err := read(ctx, args.TaskID)
		if err != nil {
			return rawToolError(err), nil
		}
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("marshal %s tool result: %w", name, err)
		}
		if !json.Valid(payload) {
			return nil, fmt.Errorf("marshal %s tool result produced invalid JSON", name)
		}
		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: string(payload)}},
			StructuredContent: json.RawMessage(payload),
		}, nil
	})
}

func rawToolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}
}

func RunStdio(ctx context.Context, backend Backend) error {
	server, err := NewServer(backend)
	if err != nil {
		return err
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}
