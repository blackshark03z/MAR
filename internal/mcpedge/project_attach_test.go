package mcpedge

import (
	"context"
	"testing"

	"mar/internal/service"
)

func TestProjectAttachAndListUseCanonicalProjectTool(t *testing.T) {
	backend := &fakeBackend{}
	attached, err := callProject(context.Background(), backend, projectArgs{Operation: "attach", Path: `D:\Research`})
	if err != nil {
		t.Fatal(err)
	}
	attachment := attached["attachment"].(service.ProjectAttachResult)
	if attachment.ProjectID != "attached-project" || attachment.Mode != "research_only" {
		t.Fatalf("bad attach: %+v", attachment)
	}
	listed, err := callProject(context.Background(), backend, projectArgs{Operation: "list", ProjectID: attachment.ProjectID, Path: ".", MaxEntries: 10})
	if err != nil {
		t.Fatal(err)
	}
	directory := listed["directory"].(service.ProjectListResult)
	if directory.ProjectID != attachment.ProjectID || len(directory.Entries) != 1 {
		t.Fatalf("bad list: %+v", directory)
	}
}
