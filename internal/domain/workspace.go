package domain

import "time"

type WorkspaceState string

const (
	WorkspacePreparing WorkspaceState = "PREPARING"
	WorkspaceReady     WorkspaceState = "READY"
	WorkspaceFailed    WorkspaceState = "FAILED"
	WorkspaceRemoving  WorkspaceState = "REMOVING"
	WorkspaceRemoved   WorkspaceState = "REMOVED"
)

type Workspace struct {
	ID           string         `json:"id"`
	TaskID       string         `json:"task_id"`
	ProjectID    string         `json:"project_id"`
	Path         string         `json:"path"`
	BaseRevision string         `json:"base_revision"`
	HeadRevision string         `json:"head_revision,omitempty"`
	State        WorkspaceState `json:"state"`
	Failure      string         `json:"failure,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	RemovedAt    *time.Time     `json:"removed_at,omitempty"`
}
