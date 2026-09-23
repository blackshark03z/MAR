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

	"mar/internal/domain"
	"mar/internal/pathidentity"
)

const maxProjectReadBytes int64 = 512 << 10

type ProjectReadResult struct {
	ProjectID string `json:"project_id"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	SizeBytes int64  `json:"size_bytes"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

func (s *TaskService) ReadProjectFile(ctx context.Context, projectID, requestedPath string) (ProjectReadResult, error) {
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" {
		return ProjectReadResult{}, errors.New("file path is required")
	}
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return ProjectReadResult{}, err
	}
	if len(projects) == 0 {
		return ProjectReadResult{}, errors.New("no MAR projects are registered")
	}

	project, target, err := resolveProjectReadTarget(projects, strings.TrimSpace(projectID), requestedPath)
	if err != nil {
		return ProjectReadResult{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return ProjectReadResult{}, fmt.Errorf("read project file metadata: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ProjectReadResult{}, errors.New("project read supports regular files only")
	}
	if info.Size() > maxProjectReadBytes {
		return ProjectReadResult{}, fmt.Errorf("project file exceeds bounded read limit of %d bytes", maxProjectReadBytes)
	}
	payload, err := os.ReadFile(target)
	if err != nil {
		return ProjectReadResult{}, fmt.Errorf("read project file: %w", err)
	}
	if bytes.IndexByte(payload, 0) >= 0 || !utf8.Valid(payload) {
		return ProjectReadResult{}, errors.New("project read supports UTF-8 text files only")
	}
	root, err := pathidentity.ResolveExisting(project.Root)
	if err != nil {
		return ProjectReadResult{}, fmt.Errorf("resolve project root: %w", err)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return ProjectReadResult{}, fmt.Errorf("resolve project-relative path: %w", err)
	}
	return ProjectReadResult{ProjectID: project.ID, Path: filepath.ToSlash(rel), Content: string(payload), SizeBytes: int64(len(payload))}, nil
}

func resolveProjectReadTarget(projects []domain.Project, projectID, requestedPath string) (domain.Project, string, error) {
	if projectID != "" {
		for _, project := range projects {
			if project.ID == projectID {
				target, err := safeProjectReadTarget(project, requestedPath)
				return project, target, err
			}
		}
		return domain.Project{}, "", fmt.Errorf("unknown project %q", projectID)
	}

	if filepath.IsAbs(requestedPath) {
		type match struct {
			project domain.Project
			target  string
			depth   int
		}
		var matches []match
		for _, project := range projects {
			target, err := safeProjectReadTarget(project, requestedPath)
			if err == nil {
				matches = append(matches, match{project: project, target: target, depth: len(filepath.Clean(project.Root))})
			}
		}
		if len(matches) == 0 {
			return domain.Project{}, "", errors.New("absolute file path is outside every registered MAR project")
		}
		sort.Slice(matches, func(i, j int) bool { return matches[i].depth > matches[j].depth })
		return matches[0].project, matches[0].target, nil
	}

	if len(projects) == 1 {
		target, err := safeProjectReadTarget(projects[0], requestedPath)
		return projects[0], target, err
	}

	var matches []struct {
		project domain.Project
		target  string
	}
	for _, project := range projects {
		target, err := safeProjectReadTarget(project, requestedPath)
		if err != nil {
			continue
		}
		if info, statErr := os.Stat(target); statErr == nil && info.Mode().IsRegular() {
			matches = append(matches, struct {
				project domain.Project
				target  string
			}{project: project, target: target})
		}
	}
	if len(matches) == 1 {
		return matches[0].project, matches[0].target, nil
	}
	ids := make([]string, 0, len(projects))
	for _, project := range projects {
		ids = append(ids, project.ID)
	}
	sort.Strings(ids)
	if len(matches) > 1 {
		return domain.Project{}, "", fmt.Errorf("file path is ambiguous across registered projects; choose one project id: %s", strings.Join(ids, ", "))
	}
	return domain.Project{}, "", fmt.Errorf("file was not found unambiguously; choose one project id if needed: %s", strings.Join(ids, ", "))
}

func safeProjectReadTarget(project domain.Project, requestedPath string) (string, error) {
	rootAbs, err := filepath.Abs(project.Root)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)
	root, err := pathidentity.ResolveExisting(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	candidate := requestedPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(rootAbs, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	candidate = filepath.Clean(candidate)
	// Reject lexical traversal before opening a path outside the registered
	// root. This preserves fail-closed behavior even when the caller lacks
	// authority to inspect the escaped location.
	lexicalRel, lexicalErr := filepath.Rel(rootAbs, candidate)
	if lexicalErr != nil || lexicalRel == ".." || strings.HasPrefix(lexicalRel, ".."+string(filepath.Separator)) || filepath.IsAbs(lexicalRel) {
		return "", errors.New("project file path escapes the registered project root")
	}
	real, err := pathidentity.ResolveWithin(rootAbs, candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("project file path escapes the registered project root")
	}
	return filepath.Clean(real), nil
}
