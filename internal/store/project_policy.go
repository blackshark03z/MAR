package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"mar/internal/domain"
)

func (s *SQLite) GetProjectPolicy(ctx context.Context, projectID string) (domain.ProjectPolicy, error) {
	var policy domain.ProjectPolicy
	var fileWrite, gitWrite, network, remoteGit, deploy int
	var updated string
	err := s.db.QueryRowContext(ctx, `
SELECT project_id, local_file_write, local_git_write, network_allowed, remote_git_write, deploy_allowed, updated_at
FROM project_policies WHERE project_id = ?`, projectID).Scan(&policy.ProjectID, &fileWrite, &gitWrite, &network, &remoteGit, &deploy, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProjectPolicy{}, ErrNotFound
	}
	if err != nil {
		return domain.ProjectPolicy{}, fmt.Errorf("get project policy: %w", err)
	}
	policy.LocalFileWrite = fileWrite != 0
	policy.LocalGitWrite = gitWrite != 0
	policy.NetworkAllowed = network != 0
	policy.RemoteGitWrite = remoteGit != 0
	policy.DeployAllowed = deploy != 0
	policy.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return domain.ProjectPolicy{}, fmt.Errorf("parse project policy updated_at: %w", err)
	}
	return policy, nil
}

func (s *SQLite) PutProjectPolicy(ctx context.Context, policy domain.ProjectPolicy) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO project_policies(project_id, local_file_write, local_git_write, network_allowed, remote_git_write, deploy_allowed, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id) DO UPDATE SET
  local_file_write=excluded.local_file_write,
  local_git_write=excluded.local_git_write,
  network_allowed=excluded.network_allowed,
  remote_git_write=excluded.remote_git_write,
  deploy_allowed=excluded.deploy_allowed,
  updated_at=excluded.updated_at`,
		policy.ProjectID, boolInt(policy.LocalFileWrite), boolInt(policy.LocalGitWrite), boolInt(policy.NetworkAllowed), boolInt(policy.RemoteGitWrite), boolInt(policy.DeployAllowed), policy.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("put project policy: %w", err)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
