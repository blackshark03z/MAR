//go:build windows

package processctl

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"mar/internal/testsupport"

	"golang.org/x/sys/windows"
)

func TestSandboxHostReadinessDoesNotRequireProjectACLMutation(t *testing.T) {
	testsupport.RequireOutsideAppContainer(t)
	root := t.TempDir()
	name, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	// Retain an already-authorized handle so restoration cannot be blocked by
	// the temporary, non-inheritable denial placed on this fixture root.
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	// OWNER RIGHTS suppresses the owner's implicit WRITE_DAC permission;
	// denying the current user's SID alone does not constrain that permission.
	ownerRights, err := windows.StringToSid("S-1-3-4")
	if err != nil {
		t.Fatal(err)
	}
	var pinner runtime.Pinner
	pinner.Pin(ownerRights)
	defer pinner.Unpin()
	restricted, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.ACCESS_MASK(windows.WRITE_DAC),
		AccessMode:        windows.DENY_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(ownerRights),
		},
	}}, dacl)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, restricted, nil); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
			t.Errorf("restore fixture root DACL: %v", err)
		}
		runtime.KeepAlive(sd)
	}()
	blocked, err := windows.CreateFile(name, windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err == nil {
		windows.CloseHandle(blocked)
		t.Fatal("fixture did not deny project root ACL mutation")
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("verify fixture root ACL denial: %v", err)
	}

	// Host NUL readiness must depend only on the small probe workspace. The
	// project root need not permit ACL changes or recursive grant propagation.
	if err := CheckSandboxHostReady(context.Background(), root); err != nil {
		t.Fatalf("host readiness depended on project ACL mutation: %v", err)
	}
}
