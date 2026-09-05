//go:build windows

package processctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

var ErrSandboxHostNotPrepared = errors.New("Windows sandbox host is not prepared")

const (
	allApplicationPackagesSID           = "S-1-15-2-1"
	allRestrictedApplicationPackagesSID = "S-1-15-2-2"
)

// CheckSandboxHostReady verifies the host prerequisite that LPAC worker
// processes can open the Windows NUL device. Windows resets that
// device's security descriptor on every boot, so this must be checked at
// runtime rather than assumed from a previous successful run.
func CheckSandboxHostReady(ctx context.Context, workspaceRoot string) error {
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return err
	}
	systemRoot := strings.TrimSpace(os.Getenv("SystemRoot"))
	if systemRoot == "" {
		systemRoot = strings.TrimSpace(os.Getenv("WINDIR"))
	}
	if systemRoot == "" {
		return fmt.Errorf("%w: Windows SystemRoot is unavailable", ErrSandboxHostNotPrepared)
	}
	cmd := filepath.Join(systemRoot, "System32", "cmd.exe")
	profileRoot := filepath.Join(root, ".mar", "runtime", "host-readiness", "profile")
	tempRoot := filepath.Join(root, ".mar", "runtime", "host-readiness", "tmp")
	appData := filepath.Join(profileRoot, "AppData", "Roaming")
	localAppData := filepath.Join(profileRoot, "AppData", "Local")
	for _, dir := range []string{profileRoot, tempRoot, appData, localAppData} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("prepare sandbox readiness environment: %w", err)
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := RunSandboxedCommand(probeCtx, SandboxCommandSpec{
		TaskID:        "mar-sandbox-host-readiness",
		OperationID:   "probe-null-device",
		WorkspaceRoot: root,
		Path:          cmd,
		Args:          []string{"/d", "/s", "/c", "type NUL > NUL"},
		Dir:           root,
		Env: []string{
			"SystemRoot=" + systemRoot,
			"WINDIR=" + systemRoot,
			"ComSpec=" + cmd,
			"PATH=" + filepath.Join(systemRoot, "System32") + string(os.PathListSeparator) + systemRoot,
			"PATHEXT=.COM;.EXE;.BAT;.CMD",
			"USERPROFILE=" + profileRoot,
			"HOME=" + profileRoot,
			"APPDATA=" + appData,
			"LOCALAPPDATA=" + localAppData,
			"TEMP=" + tempRoot,
			"TMP=" + tempRoot,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
			"GCM_INTERACTIVE=Never",
		},
		MaxOutputBytes: 4 << 10,
	})
	if err != nil {
		return fmt.Errorf("%w: AppContainer NUL probe failed: %v; output=%s", ErrSandboxHostNotPrepared, err, strings.TrimSpace(result.Output))
	}
	return nil
}

// PrepareSandboxHost grants the two well-known AppContainer package SIDs the
// minimum generic read/write/execute rights needed on the Windows NUL device.
// Modifying the device DACL requires an elevated token. The setting is a host
// boot prerequisite, not worker authority, and Windows resets it on reboot.
func PrepareSandboxHost() error {
	name, err := windows.UTF16PtrFromString("NUL")
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		name,
		windows.READ_CONTROL|windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return fmt.Errorf("open NUL for DACL update (run elevated): %w", err)
	}
	defer windows.CloseHandle(handle)

	sd, err := windows.GetSecurityInfo(handle, windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read NUL security descriptor: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read NUL DACL: %w", err)
	}

	packageSID, err := windows.StringToSid(allApplicationPackagesSID)
	if err != nil {
		return err
	}
	restrictedSID, err := windows.StringToSid(allRestrictedApplicationPackagesSID)
	if err != nil {
		return err
	}
	var pinner runtime.Pinner
	pinner.Pin(packageSID)
	pinner.Pin(restrictedSID)
	defer pinner.Unpin()

	permissions := windows.ACCESS_MASK(windows.GENERIC_READ | windows.GENERIC_WRITE | windows.GENERIC_EXECUTE)
	entries := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: permissions,
			AccessMode:        windows.SET_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(packageSID),
			},
		},
		{
			AccessPermissions: permissions,
			AccessMode:        windows.SET_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(restrictedSID),
			},
		},
	}
	updated, err := windows.ACLFromEntries(entries, dacl)
	if err != nil {
		return fmt.Errorf("build NUL DACL: %w", err)
	}
	if err := windows.SetSecurityInfo(handle, windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, updated, nil); err != nil {
		return fmt.Errorf("apply NUL DACL (run elevated): %w", err)
	}
	return nil
}
