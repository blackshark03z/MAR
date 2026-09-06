//go:build windows

package processctl_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/aci"
	"mar/internal/processctl"
)

func TestNestedWorkerJobSandboxReturnsPromptlyForFailingGoTest(t *testing.T) {
	if os.Getenv("MAR_NESTED_SANDBOX_HELPER") == "1" {
		t.Skip("helper process")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module nestedfailprobe\n\ngo 1.27.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fail_test.go"), []byte("package nestedfailprobe\n\nimport \"testing\"\n\nfunc TestFail(t *testing.T) { t.Fatal(\"intentional nested failure\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	goExe := nestedPortableGo(t)
	goRoot := filepath.Dir(filepath.Dir(goExe))
	goBin := filepath.Dir(goExe)
	sharedCache := filepath.Join(t.TempDir(), "gomodcache")
	if err := os.MkdirAll(sharedCache, 0o755); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	tree, err := processctl.NewSupervisor().Start(processctl.Spec{
		Attempt: processctl.AttemptRef{TaskID: "nested-task", AttemptID: "nested-attempt", RunEpoch: 1},
		Path:    os.Args[0],
		Args:    []string{"-test.run=^TestNestedWorkerJobSandboxHelper$"},
		Env: append(os.Environ(),
			"MAR_NESTED_SANDBOX_HELPER=1",
			"MAR_NESTED_SANDBOX_ROOT="+root,
			"MAR_NESTED_SANDBOX_GO="+goExe,
			"MAR_NESTED_SANDBOX_GOROOT="+goRoot,
			"MAR_NESTED_SANDBOX_GOMODCACHE="+sharedCache,
			"PATH="+goBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		),
		Stdout: &output,
		Stderr: &output,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.CloseUnverified()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	proof, err := tree.WaitAndConfirm(ctx)
	if err != nil {
		counters, _ := tree.Counters()
		_, _ = tree.TerminateAndConfirm(context.Background())
		t.Fatalf("nested sandbox helper did not terminate: %v counters=%+v output=%s", err, counters, output.String())
	}
	if !proof.Valid() {
		t.Fatalf("nested helper lost physical termination proof: %+v", proof)
	}
	if !strings.Contains(output.String(), "NESTED_FAIL_RETURNED") {
		t.Fatalf("nested sandbox helper did not report returned failure: %s", output.String())
	}
}

func TestNestedWorkerJobSandboxHelper(t *testing.T) {
	if os.Getenv("MAR_NESTED_SANDBOX_HELPER") != "1" {
		return
	}
	root := os.Getenv("MAR_NESTED_SANDBOX_ROOT")
	goRoot := os.Getenv("MAR_NESTED_SANDBOX_GOROOT")
	sharedCache := os.Getenv("MAR_NESTED_SANDBOX_GOMODCACHE")
	executor, err := aci.NewWindowsSandboxExecutor(root, goRoot, sharedCache)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := aci.NewContainedGitBroker()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := aci.New(aci.Config{
		Root:           root,
		TaskID:         "nested-sandbox-task",
		CommandTimeout: 120 * time.Second,
		GitBroker:      broker,
		GoModuleCache:  sharedCache,
	}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.SelfHostingSafe() {
		t.Fatal("nested sandbox is not self-hosting safe")
	}
	result, runErr := runtime.RunCommand(context.Background(), aci.Command{Name: "go", Args: []string{"test", "./..."}})
	if runErr == nil {
		t.Fatalf("nested failing go test unexpectedly succeeded: %+v", result)
	}
	if errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("nested failing go test hit the bounded command deadline instead of returning failure evidence: err=%v output=%s", runErr, result.Output)
	}
	if result.ExitCode == 0 || !strings.Contains(result.Output, "intentional nested failure") {
		t.Fatalf("nested failing go test lost failure evidence: result=%+v err=%v", result, runErr)
	}
	fmt.Println("NESTED_FAIL_RETURNED")
}

func nestedPortableGo(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for root := filepath.Clean(cwd); ; root = filepath.Dir(root) {
		candidate := filepath.Join(root, ".mar", "runtime", "go-portable", "go", "bin", "go.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
	}
	t.Fatal("portable Go executable not found")
	return ""
}
