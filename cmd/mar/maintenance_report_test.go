package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildMaintenanceStorageSnapshotIsBoundedAndSymlinkSafe(t *testing.T) {
	root := t.TempDir()

	mustWrite := func(path string, size int) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite(filepath.Join(root, "runtime", "gomodcache", "a.bin"), 200)
	mustWrite(filepath.Join(root, "runtime", "go-portable", "b.bin"), 100)
	mustWrite(filepath.Join(root, "w", "workspace.bin"), 50)

	activation := filepath.Join(root, "recovery", "activation-good")
	mustWrite(filepath.Join(activation, "activation.json"), 10)
	mustWrite(filepath.Join(activation, "data", "mar.db"), 75)
	mustWrite(filepath.Join(root, "recovery", "manual", "payload.bin"), 25)

	external := filepath.Join(t.TempDir(), "outside.bin")
	mustWrite(external, 4096)
	link := filepath.Join(root, "runtime", "external-link")
	if err := os.Symlink(external, link); err != nil {
		t.Logf("symlink unavailable on this host: %v", err)
	}

	snapshot, err := buildMaintenanceStorageSnapshot(root, 10)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TotalBytes != 460 {
		t.Fatalf("total bytes = %d, want 460", snapshot.TotalBytes)
	}
	if snapshot.ActivationBackups.Count != 1 || snapshot.ActivationBackups.Bytes != 85 {
		t.Fatalf("activation summary = %+v, want count=1 bytes=85", snapshot.ActivationBackups)
	}
	if len(snapshot.TopLevel) < 3 || snapshot.TopLevel[0].Name != "runtime" || snapshot.TopLevel[0].Bytes != 300 {
		t.Fatalf("unexpected top-level ordering: %+v", snapshot.TopLevel)
	}
	if len(snapshot.RuntimeTop) != 2 || snapshot.RuntimeTop[0].Name != "gomodcache" || snapshot.RuntimeTop[0].Bytes != 200 {
		t.Fatalf("unexpected runtime breakdown: %+v", snapshot.RuntimeTop)
	}
}
