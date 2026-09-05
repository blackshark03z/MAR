package aci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"mar/internal/model"
)

func (r *Runtime) ToolDefinitions() []model.ToolDefinition {
	return []model.ToolDefinition{
		{Name: "read_file", Description: "Read a bounded line range from a workspace file and return its SHA-256 revision.", Parameters: schema(`{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":0}},"required":["path"],"additionalProperties":false}`), Strict: true},
		{Name: "search_text", Description: "Search workspace text with bounded results.", Parameters: schema(`{"type":"object","properties":{"query":{"type":"string"},"max_results":{"type":"integer","minimum":1}},"required":["query"],"additionalProperties":false}`), Strict: true},
		{Name: "write_file", Description: "Create or replace a workspace file using an ABSENT or exact SHA-256 precondition.", Parameters: schema(`{"type":"object","properties":{"path":{"type":"string"},"expected_sha256":{"type":"string"},"content":{"type":"string"}},"required":["path","expected_sha256","content"],"additionalProperties":false}`), Strict: true},
		{Name: "replace_exact", Description: "Apply an exact search/replace patch bound to the current file SHA-256.", Parameters: schema(`{"type":"object","properties":{"path":{"type":"string"},"expected_sha256":{"type":"string"},"search":{"type":"string"},"replacement":{"type":"string"},"expected_count":{"type":"integer","minimum":1}},"required":["path","expected_sha256","search","replacement","expected_count"],"additionalProperties":false}`), Strict: true},
		{Name: "git_status", Description: "Return bounded Git status for the task workspace.", Parameters: schema(`{"type":"object","properties":{},"additionalProperties":false}`), Strict: true},
		{Name: "git_diff", Description: "Return bounded Git diff, optionally limited to workspace-relative paths.", Parameters: schema(`{"type":"object","properties":{"paths":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`), Strict: true},
		{Name: "run_command", Description: "Run an allow-listed coding verification command inside the configured executor boundary.", Parameters: schema(`{"type":"object","properties":{"name":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"cwd":{"type":"string"}},"required":["name"],"additionalProperties":false}`), Strict: true},
	}
}

func (r *Runtime) ExecuteTool(ctx context.Context, call model.ToolCall) (string, error) {
	switch call.Name {
	case "read_file":
		var args struct {
			Path      string `json:"path"`
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return "", err
		}
		result, err := r.ReadFile(args.Path, args.StartLine, args.EndLine)
		return encodeResult(result, err)
	case "search_text":
		var args struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return "", err
		}
		result, err := r.SearchText(args.Query, args.MaxResults)
		return encodeResult(result, err)
	case "write_file":
		var args struct {
			Path           string `json:"path"`
			ExpectedSHA256 string `json:"expected_sha256"`
			Content        string `json:"content"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return "", err
		}
		result, err := r.WriteFile(args.Path, args.ExpectedSHA256, []byte(args.Content))
		return encodeResult(result, err)
	case "replace_exact":
		var args struct {
			Path           string `json:"path"`
			ExpectedSHA256 string `json:"expected_sha256"`
			Search         string `json:"search"`
			Replacement    string `json:"replacement"`
			ExpectedCount  int    `json:"expected_count"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return "", err
		}
		result, err := r.ReplaceExact(args.Path, args.ExpectedSHA256, args.Search, args.Replacement, args.ExpectedCount)
		return encodeResult(result, err)
	case "git_status":
		var args struct{}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return "", err
		}
		result, err := r.GitStatus(ctx)
		return encodeResult(result, err)
	case "git_diff":
		var args struct {
			Paths []string `json:"paths"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return "", err
		}
		result, err := r.GitDiff(ctx, args.Paths)
		return encodeResult(result, err)
	case "run_command":
		var args struct {
			Name string   `json:"name"`
			Args []string `json:"args"`
			Cwd  string   `json:"cwd"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return "", err
		}
		result, err := r.RunCommand(ctx, Command{Name: args.Name, Args: args.Args, Cwd: args.Cwd})
		return encodeResult(result, err)
	default:
		return "", fmt.Errorf("unknown coding tool %q", call.Name)
	}
}

func schema(raw string) json.RawMessage { return json.RawMessage(raw) }

func decodeArgs(raw string, dst any) error {
	if raw == "" {
		raw = `{}`
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode tool arguments: multiple JSON values")
		}
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	return nil
}

func encodeResult(result any, err error) (string, error) {
	if err != nil {
		payload, marshalErr := json.Marshal(map[string]any{"ok": false, "error": err.Error()})
		if marshalErr != nil {
			return "", marshalErr
		}
		return string(payload), nil
	}
	payload, marshalErr := json.Marshal(map[string]any{"ok": true, "result": result})
	if marshalErr != nil {
		return "", marshalErr
	}
	return string(payload), nil
}
