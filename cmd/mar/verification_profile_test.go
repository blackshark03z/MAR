package main

import (
	"slices"
	"testing"

	"mar/internal/verification"
)

func TestGoStandardVerificationProfileUsesSequentialPackageBuilds(t *testing.T) {
	goExecutable := `C:\\toolchain\\go.exe`
	profile := goStandardVerificationProfile(goExecutable)
	if profile.ID != "go-standard" {
		t.Fatalf("unexpected profile id %q", profile.ID)
	}
	want := [][]string{
		{"test", "-p", "1", "-count=1", "-timeout", "180s", "./..."},
		{"vet", "-p", "1", "./..."},
		{"build", "-p", "1", "./..."},
	}
	assertVerificationCommands(t, profile.ID, profile.Commands, goExecutable, want)
}

func TestGoDocsVerificationProfileCompilesTestsWithoutRunningHostSensitiveSuite(t *testing.T) {
	goExecutable := `C:\\toolchain\\go.exe`
	profile := goDocsVerificationProfile(goExecutable)
	if profile.ID != "go-docs" {
		t.Fatalf("unexpected profile id %q", profile.ID)
	}
	want := [][]string{
		{"test", "-p", "1", "-count=1", "-run", "^$", "-timeout", "180s", "./..."},
		{"vet", "-p", "1", "./..."},
		{"build", "-p", "1", "./..."},
	}
	assertVerificationCommands(t, profile.ID, profile.Commands, goExecutable, want)
}

func TestBuiltinVerificationProfilesKeepFullAndDocsProfilesDistinct(t *testing.T) {
	profiles := builtinVerificationProfiles(`C:\\toolchain\\go.exe`)
	if len(profiles) != 2 || profiles[0].ID != "go-standard" || profiles[1].ID != "go-docs" {
		t.Fatalf("unexpected built-in verification profiles: %+v", profiles)
	}
}

func assertVerificationCommands(t *testing.T, profileID string, commands []verification.Command, goExecutable string, want [][]string) {
	t.Helper()
	if len(commands) != len(want) {
		t.Fatalf("profile %s unexpected command count %d", profileID, len(commands))
	}
	for i, cmd := range commands {
		if cmd.Name != goExecutable || cmd.Cwd != "." || !slices.Equal(cmd.Args, want[i]) {
			t.Fatalf("profile %s command %d mismatch: %+v", profileID, i+1, cmd)
		}
	}
}
