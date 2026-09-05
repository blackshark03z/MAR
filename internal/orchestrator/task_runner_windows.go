//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mar/internal/agent"
	"mar/internal/domain"
	"mar/internal/processctl"
	"mar/internal/service"
	"mar/internal/verification"
	"mar/internal/worker"
)

var ErrRecoveryRequired = errors.New("task requires physical recovery before replacement")

type taskService interface {
	Status(context.Context, string) (domain.Task, error)
	StatusSnapshot(context.Context, string) (service.TaskStatusSnapshot, error)
	BeginAttempt(context.Context, string, string, string, time.Duration) (domain.ExecutionAttempt, error)
	TransitionForAttempt(context.Context, string, string, int64, domain.TaskState) error
	LogicalFenceAttempt(context.Context, string, string, int64) error
	ConfirmAttemptProcessTermination(context.Context, processctl.TerminationProof, string) error
	FinalizeCancellation(context.Context, string) error
}

type workerProcess interface {
	Run(context.Context, worker.StartRequest) (agent.Result, processctl.TerminationProof, error)
}

type verifierEngine interface {
	Verify(context.Context, verification.VerifyRequest) (domain.TaskResult, error)
}

type integrationEngine interface {
	Integrate(context.Context, string) (domain.IntegrationAttempt, domain.TaskResult, error)
}

type RuntimeFactory func(workspacePath, taskID string) (verification.CommandRuntime, error)

type TaskRunnerConfig struct {
	WorkerID         string
	SupervisorID     string
	LeaseDuration    time.Duration
	Provider         worker.ProviderConfig
	AgentProfile     agent.Profile
	AgentConfig      agent.Config
	ResourceSummary  domain.ResourceSummary
	SandboxReadPaths []string
	GoModuleCache    string
}

func (c TaskRunnerConfig) validate() error {
	if strings.TrimSpace(c.WorkerID) == "" || strings.TrimSpace(c.SupervisorID) == "" {
		return errors.New("task runner requires worker and supervisor ids")
	}
	if c.LeaseDuration <= 0 {
		return errors.New("task runner lease duration must be positive")
	}
	if strings.TrimSpace(c.Provider.BaseURL) == "" || strings.TrimSpace(c.Provider.APIKeyEnv) == "" {
		return errors.New("task runner requires model provider configuration")
	}
	if strings.TrimSpace(c.AgentProfile.Model) == "" || strings.TrimSpace(c.AgentProfile.BaseInstructions) == "" {
		return errors.New("task runner requires agent profile")
	}
	return nil
}

type RunOutcome struct {
	TaskID       string             `json:"task_id"`
	Agent        agent.Result       `json:"agent"`
	Verification *domain.TaskResult `json:"verification,omitempty"`
	Integration  *domain.TaskResult `json:"integration,omitempty"`
}

type TaskRunner struct {
	service     taskService
	worker      workerProcess
	verifier    verifierEngine
	integration integrationEngine
	runtime     RuntimeFactory
	proofValid  func(processctl.TerminationProof) bool
	cfg         TaskRunnerConfig
}

func NewTaskRunner(s taskService, workerProcess workerProcess, verifier verifierEngine, integrationManager integrationEngine, runtimeFactory RuntimeFactory, cfg TaskRunnerConfig) (*TaskRunner, error) {
	if s == nil || workerProcess == nil || verifier == nil || integrationManager == nil || runtimeFactory == nil {
		return nil, errors.New("task runner requires service, worker, verifier, integration manager and runtime factory")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &TaskRunner{
		service:     s,
		worker:      workerProcess,
		verifier:    verifier,
		integration: integrationManager,
		runtime:     runtimeFactory,
		proofValid:  func(proof processctl.TerminationProof) bool { return proof.Valid() },
		cfg:         cfg,
	}, nil
}

// RunWorkspaceReady executes one already-admitted mutable task. The worker
// process must be physically dead before verification starts, but durable
// attempt authority remains ACTIVE until verification has persisted its exact
// revision-bound outcome. Only then is physical termination recorded and the
// integration lane allowed to advance authoritative Git state.
func (r *TaskRunner) RunWorkspaceReady(ctx context.Context, taskID string, workspace domain.Workspace) (RunOutcome, error) {
	if strings.TrimSpace(taskID) == "" || workspace.TaskID != taskID || workspace.State != domain.WorkspaceReady || strings.TrimSpace(workspace.Path) == "" {
		return RunOutcome{}, errors.New("task runner requires matching READY workspace")
	}
	task, err := r.service.Status(ctx, taskID)
	if err != nil {
		return RunOutcome{}, err
	}
	if task.State != domain.TaskWorkspaceReady {
		return RunOutcome{}, fmt.Errorf("task %s is not WORKSPACE_READY", taskID)
	}
	attempt, err := r.service.BeginAttempt(ctx, taskID, r.cfg.WorkerID, r.cfg.SupervisorID, r.cfg.LeaseDuration)
	if err != nil {
		return RunOutcome{}, err
	}
	task, err = r.service.Status(ctx, taskID)
	if err != nil {
		return RunOutcome{}, err
	}
	if task.State != domain.TaskRunning || task.RunEpoch != attempt.RunEpoch {
		return RunOutcome{}, errors.New("task/attempt state diverged after attempt admission")
	}

	start := worker.StartRequest{
		Task:             task,
		Attempt:          attempt,
		WorkspacePath:    workspace.Path,
		Provider:         r.cfg.Provider,
		AgentProfile:     r.cfg.AgentProfile,
		AgentConfig:      r.cfg.AgentConfig,
		SandboxReadPaths: append([]string{}, r.cfg.SandboxReadPaths...),
		GoModuleCache:    r.cfg.GoModuleCache,
	}
	agentResult, proof, runErr := r.worker.Run(ctx, start)
	outcome := RunOutcome{TaskID: taskID, Agent: agentResult}

	cancelRequested, cancelErr := r.cancelRequested(ctx, taskID)
	if cancelErr != nil {
		return outcome, cancelErr
	}
	if cancelRequested {
		if err := r.finalizeCancellation(ctx, attempt, proof); err != nil {
			return outcome, err
		}
		return outcome, nil
	}
	if runErr != nil {
		return outcome, r.failWorker(ctx, attempt, proof, fmt.Errorf("worker process failed: %w", runErr))
	}

	switch agentResult.Status {
	case agent.StatusCompletedCandidate:
		return r.verifyAndIntegrate(ctx, outcome, attempt, proof, workspace)
	case agent.StatusBlocked:
		return outcome, r.finishNonCandidate(ctx, attempt, proof, domain.TaskBlocked, "worker-blocked")
	case agent.StatusBudgetExhausted:
		return outcome, r.finishNonCandidate(ctx, attempt, proof, domain.TaskRetryWait, "worker-budget-exhausted")
	case agent.StatusCancelled:
		// A worker can self-cancel because authority became stale for reasons other
		// than an owner cancellation. Do not claim user cancellation without the
		// durable cancel control; block for reconciliation instead.
		return outcome, r.finishNonCandidate(ctx, attempt, proof, domain.TaskBlocked, "worker-cancelled-without-cancel-control")
	default:
		return outcome, r.finishNonCandidate(ctx, attempt, proof, domain.TaskBlocked, "worker-invalid-terminal-status")
	}
}

func (r *TaskRunner) verifyAndIntegrate(ctx context.Context, outcome RunOutcome, attempt domain.ExecutionAttempt, proof processctl.TerminationProof, workspace domain.Workspace) (RunOutcome, error) {
	if !r.proofValid(proof) {
		return outcome, r.recoveryBlock(ctx, attempt, errors.New("worker completed candidate without valid physical termination proof"))
	}
	runtime, err := r.runtime(workspace.Path, attempt.TaskID)
	if err != nil {
		blockErr := r.blockWhileAuthoritative(ctx, attempt)
		confirmErr := r.service.ConfirmAttemptProcessTermination(ctx, proof, "verification-runtime-init-failed")
		return outcome, errors.Join(fmt.Errorf("create verification runtime: %w", err), blockErr, confirmErr)
	}
	verified, verifyErr := r.verifier.Verify(ctx, verification.VerifyRequest{
		TaskID:          attempt.TaskID,
		AttemptID:       attempt.ID,
		RunEpoch:        attempt.RunEpoch,
		Runtime:         runtime,
		ResourceSummary: r.cfg.ResourceSummary,
	})
	if verifyErr != nil {
		cancelRequested, cancelErr := r.cancelRequested(ctx, attempt.TaskID)
		if cancelErr == nil && cancelRequested {
			if err := r.finalizeCancellation(ctx, attempt, proof); err != nil {
				return outcome, errors.Join(verifyErr, err)
			}
			return outcome, nil
		}
		blockErr := r.blockWhileAuthoritative(ctx, attempt)
		confirmErr := r.service.ConfirmAttemptProcessTermination(ctx, proof, "verification-error")
		return outcome, errors.Join(verifyErr, cancelErr, blockErr, confirmErr)
	}
	outcome.Verification = &verified
	if err := r.service.ConfirmAttemptProcessTermination(ctx, proof, "worker-exited-before-verification-finalization"); err != nil {
		return outcome, err
	}
	cancelRequested, err := r.cancelRequested(ctx, attempt.TaskID)
	if err != nil {
		return outcome, err
	}
	if cancelRequested {
		if err := r.service.FinalizeCancellation(ctx, attempt.TaskID); err != nil {
			return outcome, err
		}
		return outcome, nil
	}
	if verified.Verdict != domain.ResultVerified {
		return outcome, nil
	}
	_, integrated, err := r.integration.Integrate(ctx, attempt.TaskID)
	if err != nil {
		return outcome, err
	}
	outcome.Integration = &integrated
	return outcome, nil
}

func (r *TaskRunner) finishNonCandidate(ctx context.Context, attempt domain.ExecutionAttempt, proof processctl.TerminationProof, target domain.TaskState, terminalStatus string) error {
	if !r.proofValid(proof) {
		return r.recoveryBlock(ctx, attempt, errors.New("worker terminal result lacks valid physical termination proof"))
	}
	if err := r.service.TransitionForAttempt(ctx, attempt.TaskID, attempt.ID, attempt.RunEpoch, target); err != nil {
		return err
	}
	return r.service.ConfirmAttemptProcessTermination(ctx, proof, terminalStatus)
}

func (r *TaskRunner) failWorker(ctx context.Context, attempt domain.ExecutionAttempt, proof processctl.TerminationProof, cause error) error {
	if r.proofValid(proof) {
		blockErr := r.blockWhileAuthoritative(ctx, attempt)
		confirmErr := r.service.ConfirmAttemptProcessTermination(ctx, proof, "worker-process-error")
		return errors.Join(cause, blockErr, confirmErr)
	}
	return errors.Join(cause, r.recoveryBlock(ctx, attempt, errors.New("physical worker termination could not be proven")))
}

func (r *TaskRunner) recoveryBlock(ctx context.Context, attempt domain.ExecutionAttempt, cause error) error {
	blockErr := r.blockWhileAuthoritative(ctx, attempt)
	fenceErr := r.service.LogicalFenceAttempt(ctx, attempt.TaskID, attempt.ID, attempt.RunEpoch)
	return errors.Join(ErrRecoveryRequired, cause, blockErr, fenceErr)
}

func (r *TaskRunner) blockWhileAuthoritative(ctx context.Context, attempt domain.ExecutionAttempt) error {
	err := r.service.TransitionForAttempt(ctx, attempt.TaskID, attempt.ID, attempt.RunEpoch, domain.TaskBlocked)
	if err == nil {
		return nil
	}
	// Cancellation or another authority change may have fenced the attempt
	// concurrently. In that case preserving the existing durable state is safer
	// than attempting an orchestrator-only transition.
	return err
}

func (r *TaskRunner) finalizeCancellation(ctx context.Context, attempt domain.ExecutionAttempt, proof processctl.TerminationProof) error {
	if !r.proofValid(proof) {
		return errors.Join(ErrRecoveryRequired, errors.New("cancellation cannot finalize without physical termination proof"))
	}
	if err := r.service.ConfirmAttemptProcessTermination(ctx, proof, "cancelled"); err != nil {
		return err
	}
	return r.service.FinalizeCancellation(ctx, attempt.TaskID)
}

func (r *TaskRunner) cancelRequested(ctx context.Context, taskID string) (bool, error) {
	snapshot, err := r.service.StatusSnapshot(ctx, taskID)
	if err != nil {
		return false, err
	}
	return snapshot.CancelRequested, nil
}
