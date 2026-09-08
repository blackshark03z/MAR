package pathidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveExistingReturnsCanonicalExistingFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "probe.txt")
	if err := os.WriteFile(file, []byte("probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := ResolveExisting(root)
	if err != nil {
		t.Fatal(err)
	}
	resolvedFile, err := ResolveExisting(file)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(resolvedRoot) || !filepath.IsAbs(resolvedFile) {
		t.Fatalf("canonical identities must be absolute: root=%q file=%q", resolvedRoot, resolvedFile)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedFile)
	if err != nil || rel != "probe.txt" {
		t.Fatalf("canonical identities lost containment: rel=%q err=%v", rel, err)
	}
}
