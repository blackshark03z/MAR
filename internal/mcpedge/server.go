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
	ReadProjectFile(context.Context, string, string) (service.ProjectReadResult, error)
	ProjectContext(context.Context, string) ([]service.ProjectContextItem, error)
}

type submitArgs struct {
	IdempotencyKey string              `json:"idempotency_key" jsonschema:"stable idempotency key for this submission"`
	Contract       domain.GoalContract `json:"contract" jsonschema:"immutable MAR Goal Contract"`
}

func validatePublicVerificationProfile(profile string) error {
	profile = strings.TrimSpace(profile)
	switch profile {
	case "go-standard", "go-docs", "python-standard":
		return nil
	case "":
		return errors.New("verification_profile is required")
	default:
		return fmt.Errorf("verification_profile %q is unsupported; must be go-standard, go-docs, or python-standard", profile)
	}
}

type taskArgs struct {
	TaskID string `json:"task_id" jsonschema:"MAR durable task id"`
}

type projectArgs struct {
	Operation string `json:"operation" jsonschema:"context or read"`
	ProjectID string `json:"project_id,omitempty"`
	Path      string `json:"path,omitempty"`
}

type taskDomainArgs struct {
	Operation string `json:"operation" jsonschema:"status, result, or inspect"`
	TaskID    string `json:"task_id" jsonschema:"MAR durable task id"`
}

type controlArgs struct {
	Operation      string           `json:"operation" jsonschema:"steer, input, or cancel"`
	TaskID         string           `json:"task_id"`
	IdempotencyKey string           `json:"idempotency_key"`
	Kind           domain.SteerKind `json:"kind,omitempty" jsonschema:"context, priority, blocked_choice, additional_verification, or cancel when operation is steer"`
	Message        string           `json:"message,omitempty"`
	Reason         string           `json:"reason,omitempty"`
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
	server.AddReceivingMiddleware(legacyToolAliasMiddleware())

	mcp.AddTool(server, &mcp.Tool{Name: "project", Description: "Read registered project context or one bounded project file. Use operation=context or operation=read."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args projectArgs) (*mcp.CallToolResult, map[string]any, error) {
			value, err := callProject(ctx, backend, args)
			if err != nil {
				return nil, nil, err
			}
			return nil, value, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "submit", Description: "Submit one immutable MAR Goal Contract for coding or mutation work. Resolve technical project fields with project operation=context first when needed."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args submitArgs) (*mcp.CallToolResult, map[string]any, error) {
			if err := validatePublicVerificationProfile(args.Contract.VerificationProfile); err != nil {
				return nil, nil, err
			}
			task, created, err := backend.Submit(ctx, args.IdempotencyKey, args.Contract)
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"created": created, "task": compactTaskReceipt(task)}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "task", Description: "Read bounded durable task state. Use operation=status, result, or inspect."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args taskDomainArgs) (*mcp.CallToolResult, map[string]any, error) {
			value, err := callTask(ctx, backend, args)
			if err != nil {
				return nil, nil, err
			}
			return nil, value, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "control", Description: "Apply one durable owner control. Use operation=steer, input, or cancel."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args controlArgs) (*mcp.CallToolResult, map[string]any, error) {
			value, err := callControl(ctx, backend, args)
			if err != nil {
				return nil, nil, err
			}
			return nil, value, nil
		})
	addRawTaskReadTool(server, "brain_turn", "Read the pending durable GPT Web brain turn for one task.", func(ctx context.Context, taskID string) (any, error) {
		turn, available, err := backend.PendingWebTurn(ctx, taskID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"available": available, "turn": turn}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "brain_respond", Description: "Return one response for the exact pending GPT Web brain turn; coding tools execute later inside the worker sandbox."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args brainRespondArgs) (*mcp.CallToolResult, map[string]any, error) {
			turn, created, err := backend.RespondWebTurn(ctx, args.TaskID, args.TurnID, model.Message{Role: model.RoleAssistant, Content: args.Content, ToolCalls: args.ToolCalls}, args.FinishReason)
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"created": created, "turn": compactWebTurnReceipt(turn)}, nil
		})

	return server, nil
}

func callProject(ctx context.Context, backend Backend, args projectArgs) (map[string]any, error) {
	switch strings.ToLower(strings.TrimSpace(args.Operation)) {
	case "context":
		items, err := backend.ProjectContext(ctx, strings.TrimSpace(args.ProjectID))
		if err != nil {
			return nil, err
		}
		return map[string]any{"projects": items}, nil
	case "read":
		result, err := backend.ReadProjectFile(ctx, strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.Path))
		if err != nil {
			return nil, err
		}
		return map[string]any{"file": result}, nil
	default:
		return nil, errors.New("project operation must be context or read")
	}
}

func callTask(ctx context.Context, backend Backend, args taskDomainArgs) (map[string]any, error) {
	taskID := strings.TrimSpace(args.TaskID)
	if taskID == "" {
		return nil, errors.New("task_id is required")
	}
	switch strings.ToLower(strings.TrimSpace(args.Operation)) {
	case "status":
		status, err := backend.StatusSnapshot(ctx, taskID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": compactStatusReceipt(status)}, nil
	case "result":
		result, available, err := backend.Result(ctx, taskID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"available": available, "result": result}, nil
	case "inspect":
		inspection, err := backend.Inspect(ctx, taskID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"inspection": inspection}, nil
	default:
		return nil, errors.New("task operation must be status, result, or inspect")
	}
}

func callControl(ctx context.Context, backend Backend, args controlArgs) (map[string]any, error) {
	taskID := strings.TrimSpace(args.TaskID)
	key := strings.TrimSpace(args.IdempotencyKey)
	if taskID == "" || key == "" {
		return nil, errors.New("task_id and idempotency_key are required")
	}
	switch strings.ToLower(strings.TrimSpace(args.Operation)) {
	case "steer":
		control, created, err := backend.Steer(ctx, taskID, key, domain.SteerPayload{Kind: args.Kind, Message: args.Message})
		if err != nil {
			return nil, err
		}
		return map[string]any{"created": created, "control": control}, nil
	case "input":
		control, created, err := backend.Input(ctx, taskID, key, domain.InputPayload{Message: args.Message})
		if err != nil {
			return nil, err
		}
		return map[string]any{"created": created, "control": control}, nil
	case "cancel":
		control, created, err := backend.Cancel(ctx, taskID, key, domain.CancelPayload{Reason: args.Reason})
		if err != nil {
			return nil, err
		}
		return map[string]any{"created": created, "control": control}, nil
	default:
		return nil, errors.New("control operation must be steer, input, or cancel")
	}
}

func legacyToolAliasMiddleware() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			call, ok := req.(*mcp.CallToolRequest)
			if !ok || call.Params == nil {
				return next(ctx, method, req)
			}
			canonical, operation, ok := legacyToolAlias(call.Params.Name)
			if !ok {
				return next(ctx, method, req)
			}
			var args map[string]any
			if len(call.Params.Arguments) != 0 {
				if err := json.Unmarshal(call.Params.Arguments, &args); err != nil {
					return nil, fmt.Errorf("decode legacy tool arguments: %w", err)
				}
			}
			if args == nil {
				args = map[string]any{}
			}
			args["operation"] = operation
			raw, err := json.Marshal(args)
			if err != nil {
				return nil, fmt.Errorf("encode legacy tool arguments: %w", err)
			}
			call.Params.Name = canonical
			call.Params.Arguments = raw
			return next(ctx, method, req)
		}
	}
}

func legacyToolAlias(name string) (canonical, operation string, ok bool) {
	switch strings.TrimSpace(name) {
	case "project_context":
		return "project", "context", true
	case "project_read":
		return "project", "read", true
	case "status":
		return "task", "status", true
	case "result":
		return "task", "result", true
	case "inspect":
		return "task", "inspect", true
	case "steer":
		return "control", "steer", true
	case "input":
		return "control", "input", true
	case "cancel":
		return "control", "cancel", true
	default:
		return "", "", false
	}
}

func compactTaskReceipt(task domain.Task) map[string]any {
	return map[string]any{
		"task_id": task.ID, "state": task.State, "run_epoch": task.RunEpoch,
		"created_at": task.CreatedAt, "updated_at": task.UpdatedAt,
	}
}

func compactStatusReceipt(status service.TaskStatusSnapshot) map[string]any {
	receipt := map[string]any{
		"task_id": status.Task.ID, "state": status.Task.State, "run_epoch": status.Task.RunEpoch,
		"cancel_requested": status.CancelRequested, "brain_turn_available": status.BrainTurnAvailable,
		"detail": status.Detail, "next_action": status.NextAction, "updated_at": status.Task.UpdatedAt,
	}
	if status.LatestControl != nil {
		receipt["latest_control"] = map[string]any{"control_id": status.LatestControl.ID, "version": status.LatestControl.Version, "kind": status.LatestControl.Kind}
	}
	return receipt
}

func compactWebTurnReceipt(turn domain.WebTurn) map[string]any {
	return map[string]any{
		"task_id": turn.TaskID, "turn_id": turn.ID, "attempt_id": turn.AttemptID, "run_epoch": turn.RunEpoch,
		"request_id": turn.RequestID, "response_hash": turn.ResponseHash, "responded_at": turn.RespondedAt,
	}
}

func addRawTaskReadTool(server *mcp.Server, name, description string, read func(context.Context, string) (any, error)) {
	inputSchema := map[string]any{
		"type": "object", "properties": map[string]any{"task_id": map[string]any{"type": "string"}},
		"required": []string{"task_id"}, "additionalProperties": false,
	}
	server.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: inputSchema, OutputSchema: map[string]any{"type": "object"}},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, StructuredContent: json.RawMessage(payload)}, nil
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
