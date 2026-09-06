package main

import (
	"slices"
	"testing"
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
	if len(profile.Commands) != len(want) {
		t.Fatalf("unexpected command count %d", len(profile.Commands))
	}
	for i, cmd := range profile.Commands {
		if cmd.Name != goExecutable || cmd.Cwd != "." || !slices.Equal(cmd.Args, want[i]) {
			t.Fatalf("command %d mismatch: %+v", i+1, cmd)
		}
	}
}
