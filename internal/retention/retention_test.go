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
			CreatedAtUTC: created.UTC().Format(time.RFC3339Nano),
			TargetRevision: name + "-revision",
			TargetRelease: name + "-release",
		}
		raw, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(dir, activationMetadataFile), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
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
		"mar-deadbeef.exe": "exe",
		"release-manifest-deadbeef.json": "manifest",
		"keep.txt": "keep",
		"mar-deadbeef.exe.note": "keep",
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
