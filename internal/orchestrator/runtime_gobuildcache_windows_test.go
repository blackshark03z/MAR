//go:build windows

package orchestrator

import (
	"path/filepath"
	"strings"
	"testing"

	"mar/internal/store"
	"mar/internal/verification"
)

func TestNewRuntimeRejectsGoBuildCacheOutsideDataRoot(t *testing.T) {
	dataRoot := t.TempDir()
	outside := t.TempDir()
	s, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_, err = NewRuntime(s, RuntimeConfig{
		DataRoot:   dataRoot,
		Executable: "mar-test.exe",
		VerificationProfiles: []verification.Profile{
			{ID: "test-profile"},
		},
		GoBuildCache: outside,
	})
	if err == nil || !strings.Contains(err.Error(), "shared Go build cache must remain inside MAR data root") {
		t.Fatalf("outside GoBuildCache validation = %v, want fail-closed DataRoot rejection", err)
	}
}
