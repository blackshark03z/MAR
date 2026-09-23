package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"mar/internal/pathidentity"
)

const (
	defaultProjectSearchResults = 50
	maxProjectSearchResults     = 200
	maxProjectSearchFileBytes   = int64(1 << 20)
	maxProjectSearchFiles       = 5000
	maxProjectSearchBytes       = int64(64 << 20)
	maxProjectSearchLineRunes   = 500
)

type ProjectSearchMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type ProjectSearchResult struct {
	ProjectID    string               `json:"project_id"`
	Path         string               `json:"path,omitempty"`
	Query        string               `json:"query"`
	Matches      []ProjectSearchMatch `json:"matches"`
	FilesScanned int                  `json:"files_scanned"`
	BytesScanned int64                `json:"bytes_scanned"`
	Truncated    bool                 `json:"truncated"`
}

func (s *TaskService) SearchProjectText(ctx context.Context, projectID, requestedPath, query string, maxResults int) (ProjectSearchResult, error) {
	projectID = strings.TrimSpace(projectID)
	query = strings.TrimSpace(query)
	if projectID == "" {
		return ProjectSearchResult{}, errors.New("project_id is required for project search")
	}
	if query == "" {
		return ProjectSearchResult{}, errors.New("search query is required")
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return ProjectSearchResult{}, err
	}
	if strings.TrimSpace(requestedPath) == "" {
		requestedPath = "."
	}
	target, err := safeProjectReadTarget(project, requestedPath)
	if err != nil {
		return ProjectSearchResult{}, err
	}
	if maxResults <= 0 {
		maxResults = defaultProjectSearchResults
	}
	if maxResults > maxProjectSearchResults {
		maxResults = maxProjectSearchResults
	}
	root, err := pathidentity.ResolveExisting(project.Root)
	if err != nil {
		return ProjectSearchResult{}, fmt.Errorf("resolve project root: %w", err)
	}
	targetRel, err := filepath.Rel(root, target)
	if err != nil {
		return ProjectSearchResult{}, fmt.Errorf("resolve project search path: %w", err)
	}
	if targetRel == "." {
		targetRel = ""
	}
	if projectSearchMetadataPath(targetRel) {
		return ProjectSearchResult{}, errors.New("project search does not expose .git or .mar metadata")
	}

	result := ProjectSearchResult{
		ProjectID: project.ID,
		Path:      filepath.ToSlash(targetRel),
		Query:     query,
		Matches:   []ProjectSearchMatch{},
	}
	errStop := errors.New("project search bound reached")
	err = filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if path != target {
				base := strings.ToLower(entry.Name())
				if base == ".git" || base == ".mar" {
					return filepath.SkipDir
				}
				if _, err := safeProjectReadTarget(project, path); err != nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		if result.FilesScanned >= maxProjectSearchFiles {
			result.Truncated = true
			return errStop
		}
		real, err := safeProjectReadTarget(project, path)
		if err != nil {
			return nil
		}
		info, err := os.Stat(real)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxProjectSearchFileBytes {
			return nil
		}
		if result.BytesScanned+info.Size() > maxProjectSearchBytes {
			result.Truncated = true
			return errStop
		}
		payload, err := os.ReadFile(real)
		if err != nil {
			return nil
		}
		result.FilesScanned++
		result.BytesScanned += int64(len(payload))
		if bytes.IndexByte(payload, 0) >= 0 || !utf8.Valid(payload) {
			return nil
		}
		rel, err := filepath.Rel(root, real)
		if err != nil {
			return nil
		}
		for i, line := range strings.Split(string(payload), "\n") {
			if !strings.Contains(line, query) {
				continue
			}
			result.Matches = append(result.Matches, ProjectSearchMatch{
				Path: filepath.ToSlash(rel),
				Line: i + 1,
				Text: boundProjectSearchLine(line),
			})
			if len(result.Matches) >= maxResults {
				result.Truncated = true
				return errStop
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return ProjectSearchResult{}, err
	}
	sort.Slice(result.Matches, func(i, j int) bool {
		if result.Matches[i].Path == result.Matches[j].Path {
			return result.Matches[i].Line < result.Matches[j].Line
		}
		return result.Matches[i].Path < result.Matches[j].Path
	})
	return result, nil
}

func projectSearchMetadataPath(rel string) bool {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		switch strings.ToLower(part) {
		case ".git", ".mar":
			return true
		}
	}
	return false
}

func boundProjectSearchLine(value string) string {
	runes := []rune(value)
	if len(runes) <= maxProjectSearchLineRunes {
		return value
	}
	return string(runes[:maxProjectSearchLineRunes])
}
