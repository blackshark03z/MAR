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
	want := [][]string{{"test", "-p", "1", "-run", "^$", "-timeout", "180s", "./..."}}
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

func TestResearchArtifactVerificationProfileFailsClosedOnSourceChanges(t *testing.T) {
	profile := verification.ResearchArtifactProfile()
	if profile.ID != verification.ResearchArtifactProfileID {
		t.Fatalf("unexpected research profile id %q", profile.ID)
	}
	if profile.EffectiveChangeScope() != verification.ChangeScopeResearchArtifacts {
		t.Fatalf("research profile must enforce research-artifact scope: %+v", profile)
	}
	if len(profile.Commands) != 0 {
		t.Fatalf("research profile must remain language-neutral, got commands: %+v", profile.Commands)
	}
	allowed := []string{"README.md", "evals/cases.json", "research/plan.yaml", "config/study.toml", "data/results.csv", "notes/study.txt"}
	if err := profile.ValidateChangedPaths(allowed); err != nil {
		t.Fatalf("research artifacts were rejected: %v", err)
	}
	for _, path := range []string{"cmd/mar/main.go", "scripts/check.py", "ui/app.ts", "ops/run.ps1"} {
		if err := profile.ValidateChangedPaths([]string{path}); err == nil {
			t.Fatalf("research profile admitted executable/source path %q", path)
		}
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

func TestPythonPortableVerificationProfileUsesBoundedPortableSubset(t *testing.T) {
	pythonExecutable := `C:\\Python\\python.exe`
	profile := pythonPortableVerificationProfile(pythonExecutable)
	if profile.ID != "python-portable" {
		t.Fatalf("unexpected profile id %q", profile.ID)
	}
	want := [][]string{
		{"-m", "unittest", "discover", "-s", "portable_tests", "-p", "test_*.py", "-v"},
		{"-m", "compileall", "-q", "."},
	}
	assertVerificationCommands(t, profile.ID, profile.Commands, pythonExecutable, want)
}

func TestBuiltinVerificationProfilesIncludeReleaseResearchAndPythonOnlyWhenAvailable(t *testing.T) {
	goExecutable := `C:\\toolchain\\go.exe`
	pythonExecutable := `C:\\Python\\python.exe`
	withoutPython := builtinVerificationProfiles(goExecutable, "")
	if len(withoutPython) != 4 ||
		withoutPython[0].ID != "go-standard" ||
		withoutPython[1].ID != "go-docs" ||
		withoutPython[2].ID != "go-release" ||
		withoutPython[3].ID != verification.ResearchArtifactProfileID {
		t.Fatalf("unexpected Go/research built-in verification profiles: %+v", withoutPython)
	}
	withPython := builtinVerificationProfiles(goExecutable, pythonExecutable)
	if len(withPython) != 6 || withPython[4].ID != "python-standard" || withPython[5].ID != "python-portable" {
		t.Fatalf("Python verification profiles were not registered with an available interpreter: %+v", withPython)
	}
}

func assertVerificationCommands(t *testing.T, profileID string, commands []verification.Command, executable string, want [][]string) {
	t.Helper()
	if len(commands) != len(want) {
		t.Fatalf("profile %s unexpected command count %d", profileID, len(commands))
	}
	for i, cmd := range commands {
		if cmd.Name != executable || cmd.Cwd != "." || !slices.Equal(cmd.Args, want[i]) {
			t.Fatalf("profile %s command %d mismatch: %+v", profileID, i+1, cmd)
		}
	}
}
