package service

import (
	"context"
	"testing"
)

func TestFindProjectFilesRanksExactPathBeforeOtherMatches(t *testing.T) {
	svc, _, projectID, _ := newProjectContextFixture(t, "project-find", map[string]string{
		"README.md":            "root\\n",
		"docs/README.md":       "docs\\n",
		"docs/readme_notes.md": "notes\\n",
		"reader.txt":           "reader\\n",
	})
	result, err := svc.FindProjectFiles(context.Background(), projectID, "README.md", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) < 2 {
		t.Fatalf("expected multiple README matches, got %+v", result)
	}
	if result.Matches[0].Path != "README.md" || result.Matches[0].Rank != 0 {
		t.Fatalf("exact project-relative path should rank first: %+v", result.Matches)
	}
	if result.Matches[1].Path != "docs/README.md" || result.Matches[1].Rank != 1 {
		t.Fatalf("exact basename should rank second: %+v", result.Matches)
	}
}

func TestFindProjectFilesSkipsGitMetadataAndBoundsResults(t *testing.T) {
	svc, _, projectID, _ := newProjectContextFixture(t, "project-find-bounds", map[string]string{
		"alpha.txt":       "a\\n",
		"alpha-two.txt":   "b\\n",
		"nested/alpha.go": "c\\n",
	})
	result, err := svc.FindProjectFiles(context.Background(), projectID, "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 || !result.Truncated {
		t.Fatalf("expected one bounded result: %+v", result)
	}
	gitResult, err := svc.FindProjectFiles(context.Background(), projectID, "HEAD", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range gitResult.Matches {
		if match.Path == ".git/HEAD" {
			t.Fatalf("git metadata leaked through project find: %+v", gitResult)
		}
	}
}
