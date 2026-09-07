package domain

import "time"

// ProjectPolicy is owner-controlled project-level authority. A Goal Contract may
// narrow these permissions, but preflight must never allow it to widen them.
type ProjectPolicy struct {
	ProjectID      string    `json:"project_id"`
	LocalFileWrite bool      `json:"local_file_write"`
	LocalGitWrite  bool      `json:"local_git_write"`
	NetworkAllowed bool      `json:"network_allowed"`
	RemoteGitWrite bool      `json:"remote_git_write"`
	DeployAllowed  bool      `json:"deploy_allowed"`
	UpdatedAt      time.Time `json:"updated_at"`
}
