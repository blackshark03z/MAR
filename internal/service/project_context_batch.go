package service

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	defaultProjectContextBatchResults = 8
	maxProjectContextBatchResults     = 20
	defaultProjectContextBatchFiles   = 6
	maxProjectContextBatchFiles       = 12
	defaultProjectContextBatchBytes   = 48 << 10
	maxProjectContextBatchBytes       = 128 << 10
)

type ProjectContextSnippet struct {
	Path       string `json:"path"`
	Source     string `json:"source"`
	AnchorLine int    `json:"anchor_line,omitempty"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type ProjectContextBatchResult struct {
	ProjectID     string                  `json:"project_id"`
	Query         string                  `json:"query"`
	FindMatches   []ProjectFindMatch      `json:"find_matches"`
	SearchMatches []ProjectSearchMatch    `json:"search_matches"`
	Files         []ProjectContextSnippet `json:"files"`
	Bytes         int                     `json:"bytes"`
	Truncated     bool                    `json:"truncated,omitempty"`
}

type contextBatchCandidate struct {
	path       string
	source     string
	anchorLine int
}

func (s *TaskService) BuildProjectContextBatch(ctx context.Context, projectID, query string, maxResults, maxFiles, maxBytes int) (ProjectContextBatchResult, error) {
	projectID = strings.TrimSpace(projectID)
	query = strings.TrimSpace(query)
	if projectID == "" {
		return ProjectContextBatchResult{}, errors.New("project_id is required for context_batch")
	}
	if query == "" {
		return ProjectContextBatchResult{}, errors.New("query is required for context_batch")
	}
	if maxResults <= 0 {
		maxResults = defaultProjectContextBatchResults
	}
	if maxResults > maxProjectContextBatchResults {
		maxResults = maxProjectContextBatchResults
	}
	if maxFiles <= 0 {
		maxFiles = defaultProjectContextBatchFiles
	}
	if maxFiles > maxProjectContextBatchFiles {
		maxFiles = maxProjectContextBatchFiles
	}
	if maxBytes <= 0 {
		maxBytes = defaultProjectContextBatchBytes
	}
	if maxBytes > maxProjectContextBatchBytes {
		maxBytes = maxProjectContextBatchBytes
	}
	if maxBytes < 4096 {
		return ProjectContextBatchResult{}, errors.New("context_batch max_bytes must be at least 4096")
	}

	find, err := s.FindProjectFiles(ctx, projectID, query, maxResults)
	if err != nil {
		return ProjectContextBatchResult{}, err
	}
	search, err := s.SearchProjectText(ctx, projectID, "", query, maxResults)
	if err != nil {
		return ProjectContextBatchResult{}, err
	}

	candidates := make([]contextBatchCandidate, 0, maxFiles)
	seen := make(map[string]struct{}, maxFiles)
	add := func(path, source string, anchorLine int) {
		if len(candidates) >= maxFiles || strings.TrimSpace(path) == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, contextBatchCandidate{path: path, source: source, anchorLine: anchorLine})
	}
	for _, match := range search.Matches {
		add(match.Path, "search", match.Line)
	}
	for _, match := range find.Matches {
		if match.Kind == "file" {
			add(match.Path, "find", 1)
		}
	}

	result := ProjectContextBatchResult{
		ProjectID:     projectID,
		Query:         query,
		FindMatches:   find.Matches,
		SearchMatches: search.Matches,
		Files:         make([]ProjectContextSnippet, 0, len(candidates)),
	}
	remaining := maxBytes
	for index, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return ProjectContextBatchResult{}, err
		}
		filesLeft := len(candidates) - index
		if remaining < 256 || filesLeft <= 0 {
			result.Truncated = true
			break
		}
		budget := remaining / filesLeft
		if budget > 8<<10 {
			budget = 8 << 10
		}
		file, err := s.ReadProjectFile(ctx, projectID, candidate.path)
		if err != nil {
			result.Truncated = true
			continue
		}
		startLine, endLine, content, truncated := contextBatchSnippet(file.Content, candidate.anchorLine, budget)
		if content == "" {
			continue
		}
		result.Files = append(result.Files, ProjectContextSnippet{
			Path:       file.Path,
			Source:     candidate.source,
			AnchorLine: candidate.anchorLine,
			StartLine:  startLine,
			EndLine:    endLine,
			Content:    content,
			Truncated:  truncated,
		})
		result.Bytes += len(content)
		remaining -= len(content)
		if truncated {
			result.Truncated = true
		}
	}
	if len(result.Files) < len(candidates) {
		result.Truncated = true
	}
	return result, nil
}

func contextBatchSnippet(content string, anchorLine, maxBytes int) (int, int, string, bool) {
	if maxBytes <= 0 || content == "" {
		return 0, 0, "", content != ""
	}
	lines := strings.Split(content, "\n")
	if anchorLine <= 0 {
		anchorLine = 1
	}
	if anchorLine > len(lines) {
		anchorLine = len(lines)
	}
	startLine := anchorLine - 20
	if startLine < 1 {
		startLine = 1
	}
	var b strings.Builder
	endLine := startLine - 1
	truncated := false
	for i := startLine - 1; i < len(lines); i++ {
		piece := lines[i]
		if i < len(lines)-1 {
			piece += "\n"
		}
		remaining := maxBytes - b.Len()
		if remaining <= 0 {
			truncated = true
			break
		}
		if len(piece) > remaining {
			b.WriteString(contextBatchUTF8Prefix(piece, remaining))
			endLine = i + 1
			truncated = true
			break
		}
		b.WriteString(piece)
		endLine = i + 1
		if i+1 >= anchorLine+60 {
			if i+1 < len(lines) {
				truncated = true
			}
			break
		}
	}
	return startLine, endLine, b.String(), truncated
}

func contextBatchUTF8Prefix(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	if maxBytes <= 0 {
		return ""
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}
