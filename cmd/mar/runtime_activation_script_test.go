package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivateCurrentHeadScriptPreservesPromotionAndRollbackContract(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "activate-current-head.ps1")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	required := []string{
		"git -C $RepoRoot rev-parse HEAD",
		"git -C $RepoRoot status --porcelain",
		"main.embeddedReleaseVersion",
		"main.embeddedBuildTimestamp",
		"release-manifest",
		"mar-v1-stable.exe",
		"mar.db",
		"mar.db-wal",
		"mar.db-shm",
		"source_revision",
		"trusted_for_release",
		"Get-OptionalPropertyValue",
		"PSObject.Properties[$Name]",
		"start-owner-console.ps1",
		"Stop-MARListener",
		"Restore-BackupFile",
		"Activation failed; previous runtime state was restored",
	}
	for _, needle := range required {
		if !strings.Contains(text, needle) {
			t.Fatalf("activation script missing %q", needle)
		}
	}
	if strings.Contains(text, "Invoke-WebRequest -Uri 'http") && !strings.Contains(text, "127.0.0.1:8787") {
		t.Fatal("activation script must not introduce non-loopback HTTP access")
	}
}
