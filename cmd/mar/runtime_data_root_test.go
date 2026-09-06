package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareRuntimeDataRootCreatesMissingDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing", "runtime-data")
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("test precondition failed: %v", err)
	}
	if err := prepareRuntimeDataRoot(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("runtime data root was not created: info=%v err=%v", info, err)
	}
}
