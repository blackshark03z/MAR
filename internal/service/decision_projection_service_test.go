package service

import (
	"testing"

	"mar/internal/domain"
)

func TestFirstFailedVerificationCommandSelectsBoundedDurableFailure(t *testing.T) {
	evidence := domain.VerificationEvidence{Commands: []domain.VerificationCommandEvidence{
		{Name: "go", Args: []string{"test", "./..."}, Passed: true, ExitCode: 0},
		{Name: "go", Args: []string{"vet", "./..."}, Passed: false, ExitCode: 1, DurationMS: 321, OutputSHA256: "sha-fail", OutputPrefix: "vet failure"},
		{Name: "go", Args: []string{"build", "./..."}, Passed: false, ExitCode: 2, OutputSHA256: "sha-later", OutputPrefix: "later failure"},
	}}
	failure := firstFailedVerificationCommand(evidence)
	if failure == nil {
		t.Fatal("expected first failed verification command")
	}
	if failure.Index != 2 || failure.Name != "go" || failure.ExitCode != 1 || failure.DurationMS != 321 || failure.OutputSHA256 != "sha-fail" || failure.OutputPrefix != "vet failure" {
		t.Fatalf("unexpected first failure projection: %+v", failure)
	}
	failure.Args[0] = "mutated"
	if evidence.Commands[1].Args[0] != "vet" {
		t.Fatal("projection failure args alias durable evidence")
	}
}

func TestFirstFailedVerificationCommandReturnsNilWhenCommandsPass(t *testing.T) {
	evidence := domain.VerificationEvidence{Commands: []domain.VerificationCommandEvidence{{Name: "go", Passed: true, ExitCode: 0}}}
	if failure := firstFailedVerificationCommand(evidence); failure != nil {
		t.Fatalf("passing verification unexpectedly projected failure: %+v", failure)
	}
}
