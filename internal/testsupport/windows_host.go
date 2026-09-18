//go:build windows

package testsupport

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

const tokenIsAppContainerClass = 29

// RequireOutsideAppContainer keeps host-security and nested-sandbox acceptance
// tests active on normal release runs while preventing a verifier LPAC from
// pretending it has host-level authority. The decision is based on the current
// Windows token, not an environment flag.
func InAppContainer() (bool, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false, err
	}
	defer token.Close()
	var isAppContainer uint32
	var returned uint32
	if err := windows.GetTokenInformation(token, tokenIsAppContainerClass, (*byte)(unsafe.Pointer(&isAppContainer)), uint32(unsafe.Sizeof(isAppContainer)), &returned); err != nil {
		return false, err
	}
	return isAppContainer != 0, nil
}

func RequireOutsideAppContainer(t testing.TB) {
	t.Helper()
	inside, err := InAppContainer()
	if err != nil {
		t.Fatalf("query AppContainer execution state: %v", err)
	}
	if inside {
		t.Skip("host-security/nested-sandbox acceptance requires execution outside an AppContainer")
	}
}
