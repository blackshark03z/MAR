//go:build windows

package processctl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("MAR_TEST_HELPER_MODE")
	if mode == "" {
		return
	}
	if mode == "leaf" {
		for {
			time.Sleep(time.Second)
		}
	}
	if mode != "tree" {
		os.Exit(2)
	}

	child := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
	child.Env = append(os.Environ(), "MAR_TEST_HELPER_MODE=leaf")
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	pidFile := os.Getenv("MAR_TEST_CHILD_PID_FILE")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		os.Exit(4)
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestSupervisorAppliesHardCPUJobMemoryAndProcessLimits(t *testing.T) {
	s := NewSupervisor()
	want := Limits{CPUHardCapBasisPoints: 5_000, JobMemoryBytes: 256 << 20, MaxActiveProcesses: 4}
	tree, err := s.Start(Spec{
		Attempt: AttemptRef{TaskID: "task-limits", AttemptID: "attempt-limits", RunEpoch: 1},
		Path:    os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     append(os.Environ(), "MAR_TEST_HELPER_MODE=leaf"),
		Limits:  want,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.CloseUnverified()
	got, err := tree.AppliedLimits()
	if err != nil {
		t.Fatal(err)
	}
	if got.CPUHardCapBasisPoints != want.CPUHardCapBasisPoints || got.JobMemoryBytes != want.JobMemoryBytes || got.MaxActiveProcesses != want.MaxActiveProcesses {
		t.Fatalf("Windows Job Object limits mismatch: got=%+v want=%+v", got, want)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proof, err := tree.TerminateAndConfirm(ctx)
	if err != nil || !proof.Valid() {
		t.Fatalf("limited process tree did not terminate cleanly: proof=%+v err=%v", proof, err)
	}
}

func TestTerminateAndConfirmKillsInheritedChildTree(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	s := NewSupervisor()
	tree, err := s.Start(Spec{
		Attempt: AttemptRef{TaskID: "task-1", AttemptID: "attempt-1", RunEpoch: 1},
		Path:    os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     append(os.Environ(), "MAR_TEST_HELPER_MODE=tree", "MAR_TEST_CHILD_PID_FILE="+pidFile),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.CloseUnverified()

	childPID := waitForPIDFile(t, pidFile)
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := tree.Counters()
		if err != nil {
			t.Fatal(err)
		}
		if c.ActiveProcesses >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected parent+child in job, counters=%+v", c)
		}
		time.Sleep(20 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proof, err := tree.TerminateAndConfirm(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !proof.Valid() || proof.ActiveProcesses() != 0 {
		t.Fatalf("invalid termination proof: %+v", proof)
	}
	if proof.Attempt().AttemptID != "attempt-1" || proof.Attempt().RunEpoch != 1 {
		t.Fatalf("proof bound to wrong attempt: %+v", proof.Attempt())
	}
	assertProcessNotActive(t, childPID)
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(b)))
			if convErr != nil {
				t.Fatal(convErr)
			}
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child pid file not created: %s", path)
	return 0
}

func assertProcessNotActive(t *testing.T, pid int) {
	t.Helper()
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return // process object is already gone
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		t.Fatal(err)
	}
	if code == 259 { // STILL_ACTIVE
		t.Fatalf("child process %d is still active", pid)
	}
}

func TestZeroTerminationProofIsInvalid(t *testing.T) {
	var proof TerminationProof
	if proof.Valid() {
		t.Fatal("zero termination proof must be invalid")
	}
	if got := fmt.Sprint(proof.Attempt()); got == "" {
		t.Fatal("unexpected empty formatting")
	}
}
