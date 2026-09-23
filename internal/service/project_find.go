package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultProjectFindResults = 50
	maxProjectFindResults     = 200
	maxProjectFindEntries     = 20000
)

type ProjectFindMatch struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Rank int    `json:"rank"`
}

type ProjectFindResult struct {
	ProjectID string             `json:"project_id"`
	Query     string             `json:"query"`
	Matches   []ProjectFindMatch `json:"matches"`
	Scanned   int                `json:"scanned"`
	Truncated bool               `json:"truncated"`
}

func (s *TaskService) FindProjectFiles(ctx context.Context, projectID, query string, maxResults int) (ProjectFindResult, error) {
	projectID = strings.TrimSpace(projectID)
	query = strings.TrimSpace(query)
	if projectID == "" {
		return ProjectFindResult{}, errors.New("project_id is required for project find")
	}
	if query == "" {
		return ProjectFindResult{}, errors.New("find query is required")
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return ProjectFindResult{}, err
	}
	root, err := safeProjectReadTarget(project, ".")
	if err != nil {
		return ProjectFindResult{}, err
	}
	if maxResults <= 0 {
		maxResults = defaultProjectFindResults
	}
	if maxResults > maxProjectFindResults {
		maxResults = maxProjectFindResults
	}
	queryLower := strings.ToLower(filepath.ToSlash(query))
	result := ProjectFindResult{ProjectID: project.ID, Query: query, Matches: []ProjectFindMatch{}}
	if gitErr := findProjectFilesFromGit(ctx, root, queryLower, &result); gitErr == nil {
		return finishProjectFind(result, maxResults), nil
	}
	errStop := errors.New("project find bound reached")
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		baseLower := strings.ToLower(entry.Name())
		if entry.IsDir() && (baseLower == ".git" || baseLower == ".mar") {
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		result.Scanned++
		if result.Scanned > maxProjectFindEntries {
			result.Truncated = true
			return errStop
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("resolve project find path: %w", err)
		}
		rel = filepath.ToSlash(rel)
		rank, ok := projectFindRank(rel, entry.Name(), queryLower)
		if !ok {
			return nil
		}
		kind := "file"
		if entry.IsDir() {
			kind = "directory"
		}
		result.Matches = append(result.Matches, ProjectFindMatch{Path: rel, Kind: kind, Rank: rank})
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return ProjectFindResult{}, err
	}
	return finishProjectFind(result, maxResults), nil
}

func findProjectFilesFromGit(ctx context.Context, root, queryLower string, result *ProjectFindResult) error {
	listing, err := runProjectGit(ctx, root, "ls-files", "-co", "--exclude-standard")
	if err != nil {
		return err
	}
	seenDirs := make(map[string]struct{})
	for _, raw := range strings.Split(listing, "\n") {
		if err := ctx.Err(); err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimSpace(raw))
		if rel == "" {
			continue
		}
		if result.Scanned >= maxProjectFindEntries {
			result.Truncated = true
			break
		}
		result.Scanned++
		if rank, ok := projectFindRank(rel, filepath.Base(rel), queryLower); ok {
			result.Matches = append(result.Matches, ProjectFindMatch{Path: rel, Kind: "file", Rank: rank})
		}
		for dir := filepath.ToSlash(filepath.Dir(rel)); dir != "." && dir != "/" && dir != ""; dir = filepath.ToSlash(filepath.Dir(dir)) {
			seenDirs[dir] = struct{}{}
		}
	}
	if !result.Truncated {
		for dir := range seenDirs {
			if result.Scanned >= maxProjectFindEntries {
				result.Truncated = true
				break
			}
			result.Scanned++
			if rank, ok := projectFindRank(dir, filepath.Base(dir), queryLower); ok {
				result.Matches = append(result.Matches, ProjectFindMatch{Path: dir, Kind: "directory", Rank: rank})
			}
		}
	}
	return nil
}

func finishProjectFind(result ProjectFindResult, maxResults int) ProjectFindResult {
	sort.Slice(result.Matches, func(i, j int) bool {
		if result.Matches[i].Rank == result.Matches[j].Rank {
			return result.Matches[i].Path < result.Matches[j].Path
		}
		return result.Matches[i].Rank < result.Matches[j].Rank
	})
	if len(result.Matches) > maxResults {
		result.Matches = result.Matches[:maxResults]
		result.Truncated = true
	}
	return result
}

func projectFindRank(rel, base, queryLower string) (int, bool) {
	pathLower := strings.ToLower(filepath.ToSlash(rel))
	baseLower := strings.ToLower(base)
	switch {
	case pathLower == queryLower:
		return 0, true
	case baseLower == queryLower:
		return 1, true
	case strings.HasPrefix(baseLower, queryLower):
		return 2, true
	case strings.HasPrefix(pathLower, queryLower):
		return 3, true
	case strings.Contains(baseLower, queryLower):
		return 4, true
	case strings.Contains(pathLower, queryLower):
		return 5, true
	default:
		return 0, false
	}
}
