//go:build windows

package processctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestAppContainerSandboxEnforcesWorkerAuthorityBoundary(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	probe := copySandboxProbe(t, workspace)
	insideWrite := filepath.Join(workspace, "inside.txt")
	outsideWrite := filepath.Join(outside, "outside.txt")
	secretPath := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("host-secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
		TaskID:         "sandbox-authority-boundary",
		OperationID:    "authority-probe",
		WorkspaceRoot:  workspace,
		Path:           probe,
		Args:           []string{"-test.run=TestSandboxProbeHelper"},
		Dir:            workspace,
		Env:            probeEnvironment("authority", insideWrite, outsideWrite, secretPath, listener.Addr().String(), ""),
		MaxOutputBytes: 32 << 10,
	})
	if err != nil {
		t.Fatalf("sandbox authority probe failed: %v\n%s", err, result.Output)
	}
	if !strings.Contains(result.Output, "SANDBOX_AUTHORITY_OK") {
		t.Fatalf("sandbox authority probe did not report success: %s", result.Output)
	}
	if got, err := os.ReadFile(insideWrite); err != nil || string(got) != "inside" {
		t.Fatalf("workspace write was not preserved: %q %v", got, err)
	}
	if _, err := os.Stat(outsideWrite); !os.IsNotExist(err) {
		t.Fatalf("sandbox unexpectedly wrote outside workspace: %v", err)
	}
}

func TestLPACCapabilityAllowsWorkspaceWriteWithCmd(t *testing.T) {
	workspace := t.TempDir()
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		t.Fatal("SystemRoot is required")
	}
	cmd := filepath.Join(systemRoot, "System32", "cmd.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
		TaskID:         "sandbox-lpac-capability-cmd",
		OperationID:    "capability-cmd-write",
		WorkspaceRoot:  workspace,
		Path:           cmd,
		Args:           []string{"/d", "/s", "/c", "echo capability>capability.txt"},
		Dir:            workspace,
		Env:            probeEnvironment("", "", "", "", "", ""),
		MaxOutputBytes: 16 << 10,
	})
	if err != nil {
		t.Fatalf("LPAC capability cmd write failed: %v output=%s", err, result.Output)
	}
	got, err := os.ReadFile(filepath.Join(workspace, "capability.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "capability" {
		t.Fatalf("unexpected capability write: %q", got)
	}
}

func TestLPACTaskCapabilityACLIsRestoredAfterCommand(t *testing.T) {
	workspace := t.TempDir()
	taskID := "sandbox-acl-restore"
	capabilitySID, release, err := deriveCapabilitySID(taskCapabilityName(taskID), "ACL restore test")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if contains, err := pathDACLContainsSID(workspace, capabilitySID); err != nil || contains {
		t.Fatalf("workspace unexpectedly has task capability before run: contains=%v err=%v", contains, err)
	}

	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		t.Fatal("SystemRoot is required")
	}
	cmd := filepath.Join(systemRoot, "System32", "cmd.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
		TaskID:         taskID,
		OperationID:    "acl-restore-write",
		WorkspaceRoot:  workspace,
		Path:           cmd,
		Args:           []string{"/d", "/c", "echo residue-check>created.txt"},
		Dir:            workspace,
		Env:            probeEnvironment("", "", "", "", "", ""),
		MaxOutputBytes: 16 << 10,
	})
	if err != nil {
		t.Fatalf("sandbox command failed: %v output=%s", err, result.Output)
	}
	for _, path := range []string{workspace, filepath.Join(workspace, "created.txt")} {
		if contains, err := pathDACLContainsSID(path, capabilitySID); err != nil || contains {
			t.Fatalf("task capability ACL residue on %s: contains=%v err=%v", path, contains, err)
		}
	}
}

func TestSandboxKeyedLocksAreReleasedAfterCommand(t *testing.T) {
	workspace := t.TempDir()
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		t.Fatal("SystemRoot is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
		TaskID:         "sandbox-lock-release",
		OperationID:    "lock-release",
		WorkspaceRoot:  workspace,
		Path:           filepath.Join(systemRoot, "System32", "cmd.exe"),
		Args:           []string{"/d", "/c", "ver>NUL"},
		Dir:            workspace,
		Env:            probeEnvironment("", "", "", "", "", ""),
		MaxOutputBytes: 4 << 10,
	})
	if err != nil {
		t.Fatalf("sandbox command failed: %v output=%s", err, result.Output)
	}
	for name, registry := range map[string]*keyedMutexRegistry{
		"profile": sandboxProfileLocks,
		"path":    sandboxPathLocks,
	} {
		registry.mu.Lock()
		count := len(registry.entries)
		registry.mu.Unlock()
		if count != 0 {
			t.Fatalf("%s lock registry retained %d entry(ies) after command", name, count)
		}
	}
}

func TestSandboxProfileLockSerializesAcrossProcesses(t *testing.T) {
	profile := "MAR.Worker.CrossProcessProfileTest"
	marker := filepath.Join(t.TempDir(), "profile-acquired.txt")
	unlock, err := lockSandboxProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSandboxProfileLockHelper$")
	var childOutput strings.Builder
	cmd.Stdout = &childOutput
	cmd.Stderr = &childOutput
	cmd.Env = append(os.Environ(),
		"MAR_SANDBOX_PROFILE_LOCK_HELPER=1",
		"MAR_SANDBOX_PROFILE_LOCK_NAME="+profile,
		"MAR_SANDBOX_PROFILE_LOCK_MARKER="+marker,
	)
	if err := cmd.Start(); err != nil {
		unlock()
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		unlock()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("second process acquired sandbox profile lock before release: %v", err)
	}
	unlock()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cross-process profile lock helper failed: %v output=%s", err, childOutput.String())
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("second process did not acquire sandbox profile lock after release")
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "acquired" {
		t.Fatalf("cross-process sandbox profile lock helper did not complete: %q %v", got, err)
	}
}

func TestSandboxProfileLockHelper(t *testing.T) {
	if os.Getenv("MAR_SANDBOX_PROFILE_LOCK_HELPER") != "1" {
		return
	}
	unlock, err := lockSandboxProfile(os.Getenv("MAR_SANDBOX_PROFILE_LOCK_NAME"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := os.WriteFile(os.Getenv("MAR_SANDBOX_PROFILE_LOCK_MARKER"), []byte("acquired"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSandboxPathLockSerializesAcrossProcesses(t *testing.T) {
	path := t.TempDir()
	marker := filepath.Join(t.TempDir(), "acquired.txt")
	unlock, err := lockSandboxPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSandboxPathLockHelper$")
	var childOutput strings.Builder
	cmd.Stdout = &childOutput
	cmd.Stderr = &childOutput
	cmd.Env = append(os.Environ(),
		"MAR_SANDBOX_PATH_LOCK_HELPER=1",
		"MAR_SANDBOX_PATH_LOCK_PATH="+path,
		"MAR_SANDBOX_PATH_LOCK_MARKER="+marker,
	)
	if err := cmd.Start(); err != nil {
		unlock()
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		unlock()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("second process acquired sandbox path lock before release: %v", err)
	}
	unlock()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cross-process lock helper failed: %v output=%s", err, childOutput.String())
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("second process did not acquire sandbox path lock after release")
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "acquired" {
		t.Fatalf("cross-process sandbox path lock helper did not complete: %q %v", got, err)
	}
}

func TestSandboxPathLockHelper(t *testing.T) {
	if os.Getenv("MAR_SANDBOX_PATH_LOCK_HELPER") != "1" {
		return
	}
	unlock, err := lockSandboxPaths([]string{os.Getenv("MAR_SANDBOX_PATH_LOCK_PATH")})
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := os.WriteFile(os.Getenv("MAR_SANDBOX_PATH_LOCK_MARKER"), []byte("acquired"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLPACTaskCapabilityCannotReadAnotherTaskWorkspace(t *testing.T) {
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	secretPath := filepath.Join(workspaceA, "task-a-secret.txt")
	if err := os.WriteFile(secretPath, []byte("task-a-only"), 0o644); err != nil {
		t.Fatal(err)
	}
	capabilityA, releaseA, err := deriveCapabilitySID(taskCapabilityName("task-a"), "test task-a")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseA()
	restoreA, err := grantSandboxAccess(workspaceA, capabilityA, true)
	if err != nil {
		t.Fatal(err)
	}
	defer restoreA()

	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		t.Fatal("SystemRoot is required")
	}
	cmd := filepath.Join(systemRoot, "System32", "cmd.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
		TaskID:         "task-b",
		OperationID:    "cross-task-read-deny",
		WorkspaceRoot:  workspaceB,
		Path:           cmd,
		Args:           []string{"/d", "/c", "type " + secretPath},
		Dir:            workspaceB,
		Env:            probeEnvironment("", "", "", "", "", ""),
		MaxOutputBytes: 16 << 10,
	})
	if err == nil {
		t.Fatalf("task B unexpectedly read task A workspace: %q", result.Output)
	}
	if strings.Contains(result.Output, "task-a-only") {
		t.Fatalf("task A content leaked to task B: %q", result.Output)
	}
}

func TestLPACCapabilityAllowsExplicitReadWithCmd(t *testing.T) {
	workspace := t.TempDir()
	readRoot := t.TempDir()
	readPath := filepath.Join(readRoot, "runtime.txt")
	if err := os.WriteFile(readPath, []byte("runtime-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		t.Fatal("SystemRoot is required")
	}
	cmd := filepath.Join(systemRoot, "System32", "cmd.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
		TaskID:         "sandbox-lpac-capability-read",
		OperationID:    "capability-cmd-read",
		WorkspaceRoot:  workspace,
		ReadPaths:      []string{readRoot},
		Path:           cmd,
		Args:           []string{"/d", "/c", "type " + readPath},
		Dir:            workspace,
		Env:            probeEnvironment("", "", "", "", "", ""),
		MaxOutputBytes: 16 << 10,
	})
	if err != nil {
		t.Fatalf("LPAC capability cmd read failed: %v output=%s", err, result.Output)
	}
	if !strings.Contains(result.Output, "runtime-data") {
		t.Fatalf("unexpected capability read output: %q", result.Output)
	}
}

func TestLPACOptsOutOfAllApplicationPackagesRead(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	probe := copySandboxProbe(t, workspace)
	secretPath := filepath.Join(outside, "aap-only-secret.txt")
	if err := os.WriteFile(secretPath, []byte("aap-visible"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := grantAllApplicationPackagesRead(t, secretPath)
	defer restore()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
		TaskID:         "sandbox-lpac-aap-opt-out",
		OperationID:    "lpac-aap-opt-out-probe",
		WorkspaceRoot:  workspace,
		Path:           probe,
		Args:           []string{"-test.run=TestSandboxProbeHelper"},
		Dir:            workspace,
		Env:            probeEnvironment("lpac-aap-deny", "", "", secretPath, "", ""),
		MaxOutputBytes: 32 << 10,
	})
	if err != nil {
		t.Fatalf("LPAC opt-out probe failed: %v\n%s", err, result.Output)
	}
	if !strings.Contains(result.Output, "LPAC_AAP_DENY_OK") {
		t.Fatalf("LPAC opt-out probe did not report success: %s", result.Output)
	}
}

func TestLPACRegistryReadDoesNotExposeUserOrProtectedMachineHives(t *testing.T) {
	workspace := t.TempDir()
	probe := copySandboxProbe(t, workspace)
	secretKeyPath := `Software\\MAR\\SandboxRegistryProbe`
	secretKey, _, err := registry.CreateKey(registry.CURRENT_USER, secretKeyPath, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	if err := secretKey.SetStringValue("Secret", "must-not-cross-registry-boundary"); err != nil {
		secretKey.Close()
		t.Fatal(err)
	}
	if err := secretKey.Close(); err != nil {
		t.Fatal(err)
	}
	defer registry.DeleteKey(registry.CURRENT_USER, secretKeyPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
		TaskID:         "sandbox-registry-scope",
		OperationID:    "registry-scope-probe",
		WorkspaceRoot:  workspace,
		Path:           probe,
		Args:           []string{"-test.run=TestSandboxProbeHelper"},
		Dir:            workspace,
		Env:            probeEnvironment("registry-scope", "", "", secretKeyPath, "", ""),
		MaxOutputBytes: 32 << 10,
	})
	if err != nil {
		t.Fatalf("registry scope probe failed: %v\n%s", err, result.Output)
	}
	if !strings.Contains(result.Output, "LPAC_REGISTRY_SCOPE_OK") {
		t.Fatalf("registry scope probe did not report success: %s", result.Output)
	}
}

func TestAppContainerSandboxAllowsExplicitReadScopeWithoutWrite(t *testing.T) {
	workspace := t.TempDir()
	readRoot := t.TempDir()
	probe := copySandboxProbe(t, workspace)
	readPath := filepath.Join(readRoot, "runtime.txt")
	writePath := filepath.Join(readRoot, "must-not-write.txt")
	if err := os.WriteFile(readPath, []byte("runtime-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
		TaskID:         "sandbox-read-scope",
		OperationID:    "read-scope-probe",
		WorkspaceRoot:  workspace,
		ReadPaths:      []string{readRoot},
		Path:           probe,
		Args:           []string{"-test.run=TestSandboxProbeHelper"},
		Dir:            workspace,
		Env:            probeEnvironment("read-scope", "", writePath, readPath, "", ""),
		MaxOutputBytes: 32 << 10,
	})
	if err != nil {
		t.Fatalf("sandbox read-scope probe failed: %v\n%s", err, result.Output)
	}
	if !strings.Contains(result.Output, "SANDBOX_READ_SCOPE_OK") {
		t.Fatalf("sandbox read-scope probe did not report success: %s", result.Output)
	}
	if _, err := os.Stat(writePath); !os.IsNotExist(err) {
		t.Fatalf("read-only sandbox grant allowed write: %v", err)
	}
}

func TestSandboxSharedReadPathSupportsConcurrentTasks(t *testing.T) {
	shared := t.TempDir()
	readPath := filepath.Join(shared, "runtime.txt")
	if err := os.WriteFile(readPath, []byte("shared-runtime"), 0o644); err != nil {
		t.Fatal(err)
	}
	type runCase struct {
		taskID    string
		workspace string
		marker    string
		release   string
	}
	cases := []runCase{
		{taskID: "sandbox-concurrent-shared-a", workspace: t.TempDir()},
		{taskID: "sandbox-concurrent-shared-b", workspace: t.TempDir()},
	}
	for i := range cases {
		cases[i].marker = filepath.Join(cases[i].workspace, "entered.txt")
		cases[i].release = filepath.Join(cases[i].workspace, "release.txt")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	type outcome struct {
		index  int
		result SandboxCommandResult
		err    error
	}
	outcomes := make(chan outcome, len(cases))
	for i, item := range cases {
		probe := copySandboxProbe(t, item.workspace)
		go func(index int, current runCase, executable string) {
			result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
				TaskID:         current.taskID,
				OperationID:    "shared-read-concurrency",
				WorkspaceRoot:  current.workspace,
				ReadPaths:      []string{shared},
				Path:           executable,
				Args:           []string{"-test.run=TestSandboxProbeHelper"},
				Dir:            current.workspace,
				Env:            probeEnvironment("shared-read-hold", current.marker, current.release, readPath, "", ""),
				MaxOutputBytes: 16 << 10,
			})
			outcomes <- outcome{index: index, result: result, err: err}
		}(i, item, probe)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		both := true
		for _, item := range cases {
			if _, err := os.Stat(item.marker); err != nil {
				both = false
				break
			}
		}
		if both {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sandbox commands sharing a read path were serialized for their full lifetimes")
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, item := range cases {
		if err := os.WriteFile(item.release, []byte("release"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for range cases {
		got := <-outcomes
		if got.err != nil || !strings.Contains(got.result.Output, "SANDBOX_SHARED_READ_CONCURRENT_OK") {
			t.Fatalf("concurrent sandbox %d failed: err=%v output=%s", got.index, got.err, got.result.Output)
		}
	}
	for _, item := range cases {
		sid, releaseSID, err := deriveCapabilitySID(taskCapabilityName(item.taskID), "concurrent shared-read residue test")
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{shared, item.workspace, item.marker} {
			contains, checkErr := pathDACLContainsSID(path, sid)
			if checkErr != nil || contains {
				releaseSID()
				t.Fatalf("task capability ACL residue on %s: contains=%v err=%v", path, contains, checkErr)
			}
		}
		releaseSID()
	}
}

func TestSandboxSuccessfulRootCancellationConfirmsDescendantTerminationBeforeReturn(t *testing.T) {
	workspace := t.TempDir()
	probe := copySandboxProbe(t, workspace)
	marker := filepath.Join(workspace, "success-descendant-marker.txt")

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(150*time.Millisecond, cancel)
	result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
		TaskID:         "sandbox-success-descendant-fence",
		OperationID:    "success-descendant-cancel",
		WorkspaceRoot:  workspace,
		Path:           probe,
		Args:           []string{"-test.run=TestSandboxProbeHelper"},
		Dir:            workspace,
		Env:            probeEnvironment("spawn-descendant-return", "", "", "", "", marker),
		MaxOutputBytes: 32 << 10,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation after successful root exit, got %v output=%s", err, result.Output)
	}
	// The descendant tries to mutate after 650ms. Returning from the sandbox
	// boundary is safe only if independent cleanup already proved its Job empty.
	time.Sleep(900 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("successful-root descendant survived verified cleanup and mutated workspace: %v", err)
	}
}

func TestAppContainerSandboxTimeoutKillsDescendantTree(t *testing.T) {
	workspace := t.TempDir()
	probe := copySandboxProbe(t, workspace)
	marker := filepath.Join(workspace, "descendant-marker.txt")

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	result, err := RunSandboxedCommand(ctx, SandboxCommandSpec{
		TaskID:         "sandbox-descendant-fence",
		OperationID:    "descendant-timeout",
		WorkspaceRoot:  workspace,
		Path:           probe,
		Args:           []string{"-test.run=TestSandboxProbeHelper"},
		Dir:            workspace,
		Env:            probeEnvironment("spawn-descendant", "", "", "", "", marker),
		MaxOutputBytes: 32 << 10,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline termination, got %v output=%s", err, result.Output)
	}
	time.Sleep(900 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("descendant survived sandbox Job Object and mutated workspace: %v", err)
	}
}

func TestSandboxProbeHelper(t *testing.T) {
	mode := os.Getenv("MAR_SANDBOX_PROBE")
	if mode == "" {
		return
	}
	switch mode {
	case "authority":
		inside := os.Getenv("MAR_PROBE_INSIDE")
		outside := os.Getenv("MAR_PROBE_OUTSIDE")
		secret := os.Getenv("MAR_PROBE_SECRET")
		address := os.Getenv("MAR_PROBE_ADDRESS")
		if err := os.WriteFile(inside, []byte("inside"), 0o644); err != nil {
			t.Fatalf("workspace write denied: %v", err)
		}
		if err := os.WriteFile(outside, []byte("outside"), 0o644); err == nil {
			t.Fatal("outside workspace write unexpectedly succeeded")
		}
		if b, err := os.ReadFile(secret); err == nil {
			t.Fatalf("outside secret read unexpectedly succeeded: %q", b)
		}
		conn, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			t.Fatal("network access unexpectedly succeeded without AppContainer capability")
		}
		if got := os.Getenv("MAR_PARENT_SECRET"); got != "" {
			t.Fatalf("ambient parent secret leaked through environment: %q", got)
		}
		fmt.Println("SANDBOX_AUTHORITY_OK")
	case "lpac-aap-deny":
		secret := os.Getenv("MAR_PROBE_SECRET")
		if b, err := os.ReadFile(secret); err == nil {
			t.Fatalf("LPAC unexpectedly inherited ALL APPLICATION PACKAGES read access: %q", b)
		}
		fmt.Println("LPAC_AAP_DENY_OK")
	case "registry-scope":
		var key windows.Handle
		if err := windows.RegOpenKeyEx(windows.HKEY_LOCAL_MACHINE, windows.StringToUTF16Ptr(`SOFTWARE\\Microsoft\\Cryptography`), 0, windows.KEY_READ, &key); err != nil {
			t.Fatalf("registryRead cannot access required HKLM system scope: %v", err)
		}
		_ = windows.RegCloseKey(key)
		if secretKey, err := registry.OpenKey(registry.CURRENT_USER, os.Getenv("MAR_PROBE_SECRET"), registry.QUERY_VALUE); err == nil {
			defer secretKey.Close()
			if value, _, valueErr := secretKey.GetStringValue("Secret"); valueErr == nil && value == "must-not-cross-registry-boundary" {
				t.Fatal("registryRead exposed an ambient HKCU user secret")
			}
		}
		for _, probe := range []struct {
			root windows.Handle
			path string
			name string
		}{
			{windows.HKEY_LOCAL_MACHINE, `SAM\\SAM`, "HKLM SAM"},
			{windows.HKEY_LOCAL_MACHINE, `SECURITY`, "HKLM SECURITY"},
		} {
			key = 0
			if err := windows.RegOpenKeyEx(probe.root, windows.StringToUTF16Ptr(probe.path), 0, windows.KEY_READ, &key); err == nil {
				_ = windows.RegCloseKey(key)
				t.Fatalf("registryRead unexpectedly exposed %s", probe.name)
			}
		}
		fmt.Println("LPAC_REGISTRY_SCOPE_OK")
	case "read-scope":
		readPath := os.Getenv("MAR_PROBE_SECRET")
		writePath := os.Getenv("MAR_PROBE_OUTSIDE")
		b, err := os.ReadFile(readPath)
		if err != nil || string(b) != "runtime-data" {
			t.Fatalf("explicit runtime read scope unavailable: %q %v", b, err)
		}
		if err := os.WriteFile(writePath, []byte("bad"), 0o644); err == nil {
			t.Fatal("explicit read-only scope unexpectedly allowed write")
		}
		fmt.Println("SANDBOX_READ_SCOPE_OK")
	case "shared-read-hold":
		readPath := os.Getenv("MAR_PROBE_SECRET")
		marker := os.Getenv("MAR_PROBE_INSIDE")
		release := os.Getenv("MAR_PROBE_OUTSIDE")
		b, err := os.ReadFile(readPath)
		if err != nil || string(b) != "shared-runtime" {
			t.Fatalf("shared runtime read unavailable: %q %v", b, err)
		}
		if err := os.WriteFile(marker, []byte("entered"), 0o644); err != nil {
			t.Fatalf("write concurrency marker: %v", err)
		}
		deadline := time.Now().Add(6 * time.Second)
		for {
			if _, err := os.Stat(release); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for concurrent release")
			}
			time.Sleep(20 * time.Millisecond)
		}
		fmt.Println("SANDBOX_SHARED_READ_CONCURRENT_OK")
	case "spawn-descendant", "spawn-descendant-return":
		marker := os.Getenv("MAR_PROBE_MARKER")
		cmd := exec.Command(os.Args[0], "-test.run=TestSandboxProbeHelper")
		cmd.Env = append(os.Environ(), "MAR_SANDBOX_PROBE=delayed-descendant", "MAR_PROBE_MARKER="+marker)
		if err := cmd.Start(); err != nil {
			t.Fatalf("spawn sandbox descendant: %v", err)
		}
		fmt.Println("SANDBOX_DESCENDANT_STARTED")
		if mode == "spawn-descendant" {
			time.Sleep(30 * time.Second)
		}
	case "delayed-descendant":
		time.Sleep(650 * time.Millisecond)
		if err := os.WriteFile(os.Getenv("MAR_PROBE_MARKER"), []byte("escaped"), 0o644); err != nil {
			t.Fatalf("delayed descendant write: %v", err)
		}
	default:
		t.Fatalf("unknown sandbox probe mode %q", mode)
	}
}

func copySandboxProbe(t *testing.T, workspace string) string {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	dest := filepath.Join(workspace, "sandbox-probe.exe")
	out, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return dest
}

func pathDACLContainsSID(path string, target *windows.SID) (bool, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return false, err
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return false, err
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid.IsValid() && sid.Equals(target) {
			return true, nil
		}
	}
	return false, nil
}

func grantAllApplicationPackagesRead(t *testing.T, path string) func() {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	sid, err := windows.StringToSid(allApplicationPackagesSID)
	if err != nil {
		t.Fatal(err)
	}
	var pinner runtime.Pinner
	pinner.Pin(sid)
	entry := windows.EXPLICIT_ACCESS{
		AccessPermissions: windows.ACCESS_MASK(windows.FILE_GENERIC_READ),
		AccessMode:        windows.GRANT_ACCESS,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
	updated, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{entry}, dacl)
	if err != nil {
		pinner.Unpin()
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, updated, nil); err != nil {
		pinner.Unpin()
		t.Fatal(err)
	}
	pinner.Unpin()
	return func() {
		_ = sd
		if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
			t.Errorf("restore test file DACL: %v", err)
		}
	}
}

func probeEnvironment(mode, inside, outside, secret, address, marker string) []string {
	keys := []string{"SystemRoot", "WINDIR", "ComSpec", "PATH", "PATHEXT", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "TEMP", "TMP", "ProgramFiles", "ProgramData"}
	env := make([]string, 0, len(keys)+6)
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	return append(env,
		"MAR_SANDBOX_PROBE="+mode,
		"MAR_PROBE_INSIDE="+inside,
		"MAR_PROBE_OUTSIDE="+outside,
		"MAR_PROBE_SECRET="+secret,
		"MAR_PROBE_ADDRESS="+address,
		"MAR_PROBE_MARKER="+marker,
	)
}
