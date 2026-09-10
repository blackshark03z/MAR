package domain

import "time"

// ObservationArtifact indexes durable tool evidence whose bytes live outside
// SQLite and outside candidate Git content. SourceBytes describes the known
// size of the underlying observation source; CapturedBytes is the immutable
// artifact content actually retained. Complete is false whenever an upstream
// capture boundary or the artifact ceiling discarded source bytes.
type ObservationArtifact struct {
	Handle        string    `json:"handle"`
	TaskID        string    `json:"task_id"`
	AttemptID     string    `json:"attempt_id"`
	RunEpoch      int64     `json:"run_epoch"`
	ToolCallID    string    `json:"tool_call_id"`
	Kind          string    `json:"kind"`
	SHA256        string    `json:"sha256"`
	CapturedBytes int64     `json:"captured_bytes"`
	SourceBytes   int64     `json:"source_bytes"`
	Complete      bool      `json:"complete"`
	Truncated     bool      `json:"truncated"`
	CreatedAt     time.Time `json:"created_at"`
}

type ObservationArtifactChunk struct {
	Artifact   ObservationArtifact `json:"artifact"`
	Offset     int64               `json:"offset"`
	Data       string              `json:"data"`
	NextOffset int64               `json:"next_offset"`
	EOF        bool                `json:"eof"`
}
