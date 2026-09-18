package main

import (
	"slices"
	"testing"

	"mar/internal/verification"
)

func TestGoStandardVerificationProfileUsesCacheFriendlyFullGate(t *testing.T) {
	goExecutable := `C:\\toolchain\\go.exe`
	profile := goStandardVerificationProfile(goExecutable)
	if profile.ID != "go-standard" {
		t.Fatalf("unexpected profile id %q", profile.ID)
	}
	want := [][]string{
		{"test", "-p", "1", "-timeout", "180s", "./..."},
		{"vet", "-p", "1", "./..."},
		{"build", "-p", "1", "./..."},
	}
	assertVerificationCommands(t, profile.ID, profile.Commands, goExecutable, want)
	if slices.Contains(profile.Commands[0].Args, "-count=1") {
		t.Fatal("go-standard must not disable Go test caching")
	}
	if slices.Contains(profile.Commands[0].Args, "-v") {
		t.Fatal("go-standard should avoid verbose success output in the daily gate")
	}
}

func TestGoDocsVerificationProfileUsesMinimalCachedCompileGate(t *testing.T) {
	goExecutable := `C:\\toolchain\\go.exe`
	profile := goDocsVerificationProfile(goExecutable)
	if profile.ID != "go-docs" {
		t.Fatalf("unexpected profile id %q", profile.ID)
	}
	if profile.EffectiveChangeScope() != verification.ChangeScopeDocumentationOnly {
		t.Fatalf("go-docs must enforce documentation-only change admission: %+v", profile)
	}
	want := [][]string{
		{"test", "-p", "1", "-run", "^$", "-timeout", "180s", "./..."},
	}
	assertVerificationCommands(t, profile.ID, profile.Commands, goExecutable, want)
	if slices.Contains(profile.Commands[0].Args, "-count=1") {
		t.Fatal("go-docs must keep the compile/discovery gate cache-friendly")
	}
}

func TestGoReleaseVerificationProfilePreservesUncachedCanonicalGate(t *testing.T) {
	goExecutable := `C:\\toolchain\\go.exe`
	profile := goReleaseVerificationProfile(goExecutable)
	if profile.ID != "go-release" {
		t.Fatalf("unexpected profile id %q", profile.ID)
	}
	want := [][]string{
		{"test", "-v", "-p", "1", "-count=1", "-timeout", "180s", "./..."},
		{"vet", "-p", "1", "./..."},
		{"build", "-p", "1", "./..."},
	}
	assertVerificationCommands(t, profile.ID, profile.Commands, goExecutable, want)
}

func TestGoDocsVerificationProfileRejectsSourcePaths(t *testing.T) {
	profile := goDocsVerificationProfile(`C:\\toolchain\\go.exe`)
	if err := profile.ValidateChangedPaths([]string{"README.md", "docs/guide.rst"}); err != nil {
		t.Fatalf("documentation paths were rejected: %v", err)
	}
	if err := profile.ValidateChangedPaths([]string{"README.md", "cmd/mar/main.go"}); err == nil {
		t.Fatal("go-docs admitted a source change")
	}
}

func TestPythonStandardVerificationProfileUsesBoundedStandardModules(t *testing.T) {
	pythonExecutable := `C:\\Python\\python.exe`
	profile := pythonStandardVerificationProfile(pythonExecutable)
	if profile.ID != "python-standard" {
		t.Fatalf("unexpected profile id %q", profile.ID)
	}
	want := [][]string{
		{"-m", "unittest", "discover", "-v"},
		{"-m", "compileall", "-q", "."},
	}
	assertVerificationCommands(t, profile.ID, profile.Commands, pythonExecutable, want)
}

func TestBuiltinVerificationProfilesIncludeReleaseAndPythonOnlyWhenAvailable(t *testing.T) {
	goExecutable := `C:\\toolchain\\go.exe`
	pythonExecutable := `C:\\Python\\python.exe`
	withoutPython := builtinVerificationProfiles(goExecutable, "")
	if len(withoutPython) != 3 || withoutPython[0].ID != "go-standard" || withoutPython[1].ID != "go-docs" || withoutPython[2].ID != "go-release" {
		t.Fatalf("unexpected Go-only built-in verification profiles: %+v", withoutPython)
	}
	withPython := builtinVerificationProfiles(goExecutable, pythonExecutable)
	if len(withPython) != 4 || withPython[3].ID != "python-standard" {
		t.Fatalf("python-standard was not registered with an available interpreter: %+v", withPython)
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
