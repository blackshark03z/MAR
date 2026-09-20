package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultProjectListEntries = 200
	maxProjectListEntries     = 500
)

type ProjectListEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}
type ProjectListResult struct {
	ProjectID string             `json:"project_id"`
	Path      string             `json:"path"`
	Entries   []ProjectListEntry `json:"entries"`
	Truncated bool               `json:"truncated"`
}

func (s *TaskService) ListProjectDirectory(ctx context.Context, projectID, requestedPath string, maxEntries int) (ProjectListResult, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ProjectListResult{}, errors.New("project_id is required for project list")
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return ProjectListResult{}, err
	}
	if strings.TrimSpace(requestedPath) == "" {
		requestedPath = "."
	}
	target, err := safeProjectReadTarget(project, requestedPath)
	if err != nil {
		return ProjectListResult{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return ProjectListResult{}, fmt.Errorf("read project directory metadata: %w", err)
	}
	if !info.IsDir() {
		return ProjectListResult{}, errors.New("project list supports directories only")
	}
	if maxEntries <= 0 {
		maxEntries = defaultProjectListEntries
	}
	if maxEntries > maxProjectListEntries {
		maxEntries = maxProjectListEntries
	}
	dirEntries, err := os.ReadDir(target)
	if err != nil {
		return ProjectListResult{}, fmt.Errorf("list project directory: %w", err)
	}
	truncated := len(dirEntries) > maxEntries
	if truncated {
		dirEntries = dirEntries[:maxEntries]
	}
	root, err := filepath.Abs(project.Root)
	if err != nil {
		return ProjectListResult{}, fmt.Errorf("resolve project root: %w", err)
	}
	entries := make([]ProjectListEntry, 0, len(dirEntries))
	for _, entry := range dirEntries {
		entryPath := filepath.Join(target, entry.Name())
		rel, err := filepath.Rel(root, entryPath)
		if err != nil {
			return ProjectListResult{}, fmt.Errorf("resolve project-relative list entry: %w", err)
		}
		kind := "file"
		if entry.IsDir() {
			kind = "directory"
		} else if entry.Type()&os.ModeSymlink != 0 {
			kind = "symlink"
		} else if !entry.Type().IsRegular() {
			kind = "other"
		}
		item := ProjectListEntry{Name: entry.Name(), Path: filepath.ToSlash(rel), Kind: kind}
		if kind == "file" {
			if ei, e := entry.Info(); e == nil {
				item.SizeBytes = ei.Size()
			}
		}
		entries = append(entries, item)
	}
	relPath, err := filepath.Rel(root, target)
	if err != nil {
		return ProjectListResult{}, fmt.Errorf("resolve project-relative directory: %w", err)
	}
	if relPath == "." {
		relPath = ""
	}
	return ProjectListResult{ProjectID: project.ID, Path: filepath.ToSlash(relPath), Entries: entries, Truncated: truncated}, nil
}
