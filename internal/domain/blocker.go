package domain

import "time"

// BlockerPhase identifies the lifecycle phase whose durable reality prevents forward progress.
type BlockerPhase string

const (
	BlockerPhasePreflight BlockerPhase = "PREFLIGHT"
	BlockerPhaseWorkspace BlockerPhase = "WORKSPACE"
	BlockerPhaseExecution BlockerPhase = "EXECUTION"
	BlockerPhaseInvariant BlockerPhase = "INVARIANT"
	BlockerPhaseVerify    BlockerPhase = "VERIFICATION"
	BlockerPhaseIntegrate BlockerPhase = "INTEGRATION"
)

// TaskBlocker is the current durable explanation for why a task is BLOCKED.
type TaskBlocker struct {
	TaskID    string       `json:"task_id"`
	Phase     BlockerPhase `json:"phase"`
	Code      string       `json:"code"`
	Detail    string       `json:"detail"`
	Recovery  string       `json:"recovery"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}
