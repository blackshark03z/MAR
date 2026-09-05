package domain

import "time"

type TaskState string

const (
	TaskSubmitted        TaskState = "SUBMITTED"
	TaskPreflight        TaskState = "PREFLIGHT"
	TaskWaitingResource  TaskState = "WAITING_RESOURCE"
	TaskWorkspaceReady   TaskState = "WORKSPACE_READY"
	TaskRunning          TaskState = "RUNNING"
	TaskVerifying        TaskState = "VERIFYING"
	TaskReviewing        TaskState = "REVIEWING"
	TaskReadyToIntegrate TaskState = "READY_TO_INTEGRATE"
	TaskIntegrating      TaskState = "INTEGRATING"
	TaskVerified         TaskState = "VERIFIED"
	TaskComplete         TaskState = "COMPLETE"
	TaskInputRequired    TaskState = "INPUT_REQUIRED"
	TaskBlocked          TaskState = "BLOCKED"
	TaskRetryWait        TaskState = "RETRY_WAIT"
	TaskFailed           TaskState = "FAILED"
	TaskCancelled        TaskState = "CANCELLED"
)

type Project struct {
	ID        string    `json:"id"`
	Root      string    `json:"root"`
	CreatedAt time.Time `json:"created_at"`
}

type Task struct {
	ID             string       `json:"id"`
	IdempotencyKey string       `json:"idempotency_key"`
	Contract       GoalContract `json:"contract"`
	ContractHash   string       `json:"contract_hash"`
	State          TaskState    `json:"state"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}
