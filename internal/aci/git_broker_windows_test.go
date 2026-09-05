//go:build windows

package aci

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContainedGitBrokerDisablesRepositoryExternalExecution(t *testing.T) {
	root := t.TempDir()
	runGitSetup(t, root, "init")
	runGitSetup(t, root, "config", "user.email", "mar-test@example.invalid")
	runGitSetup(t, root, "config", "user.name", "MAR Test")
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitSetup(t, root, "add", "a.txt")
	runGitSetup(t, root, "commit", "-m", "base")
	if err := os.WriteFile(path, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "external-helper-ran.txt")
	script := filepath.Join(root, "external-helper.cmd")
	scriptBody := "@echo off\r\necho external>\"" + marker + "\"\r\nexit /b 0\r\n"
	if err := os.WriteFile(script, []byte(scriptBody), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitSetup(t, root, "config", "diff.external", script)
	runGitSetup(t, root, "config", "core.fsmonitor", script)

	broker, err := NewContainedGitBroker()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := broker.Status(ctx, "task-git-broker-security", root, 16<<10)
	if err != nil {
		t.Fatalf("typed status failed: %v output=%s", err, status.Output)
	}
	diff, err := broker.Diff(ctx, "task-git-broker-security", root, []string{"a.txt"}, 16<<10)
	if err != nil {
		t.Fatalf("typed diff failed: %v output=%s", err, diff.Output)
	}
	if !strings.Contains(diff.Output, "-one") || !strings.Contains(diff.Output, "+two") {
		t.Fatalf("unexpected typed diff output: %q", diff.Output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("repository config escaped typed Git broker and executed helper: %v", err)
	}
}

func TestGitBrokerEnvironmentDoesNotInheritAmbientSecrets(t *testing.T) {
	root := t.TempDir()
	broker, err := NewContainedGitBroker()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAR_GIT_BROKER_SECRET", "must-not-cross-broker")
	env, err := gitBrokerEnvironment(root, broker.gitPath)
	if err != nil {
		t.Fatal(err)
	}
	joined := "\n" + strings.Join(env, "\n") + "\n"
	if strings.Contains(joined, "MAR_GIT_BROKER_SECRET=") || strings.Contains(joined, "must-not-cross-broker") {
		t.Fatalf("ambient secret leaked into typed Git broker environment: %v", env)
	}
}
