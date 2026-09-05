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
	case "spawn-descendant":
		marker := os.Getenv("MAR_PROBE_MARKER")
		cmd := exec.Command(os.Args[0], "-test.run=TestSandboxProbeHelper")
		cmd.Env = append(os.Environ(), "MAR_SANDBOX_PROBE=delayed-descendant", "MAR_PROBE_MARKER="+marker)
		if err := cmd.Start(); err != nil {
			t.Fatalf("spawn sandbox descendant: %v", err)
		}
		fmt.Println("SANDBOX_DESCENDANT_STARTED")
		time.Sleep(30 * time.Second)
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
