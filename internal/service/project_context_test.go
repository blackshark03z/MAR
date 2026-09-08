package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"mar/internal/store"
)

func TestProjectContextReturnsCurrentHeadAndPolicyForGoalCompilation(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewTaskService(db)
	root := t.TempDir()
	runProjectContextGit(t, root, "init")
	runProjectContextGit(t, root, "config", "user.email", "mar-context@example.invalid")
	runProjectContextGit(t, root, "config", "user.name", "MAR Context Test")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/context\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runProjectContextGit(t, root, "add", "go.mod")
	runProjectContextGit(t, root, "commit", "-m", "base")
	head := runProjectContextGit(t, root, "rev-parse", "HEAD")
	project, _, err := svc.RegisterProject(context.Background(), "mar", root)
	if err != nil {
		t.Fatal(err)
	}
	items, err := svc.ProjectContext(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ProjectID != project.ID || items[0].Head != head || !items[0].Policy.LocalFileWrite || !items[0].Policy.LocalGitWrite {
		t.Fatalf("unexpected project context: %+v", items)
	}
}

func runProjectContextGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return stringTrimSpace(string(out))
}

func stringTrimSpace(value string) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r' || value[len(value)-1] == ' ' || value[len(value)-1] == '\t') {
		value = value[:len(value)-1]
	}
	return value
}
