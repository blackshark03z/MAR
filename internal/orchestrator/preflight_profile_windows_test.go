//go:build windows

package orchestrator

import (
	"testing"

	"mar/internal/verification"
)

func TestPreflightRequiresGoModuleOnlyForGoProfiles(t *testing.T) {
	for _, profile := range []verification.Profile{
		{ID: "python-standard"},
		verification.ResearchArtifactProfile(),
	} {
		if verificationProfileRequiresGoModule(profile) {
			t.Fatalf("%s unexpectedly requires go.mod", profile.ID)
		}
	}
	for _, id := range []string{"go-standard", "go-docs", "go-release"} {
		if !verificationProfileRequiresGoModule(verification.Profile{ID: id}) {
			t.Fatalf("%s must continue to require go.mod", id)
		}
	}
}
