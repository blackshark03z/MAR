package domain

import "time"

type WorkspaceCheckpointState string

const (
	WorkspaceCheckpointCaptured    WorkspaceCheckpointState = "CAPTURED"
	WorkspaceCheckpointCompacted   WorkspaceCheckpointState = "COMPACTED"
	WorkspaceCheckpointRehydrating WorkspaceCheckpointState = "REHYDRATING"
	WorkspaceCheckpointRehydrated  WorkspaceCheckpointState = "REHYDRATED"
)

type WorkspaceCheckpoint struct {
	ID               string                   `json:"id"`
	TaskID           string                   `json:"task_id"`
	WorkspaceID      string                   `json:"workspace_id"`
	ProjectID        string                   `json:"project_id"`
	Version          int64                    `json:"version"`
	OriginalHead     string                   `json:"original_head"`
	SnapshotRevision string                   `json:"snapshot_revision"`
	RefName          string                   `json:"ref_name"`
	StatusHash       string                   `json:"status_hash"`
	Dirty            bool                     `json:"dirty"`
	State            WorkspaceCheckpointState `json:"state"`
	CreatedAt        time.Time                `json:"created_at"`
	CompactedAt      *time.Time               `json:"compacted_at,omitempty"`
	RehydratedAt     *time.Time               `json:"rehydrated_at,omitempty"`
}
