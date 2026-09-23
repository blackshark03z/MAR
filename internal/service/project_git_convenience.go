package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ProjectBranchListResult struct {
	ProjectID string
	Current   string
	Branches  []string
}

type ProjectBranchResult struct {
	ProjectID string
	Name      string
	Revision  string
}

type ProjectWorktreeResult struct {
	ProjectID string
	Path      string
	Baseline  string
	Head      string
	Purpose   string
}

func (s *TaskService) ListProjectBranches(ctx context.Context, projectID string) (ProjectBranchListResult, error) {
	project, _, err := s.fastProject(ctx, projectID)
	if err != nil {
		return ProjectBranchListResult{}, err
	}
	currentRaw, err := runProjectGit(ctx, project.Root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ProjectBranchListResult{}, err
	}
	branchesRaw, err := runProjectGit(ctx, project.Root, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return ProjectBranchListResult{}, err
	}
	lines := strings.Split(strings.TrimSpace(branchesRaw), "\n")
	branches := make([]string, 0, len(lines))
	for _, line := range lines {
		if value := strings.TrimSpace(line); value != "" {
			branches = append(branches, value)
		}
	}
	return ProjectBranchListResult{ProjectID: project.ID, Current: strings.TrimSpace(currentRaw), Branches: branches}, nil
}

func (s *TaskService) CreateProjectBranch(ctx context.Context, projectID, name string) (ProjectBranchResult, error) {
	project, policy, err := s.fastProject(ctx, projectID)
	if err != nil {
		return ProjectBranchResult{}, err
	}
	if !policy.LocalGitWrite {
		return ProjectBranchResult{}, errors.New("project local_git_write policy is disabled")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ProjectBranchResult{}, errors.New("branch name is required")
	}
	if _, err := runProjectGit(ctx, project.Root, "check-ref-format", "--branch", name); err != nil {
		return ProjectBranchResult{}, fmt.Errorf("invalid branch name: %w", err)
	}
	if _, err := runProjectGit(ctx, project.Root, "branch", name); err != nil {
		return ProjectBranchResult{}, err
	}
	revision, err := runProjectGit(ctx, project.Root, "rev-parse", name)
	if err != nil {
		return ProjectBranchResult{}, err
	}
	return ProjectBranchResult{ProjectID: project.ID, Name: name, Revision: strings.TrimSpace(revision)}, nil
}

func (s *TaskService) CreateProjectWorktree(ctx context.Context, projectID, baseline, purpose string) (ProjectWorktreeResult, error) {
	project, policy, err := s.fastProject(ctx, projectID)
	if err != nil {
		return ProjectWorktreeResult{}, err
	}
	if !policy.LocalGitWrite {
		return ProjectWorktreeResult{}, errors.New("project local_git_write policy is disabled")
	}
	baseline = strings.TrimSpace(baseline)
	if baseline == "" {
		return ProjectWorktreeResult{}, errors.New("baseline is required")
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		purpose = "work"
	}
	commit, err := runProjectGit(ctx, project.Root, "rev-parse", "--verify", baseline+"^{commit}")
	if err != nil {
		return ProjectWorktreeResult{}, fmt.Errorf("resolve worktree baseline: %w", err)
	}
	commit = strings.TrimSpace(commit)
	parent := filepath.Join(os.TempDir(), "mar-worktrees", fastPathSlug(project.ID))
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return ProjectWorktreeResult{}, fmt.Errorf("create worktree parent: %w", err)
	}
	target := filepath.Join(parent, fastPathSlug(purpose)+"-"+commit[:12]+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if _, err := runProjectGit(ctx, project.Root, "worktree", "add", "--detach", target, commit); err != nil {
		return ProjectWorktreeResult{}, err
	}
	return ProjectWorktreeResult{ProjectID: project.ID, Path: target, Baseline: commit, Head: commit, Purpose: purpose}, nil
}

func fastPathSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "work"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}
