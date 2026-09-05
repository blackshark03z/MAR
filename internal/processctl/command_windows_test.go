//go:build windows

package processctl

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunContainedHelper(t *testing.T) {
	mode := os.Getenv("MAR_CONTAINED_COMMAND_HELPER")
	if mode == "" {
		return
	}
	if mode == "success" {
		_, _ = os.Stdout.WriteString("contained-ok")
		return
	}
	if mode != "tree" && mode != "orphan" {
		os.Exit(2)
	}
	child := exec.Command(os.Args[0], "-test.run=TestRunContainedLeaf")
	child.Env = append(os.Environ(), "MAR_CONTAINED_COMMAND_LEAF=1")
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(os.Getenv("MAR_CONTAINED_CHILD_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		os.Exit(4)
	}
	if mode == "orphan" {
		return
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestRunContainedLeaf(t *testing.T) {
	if os.Getenv("MAR_CONTAINED_COMMAND_LEAF") != "1" {
		return
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestRunContainedCommandSuccess(t *testing.T) {
	out, err := RunContainedCommand(context.Background(), CommandSpec{
		TaskID:      "task-control",
		OperationID: "success",
		Path:        os.Args[0],
		Args:        []string{"-test.run=TestRunContainedHelper"},
		Env:         append(os.Environ(), "MAR_CONTAINED_COMMAND_HELPER=success"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "contained-ok") {
		t.Fatalf("missing contained output: %q", out)
	}
}

func TestRunContainedCommandParentExitThenTimeoutKillsRemainingChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "orphan-child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := RunContainedCommand(ctx, CommandSpec{
		TaskID:      "task-control",
		OperationID: "orphan-timeout",
		Path:        os.Args[0],
		Args:        []string{"-test.run=TestRunContainedHelper"},
		Env: append(os.Environ(),
			"MAR_CONTAINED_COMMAND_HELPER=orphan",
			"MAR_CONTAINED_CHILD_PID_FILE="+pidFile,
		),
	})
	if err == nil {
		t.Fatal("expected timeout while descendant kept job alive")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("cleanup waited too long after parent was already reaped: %v", time.Since(started))
	}
	pid := waitForPIDFile(t, pidFile)
	assertProcessNotActive(t, pid)
}

func TestRunContainedCommandCancellationKillsDescendant(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := RunContainedCommand(ctx, CommandSpec{
		TaskID:      "task-control",
		OperationID: "cancel-tree",
		Path:        os.Args[0],
		Args:        []string{"-test.run=TestRunContainedHelper"},
		Env: append(os.Environ(),
			"MAR_CONTAINED_COMMAND_HELPER=tree",
			"MAR_CONTAINED_CHILD_PID_FILE="+pidFile,
		),
	})
	if err == nil {
		t.Fatal("expected context cancellation")
	}
	pid := waitForPIDFile(t, pidFile)
	assertProcessNotActive(t, pid)
}
