//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"mar/internal/aci"
	"mar/internal/agent"
	"mar/internal/domain"
	"mar/internal/processctl"
	"mar/internal/service"
	"mar/internal/verification"
	"mar/internal/worker"
)

type fakeTaskService struct {
	task    domain.Task
	attempt domain.ExecutionAttempt
	cancel  bool
	events  []string
}

func (s *fakeTaskService) Status(context.Context, string) (domain.Task, error) { return s.task, nil }

func (s *fakeTaskService) StatusSnapshot(context.Context, string) (service.TaskStatusSnapshot, error) {
	return service.TaskStatusSnapshot{Task: s.task, CancelRequested: s.cancel}, nil
}

func (s *fakeTaskService) BeginAttempt(_ context.Context, taskID, workerID, supervisorID string, _ time.Duration) (domain.ExecutionAttempt, error) {
	s.events = append(s.events, "begin")
	s.task.State = domain.TaskRunning
	s.task.RunEpoch++
	s.attempt = domain.ExecutionAttempt{
		ID:             "attempt-1",
		TaskID:         taskID,
		RunEpoch:       s.task.RunEpoch,
		WorkerID:       workerID,
		SupervisorID:   supervisorID,
		AuthorityState: domain.AttemptActive,
	}
	return s.attempt, nil
}

func (s *fakeTaskService) TransitionForAttempt(_ context.Context, _ string, _ string, _ int64, to domain.TaskState) error {
	s.events = append(s.events, "transition:"+string(to))
	if s.attempt.AuthorityState != domain.AttemptActive {
		return errors.New("attempt is not active")
	}
	s.task.State = to
	return nil
}

func (s *fakeTaskService) LogicalFenceAttempt(context.Context, string, string, int64) error {
	s.events = append(s.events, "fence")
	s.attempt.AuthorityState = domain.AttemptLogicallyFenced
	return nil
}

func (s *fakeTaskService) ConfirmAttemptProcessTermination(context.Context, processctl.TerminationProof, string) error {
	s.events = append(s.events, "confirm")
	s.attempt.AuthorityState = domain.AttemptPhysicallyTerminated
	return nil
}

func (s *fakeTaskService) FinalizeCancellation(context.Context, string) error {
	s.events = append(s.events, "finalize_cancel")
	if s.attempt.AuthorityState != domain.AttemptPhysicallyTerminated {
		return errors.New("cancellation finalized before physical termination")
	}
	s.task.State = domain.TaskCancelled
	return nil
}

type fakeWorkerProcess struct {
	service *fakeTaskService
	result  agent.Result
	err     error
	onRun   func()
}

func (w *fakeWorkerProcess) Run(context.Context, worker.StartRequest) (agent.Result, processctl.TerminationProof, error) {
	w.service.events = append(w.service.events, "worker")
	if w.onRun != nil {
		w.onRun()
	}
	return w.result, processctl.TerminationProof{}, w.err
}

type fakeVerifier struct {
	service *fakeTaskService
	result  domain.TaskResult
	err     error
}

func (v *fakeVerifier) Verify(context.Context, verification.VerifyRequest) (domain.TaskResult, error) {
	v.service.events = append(v.service.events, "verify")
	if v.err == nil {
		v.service.task.State = domain.TaskVerified
	}
	return v.result, v.err
}

type fakeIntegrator struct {
	service *fakeTaskService
	called  bool
}

func (i *fakeIntegrator) Integrate(context.Context, string) (domain.IntegrationAttempt, domain.TaskResult, error) {
	i.service.events = append(i.service.events, "integrate")
	if i.service.attempt.AuthorityState != domain.AttemptPhysicallyTerminated {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, errors.New("integration began before physical termination was durably recorded")
	}
	i.called = true
	i.service.task.State = domain.TaskComplete
	return domain.IntegrationAttempt{}, domain.TaskResult{Verdict: domain.ResultVerified, IntegrationStatus: "APPLIED"}, nil
}

type fakeVerificationRuntime struct{ root string }

func (r fakeVerificationRuntime) Root() string { return r.root }
func (fakeVerificationRuntime) RunCommand(context.Context, aci.Command) (aci.ExecResult, error) {
	return aci.ExecResult{}, nil
}
func (fakeVerificationRuntime) SelfHostingSafe() bool { return true }

func testRunner(t *testing.T, svc *fakeTaskService, workerProcess *fakeWorkerProcess, verifier *fakeVerifier, integrator *fakeIntegrator) *TaskRunner {
	t.Helper()
	runner, err := NewTaskRunner(svc, workerProcess, verifier, integrator, func(path, _ string) (verification.CommandRuntime, error) {
		return fakeVerificationRuntime{root: path}, nil
	}, TaskRunnerConfig{
		WorkerID:      "worker-runtime",
		SupervisorID:  "supervisor-runtime",
		LeaseDuration: time.Minute,
		Provider:      worker.ProviderConfig{BaseURL: "https://provider.invalid", APIKeyEnv: "MAR_TEST_KEY"},
		AgentProfile:  agent.Profile{Model: "test-model", BaseInstructions: "bounded coding worker"},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner.proofValid = func(processctl.TerminationProof) bool { return true }
	return runner
}

func readyTaskAndWorkspace() (domain.Task, domain.Workspace) {
	task := domain.Task{ID: "task-1", State: domain.TaskWorkspaceReady, RunEpoch: 0}
	workspace := domain.Workspace{ID: "workspace-1", TaskID: task.ID, State: domain.WorkspaceReady, Path: `D:\MAR\test-workspace`}
	return task, workspace
}

func TestCompletedCandidateVerifiesBeforeRecordingTerminationAndIntegratesAfter(t *testing.T) {
	task, workspace := readyTaskAndWorkspace()
	svc := &fakeTaskService{task: task}
	workerProcess := &fakeWorkerProcess{service: svc, result: agent.Result{Status: agent.StatusCompletedCandidate}}
	verifier := &fakeVerifier{service: svc, result: domain.TaskResult{Verdict: domain.ResultVerified}}
	integrator := &fakeIntegrator{service: svc}
	runner := testRunner(t, svc, workerProcess, verifier, integrator)

	outcome, err := runner.RunWorkspaceReady(context.Background(), task.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Verification == nil || outcome.Integration == nil || !integrator.called {
		t.Fatalf("expected verification and integration outcome: %+v", outcome)
	}
	want := []string{"begin", "worker", "verify", "confirm", "integrate"}
	if !reflect.DeepEqual(svc.events, want) {
		t.Fatalf("unexpected authoritative ordering: got=%v want=%v", svc.events, want)
	}
}

func TestCancellationFinalizesOnlyAfterPhysicalTermination(t *testing.T) {
	task, workspace := readyTaskAndWorkspace()
	svc := &fakeTaskService{task: task}
	workerProcess := &fakeWorkerProcess{service: svc, result: agent.Result{Status: agent.StatusCancelled}}
	workerProcess.onRun = func() {
		svc.cancel = true
		svc.attempt.AuthorityState = domain.AttemptLogicallyFenced
	}
	verifier := &fakeVerifier{service: svc}
	integrator := &fakeIntegrator{service: svc}
	runner := testRunner(t, svc, workerProcess, verifier, integrator)

	if _, err := runner.RunWorkspaceReady(context.Background(), task.ID, workspace); err != nil {
		t.Fatal(err)
	}
	want := []string{"begin", "worker", "confirm", "finalize_cancel"}
	if !reflect.DeepEqual(svc.events, want) {
		t.Fatalf("unexpected cancellation ordering: got=%v want=%v", svc.events, want)
	}
	if svc.task.State != domain.TaskCancelled || integrator.called {
		t.Fatalf("cancellation outcome mismatch: state=%s integrated=%v", svc.task.State, integrator.called)
	}
}

func TestMissingPhysicalProofBlocksAndLogicallyFences(t *testing.T) {
	task, workspace := readyTaskAndWorkspace()
	svc := &fakeTaskService{task: task}
	workerProcess := &fakeWorkerProcess{service: svc, result: agent.Result{Status: agent.StatusCompletedCandidate}}
	verifier := &fakeVerifier{service: svc}
	integrator := &fakeIntegrator{service: svc}
	runner := testRunner(t, svc, workerProcess, verifier, integrator)
	runner.proofValid = func(processctl.TerminationProof) bool { return false }

	_, err := runner.RunWorkspaceReady(context.Background(), task.ID, workspace)
	if !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("expected recovery-required error, got %v", err)
	}
	want := []string{"begin", "worker", "transition:BLOCKED", "fence"}
	if !reflect.DeepEqual(svc.events, want) {
		t.Fatalf("unexpected recovery ordering: got=%v want=%v", svc.events, want)
	}
	if integrator.called {
		t.Fatal("integration must not run without physical proof")
	}
}

func TestBudgetExhaustionMovesToRetryWaitBeforeTerminationRecord(t *testing.T) {
	task, workspace := readyTaskAndWorkspace()
	svc := &fakeTaskService{task: task}
	workerProcess := &fakeWorkerProcess{service: svc, result: agent.Result{Status: agent.StatusBudgetExhausted}}
	verifier := &fakeVerifier{service: svc}
	integrator := &fakeIntegrator{service: svc}
	runner := testRunner(t, svc, workerProcess, verifier, integrator)

	if _, err := runner.RunWorkspaceReady(context.Background(), task.ID, workspace); err != nil {
		t.Fatal(err)
	}
	want := []string{"begin", "worker", "transition:RETRY_WAIT", "confirm"}
	if !reflect.DeepEqual(svc.events, want) {
		t.Fatalf("unexpected budget ordering: got=%v want=%v", svc.events, want)
	}
}

func TestVerificationInfrastructureErrorBlocksBeforeTerminationRecord(t *testing.T) {
	task, workspace := readyTaskAndWorkspace()
	svc := &fakeTaskService{task: task}
	workerProcess := &fakeWorkerProcess{service: svc, result: agent.Result{Status: agent.StatusCompletedCandidate}}
	verifier := &fakeVerifier{service: svc, err: errors.New("verification infrastructure failed")}
	integrator := &fakeIntegrator{service: svc}
	runner := testRunner(t, svc, workerProcess, verifier, integrator)

	_, err := runner.RunWorkspaceReady(context.Background(), task.ID, workspace)
	if err == nil {
		t.Fatal("expected verification infrastructure error")
	}
	want := []string{"begin", "worker", "verify", "transition:BLOCKED", "confirm"}
	if !reflect.DeepEqual(svc.events, want) {
		t.Fatalf("unexpected verification failure ordering: got=%v want=%v", svc.events, want)
	}
	if integrator.called {
		t.Fatal("integration must not run after verification error")
	}
}
