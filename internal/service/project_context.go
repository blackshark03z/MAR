package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"mar/internal/domain"
)

type ProjectContextItem struct {
	ProjectID string               `json:"project_id"`
	Head      string               `json:"head"`
	Policy    domain.ProjectPolicy `json:"policy"`
}

func (s *TaskService) ProjectContext(ctx context.Context, projectID string) ([]ProjectContextItem, error) {
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, errors.New("no MAR projects are registered")
	}
	projectID = strings.TrimSpace(projectID)
	items := make([]ProjectContextItem, 0, len(projects))
	for _, project := range projects {
		if projectID != "" && project.ID != projectID {
			continue
		}
		cmd := exec.CommandContext(ctx, "git", "-C", project.Root, "rev-parse", "HEAD")
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("read current HEAD for project %q: %w", project.ID, err)
		}
		policy, err := s.store.GetProjectPolicy(ctx, project.ID)
		if err != nil {
			return nil, fmt.Errorf("read project policy for %q: %w", project.ID, err)
		}
		items = append(items, ProjectContextItem{ProjectID: project.ID, Head: strings.TrimSpace(string(out)), Policy: policy})
	}
	if projectID != "" && len(items) == 0 {
		return nil, fmt.Errorf("unknown project %q", projectID)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ProjectID < items[j].ProjectID })
	return items, nil
}
