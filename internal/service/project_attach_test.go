package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mar/internal/store"
)

func newAttachService(t *testing.T) *TaskService {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewTaskService(db)
}

func TestAttachLocalPathReusesExistingProject(t *testing.T) {
	svc := newAttachService(t)
	root := t.TempDir()
	nested := filepath.Join(root, "docs")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(nested, "note.txt")
	if err := os.WriteFile(file, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent, _, err := svc.RegisterProject(context.Background(), "parent", root)
	if err != nil {
		t.Fatal(err)
	}
	project, _, err := svc.RegisterProject(context.Background(), "existing", nested)
	if err != nil {
		t.Fatal(err)
	}
	beforeParent, _ := svc.ProjectPolicy(context.Background(), parent.ID)
	beforeProject, _ := svc.ProjectPolicy(context.Background(), project.ID)
	got, err := svc.AttachLocalPath(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	afterParent, _ := svc.ProjectPolicy(context.Background(), parent.ID)
	afterProject, _ := svc.ProjectPolicy(context.Background(), project.ID)
	if got.ProjectID != project.ID || !got.Reused || got.Created || got.RelativeTarget != "note.txt" {
		t.Fatalf("unexpected attach: %+v", got)
	}
	if beforeParent != afterParent || beforeProject != afterProject {
		t.Fatal("existing policy changed")
	}
}

func TestAttachLocalPathGitTopLevel(t *testing.T) {
	svc := newAttachService(t)
	root := t.TempDir()
	runProjectContextGit(t, root, "init")
	runProjectContextGit(t, root, "config", "user.email", "attach@example.invalid")
	runProjectContextGit(t, root, "config", "user.name", "Attach Test")
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(nested, "note.txt")
	if err := os.WriteFile(file, []byte("git attach\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runProjectContextGit(t, root, "add", ".")
	runProjectContextGit(t, root, "commit", "-m", "base")
	got, err := svc.AttachLocalPath(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Created || got.Mode != "git" || got.RelativeTarget != "nested/note.txt" || got.WorkspaceCreated {
		t.Fatalf("unexpected git attach: %+v", got)
	}
}

func TestAttachLocalPathWithNetworkEnablesExplicitGitFastPath(t *testing.T) {
	svc := newAttachService(t)
	root := t.TempDir()
	runProjectContextGit(t, root, "init")
	runProjectContextGit(t, root, "config", "user.email", "attach-network@example.invalid")
	runProjectContextGit(t, root, "config", "user.name", "Attach Network Test")
	if err := os.WriteFile(filepath.Join(root, "main.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runProjectContextGit(t, root, "add", ".")
	runProjectContextGit(t, root, "commit", "-m", "base")
	got, err := svc.AttachLocalPathWithNetwork(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Policy.LocalFileWrite || !got.Policy.LocalGitWrite || !got.Policy.NetworkAllowed || got.Policy.RemoteGitWrite || got.Policy.DeployAllowed {
		t.Fatalf("unexpected explicit fast-path policy: %+v", got.Policy)
	}
	stored, err := svc.ProjectPolicy(context.Background(), got.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.NetworkAllowed {
		t.Fatalf("network permission was not persisted: %+v", stored)
	}
}

func TestAttachLocalPathResearchOnlyContext(t *testing.T) {
	svc := newAttachService(t)
	root := t.TempDir()
	file := filepath.Join(root, "research.txt")
	if err := os.WriteFile(file, []byte("research\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := svc.AttachLocalPath(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	p := got.Policy
	if got.Mode != researchOnlyProjectMode || p.LocalFileWrite || p.LocalGitWrite || p.NetworkAllowed || p.RemoteGitWrite || p.DeployAllowed {
		t.Fatalf("unsafe research attach: %+v", got)
	}
	items, err := svc.ProjectContext(context.Background(), got.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Head != "" || items[0].Capability.State != researchOnlyProjectMode || len(items[0].Capability.SupportedVerificationProfiles) != 0 {
		t.Fatalf("bad research context: %+v", items)
	}
}

func TestAttachLocalPathRejectsMissingPath(t *testing.T) {
	svc := newAttachService(t)
	before, _ := svc.store.ListProjects(context.Background())
	_, err := svc.AttachLocalPath(context.Background(), filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected failure")
	}
	after, _ := svc.store.ListProjects(context.Background())
	if len(before) != len(after) {
		t.Fatal("missing path registered project")
	}
}

func TestListProjectDirectoryIsBoundedAndRejectsEscape(t *testing.T) {
	svc := newAttachService(t)
	root := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	project, _, err := svc.RegisterProject(context.Background(), "list-project", root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ListProjectDirectory(context.Background(), project.ID, ".", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 || !got.Truncated {
		t.Fatalf("bad list: %+v", got)
	}
	_, err = svc.ListProjectDirectory(context.Background(), project.ID, "..", 10)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected escape rejection: %v", err)
	}
}
