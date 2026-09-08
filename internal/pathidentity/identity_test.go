package pathidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveExistingReturnsTrustedExistingFileAndDirectory(t *testing.T) {
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
		t.Fatalf("trusted identities must be absolute: root=%q file=%q", resolvedRoot, resolvedFile)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedFile)
	if err != nil || rel != "probe.txt" {
		t.Fatalf("trusted identities lost containment: rel=%q err=%v", rel, err)
	}
	within, err := ResolveWithin(root, file)
	if err != nil || filepath.Clean(within) != filepath.Clean(file) {
		t.Fatalf("ordinary in-root file did not resolve: within=%q err=%v", within, err)
	}
}

func TestResolveWithinRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if _, err := ResolveWithin(root, filepath.Join(root, "..", filepath.Base(outside))); err == nil {
		t.Fatal("lexical traversal escaped trusted root")
	}
	link := filepath.Join(root, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is unavailable on this host: %v", err)
	}
	if _, err := ResolveWithin(root, link); err == nil {
		t.Fatal("symlink/reparse escape was accepted")
	}
}
