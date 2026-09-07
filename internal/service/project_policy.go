package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"mar/internal/domain"
)

func (s *TaskService) ProjectPolicy(ctx context.Context, projectID string) (domain.ProjectPolicy, error) {
	if strings.TrimSpace(projectID) == "" {
		return domain.ProjectPolicy{}, errors.New("project id is required")
	}
	return s.store.GetProjectPolicy(ctx, projectID)
}

func (s *TaskService) UpdateProjectPolicy(ctx context.Context, projectID string, localFileWrite, localGitWrite bool) (domain.ProjectPolicy, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.ProjectPolicy{}, errors.New("project id is required")
	}
	if _, err := s.store.GetProject(ctx, projectID); err != nil {
		return domain.ProjectPolicy{}, err
	}
	policy := domain.ProjectPolicy{ProjectID: projectID, LocalFileWrite: localFileWrite, LocalGitWrite: localGitWrite, UpdatedAt: s.now().UTC()}
	if err := s.store.PutProjectPolicy(ctx, policy); err != nil {
		return domain.ProjectPolicy{}, err
	}
	return policy, nil
}

func defaultProjectPolicy(projectID string, now time.Time) domain.ProjectPolicy {
	return domain.ProjectPolicy{ProjectID: projectID, LocalFileWrite: true, LocalGitWrite: true, UpdatedAt: now.UTC()}
}
