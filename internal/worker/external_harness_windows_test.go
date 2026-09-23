//go:build windows

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/agent"
	"mar/internal/testsupport"
)

func TestRunChildExternalHarnessPreparesMissingTempRoot(t *testing.T) {
	testsupport.RequireOutsideAppContainer(t)
	// Regression: runExternalHarnessChild must prepare the selected missing temp root,
	// not fall back to another owner/global temp location.
	base := t.TempDir()
	missingTemp := filepath.Join(base, "missing-temp-root")
	t.Setenv("TEMP", missingTemp)
	t.Setenv("TMP", missingTemp)
	if _, err := os.Stat(missingTemp); !os.IsNotExist(err) {
		t.Fatalf("missing temp precondition failed: %v", err)
	}

	start := workerProcessTestStart()
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("external harness child failed with missing temp root: %v\n%s", err, output.String())
	}
	if info, err := os.Stat(missingTemp); err != nil || !info.IsDir() {
		t.Fatalf("selected missing temp root was not prepared: info=%v err=%v", info, err)
	}
}

func TestRunChildExternalHarnessBypassesMAROwnedCognition(t *testing.T) {
	testsupport.RequireOutsideAppContainer(t)
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

func TestRunChildExternalHarnessMapsTaskNetworkAuthorityToLPAC(t *testing.T) {
	testsupport.RequireOutsideAppContainer(t)
	const target = "example.com:443"
	hostConn, err := net.DialTimeout("tcp", target, 3*time.Second)
	if err != nil {
		t.Skipf("host public Internet probe unavailable: %v", err)
	}
	_ = hostConn.Close()

	for _, networkAllowed := range []bool{false, true} {
		t.Run(fmt.Sprintf("network_allowed_%t", networkAllowed), func(t *testing.T) {
			start := workerProcessTestStart()
			workspace := t.TempDir()
			executable, err := filepath.Abs(os.Args[0])
			if err != nil {
				t.Fatal(err)
			}
			start.Task.ID = fmt.Sprintf("task-harness-network-%t", networkAllowed)
			start.Attempt.TaskID = start.Task.ID
			start.Task.Contract.Authority.NetworkAllowed = networkAllowed
			start.Task.ContractHash, err = start.Task.Contract.Hash()
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
			start.HarnessArguments = []string{"-test.run=^TestExternalHarnessNetworkExecutableHelper$", "-test.v"}
			start.SandboxReadPaths = []string{filepath.Dir(executable)}
			start.CommandTimeout = 30 * time.Second

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
				t.Fatalf("external harness network child failed: %v\n%s", err, output.String())
			}

			var terminal frame
			if err := json.NewDecoder(&output).Decode(&terminal); err != nil {
				t.Fatalf("decode external harness network terminal frame: %v\n%s", err, output.String())
			}
			if terminal.Type != frameResult {
				t.Fatalf("external harness network returned frame type %q error=%q", terminal.Type, terminal.Error)
			}
			var result agent.Result
			if err := json.Unmarshal(terminal.Payload, &result); err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("HARNESS_NETWORK_%t_OK", networkAllowed)
			if result.Status != agent.StatusCompletedCandidate || !strings.Contains(result.Summary, want) {
				t.Fatalf("external harness network authority mismatch: %+v", result)
			}
		})
	}
}

func TestExternalHarnessNetworkExecutableHelper(t *testing.T) {
	if _, err := os.Stat("external-harness.input"); err != nil {
		t.Skip("external harness network helper runs only inside the governed fixture workspace")
	}
	inputPath := strings.TrimSpace(os.Getenv(ExternalHarnessInputPathEnv))
	if inputPath == "" {
		t.Fatal("missing task-bound external harness input")
	}
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	var input ExternalHarnessInput
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	conn, dialErr := net.DialTimeout("tcp", "example.com:443", 3*time.Second)
	if conn != nil {
		_ = conn.Close()
	}
	if input.GoalContract.Authority.NetworkAllowed && dialErr != nil {
		t.Fatalf("network_allowed=true did not grant outbound Internet: %v", dialErr)
	}
	if !input.GoalContract.Authority.NetworkAllowed && dialErr == nil {
		t.Fatal("network_allowed=false unexpectedly granted outbound Internet")
	}
	fmt.Printf("HARNESS_NETWORK_%t_OK\n", input.GoalContract.Authority.NetworkAllowed)
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
