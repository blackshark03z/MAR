package service

import (
	"context"
	"strings"
	"testing"
)

func TestProjectContextBatchFindsAndReadsRelevantSource(t *testing.T) {
	svc, _, projectID, _ := newProjectContextFixture(t, "context-batch", map[string]string{
		"internal/needle.go": "package internal\n\nfunc NeedleContextBatch() {}\n",
		"docs/noise.txt":     "generic documentation only\n",
	})
	result, err := svc.BuildProjectContextBatch(context.Background(), projectID, "NeedleContextBatch", 8, 4, 16<<10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SearchMatches) == 0 || result.SearchMatches[0].Path != "internal/needle.go" {
		t.Fatalf("expected source search match first: %+v", result.SearchMatches)
	}
	if len(result.Files) == 0 || result.Files[0].Path != "internal/needle.go" {
		t.Fatalf("expected relevant source snippet first: %+v", result.Files)
	}
	if !strings.Contains(result.Files[0].Content, "NeedleContextBatch") {
		t.Fatalf("expected snippet content, got %q", result.Files[0].Content)
	}
	if result.Bytes > 16<<10 {
		t.Fatalf("context batch exceeded byte bound: %d", result.Bytes)
	}
}

func TestProjectContextBatchRejectsBlankQuery(t *testing.T) {
	svc, _, projectID, _ := newProjectContextFixture(t, "context-batch-blank", map[string]string{
		"README.md": "root\n",
	})
	if _, err := svc.BuildProjectContextBatch(context.Background(), projectID, " ", 8, 4, 16<<10); err == nil {
		t.Fatal("expected blank query rejection")
	}
}
