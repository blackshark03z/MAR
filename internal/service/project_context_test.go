package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"mar/internal/store"
	"mar/internal/testsupport"
)

func TestProjectContextReturnsCurrentHeadPolicyAndGoCapabilityForGoalCompilation(t *testing.T) {
	svc, root, projectID, head := newProjectContextFixture(t, "go-only", map[string]string{
		"go.mod": "module example.com/context\n\ngo 1.27\n",
	})
	items, err := svc.ProjectContext(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ProjectID != projectID || items[0].Head != head || !items[0].Policy.LocalFileWrite || !items[0].Policy.LocalGitWrite {
		t.Fatalf("unexpected project context: %+v", items)
	}
	want := ProjectCapability{
		State:                          "supported",
		Ecosystems:                     []string{"go"},
		Languages:                      []string{"go"},
		EvidenceMarkers:                []string{"go.mod"},
		SupportedVerificationProfiles:  []string{"go-standard", "go-docs", "go-release"},
		RecommendedVerificationProfile: "go-standard",
	}
	if !reflect.DeepEqual(items[0].Capability, want) {
		t.Fatalf("go capability=%+v want=%+v root=%s", items[0].Capability, want, root)
	}
}

func TestProjectContextDetectsPythonMixedAndUnknownCapabilities(t *testing.T) {
	tests := []struct {
		name    string
		markers map[string]string
		want    ProjectCapability
	}{
		{
			name:    "python-only",
			markers: map[string]string{"requirements-dev.txt": "pytest>=8,<10\n"},
			want: ProjectCapability{
				State:                          "supported",
				Ecosystems:                     []string{"python"},
				Languages:                      []string{"python"},
				EvidenceMarkers:                []string{"requirements-dev.txt"},
				SupportedVerificationProfiles:  []string{"python-standard"},
				RecommendedVerificationProfile: "python-standard",
			},
		},
		{
			name: "mixed",
			markers: map[string]string{
				"go.mod":         "module example.com/mixed\n\ngo 1.27\n",
				"pyproject.toml": "[project]\nname='mixed'\n",
			},
			want: ProjectCapability{
				State:                         "mixed",
				Ecosystems:                    []string{"go", "python"},
				Languages:                     []string{"go", "python"},
				EvidenceMarkers:               []string{"go.mod", "pyproject.toml"},
				SupportedVerificationProfiles: []string{"go-standard", "go-docs", "go-release", "python-standard"},
			},
		},
		{
			name:    "unknown",
			markers: map[string]string{"README.md": "# unknown\n"},
			want:    ProjectCapability{State: "unknown"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _, _ := newProjectContextFixture(t, tt.name, tt.markers)
			items, err := svc.ProjectContext(context.Background(), "")
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 {
				t.Fatalf("items=%d want=1", len(items))
			}
			if !reflect.DeepEqual(items[0].Capability, tt.want) {
				t.Fatalf("capability=%+v want=%+v", items[0].Capability, tt.want)
			}
		})
	}
}

func newProjectContextFixture(t *testing.T, projectID string, markers map[string]string) (*TaskService, string, string, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := NewTaskService(db)
	root := t.TempDir()
	runProjectContextGit(t, root, "init")
	runProjectContextGit(t, root, "config", "user.email", "mar-context@example.invalid")
	runProjectContextGit(t, root, "config", "user.name", "MAR Context Test")
	for marker, content := range markers {
		if err := os.WriteFile(filepath.Join(root, marker), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runProjectContextGit(t, root, "add", ".")
	runProjectContextGit(t, root, "commit", "-m", "base")
	head := runProjectContextGit(t, root, "rev-parse", "HEAD")
	project, _, err := svc.RegisterProject(context.Background(), projectID, root)
	if err != nil {
		t.Fatal(err)
	}
	return svc, root, project.ID, head
}

func runProjectContextGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	git := testsupport.RequireExecutable(t, "git")
	cmd := exec.Command(git, append([]string{"-C", root}, args...)...)
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
