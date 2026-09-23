package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mar/internal/store"
)

func TestReadProjectFileInfersUniqueProjectWithoutGoalContract(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewTaskService(db)
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootB, "note.txt"), []byte("hello from bounded read\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.RegisterProject(context.Background(), "a", rootA); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.RegisterProject(context.Background(), "b", rootB); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ReadProjectFile(context.Background(), "", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("hello from bounded read\n"))
	if got.ProjectID != "b" || got.Path != "note.txt" || got.Content != "hello from bounded read\n" || got.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("unexpected project read: %+v", got)
	}
}

func TestReadProjectFileRejectsTraversalAndAmbiguousRelativePath(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewTaskService(db)
	rootA := t.TempDir()
	rootB := t.TempDir()
	for _, root := range []string{rootA, rootB} {
		if err := os.WriteFile(filepath.Join(root, "same.txt"), []byte("same\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := svc.RegisterProject(context.Background(), "a", rootA); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.RegisterProject(context.Background(), "b", rootB); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadProjectFile(context.Background(), "", "same.txt"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous project error, got %v", err)
	}
	outside := filepath.Join(filepath.Dir(rootA), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadProjectFile(context.Background(), "a", outside); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected escape rejection, got %v", err)
	}
}
