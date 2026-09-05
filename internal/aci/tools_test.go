package aci

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mar/internal/model"
)

func TestToolDefinitionsAreValidAndUnique(t *testing.T) {
	r, _ := newTestRuntime(t, &fakeExecutor{level: IsolationEnforcedSandbox}, false)
	seen := map[string]bool{}
	for _, def := range r.ToolDefinitions() {
		if def.Name == "" || seen[def.Name] {
			t.Fatalf("invalid/duplicate tool definition %q", def.Name)
		}
		seen[def.Name] = true
		if !json.Valid(def.Parameters) {
			t.Fatalf("tool %s has invalid JSON schema: %s", def.Name, def.Parameters)
		}
	}
	for _, want := range []string{"read_file", "search_text", "write_file", "replace_exact", "git_status", "git_diff", "run_command"} {
		if !seen[want] {
			t.Fatalf("missing tool %s", want)
		}
	}
}

func TestExecuteToolReadWritePatchRoundTrip(t *testing.T) {
	r, root := newTestRuntime(t, &fakeExecutor{level: IsolationEnforcedSandbox}, false)
	ctx := context.Background()
	out, err := r.ExecuteTool(ctx, model.ToolCall{Name: "write_file", Arguments: string(`{"path":"a.txt","expected_sha256":"ABSENT","content":"hello world\n"}`)})
	if err != nil || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("write tool failed: %s %v", out, err)
	}
	readOut, err := r.ExecuteTool(ctx, model.ToolCall{Name: "read_file", Arguments: string(`{"path":"a.txt","start_line":1,"end_line":2}`)})
	if err != nil || !strings.Contains(readOut, "hello world") {
		t.Fatalf("read tool failed: %s %v", readOut, err)
	}
	var envelope struct {
		Result ReadResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(readOut), &envelope); err != nil {
		t.Fatal(err)
	}
	patchArgs, _ := json.Marshal(map[string]any{
		"path": "a.txt", "expected_sha256": envelope.Result.SHA256,
		"search": "hello", "replacement": "goodbye", "expected_count": 1,
	})
	patchOut, err := r.ExecuteTool(ctx, model.ToolCall{Name: "replace_exact", Arguments: string(patchArgs)})
	if err != nil || !strings.Contains(patchOut, `"ok":true`) {
		t.Fatalf("patch tool failed: %s %v", patchOut, err)
	}
	b, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(b) != "goodbye world\n" {
		t.Fatalf("unexpected final file %q err=%v", b, err)
	}
}

func TestExecuteToolReturnsBoundedErrorObservation(t *testing.T) {
	r, _ := newTestRuntime(t, &fakeExecutor{level: IsolationEnforcedSandbox}, false)
	out, err := r.ExecuteTool(context.Background(), model.ToolCall{Name: "read_file", Arguments: string(`{"path":"missing.txt"}`)})
	if err != nil {
		t.Fatalf("runtime tool error should be an observation, got %v", err)
	}
	if !strings.Contains(out, `"ok":false`) || !strings.Contains(out, "missing") {
		t.Fatalf("unexpected tool error observation: %s", out)
	}
}

func TestExecuteToolRejectsMalformedUnknownAndExtraArguments(t *testing.T) {
	r, _ := newTestRuntime(t, &fakeExecutor{level: IsolationEnforcedSandbox}, false)
	cases := []model.ToolCall{
		{Name: "read_file", Arguments: string(`{"path":"a","extra":1}`)},
		{Name: "read_file", Arguments: string(`{"path":`)},
		{Name: "does_not_exist", Arguments: `{}`},
	}
	for _, call := range cases {
		if _, err := r.ExecuteTool(context.Background(), call); err == nil {
			t.Fatalf("invalid call was accepted: %+v", call)
		}
	}
}
