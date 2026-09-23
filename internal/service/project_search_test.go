package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mar/internal/store"
)

func TestSearchProjectTextIsBoundedAndSkipsGitMetadata(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewTaskService(db)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"a.txt":       "needle one\n",
		"b.txt":       "needle two\n",
		"c.txt":       "needle three\n",
		".git/config": "needle secret\n",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := svc.RegisterProject(context.Background(), "search-project", root); err != nil {
		t.Fatal(err)
	}
	got, err := svc.SearchProjectText(context.Background(), "search-project", ".", "needle", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Matches) != 2 || !got.Truncated {
		t.Fatalf("unexpected bounded search result: %+v", got)
	}
	for _, match := range got.Matches {
		if strings.HasPrefix(match.Path, ".git/") {
			t.Fatalf("git metadata leaked through project search: %+v", match)
		}
	}
}

func TestSearchProjectTextRejectsEscape(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewTaskService(db)
	root := t.TempDir()
	if _, _, err := svc.RegisterProject(context.Background(), "search-project", root); err != nil {
		t.Fatal(err)
	}
	_, err = svc.SearchProjectText(context.Background(), "search-project", "..", "needle", 10)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected search escape rejection, got %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("needle secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = svc.SearchProjectText(context.Background(), "search-project", ".git", "needle", 10)
	if err == nil || !strings.Contains(err.Error(), "does not expose") {
		t.Fatalf("expected direct metadata search rejection, got %v", err)
	}
	nestedGit := filepath.Join(root, "nested", ".git")
	if err := os.MkdirAll(nestedGit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedGit, "config"), []byte("needle nested secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = svc.SearchProjectText(context.Background(), "search-project", filepath.Join("nested", ".git"), "needle", 10)
	if err == nil || !strings.Contains(err.Error(), "does not expose") {
		t.Fatalf("expected nested metadata search rejection, got %v", err)
	}
}
