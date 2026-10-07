package retention

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func writeActivation(t *testing.T, root, name string, created time.Time, marked bool) string {
	t.Helper()
	dir := filepath.Join(root, "recovery", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "payload.bin"), []byte("payload-"+name), 0o644); err != nil {
		t.Fatal(err)
	}
	if marked {
		meta := activationMetadata{
			CreatedAtUTC:   created.UTC().Format(time.RFC3339Nano),
			TargetRevision: name + "-revision",
			TargetRelease:  name + "-release",
		}
		raw, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(dir, activationMetadataFile), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeActivationWithUTF8BOM(t *testing.T, root, name string, created time.Time) string {
	t.Helper()
	dir := writeActivation(t, root, name, created, true)
	marker := filepath.Join(dir, activationMetadataFile)
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, raw...)
	if err := os.WriteFile(marker, withBOM, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadActivationMetadataAcceptsOptionalUTF8BOM(t *testing.T) {
	root := t.TempDir()
	created := time.Date(2026, 9, 18, 12, 0, 0, 123456789, time.UTC)
	dir := writeActivationWithUTF8BOM(t, root, "activation-bom", created)

	got, valid, err := readActivationMetadata(filepath.Join(dir, activationMetadataFile))
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("UTF-8 BOM activation metadata was rejected")
	}
	if !got.Equal(created) {
		t.Fatalf("created time = %s, want %s", got, created)
	}
}

func TestPruneRemovesOldPowerShellUTF8BOMActivation(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	oldName := "activation-20260918-120000-bom"
	writeActivationWithUTF8BOM(t, root, oldName, base)
	for i := 1; i <= 5; i++ {
		name := "activation-new-" + string(rune('0'+i))
		writeActivation(t, root, name, base.Add(time.Duration(i)*time.Minute), true)
	}

	result, err := Prune(root, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.RemovedActivationBackups, []string{oldName}) {
		t.Fatalf("removed backups = %v, want [%s]", result.RemovedActivationBackups, oldName)
	}
	if _, err := os.Stat(filepath.Join(root, "recovery", oldName)); !os.IsNotExist(err) {
		t.Fatalf("old BOM-marked activation still exists: %v", err)
	}
}

func TestReadActivationMetadataRejectsUnsupportedBOMAndMissingMetadata(t *testing.T) {
	root := t.TempDir()
	recovery := filepath.Join(root, "recovery")
	if err := os.MkdirAll(recovery, 0o755); err != nil {
		t.Fatal(err)
	}

	utf16 := filepath.Join(recovery, "utf16.json")
	if err := os.WriteFile(utf16, []byte{0xFF, 0xFE, '{', 0x00, '}', 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, valid, err := readActivationMetadata(utf16); err != nil || valid {
		t.Fatalf("UTF-16 marker admitted: valid=%v err=%v", valid, err)
	}

	missing := filepath.Join(recovery, "missing.json")
	if err := os.WriteFile(missing, []byte(`{"created_at_utc":"2026-09-18T12:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, valid, err := readActivationMetadata(missing); err != nil || valid {
		t.Fatalf("marker missing required metadata admitted: valid=%v err=%v", valid, err)
	}
}

func TestPrunePreservesActivationWithUnsafeSymlinkTreeWhenSupported(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	unsafe := writeActivation(t, root, "activation-unsafe-tree", base, true)
	external := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(external, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(unsafe, "unsafe-link")); err != nil {
		t.Skipf("symlink unavailable on this host: %v", err)
	}
	writeActivation(t, root, "activation-newer-safe", base.Add(time.Minute), true)

	result, err := Prune(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RemovedActivationBackups) != 0 {
		t.Fatalf("unsafe activation tree must remain ineligible, removed=%v", result.RemovedActivationBackups)
	}
	if _, err := os.Lstat(unsafe); err != nil {
		t.Fatalf("unsafe activation tree was removed: %v", err)
	}
}

func TestPruneKeepsNewestFiveMarkedActivationsAndBoundedStaging(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	var names []string
	for i := 0; i < 7; i++ {
		name := "activation-20260918-12000" + string(rune('0'+i)) + "-deadbeef"
		writeActivation(t, root, name, base.Add(time.Duration(i)*time.Minute), true)
		names = append(names, name)
	}
	unmarked := writeActivation(t, root, "activation-unmarked", base.Add(-time.Hour), false)
	manual := filepath.Join(root, "recovery", "manual-recovery")
	if err := os.MkdirAll(manual, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manual, "keep.bin"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	staging := filepath.Join(root, "runtime", "staging")
	if err := os.MkdirAll(filepath.Join(staging, "mar-directory.exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"mar-deadbeef.exe":               "exe",
		"release-manifest-deadbeef.json": "manifest",
		"keep.txt":                       "keep",
		"mar-deadbeef.exe.note":          "keep",
	} {
		if err := os.WriteFile(filepath.Join(staging, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Prune(root, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.RemovedActivationBackups, names[:2]) {
		t.Fatalf("removed backups = %v, want %v", result.RemovedActivationBackups, names[:2])
	}
	for _, name := range names[:2] {
		if _, err := os.Stat(filepath.Join(root, "recovery", name)); !os.IsNotExist(err) {
			t.Fatalf("old activation %s still exists: %v", name, err)
		}
	}
	for _, name := range names[2:] {
		if _, err := os.Stat(filepath.Join(root, "recovery", name)); err != nil {
			t.Fatalf("retained activation %s missing: %v", name, err)
		}
	}
	for _, path := range []string{unmarked, manual, filepath.Join(staging, "keep.txt"), filepath.Join(staging, "mar-deadbeef.exe.note"), filepath.Join(staging, "mar-directory.exe")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unknown/unmarked path was removed: %s: %v", path, err)
		}
	}
	wantStaging := []string{"mar-deadbeef.exe", "release-manifest-deadbeef.json"}
	if !slices.Equal(result.RemovedStagingFiles, wantStaging) {
		t.Fatalf("removed staging = %v, want %v", result.RemovedStagingFiles, wantStaging)
	}
	if result.FreedBytes <= 0 {
		t.Fatal("expected positive freed-byte evidence")
	}
}

func TestPruneOrdersByActivationMetadataNotDirectoryMtime(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	old := writeActivation(t, root, "activation-old", base, true)
	newer := writeActivation(t, root, "activation-new", base.Add(time.Hour), true)
	if err := os.Chtimes(old, base.Add(24*time.Hour), base.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	result, err := Prune(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.RemovedActivationBackups, []string{"activation-old"}) {
		t.Fatalf("metadata ordering not respected: %v", result.RemovedActivationBackups)
	}
	if _, err := os.Stat(newer); err != nil {
		t.Fatalf("newer metadata backup missing: %v", err)
	}
}

func TestPruneKeepsAllMarkedBackupsBelowLimitAndMalformedMarker(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		writeActivation(t, root, "activation-small-"+string(rune('a'+i)), base.Add(time.Duration(i)*time.Minute), true)
	}
	malformed := writeActivation(t, root, "activation-malformed", base.Add(-time.Hour), false)
	if err := os.WriteFile(filepath.Join(malformed, activationMetadataFile), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Prune(root, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RemovedActivationBackups) != 0 {
		t.Fatalf("backups below keep limit were removed: %v", result.RemovedActivationBackups)
	}
	if _, err := os.Stat(malformed); err != nil {
		t.Fatalf("malformed-marker activation was removed: %v", err)
	}
}

func TestPrunePreservesSymlinkCandidatesWhenSupported(t *testing.T) {
	root := t.TempDir()
	target := writeActivation(t, root, "activation-target", time.Now().UTC(), true)
	link := filepath.Join(root, "recovery", "activation-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable on this host: %v", err)
	}
	if _, err := Prune(root, 1); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("activation symlink was not preserved: info=%v err=%v", info, err)
	}
}

func TestPruneRejectsSymlinkDataRootWhenSupported(t *testing.T) {
	realRoot := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "linked-root")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Skipf("symlink unavailable on this host: %v", err)
	}
	if _, err := Prune(link, 5); err == nil {
		t.Fatal("symlink data root was admitted")
	}
}

func TestPruneHistoricalRuntimeScratchKeepsAuditEvidenceAndUnknownRuntimeState(t *testing.T) {
	root := t.TempDir()
	ownerScratch := filepath.Join(root, "runtime", "owner_realuse_demo", "w", "workspace")
	audit := filepath.Join(root, "runtime", "audit-20260915")
	paths := map[string]string{
		filepath.Join(ownerScratch, "cache.bin"):                         "owner scratch",
		filepath.Join(audit, "candidate", "build.bin"):                  "candidate scratch",
		filepath.Join(audit, "temp-recovery-qualify", "state.bin"):     "recovery scratch",
		filepath.Join(audit, "orchestrator.test.exe"):                   "test binary",
		filepath.Join(audit, "test-summary.json"):                       "authoritative evidence",
		filepath.Join(audit, "consolidated-host-tests.jsonl"):           "authoritative evidence",
		filepath.Join(audit, "unknown-state", "keep.bin"):              "unknown keep",
		filepath.Join(root, "runtime", "unrelated-runtime", "keep.bin"): "unrelated keep",
	}
	for path, body := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Prune(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantRemoved := []string{
		"runtime/audit-20260915/candidate",
		"runtime/audit-20260915/orchestrator.test.exe",
		"runtime/audit-20260915/temp-recovery-qualify",
		"runtime/owner_realuse_demo",
	}
	if !slices.Equal(result.RemovedHistoricalScratch, wantRemoved) {
		t.Fatalf("removed historical scratch = %v, want %v", result.RemovedHistoricalScratch, wantRemoved)
	}
	for _, path := range []string{
		filepath.Join(audit, "test-summary.json"),
		filepath.Join(audit, "consolidated-host-tests.jsonl"),
		filepath.Join(audit, "unknown-state", "keep.bin"),
		filepath.Join(root, "runtime", "unrelated-runtime", "keep.bin"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("retention removed evidence or unknown runtime state %s: %v", path, err)
		}
	}
	if result.FreedBytes <= 0 {
		t.Fatal("expected positive freed-byte evidence")
	}
}

func TestPruneHistoricalDataRootsKeepsMetadataShells(t *testing.T) {
	root := t.TempDir()
	paths := map[string]string{
		filepath.Join(root, "selfdev", "w", "scratch.bin"):                                  "scratch",
		filepath.Join(root, "selfdev", "runtime", "cloudflared.exe"):                    "binary",
		filepath.Join(root, "selfdev", "runtime", "ui.stdout.log"):                     "keep-log",
		filepath.Join(root, "selfdev", "mar.db"):                                         "keep-db",
		filepath.Join(root, "remote-full-goal-final", "data", "w", "scratch.bin"):      "scratch",
		filepath.Join(root, "remote-full-goal-final", "data", "runtime", "tool.exe"):   "runtime",
		filepath.Join(root, "remote-full-goal-final", "data", "mar.db"):                  "keep-db",
		filepath.Join(root, "remote-full-goal-final", "control", "state.json"):           "keep-control",
		filepath.Join(root, "remote-full-goal-final", "mar.exe"):                          "binary",
		filepath.Join(root, "remote-full-goal-final", "remote-client.exe"):                "binary",
		filepath.Join(root, "remote-full-goal-final", "remote-owner-driver.exe"):          "binary",
		filepath.Join(root, "selfhost-v4", "w", "scratch.bin"):                          "scratch",
		filepath.Join(root, "selfhost-v4", "project", "go.mod"):                          "keep-project",
		filepath.Join(root, "selfhost-v4", "mar.db"):                                      "keep-db",
		filepath.Join(root, "final-uat-f01731e-v2", "w", "scratch.bin"):                  "scratch",
		filepath.Join(root, "final-uat-f01731e-v2", "typed-selfhost-control", "state"):   "keep-control",
		filepath.Join(root, "final-uat-f01731e-v2", "mar.db"):                              "keep-db",
	}
	for path, body := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Prune(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "selfdev", "w"),
		filepath.Join(root, "selfdev", "runtime", "cloudflared.exe"),
		filepath.Join(root, "remote-full-goal-final", "data", "w"),
		filepath.Join(root, "remote-full-goal-final", "data", "runtime"),
		filepath.Join(root, "remote-full-goal-final", "mar.exe"),
		filepath.Join(root, "remote-full-goal-final", "remote-client.exe"),
		filepath.Join(root, "remote-full-goal-final", "remote-owner-driver.exe"),
		filepath.Join(root, "selfhost-v4", "w"),
		filepath.Join(root, "final-uat-f01731e-v2", "w"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("historical scratch still exists %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(root, "selfdev", "runtime", "ui.stdout.log"),
		filepath.Join(root, "selfdev", "mar.db"),
		filepath.Join(root, "remote-full-goal-final", "data", "mar.db"),
		filepath.Join(root, "remote-full-goal-final", "control", "state.json"),
		filepath.Join(root, "selfhost-v4", "project", "go.mod"),
		filepath.Join(root, "selfhost-v4", "mar.db"),
		filepath.Join(root, "final-uat-f01731e-v2", "typed-selfhost-control", "state"),
		filepath.Join(root, "final-uat-f01731e-v2", "mar.db"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("historical metadata shell was removed %s: %v", path, err)
		}
	}
	if result.FreedBytes <= 0 {
		t.Fatal("expected positive freed-byte evidence")
	}
}

func TestPruneLegacyRuntimeExecutablesKeepsLiveBinariesAndUnknownExecutables(t *testing.T) {
	root := t.TempDir()
	runtimeRoot := filepath.Join(root, "runtime")
	files := map[string]string{
		"mar-v1-stable.exe":              "live",
		"tunnel-client.exe":               "live",
		"cloudflared.exe":                 "live",
		"unknown-tool.exe":                "keep-unknown",
		"mar-head-deadbeef.exe":           "remove",
		"mar-v1-stable.prev.exe":          "remove",
		"mar-v4-candidate.exe~":           "remove",
		"mar.exe":                         "remove",
		"mcp_call.exe":                    "remove",
		"owner_web_client.exe":            "remove",
		"selfhost_v4.exe":                 "remove",
		"tunnel-client-v0.0.14-backup.exe": "remove",
	}
	for name, body := range files {
		if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runtimeRoot, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Prune(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mar-v1-stable.exe", "tunnel-client.exe", "cloudflared.exe", "unknown-tool.exe"} {
		if _, err := os.Stat(filepath.Join(runtimeRoot, name)); err != nil {
			t.Fatalf("live or unknown executable removed %s: %v", name, err)
		}
	}
	for _, name := range []string{"mar-head-deadbeef.exe", "mar-v1-stable.prev.exe", "mar-v4-candidate.exe~", "mar.exe", "mcp_call.exe", "owner_web_client.exe", "selfhost_v4.exe", "tunnel-client-v0.0.14-backup.exe"} {
		if _, err := os.Stat(filepath.Join(runtimeRoot, name)); !os.IsNotExist(err) {
			t.Fatalf("legacy executable still exists %s: %v", name, err)
		}
	}
	if result.FreedBytes <= 0 {
		t.Fatal("expected positive freed-byte evidence")
	}
}

func TestPruneLegacyRecoveryScratchKeepsMarkedRollbackAndUnknownRecovery(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	markedName := "activation-20260918-marked-keep"
	marked := writeActivation(t, root, markedName, base, true)
	legacy := []string{
		"cads-purge-old",
		"cads-bench-purge-old",
		"activate-requalification-old",
		"python-support-old",
		"activation-20260918-old-unmarked",
		"activate-retention-old",
		"owner-reconcile-old",
		"index-refresh-old",
	}
	for _, name := range legacy {
		path := filepath.Join(root, "recovery", name, "payload.bin")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("legacy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	unknown := filepath.Join(root, "recovery", "manual-keep", "payload.bin")
	if err := os.MkdirAll(filepath.Dir(unknown), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unknown, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Prune(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range legacy {
		if _, err := os.Stat(filepath.Join(root, "recovery", name)); !os.IsNotExist(err) {
			t.Fatalf("legacy recovery still exists %s: %v", name, err)
		}
	}
	for _, path := range []string{marked, filepath.Dir(unknown)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("protected recovery path was removed %s: %v", path, err)
		}
	}
	if result.FreedBytes <= 0 {
		t.Fatal("expected positive freed-byte evidence")
	}
}

func TestPrunePressureCachesClearsOnlyPositiveAllowlistAndPreservesRoots(t *testing.T) {
	root := t.TempDir()
	buildCache := filepath.Join(root, "runtime", "go-build-cache")
	legacyCache := filepath.Join(root, "runtime", "diag-gocache")
	moduleCache := filepath.Join(root, "runtime", "gomodcache")
	audit := filepath.Join(root, "runtime", "audit-keep")
	for _, dir := range []string{buildCache, legacyCache, moduleCache, audit} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(buildCache, "a", "cache.bin"): "build-cache",
		filepath.Join(legacyCache, "diag.bin"):      "diag-cache",
		filepath.Join(moduleCache, "module.zip"):    "offline-module",
		filepath.Join(audit, "evidence.json"):       "evidence",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := PrunePressureCaches(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"runtime/diag-gocache", "runtime/go-build-cache"}
	if !slices.Equal(result.ClearedRebuildableCaches, want) {
		t.Fatalf("cleared caches = %v, want %v", result.ClearedRebuildableCaches, want)
	}
	if result.FreedBytes <= 0 {
		t.Fatal("expected positive pressure-prune bytes")
	}
	for _, dir := range []string{buildCache, legacyCache} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Fatalf("cache root must be preserved for ACL identity: %s err=%v", dir, err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("cache root not emptied: %s entries=%d err=%v", dir, len(entries), err)
		}
	}
	for _, path := range []string{filepath.Join(moduleCache, "module.zip"), filepath.Join(audit, "evidence.json")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("non-allowlisted runtime state was removed: %s: %v", path, err)
		}
	}
}

func TestPrunePressureCachesFailsClosedOnSymlinkedCacheRootWhenSupported(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "runtime", "go-build-cache")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable on this host: %v", err)
	}
	if _, err := PrunePressureCaches(root); err == nil {
		t.Fatal("symlinked rebuildable cache root was admitted")
	}
}
