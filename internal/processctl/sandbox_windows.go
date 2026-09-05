//go:build windows

package processctl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	winjob "github.com/kolesnikovae/go-winjob"
	"golang.org/x/sys/windows"
)

const (
	procThreadAttributeSecurityCapabilities     = 0x00020009
	procThreadAttributeAllApplicationPackages   = 0x0002000F
	processCreationAllApplicationPackagesOptOut = uint32(1)
)

type SandboxCommandSpec struct {
	TaskID         string
	OperationID    string
	WorkspaceRoot  string
	ReadPaths      []string
	Path           string
	Args           []string
	Dir            string
	Env            []string
	MaxOutputBytes int
}

type SandboxCommandResult struct {
	Output   string
	ExitCode int
}

type securityCapabilities struct {
	AppContainerSid *windows.SID
	Capabilities    *windows.SIDAndAttributes
	CapabilityCount uint32
	Reserved        uint32
}

type keyedMutexEntry struct {
	mu   sync.Mutex
	refs int
}

type keyedMutexRegistry struct {
	mu      sync.Mutex
	entries map[string]*keyedMutexEntry
}

func newKeyedMutexRegistry() *keyedMutexRegistry {
	return &keyedMutexRegistry{entries: make(map[string]*keyedMutexEntry)}
}

func (r *keyedMutexRegistry) lock(keys ...string) func() {
	unique := make(map[string]struct{}, len(keys))
	normalized := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := unique[key]; exists {
			continue
		}
		unique[key] = struct{}{}
		normalized = append(normalized, key)
	}
	sort.Strings(normalized)
	entries := make([]*keyedMutexEntry, 0, len(normalized))
	r.mu.Lock()
	for _, key := range normalized {
		entry := r.entries[key]
		if entry == nil {
			entry = &keyedMutexEntry{}
			r.entries[key] = entry
		}
		entry.refs++
		entries = append(entries, entry)
	}
	r.mu.Unlock()
	for _, entry := range entries {
		entry.mu.Lock()
	}
	return func() {
		for i := len(entries) - 1; i >= 0; i-- {
			entries[i].mu.Unlock()
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		for i, key := range normalized {
			entry := entries[i]
			entry.refs--
			if entry.refs == 0 && r.entries[key] == entry {
				delete(r.entries, key)
			}
		}
	}
}

var (
	userenvDLL                        = windows.NewLazySystemDLL("userenv.dll")
	kernelBaseDLL                     = windows.NewLazySystemDLL("kernelbase.dll")
	procCreateAppContainerProfile     = userenvDLL.NewProc("CreateAppContainerProfile")
	procDeriveAppContainerSidFromName = userenvDLL.NewProc("DeriveAppContainerSidFromAppContainerName")
	procDeleteAppContainerProfile     = userenvDLL.NewProc("DeleteAppContainerProfile")
	procDeriveCapabilitySidsFromName  = kernelBaseDLL.NewProc("DeriveCapabilitySidsFromName")
	sandboxProfileLocks               = newKeyedMutexRegistry()
	sandboxPathLocks                  = newKeyedMutexRegistry()
)

func RunSandboxedCommand(ctx context.Context, spec SandboxCommandSpec) (result SandboxCommandResult, retErr error) {
	if strings.TrimSpace(spec.TaskID) == "" || strings.TrimSpace(spec.OperationID) == "" {
		return SandboxCommandResult{ExitCode: -1}, errors.New("task id and operation id are required")
	}
	if strings.TrimSpace(spec.Path) == "" || strings.TrimSpace(spec.WorkspaceRoot) == "" {
		return SandboxCommandResult{ExitCode: -1}, errors.New("command path and workspace root are required")
	}
	workspace, err := filepath.Abs(spec.WorkspaceRoot)
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, err
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("resolve sandbox workspace: %w", err)
	}
	dir := spec.Dir
	if strings.TrimSpace(dir) == "" {
		dir = workspace
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("resolve sandbox cwd: %w", err)
	}
	if !pathInside(workspace, dir) {
		return SandboxCommandResult{ExitCode: -1}, errors.New("sandbox command cwd escapes task workspace")
	}

	profileName := sandboxProfileName(spec.TaskID)
	unlock := lockSandboxProfile(profileName)
	defer unlock()

	sid, err := ensureAppContainerProfile(profileName)
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, err
	}
	defer windows.FreeSid(sid)
	defer deleteAppContainerProfile(profileName)

	capabilitySID, releaseCapability, err := deriveCapabilitySID(taskCapabilityName(spec.TaskID), "task file-scope")
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, err
	}
	defer releaseCapability()
	// Go's Windows file runtime initializes Winsock even for ordinary file I/O.
	// registryRead supplies the OS runtime configuration needed for WSAStartup;
	// acceptance tests prove it does not expose ambient HKCU secrets or protected
	// SAM/SECURITY hives, and no network capability is granted.
	registryReadSID, releaseRegistryRead, err := deriveCapabilitySID("registryRead", "Windows runtime registry read")
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, err
	}
	defer releaseRegistryRead()

	grants := map[string]bool{workspace: true}
	for _, readPath := range spec.ReadPaths {
		readPath = strings.TrimSpace(readPath)
		if readPath == "" {
			continue
		}
		abs, err := filepath.Abs(readPath)
		if err != nil {
			return SandboxCommandResult{ExitCode: -1}, err
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("resolve sandbox read path: %w", err)
		}
		if _, exists := grants[resolved]; !exists {
			grants[resolved] = false
		}
	}
	paths := make([]string, 0, len(grants))
	for path := range grants {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	unlockPaths := lockSandboxPaths(paths)
	defer unlockPaths()

	restores := make([]func() error, 0, len(paths))
	defer func() {
		var restoreErr error
		for i := len(restores) - 1; i >= 0; i-- {
			if err := restores[i](); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		}
		if restoreErr == nil {
			return
		}
		wrapped := fmt.Errorf("restore sandbox ACL grants: %w", restoreErr)
		if retErr != nil {
			retErr = errors.Join(retErr, wrapped)
			return
		}
		retErr = wrapped
		if result.ExitCode == 0 {
			result.ExitCode = -1
		}
	}()
	for _, path := range paths {
		restore, err := grantSandboxAccess(path, capabilitySID, grants[path])
		if err != nil {
			return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("grant sandbox access %q: %w", path, err)
		}
		restores = append(restores, restore)
	}

	return launchAppContainerCommand(ctx, profileName, sid, []*windows.SID{capabilitySID, registryReadSID}, spec, workspace, dir)
}

func launchAppContainerCommand(ctx context.Context, profileName string, sid *windows.SID, capabilitySIDs []*windows.SID, spec SandboxCommandSpec, workspace, dir string) (SandboxCommandResult, error) {
	output := &lockedBuffer{max: spec.MaxOutputBytes}

	var sa windows.SecurityAttributes
	sa.Length = uint32(unsafe.Sizeof(sa))
	sa.InheritHandle = 1

	var stdinRead, stdinWrite windows.Handle
	if err := windows.CreatePipe(&stdinRead, &stdinWrite, &sa, 0); err != nil {
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("create sandbox stdin pipe: %w", err)
	}
	defer func() {
		if stdinRead != 0 {
			_ = windows.CloseHandle(stdinRead)
		}
	}()
	defer func() {
		if stdinWrite != 0 {
			_ = windows.CloseHandle(stdinWrite)
		}
	}()
	if err := windows.SetHandleInformation(stdinWrite, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		return SandboxCommandResult{ExitCode: -1}, err
	}

	var outputRead, outputWrite windows.Handle
	if err := windows.CreatePipe(&outputRead, &outputWrite, &sa, 0); err != nil {
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("create sandbox output pipe: %w", err)
	}
	defer func() {
		if outputWrite != 0 {
			_ = windows.CloseHandle(outputWrite)
		}
	}()
	if err := windows.SetHandleInformation(outputRead, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		windows.CloseHandle(outputRead)
		return SandboxCommandResult{ExitCode: -1}, err
	}
	outputFile := os.NewFile(uintptr(outputRead), "mar-sandbox-output")
	if outputFile == nil {
		windows.CloseHandle(outputRead)
		return SandboxCommandResult{ExitCode: -1}, errors.New("wrap sandbox output handle")
	}
	outputDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(output, outputFile)
		_ = outputFile.Close()
		close(outputDone)
	}()

	attrs, err := windows.NewProcThreadAttributeList(3)
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("allocate process attribute list: %w", err)
	}
	defer attrs.Delete()

	handles := []windows.Handle{stdinRead, outputWrite}
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("set inherited handle list: %w", err)
	}
	capabilityAttrs := make([]windows.SIDAndAttributes, 0, len(capabilitySIDs))
	for _, capabilitySID := range capabilitySIDs {
		capabilityAttrs = append(capabilityAttrs, windows.SIDAndAttributes{Sid: capabilitySID, Attributes: windows.SE_GROUP_ENABLED})
	}
	if len(capabilityAttrs) == 0 {
		return SandboxCommandResult{ExitCode: -1}, errors.New("LPAC requires at least one explicit capability")
	}
	caps := securityCapabilities{AppContainerSid: sid, Capabilities: &capabilityAttrs[0], CapabilityCount: uint32(len(capabilityAttrs))}
	if err := attrs.Update(procThreadAttributeSecurityCapabilities, unsafe.Pointer(&caps), unsafe.Sizeof(caps)); err != nil {
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("set AppContainer security capabilities: %w", err)
	}
	lpacPolicy := processCreationAllApplicationPackagesOptOut
	if err := attrs.Update(procThreadAttributeAllApplicationPackages, unsafe.Pointer(&lpacPolicy), unsafe.Sizeof(lpacPolicy)); err != nil {
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("set LPAC all-application-packages opt-out: %w", err)
	}

	argv := append([]string{spec.Path}, spec.Args...)
	cmdLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, err
	}
	appName, err := windows.UTF16PtrFromString(spec.Path)
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, err
	}
	currentDir, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, err
	}
	envBlock, err := windowsEnvironmentBlock(spec.Env)
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, err
	}

	job, err := winjob.Create("", winjob.LimitKillOnJobClose)
	if err != nil {
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("create sandbox Job Object: %w", err)
	}
	defer job.Close()

	si := windows.StartupInfoEx{}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput = stdinRead
	si.StdOutput = outputWrite
	si.StdErr = outputWrite
	si.ProcThreadAttributeList = attrs.List()

	var pi windows.ProcessInformation
	creationFlags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_NO_WINDOW)
	if err := windows.CreateProcess(appName, cmdLine, nil, nil, true, creationFlags, &envBlock[0], currentDir, &si.StartupInfo, &pi); err != nil {
		runtime.KeepAlive(capabilityAttrs)
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("create AppContainer process %s: %w", profileName, err)
	}
	runtime.KeepAlive(capabilityAttrs)
	defer windows.CloseHandle(pi.Thread)
	defer windows.CloseHandle(pi.Process)

	proc, err := os.FindProcess(int(pi.ProcessId))
	if err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("open sandbox process: %w", err)
	}
	defer proc.Release()
	if err := job.Assign(proc); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("assign AppContainer process to Job Object: %w", err)
	}
	contained, err := job.Contains(proc)
	if err != nil || !contained {
		_ = job.Terminate()
		if err != nil {
			return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("verify sandbox Job Object: %w", err)
		}
		return SandboxCommandResult{ExitCode: -1}, errors.New("sandbox process escaped expected Job Object")
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		_ = job.Terminate()
		return SandboxCommandResult{ExitCode: -1}, fmt.Errorf("resume sandbox process: %w", err)
	}

	_ = windows.CloseHandle(stdinRead)
	stdinRead = 0
	_ = windows.CloseHandle(stdinWrite)
	stdinWrite = 0
	_ = windows.CloseHandle(outputWrite)
	outputWrite = 0

	exitCode, waitErr := waitSandboxProcess(ctx, job, pi.Process)
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if waitErr != nil {
		_ = job.Terminate()
		if err := waitForNoActive(cleanupCtx, job); err != nil {
			return SandboxCommandResult{Output: output.String(), ExitCode: -1}, fmt.Errorf("%s; sandbox termination unconfirmed: %v", waitErr, err)
		}
		select {
		case <-outputDone:
		case <-cleanupCtx.Done():
		}
		return SandboxCommandResult{Output: output.String(), ExitCode: -1}, waitErr
	}
	if err := waitForNoActive(ctx, job); err != nil {
		_ = job.Terminate()
		return SandboxCommandResult{Output: output.String(), ExitCode: exitCode}, err
	}
	select {
	case <-outputDone:
	case <-ctx.Done():
		return SandboxCommandResult{Output: output.String(), ExitCode: exitCode}, ctx.Err()
	}
	result := SandboxCommandResult{Output: output.String(), ExitCode: exitCode}
	if exitCode != 0 {
		return result, SandboxExitError{Code: exitCode}
	}
	return result, nil
}

type SandboxExitError struct{ Code int }

func (e SandboxExitError) Error() string {
	return fmt.Sprintf("sandboxed command exited with code %d", e.Code)
}
func (e SandboxExitError) ExitCode() int { return e.Code }

func waitSandboxProcess(ctx context.Context, job *winjob.JobObject, process windows.Handle) (int, error) {
	waitDone := make(chan error, 1)
	go func() {
		status, err := windows.WaitForSingleObject(process, windows.INFINITE)
		if err != nil {
			waitDone <- err
			return
		}
		if status != windows.WAIT_OBJECT_0 {
			waitDone <- fmt.Errorf("unexpected process wait status %d", status)
			return
		}
		waitDone <- nil
	}()
	select {
	case err := <-waitDone:
		if err != nil {
			return -1, err
		}
		var code uint32
		if err := windows.GetExitCodeProcess(process, &code); err != nil {
			return -1, err
		}
		return int(code), nil
	case <-ctx.Done():
		if err := job.Terminate(); err != nil {
			return -1, fmt.Errorf("%w; terminate sandbox job: %v", ctx.Err(), err)
		}
		return -1, ctx.Err()
	}
}

func ensureAppContainerProfile(name string) (*windows.SID, error) {
	namePtr, _ := windows.UTF16PtrFromString(name)
	displayPtr, _ := windows.UTF16PtrFromString("MAR Worker Sandbox")
	descPtr, _ := windows.UTF16PtrFromString("MAR task-scoped autonomous worker sandbox")
	var sid *windows.SID
	hr, _, _ := procCreateAppContainerProfile.Call(
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(displayPtr)),
		uintptr(unsafe.Pointer(descPtr)),
		0,
		0,
		uintptr(unsafe.Pointer(&sid)),
	)
	if uint32(hr) == 0 && sid != nil {
		return sid, nil
	}
	if uint32(hr) != 0x800700B7 {
		return nil, fmt.Errorf("CreateAppContainerProfile failed: HRESULT 0x%08x", uint32(hr))
	}
	hr, _, _ = procDeriveAppContainerSidFromName.Call(uintptr(unsafe.Pointer(namePtr)), uintptr(unsafe.Pointer(&sid)))
	if uint32(hr) != 0 || sid == nil {
		return nil, fmt.Errorf("DeriveAppContainerSidFromAppContainerName failed: HRESULT 0x%08x", uint32(hr))
	}
	return sid, nil
}

func deleteAppContainerProfile(name string) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return
	}
	_, _, _ = procDeleteAppContainerProfile.Call(uintptr(unsafe.Pointer(namePtr)))
}

func sandboxProfileName(taskID string) string {
	sum := sha256.Sum256([]byte(taskID))
	return "MAR.Worker." + hex.EncodeToString(sum[:12])
}

func taskCapabilityName(taskID string) string {
	sum := sha256.Sum256([]byte(taskID))
	return "MAR.Worker.FileScope." + hex.EncodeToString(sum[:16])
}

func deriveCapabilitySID(name, label string) (*windows.SID, func(), error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, nil, err
	}
	var groupSids **windows.SID
	var groupCount uint32
	var capabilitySids **windows.SID
	var capabilityCount uint32
	result, _, callErr := procDeriveCapabilitySidsFromName.Call(
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(&groupSids)),
		uintptr(unsafe.Pointer(&groupCount)),
		uintptr(unsafe.Pointer(&capabilitySids)),
		uintptr(unsafe.Pointer(&capabilityCount)),
	)
	if result == 0 {
		if callErr == windows.ERROR_SUCCESS {
			callErr = errors.New("DeriveCapabilitySidsFromName failed")
		}
		return nil, nil, fmt.Errorf("derive %s capability SID: %w", label, callErr)
	}
	groups := unsafe.Slice(groupSids, int(groupCount))
	capabilities := unsafe.Slice(capabilitySids, int(capabilityCount))
	cleanup := func() {
		for _, groupSID := range groups {
			if groupSID != nil {
				_, _ = windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(groupSID))))
			}
		}
		for _, capabilitySID := range capabilities {
			if capabilitySID != nil {
				_, _ = windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(capabilitySID))))
			}
		}
		if groupSids != nil {
			_, _ = windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(groupSids))))
		}
		if capabilitySids != nil {
			_, _ = windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(capabilitySids))))
		}
	}
	if len(capabilities) != 1 || capabilities[0] == nil {
		cleanup()
		return nil, nil, fmt.Errorf("derive %s capability SID: expected one capability SID, got %d", label, len(capabilities))
	}
	return capabilities[0], cleanup, nil
}

func lockSandboxProfile(name string) func() {
	return sandboxProfileLocks.lock(name)
}

func lockSandboxPaths(paths []string) func() {
	keys := make([]string, 0, len(paths))
	for _, path := range paths {
		keys = append(keys, strings.ToLower(filepath.Clean(path)))
	}
	return sandboxPathLocks.lock(keys...)
}

func grantSandboxAccess(path string, sid *windows.SID, writable bool) (func() error, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return nil, err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return nil, err
	}
	permissions := windows.ACCESS_MASK(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_EXECUTE)
	if writable {
		permissions |= windows.ACCESS_MASK(windows.FILE_GENERIC_WRITE | windows.DELETE)
	}
	inheritance := uint32(0)
	if info.IsDir() {
		inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	entry := windows.EXPLICIT_ACCESS{
		AccessPermissions: permissions,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
	newACL, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{entry}, dacl)
	if err != nil {
		return nil, err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, newACL, nil); err != nil {
		return nil, err
	}
	return func() error {
		// Keep the original self-relative descriptor live while restoring its DACL.
		_ = sd
		return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	}, nil
}

func windowsEnvironmentBlock(env []string) ([]uint16, error) {
	clean := make([]string, 0, len(env))
	for _, item := range env {
		if item == "" || strings.IndexByte(item, '=') <= 0 || strings.IndexByte(item, 0) >= 0 {
			continue
		}
		clean = append(clean, item)
	}
	sort.Slice(clean, func(i, j int) bool { return strings.ToUpper(clean[i]) < strings.ToUpper(clean[j]) })
	block := make([]uint16, 0, 256)
	for _, item := range clean {
		u, err := windows.UTF16FromString(item)
		if err != nil {
			return nil, err
		}
		block = append(block, u...)
	}
	block = append(block, 0)
	if len(block) == 1 {
		block = append(block, 0)
	}
	return block, nil
}

func pathInside(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
