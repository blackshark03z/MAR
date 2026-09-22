//go:build windows

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/agent"
)

func TestRunChildExternalHarnessBypassesMAROwnedCognition(t *testing.T) {
	start := workerProcessTestStart()
	workspace := t.TempDir()
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	start.WorkspacePath = workspace
	if err := os.WriteFile(filepath.Join(workspace, "external-harness.input"), []byte("run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	start.Provider = ProviderConfig{BrainMode: BrainHarness}
	start.AgentProfile = agent.Profile{}
	start.AgentConfig = agent.Config{}
	start.HarnessExecutable = executable
	start.HarnessArguments = []string{"-test.run=^TestExternalHarnessExecutableHelper$", "-test.v"}
	start.SandboxReadPaths = []string{filepath.Dir(executable)}
	start.CommandTimeout = 30 * time.Second

	if err := start.Validate(); err != nil {
		t.Fatalf("external harness start request rejected without MAR agent profile: %v", err)
	}

	first, err := marshalFrame(frameStart, 0, "", start, "")
	if err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	if err := json.NewEncoder(&input).Encode(first); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := RunChild(ctx, &input, &output); err != nil {
		t.Fatalf("external harness child failed: %v\n%s", err, output.String())
	}

	var terminal frame
	if err := json.NewDecoder(&output).Decode(&terminal); err != nil {
		t.Fatalf("decode external harness terminal frame: %v\n%s", err, output.String())
	}
	if terminal.Type != frameResult {
		t.Fatalf("external harness returned frame type %q error=%q", terminal.Type, terminal.Error)
	}
	var result agent.Result
	if err := json.Unmarshal(terminal.Payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != agent.StatusCompletedCandidate || !strings.Contains(result.Summary, "HARNESS_OK") {
		t.Fatalf("unexpected external harness result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(workspace, "external-harness.ok")); err != nil {
		t.Fatalf("external harness did not mutate governed workspace: %v", err)
	}
}

func TestExternalHarnessExecutableHelper(t *testing.T) {
	if _, err := os.Stat("external-harness.input"); err != nil {
		t.Skip("external harness helper runs only inside the governed fixture workspace")
	}
	if err := os.WriteFile("external-harness.ok", []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fmt.Println("HARNESS_OK")
}
