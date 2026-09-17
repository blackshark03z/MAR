//go:build windows

package orchestrator

import (
	"testing"

	"mar/internal/verification"
)

func TestPythonStandardPreflightDoesNotRequireGoModule(t *testing.T) {
	if verificationProfileRequiresGoModule(verification.Profile{ID: "python-standard"}) {
		t.Fatal("python-standard unexpectedly requires go.mod")
	}
	if !verificationProfileRequiresGoModule(verification.Profile{ID: "go-standard"}) {
		t.Fatal("go-standard must continue to require go.mod")
	}
	if !verificationProfileRequiresGoModule(verification.Profile{ID: "go-docs"}) {
		t.Fatal("go-docs must continue to require go.mod")
	}
}
