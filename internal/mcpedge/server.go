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
	SearchProjectText(context.Context, string, string, string, int) (service.ProjectSearchResult, error)
	BuildProjectContextBatch(context.Context, string, string, int, int, int) (service.ProjectContextBatchResult, error)
	FindProjectFiles(context.Context, string, string, int) (service.ProjectFindResult, error)
	ProjectGitStatus(context.Context, string) (service.ProjectGitStatusResult, error)
	ProjectGitDiff(context.Context, string, string) (service.ProjectGitDiffResult, error)
	WriteProjectFile(context.Context, service.ProjectWriteRequest) (service.ProjectWriteResult, error)
	ApplyProjectPatch(context.Context, service.ProjectPatchRequest) (service.ProjectPatchResult, error)
	CreateProjectDirectory(context.Context, string, string) (service.ProjectFSActionResult, error)
	RemoveProjectFile(context.Context, string, string, string) (service.ProjectFSActionResult, error)
	RenameProjectFile(context.Context, string, string, string, string) (service.ProjectFSActionResult, error)
	RunProjectCommand(context.Context, string, string, []string, string, int, int) (service.ProjectCommandResult, error)
	RunProjectCommands(context.Context, string, []service.ProjectVerifyCommand) (service.ProjectCommandBatchResult, error)
	StageProjectPaths(context.Context, string, []string) (service.ProjectGitActionResult, error)
	CommitProject(context.Context, string, string) (service.ProjectGitActionResult, error)
	PushProject(context.Context, string, string) (service.ProjectGitActionResult, error)
	ListProjectBranches(context.Context, string) (service.ProjectBranchListResult, error)
	CreateProjectBranch(context.Context, string, string) (service.ProjectBranchResult, error)
	CreateProjectWorktree(context.Context, string, string, string) (service.ProjectWorktreeResult, error)
	ApplyAndVerifyProject(context.Context, string, []service.ProjectOwnedChange, []service.ProjectVerifyCommand) (service.ProjectApplyVerifyResult, error)
	ListProjectDirectory(context.Context, string, string, int) (service.ProjectListResult, error)
	AttachLocalPathWithNetwork(context.Context, string, bool) (service.ProjectAttachResult, error)
	DetachLocalProject(context.Context, string) (service.ProjectDetachResult, error)
	ProjectContext(context.Context, string) ([]service.ProjectContextItem, error)
}

type submitArgs struct {
	IdempotencyKey string              `json:"idempotency_key" jsonschema:"stable idempotency key for this submission"`
	Contract       domain.GoalContract `json:"contract" jsonschema:"immutable MAR Goal Contract"`
}

func validatePublicVerificationProfile(ctx context.Context, backend Backend, contract domain.GoalContract) error {
	profile := strings.TrimSpace(contract.VerificationProfile)
	if profile == "" {
		return errors.New("verification_profile is required")
	}
	projectID := strings.TrimSpace(contract.ProjectID)
	if projectID == "" {
		return errors.New("project_id is required")
	}
	items, err := backend.ProjectContext(ctx, projectID)
	if err != nil {
		return fmt.Errorf("resolve project verification capability: %w", err)
	}
	if len(items) != 1 {
		return fmt.Errorf("project %q capability resolution returned %d matches", projectID, len(items))
	}
	supported := items[0].Capability.SupportedVerificationProfiles
	for _, allowed := range supported {
		if profile == strings.TrimSpace(allowed) {
			return nil
		}
	}
	if len(supported) == 0 {
		return fmt.Errorf("project %q advertises no supported verification profiles", projectID)
	}
	return fmt.Errorf("verification_profile %q is unsupported for project %q; supported profiles: %s", profile, projectID, strings.Join(supported, ", "))
}

type cognitionDeltaBackend interface {
	CognitionDelta(context.Context, domain.WebTurn, string) (service.CognitionDelta, error)
}

type automaticCognitionDeltaBackend interface {
	AutomaticCognitionDelta(context.Context, domain.WebTurn) (service.CognitionDelta, error)
}

type brainTurnArgs struct {
	TaskID          string `json:"task_id" jsonschema:"MAR durable task id"`
	ResponseMode    string `json:"response_mode,omitempty" jsonschema:"compat (default) or structured"`
	ContextMode     string `json:"context_mode,omitempty" jsonschema:"full (default) or delta; delta requires structured response mode"`
	CognitionCursor string `json:"cognition_cursor,omitempty" jsonschema:"opaque cursor from a prior brain_turn delta/full view"`
}

type brainTurnFastArgs struct {
	TaskID string `json:"task_id" jsonschema:"MAR durable task id"`
}

type projectReadRange struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type projectArgs struct {
	Operation        string             `json:"operation" jsonschema:"context, context_batch, read, read_many, find, search, git_status, git_diff, attach, detach, or list"`
	ProjectID        string             `json:"project_id,omitempty"`
	Path             string             `json:"path,omitempty"`
	NetworkAllowed   bool               `json:"network_allowed,omitempty" jsonschema:"for attach, explicitly allow trusted-owner host commands; omitted keeps network denied"`
	Query            string             `json:"query,omitempty"`
	StartLine        int                `json:"start_line,omitempty" jsonschema:"1-based first line for bounded read; omitted means full file"`
	EndLine          int                `json:"end_line,omitempty" jsonschema:"inclusive last line for bounded read; 0 means through EOF"`
	Reads            []projectReadRange `json:"reads,omitempty" jsonschema:"1..16 bounded file/range reads for read_many"`
	MaxEntries       int                `json:"max_entries,omitempty" jsonschema:"bounded directory entry cap for list"`
	MaxResults       int                `json:"max_results,omitempty" jsonschema:"bounded text-match cap for search"`
	MaxBytes         int                `json:"max_bytes,omitempty" jsonschema:"bounded snippet budget for context_batch"`
	IncludeGitStatus bool               `json:"include_git_status,omitempty" jsonschema:"for context_batch, include bounded git status in the same response"`
}

type actionChangeArgs struct {
	Path           string  `json:"path"`
	ExpectedSHA256 string  `json:"expected_sha256"`
	Search         string  `json:"search,omitempty"`
	Replacement    string  `json:"replacement,omitempty"`
	ExpectedCount  int     `json:"expected_count,omitempty"`
	Content        *string `json:"content,omitempty"`
}

type actionVerifyArgs struct {
	Executable     string   `json:"executable"`
	Args           []string `json:"args,omitempty"`
	Cwd            string   `json:"cwd,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	MaxOutputBytes int      `json:"max_output_bytes,omitempty"`
}

type actionArgs struct {
	Operation      string             `json:"operation" jsonschema:"write, patch, mkdir, remove, rename, run, run_many, apply_and_verify, git_branch_list, git_branch_create, git_worktree_create, git_stage, git_stage_commit, git_commit, or git_push"`
	ProjectID      string             `json:"project_id"`
	Path           string             `json:"path,omitempty"`
	Destination    string             `json:"destination,omitempty"`
	ExpectedSHA256 string             `json:"expected_sha256,omitempty"`
	Search         string             `json:"search,omitempty"`
	Replacement    string             `json:"replacement,omitempty"`
	Content        string             `json:"content,omitempty"`
	ExpectedCount  int                `json:"expected_count,omitempty"`
	Executable     string             `json:"executable,omitempty"`
	Args           []string           `json:"args,omitempty"`
	Cwd            string             `json:"cwd,omitempty"`
	TimeoutSeconds int                `json:"timeout_seconds,omitempty"`
	MaxOutputBytes int                `json:"max_output_bytes,omitempty"`
	Paths          []string           `json:"paths,omitempty"`
	Message        string             `json:"message,omitempty"`
	Remote         string             `json:"remote,omitempty"`
	Branch         string             `json:"branch,omitempty"`
	Baseline       string             `json:"baseline,omitempty"`
	Purpose        string             `json:"purpose,omitempty"`
	Changes        []actionChangeArgs `json:"changes,omitempty"`
	Verify         []actionVerifyArgs `json:"verify,omitempty"`
	Commands       []actionVerifyArgs `json:"commands,omitempty"`
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

	mcp.AddTool(server, &mcp.Tool{Name: "project", Description: "Attach a local path for research, inspect registered project context, build one lightweight context_batch from existing find/search/read primitives, read one or many bounded file ranges, find paths, search text, inspect bounded Git status/diff, or list one bounded directory. For a Git attach that needs trusted-owner host verification, set network_allowed=true explicitly; omission remains fail-closed. Use operation=context, context_batch, read, read_many, find, search, git_status, git_diff, attach, detach, or list."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args projectArgs) (*mcp.CallToolResult, map[string]any, error) {
			value, err := callProject(ctx, backend, args)
			if err != nil {
				return nil, nil, err
			}
			return nil, value, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "action", Description: "Trusted Owner Fast Path for ordinary development without creating a MAR task. Use operation=write, patch, mkdir, remove, rename, run, run_many, apply_and_verify, git_branch_list, git_branch_create, git_worktree_create, git_stage, git_stage_commit, git_commit, or git_push. Governed submit/task remains available for high-assurance work."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args actionArgs) (*mcp.CallToolResult, map[string]any, error) {
			value, err := callAction(ctx, backend, args)
			if err != nil {
				return nil, nil, err
			}
			return nil, value, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "submit", Description: "Submit one immutable MAR Goal Contract for coding or mutation work. Resolve project fields with project operation=context first and use its capability/profile guidance instead of guessing verification_profile; submitted contracts still require one concrete supported profile."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args submitArgs) (*mcp.CallToolResult, map[string]any, error) {
			if err := validatePublicVerificationProfile(ctx, backend, args.Contract); err != nil {
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
	addBrainTurnTool(server, backend)
	if _, ok := backend.(automaticCognitionDeltaBackend); ok {
		addBrainTurnFastTool(server, backend)
	}
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
	case "context_batch":
		projectID := strings.TrimSpace(args.ProjectID)
		result, err := backend.BuildProjectContextBatch(ctx, projectID, args.Query, args.MaxResults, args.MaxEntries, args.MaxBytes)
		if err != nil {
			return nil, err
		}
		response := map[string]any{"context_batch": result}
		if args.IncludeGitStatus {
			status, err := backend.ProjectGitStatus(ctx, projectID)
			if err != nil {
				return nil, err
			}
			response["git_status"] = status
		}
		return response, nil
	case "read":
		result, err := backend.ReadProjectFile(ctx, strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.Path))
		if err != nil {
			return nil, err
		}
		result, err = boundedProjectReadRange(result, args.StartLine, args.EndLine)
		if err != nil {
			return nil, err
		}
		return map[string]any{"file": result}, nil
	case "read_many":
		if len(args.Reads) == 0 || len(args.Reads) > 16 {
			return nil, errors.New("project read_many requires 1..16 reads")
		}
		files := make([]service.ProjectReadResult, 0, len(args.Reads))
		for _, read := range args.Reads {
			result, err := backend.ReadProjectFile(ctx, strings.TrimSpace(args.ProjectID), strings.TrimSpace(read.Path))
			if err != nil {
				return nil, err
			}
			result, err = boundedProjectReadRange(result, read.StartLine, read.EndLine)
			if err != nil {
				return nil, err
			}
			files = append(files, result)
		}
		return map[string]any{"files": files}, nil
	case "find":
		result, err := backend.FindProjectFiles(ctx, strings.TrimSpace(args.ProjectID), args.Query, args.MaxResults)
		if err != nil {
			return nil, err
		}
		return map[string]any{"find": result}, nil
	case "search":
		result, err := backend.SearchProjectText(ctx, strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.Path), args.Query, args.MaxResults)
		if err != nil {
			return nil, err
		}
		return map[string]any{"search": result}, nil
	case "git_status":
		result, err := backend.ProjectGitStatus(ctx, strings.TrimSpace(args.ProjectID))
		if err != nil {
			return nil, err
		}
		return map[string]any{"git_status": result}, nil
	case "git_diff":
		result, err := backend.ProjectGitDiff(ctx, strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.Path))
		if err != nil {
			return nil, err
		}
		return map[string]any{"git_diff": result}, nil
	case "attach":
		result, err := backend.AttachLocalPathWithNetwork(ctx, strings.TrimSpace(args.Path), args.NetworkAllowed)
		if err != nil {
			return nil, err
		}
		return map[string]any{"attachment": result}, nil
	case "detach":
		result, err := backend.DetachLocalProject(ctx, strings.TrimSpace(args.ProjectID))
		if err != nil {
			return nil, err
		}
		return map[string]any{"detachment": result}, nil
	case "list":
		result, err := backend.ListProjectDirectory(ctx, strings.TrimSpace(args.ProjectID), strings.TrimSpace(args.Path), args.MaxEntries)
		if err != nil {
			return nil, err
		}
		return map[string]any{"directory": result}, nil
	default:
		return nil, errors.New("project operation must be context, context_batch, read, read_many, find, search, git_status, git_diff, attach, detach, or list")
	}
}

func callAction(ctx context.Context, backend Backend, args actionArgs) (map[string]any, error) {
	projectID := strings.TrimSpace(args.ProjectID)
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}
	switch strings.ToLower(strings.TrimSpace(args.Operation)) {
	case "write":
		result, err := backend.WriteProjectFile(ctx, service.ProjectWriteRequest{ProjectID: projectID, Path: args.Path, ExpectedSHA256: args.ExpectedSHA256, Content: args.Content})
		if err != nil {
			return nil, err
		}
		return map[string]any{"write": result}, nil
	case "patch":
		result, err := backend.ApplyProjectPatch(ctx, service.ProjectPatchRequest{ProjectID: projectID, Path: args.Path, ExpectedSHA256: args.ExpectedSHA256, Search: args.Search, Replacement: args.Replacement, ExpectedCount: args.ExpectedCount})
		if err != nil {
			return nil, err
		}
		return map[string]any{"patch": result}, nil
	case "mkdir":
		result, err := backend.CreateProjectDirectory(ctx, projectID, args.Path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"mkdir": result}, nil
	case "remove":
		result, err := backend.RemoveProjectFile(ctx, projectID, args.Path, args.ExpectedSHA256)
		if err != nil {
			return nil, err
		}
		return map[string]any{"remove": result}, nil
	case "rename":
		destination := strings.TrimSpace(args.Destination)
		if destination == "" {
			destination = strings.TrimSpace(args.Replacement)
		}
		result, err := backend.RenameProjectFile(ctx, projectID, args.Path, destination, args.ExpectedSHA256)
		if err != nil {
			return nil, err
		}
		return map[string]any{"rename": result}, nil
	case "run":
		result, err := backend.RunProjectCommand(ctx, projectID, args.Executable, args.Args, args.Cwd, args.TimeoutSeconds, args.MaxOutputBytes)
		if err != nil {
			return nil, err
		}
		return map[string]any{"run": result}, nil
	case "run_many":
		commands := make([]service.ProjectVerifyCommand, 0, len(args.Commands))
		for _, command := range args.Commands {
			commands = append(commands, service.ProjectVerifyCommand{Executable: command.Executable, Args: command.Args, Cwd: command.Cwd, TimeoutSeconds: command.TimeoutSeconds, MaxOutputBytes: command.MaxOutputBytes})
		}
		result, err := backend.RunProjectCommands(ctx, projectID, commands)
		if err != nil {
			return nil, err
		}
		return map[string]any{"run_many": result}, nil
	case "apply_and_verify":
		changes := make([]service.ProjectOwnedChange, 0, len(args.Changes))
		for _, change := range args.Changes {
			changes = append(changes, service.ProjectOwnedChange{
				Path: change.Path, ExpectedSHA256: change.ExpectedSHA256, Search: change.Search,
				Replacement: change.Replacement, ExpectedCount: change.ExpectedCount, Content: change.Content,
			})
		}
		verification := make([]service.ProjectVerifyCommand, 0, len(args.Verify))
		for _, task := range args.Verify {
			verification = append(verification, service.ProjectVerifyCommand{
				Executable: task.Executable, Args: task.Args, Cwd: task.Cwd,
				TimeoutSeconds: task.TimeoutSeconds, MaxOutputBytes: task.MaxOutputBytes,
			})
		}
		result, err := backend.ApplyAndVerifyProject(ctx, projectID, changes, verification)
		if err != nil {
			return nil, err
		}
		return map[string]any{"apply_and_verify": result}, nil
	case "git_branch_list":
		result, err := backend.ListProjectBranches(ctx, projectID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"git_branch_list": result}, nil
	case "git_branch_create":
		result, err := backend.CreateProjectBranch(ctx, projectID, args.Branch)
		if err != nil {
			return nil, err
		}
		return map[string]any{"git_branch_create": result}, nil
	case "git_worktree_create":
		result, err := backend.CreateProjectWorktree(ctx, projectID, args.Baseline, args.Purpose)
		if err != nil {
			return nil, err
		}
		return map[string]any{"git_worktree_create": result}, nil
	case "git_stage":
		result, err := backend.StageProjectPaths(ctx, projectID, args.Paths)
		if err != nil {
			return nil, err
		}
		return map[string]any{"git_stage": result}, nil
	case "git_stage_commit":
		staged, err := backend.StageProjectPaths(ctx, projectID, args.Paths)
		if err != nil {
			return nil, err
		}
		committed, err := backend.CommitProject(ctx, projectID, args.Message)
		if err != nil {
			return nil, err
		}
		committed.Operation = "git_stage_commit"
		committed.Paths = append([]string(nil), staged.Paths...)
		return map[string]any{"git_stage_commit": committed}, nil
	case "git_commit":
		result, err := backend.CommitProject(ctx, projectID, args.Message)
		if err != nil {
			return nil, err
		}
		return map[string]any{"git_commit": result}, nil
	case "git_push":
		result, err := backend.PushProject(ctx, projectID, args.Remote)
		if err != nil {
			return nil, err
		}
		return map[string]any{"git_push": result}, nil
	default:
		return nil, errors.New("action operation must be write, patch, mkdir, remove, rename, run, run_many, apply_and_verify, git_branch_list, git_branch_create, git_worktree_create, git_stage, git_stage_commit, git_commit, or git_push")
	}
}

func boundedProjectReadRange(result service.ProjectReadResult, startLine, endLine int) (service.ProjectReadResult, error) {
	if startLine <= 0 && endLine <= 0 {
		return result, nil
	}
	if startLine <= 0 {
		startLine = 1
	}
	if endLine < 0 || (endLine > 0 && endLine < startLine) {
		return service.ProjectReadResult{}, errors.New("invalid project read line range")
	}
	lines := strings.SplitAfter(result.Content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		if startLine > 1 {
			return service.ProjectReadResult{}, errors.New("start_line exceeds file line count")
		}
		result.StartLine = 1
		result.EndLine = 0
		return result, nil
	}
	if startLine > len(lines) {
		return service.ProjectReadResult{}, errors.New("start_line exceeds file line count")
	}
	last := len(lines)
	if endLine > 0 && endLine < last {
		last = endLine
	}
	result.Content = strings.Join(lines[startLine-1:last], "")
	result.StartLine = startLine
	result.EndLine = last
	result.Truncated = startLine > 1 || last < len(lines)
	return result, nil
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

func addBrainTurnTool(server *mcp.Server, backend Backend) {
	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string"},
			"response_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"compat", "structured"},
				"description": "compat (default) repeats the full JSON in TextContent and StructuredContent; structured keeps the exact JSON only in StructuredContent and emits a compact text receipt",
			},
			"context_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"full", "delta"},
				"description": "full (default) returns the exact pending WebTurn; delta is opt-in and requires structured mode",
			},
			"cognition_cursor": map[string]any{
				"type":        "string",
				"description": "opaque non-authoritative cursor from a prior cognition view; missing or stale cursor falls back to full current state",
			},
		},
		"required":             []string{"task_id"},
		"additionalProperties": false,
	}
	server.AddTool(&mcp.Tool{Name: "brain_turn", Description: "Read the pending durable external-cognition turn for one task. Prefer structured+delta for modern MCP clients that support StructuredContent: use response_mode=structured, context_mode=delta, and carry cognition_cursor between turns; a missing or stale cursor safely falls back to full current state. compat/full remain defaults for compatibility.", InputSchema: inputSchema, OutputSchema: map[string]any{"type": "object"}},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if req == nil || req.Params == nil {
				return rawToolError(errors.New("tool request parameters are required")), nil
			}
			var args brainTurnArgs
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return rawToolError(fmt.Errorf("decode tool arguments: %w", err)), nil
			}
			args.TaskID = strings.TrimSpace(args.TaskID)
			if args.TaskID == "" {
				return rawToolError(errors.New("task_id is required")), nil
			}
			mode := strings.ToLower(strings.TrimSpace(args.ResponseMode))
			if mode == "" {
				mode = "compat"
			}
			if mode != "compat" && mode != "structured" {
				return rawToolError(fmt.Errorf("brain_turn response_mode %q is unsupported; must be compat or structured", args.ResponseMode)), nil
			}
			contextMode := strings.ToLower(strings.TrimSpace(args.ContextMode))
			if contextMode == "" {
				contextMode = "full"
			}
			if contextMode != "full" && contextMode != "delta" {
				return rawToolError(fmt.Errorf("brain_turn context_mode %q is unsupported; must be full or delta", args.ContextMode)), nil
			}
			if contextMode == "delta" && mode != "structured" {
				return rawToolError(errors.New("brain_turn context_mode delta requires response_mode structured")), nil
			}
			turn, available, err := backend.PendingWebTurn(ctx, args.TaskID)
			if err != nil {
				return rawToolError(err), nil
			}
			value := map[string]any{"available": available, "turn": turn}
			if contextMode == "delta" && available {
				deltaBackend, ok := backend.(cognitionDeltaBackend)
				if !ok {
					return rawToolError(errors.New("brain_turn delta context is unavailable for this backend")), nil
				}
				view, err := deltaBackend.CognitionDelta(ctx, turn, strings.TrimSpace(args.CognitionCursor))
				if err != nil {
					return rawToolError(err), nil
				}
				value = map[string]any{
					"available": true,
					"turn": map[string]any{
						"turn_id": turn.ID, "task_id": turn.TaskID, "attempt_id": turn.AttemptID,
						"run_epoch": turn.RunEpoch, "request_id": turn.RequestID, "request_hash": turn.RequestHash,
						"integrity_hash": turn.IntegrityHash, "created_at": turn.CreatedAt,
					},
					"cognition": view,
				}
			}
			payload, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("marshal brain_turn tool result: %w", err)
			}
			if mode == "structured" {
				turnID := "-"
				attemptID := "-"
				runEpoch := int64(0)
				if available {
					turnID = turn.ID
					attemptID = turn.AttemptID
					runEpoch = turn.RunEpoch
				}
				receipt := fmt.Sprintf("brain_turn structured payload is authoritative; task_id=%s turn_id=%s attempt_id=%s run_epoch=%d", args.TaskID, turnID, attemptID, runEpoch)
				return &mcp.CallToolResult{
					Content:           []mcp.Content{&mcp.TextContent{Text: receipt}},
					StructuredContent: json.RawMessage(payload),
				}, nil
			}
			return &mcp.CallToolResult{
				Content:           []mcp.Content{&mcp.TextContent{Text: string(payload)}},
				StructuredContent: json.RawMessage(payload),
			}, nil
		})
}

func addBrainTurnFastTool(server *mcp.Server, backend Backend) {
	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string"},
		},
		"required":             []string{"task_id"},
		"additionalProperties": false,
	}
	server.AddTool(&mcp.Tool{Name: "brain_turn_fast", Description: "Read the pending durable external-cognition turn using an automatically selected safe delta base. No cognition cursor is required; uncertain history falls back to full current state.", InputSchema: inputSchema, OutputSchema: map[string]any{"type": "object"}},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if req == nil || req.Params == nil {
				return rawToolError(errors.New("tool request parameters are required")), nil
			}
			var args brainTurnFastArgs
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return rawToolError(fmt.Errorf("decode tool arguments: %w", err)), nil
			}
			args.TaskID = strings.TrimSpace(args.TaskID)
			if args.TaskID == "" {
				return rawToolError(errors.New("task_id is required")), nil
			}
			turn, available, err := backend.PendingWebTurn(ctx, args.TaskID)
			if err != nil {
				return rawToolError(err), nil
			}
			value := map[string]any{"available": available}
			mode := "none"
			turnID := "-"
			attemptID := "-"
			runEpoch := int64(0)
			if available {
				fastBackend, ok := backend.(automaticCognitionDeltaBackend)
				if !ok {
					return rawToolError(errors.New("brain_turn_fast is unavailable for this backend")), nil
				}
				view, err := fastBackend.AutomaticCognitionDelta(ctx, turn)
				if err != nil {
					return rawToolError(err), nil
				}
				mode = view.Mode
				turnID = turn.ID
				attemptID = turn.AttemptID
				runEpoch = turn.RunEpoch
				value = map[string]any{
					"available": true,
					"turn": map[string]any{
						"turn_id": turn.ID, "task_id": turn.TaskID, "attempt_id": turn.AttemptID,
						"run_epoch": turn.RunEpoch, "request_id": turn.RequestID, "request_hash": turn.RequestHash,
						"integrity_hash": turn.IntegrityHash, "created_at": turn.CreatedAt,
					},
					"cognition": view,
				}
			}
			payload, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("marshal brain_turn_fast tool result: %w", err)
			}
			receipt := fmt.Sprintf("brain_turn_fast structured payload is authoritative; task_id=%s turn_id=%s attempt_id=%s run_epoch=%d mode=%s", args.TaskID, turnID, attemptID, runEpoch, mode)
			return &mcp.CallToolResult{
				Content:           []mcp.Content{&mcp.TextContent{Text: receipt}},
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
