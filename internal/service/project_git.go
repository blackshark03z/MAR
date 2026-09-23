package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxProjectGitOutputBytes = 256 << 10

type ProjectGitStatusResult struct {
	ProjectID string `json:"project_id"`
	Head      string `json:"head"`
	Branch    string `json:"branch"`
	Porcelain string `json:"porcelain"`
	Truncated bool   `json:"truncated"`
}

type ProjectGitDiffResult struct {
	ProjectID string `json:"project_id"`
	Path      string `json:"path,omitempty"`
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}

func (s *TaskService) ProjectGitStatus(ctx context.Context, projectID string) (ProjectGitStatusResult, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ProjectGitStatusResult{}, errors.New("project_id is required for project git_status")
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatusResult{}, err
	}
	identity, err := runProjectGit(ctx, project.Root, "rev-parse", "HEAD", "--abbrev-ref", "HEAD")
	if err != nil {
		return ProjectGitStatusResult{}, err
	}
	identityLines := strings.Fields(identity)
	if len(identityLines) != 2 {
		return ProjectGitStatusResult{}, fmt.Errorf("git rev-parse returned %d identity fields", len(identityLines))
	}
	status, err := runProjectGit(ctx, project.Root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return ProjectGitStatusResult{}, err
	}
	status, truncated := boundProjectGitOutput(status)
	return ProjectGitStatusResult{
		ProjectID: project.ID,
		Head:      identityLines[0],
		Branch:    identityLines[1],
		Porcelain: status,
		Truncated: truncated,
	}, nil
}

func (s *TaskService) ProjectGitDiff(ctx context.Context, projectID, requestedPath string) (ProjectGitDiffResult, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ProjectGitDiffResult{}, errors.New("project_id is required for project git_diff")
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return ProjectGitDiffResult{}, err
	}
	args := []string{"diff", "--no-ext-diff", "--no-color", "HEAD", "--"}
	relPath := ""
	if strings.TrimSpace(requestedPath) != "" {
		target, err := safeProjectReadTarget(project, requestedPath)
		if err != nil {
			return ProjectGitDiffResult{}, err
		}
		root, err := filepath.Abs(project.Root)
		if err != nil {
			return ProjectGitDiffResult{}, fmt.Errorf("resolve project root: %w", err)
		}
		rel, err := filepath.Rel(root, target)
		if err != nil {
			return ProjectGitDiffResult{}, fmt.Errorf("resolve project-relative diff path: %w", err)
		}
		relPath = filepath.ToSlash(rel)
		args = append(args, rel)
	}
	diff, err := runProjectGit(ctx, project.Root, args...)
	if err != nil {
		return ProjectGitDiffResult{}, err
	}
	diff, truncated := boundProjectGitOutput(diff)
	return ProjectGitDiffResult{ProjectID: project.ID, Path: relPath, Diff: diff, Truncated: truncated}, nil
}

func runProjectGit(ctx context.Context, root string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", filepath.Clean(root)}, args...)
	out, err := exec.CommandContext(ctx, "git", cmdArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func boundProjectGitOutput(value string) (string, bool) {
	if len(value) <= maxProjectGitOutputBytes {
		return value, false
	}
	return value[:maxProjectGitOutputBytes], true
}
