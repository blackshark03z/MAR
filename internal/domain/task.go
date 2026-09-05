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

type AttemptAuthorityState string

const (
	AttemptActive               AttemptAuthorityState = "ACTIVE"
	AttemptLogicallyFenced      AttemptAuthorityState = "LOGICALLY_FENCED"
	AttemptTerminating          AttemptAuthorityState = "TERMINATING"
	AttemptPhysicallyTerminated AttemptAuthorityState = "PHYSICALLY_TERMINATED"
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
	RunEpoch       int64        `json:"run_epoch"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

type ExecutionAttempt struct {
	ID             string                `json:"attempt_id"`
	TaskID         string                `json:"task_id"`
	RunEpoch       int64                 `json:"run_epoch"`
	WorkerID       string                `json:"worker_id"`
	SupervisorID   string                `json:"supervisor_id"`
	AuthorityState AttemptAuthorityState `json:"authority_state"`
	StartedAt      time.Time             `json:"started_at"`
	HeartbeatAt    time.Time             `json:"heartbeat_at"`
	LeaseDeadline  time.Time             `json:"lease_deadline"`
	TerminatedAt   *time.Time            `json:"terminated_at,omitempty"`
	TerminalStatus string                `json:"terminal_status,omitempty"`
}
