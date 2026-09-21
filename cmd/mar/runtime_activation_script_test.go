package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivateCurrentHeadScriptPreservesPromotionRollbackAndRetentionContract(t *testing.T) {
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
		"runtime_identity",
		"return $response",
		"start-owner-console.ps1",
		"Stop-MARListener",
		"Stop-MARStableProcesses",
		"Get-MARStableProcesses -StableExe $StableExe",
		"Restore-BackupFile",
		"retention-prune",
		"-keep-activation 5",
		"UTF8Encoding($false)",
		"WriteAllText",
		`Write-Warning "MAR retention cleanup failed`,
		"Activation failed; previous runtime state was restored",
	}
	for _, needle := range required {
		if !strings.Contains(text, needle) {
			t.Fatalf("activation script missing %q", needle)
		}
	}

	if strings.Contains(text, `Set-Content -LiteralPath (Join-Path $backupRoot 'activation.json') -Encoding UTF8`) {
		t.Fatal("activation metadata must not use Windows PowerShell UTF8 Set-Content because it writes a BOM")
	}

	trusted := strings.Index(text, `$identity = Wait-MARRuntime -ExpectedRevision $head -RequireTrusted $true`)
	retention := strings.Index(text, `$stableExe retention-prune`)
	pass := strings.Index(text, `Write-Host "MAR ACTIVATION PASS"`)
	if trusted < 0 || retention < 0 || pass < 0 {
		t.Fatalf("activation ordering anchors missing: trusted=%d retention=%d pass=%d", trusted, retention, pass)
	}
	if !(trusted < retention && retention < pass) {
		t.Fatalf("retention must run only after trusted activation and before PASS output: trusted=%d retention=%d pass=%d", trusted, retention, pass)
	}

	stopStable := strings.Index(text, `Stop-MARStableProcesses -StableExe $stableExe`)
	promote := strings.Index(text, `Copy-Item -LiteralPath $candidateExe -Destination $stableExe -Force`)
	if stopStable < 0 || promote < 0 || stopStable > promote {
		t.Fatalf("all stable-binary processes must stop before promotion: stop=%d promote=%d", stopStable, promote)
	}
	if strings.Count(text, `Stop-MARStableProcesses -StableExe $stableExe`) < 2 {
		t.Fatal("activation must stop all stable-binary processes before both promotion and rollback restore")
	}

	if strings.Contains(text, "Invoke-WebRequest -Uri 'http") && !strings.Contains(text, "127.0.0.1:8787") {
		t.Fatal("activation script must not introduce non-loopback HTTP access")
	}
}
