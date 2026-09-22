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
	"mar/internal/model"
	"mar/internal/processctl"
	"mar/internal/service"
	"mar/internal/verification"
	"mar/internal/worker"
)

type fakeTaskService struct {
	task                   domain.Task
	attempt                domain.ExecutionAttempt
	cancel                 bool
	events                 []string
	terminalStatus         string
	rejectCancelledContext bool
	onConfirm              func()
}

func (s *fakeTaskService) Status(context.Context, string) (domain.Task, error) { return s.task, nil }

func (s *fakeTaskService) StatusSnapshot(ctx context.Context, _ string) (service.TaskStatusSnapshot, error) {
	if s.rejectCancelledContext && ctx.Err() != nil {
		return service.TaskStatusSnapshot{}, errors.New("cancelled context reached durable finalization")
	}
	return service.TaskStatusSnapshot{Task: s.task, CancelRequested: s.cancel}, nil
}

func (s *fakeTaskService) TaskConvergenceBudget(context.Context, string) (service.TaskConvergenceBudget, error) {
	return service.TaskConvergenceBudget{
		RemainingModelDecisions:  service.DefaultTaskMaxModelDecisions,
		RemainingWorkerToolCalls: service.DefaultTaskMaxWorkerToolCalls,
		RemainingModelTokens:     service.DefaultTaskMaxModelTotalTokens,
		RemainingActiveExecution: service.DefaultTaskMaxActiveExecution,
		RemainingAttempts:        service.DefaultTaskMaxAttempts,
	}, nil
}

func (s *fakeTaskService) BlockForConvergenceBudget(context.Context, string) error {
	s.events = append(s.events, "budget_block")
	s.task.State = domain.TaskBlocked
	return nil
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

func (s *fakeTaskService) HeartbeatAttempt(_ context.Context, _ string, _ string, _ int64, lease time.Duration) error {
	s.events = append(s.events, "heartbeat")
	now := time.Now().UTC()
	s.attempt.HeartbeatAt = now
	s.attempt.LeaseDeadline = now.Add(lease)
	return nil
}

func (s *fakeTaskService) TransitionForAttempt(ctx context.Context, _ string, _ string, _ int64, to domain.TaskState) error {
	if s.rejectCancelledContext && ctx.Err() != nil {
		return errors.New("cancelled context reached durable transition")
	}
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

func (s *fakeTaskService) ConfirmAttemptProcessTermination(ctx context.Context, _ processctl.TerminationProof, terminalStatus string) error {
	if s.rejectCancelledContext && ctx.Err() != nil {
		return errors.New("cancelled context reached physical termination finalization")
	}
	s.events = append(s.events, "confirm")
	s.attempt.AuthorityState = domain.AttemptPhysicallyTerminated
	s.terminalStatus = terminalStatus
	if s.onConfirm != nil {
		s.onConfirm()
	}
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
	request verification.VerifyRequest
}

func (v *fakeVerifier) Verify(_ context.Context, request verification.VerifyRequest) (domain.TaskResult, error) {
	v.request = request
	v.service.events = append(v.service.events, "verify")
	if v.err == nil {
		v.service.task.State = domain.TaskVerified
	}
	return v.result, v.err
}

type fakeIntegrator struct {
	service                *fakeTaskService
	called                 bool
	rejectCancelledContext bool
}

func (i *fakeIntegrator) Integrate(ctx context.Context, _ string) (domain.IntegrationAttempt, domain.TaskResult, error) {
	if i.rejectCancelledContext && ctx.Err() != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, errors.New("cancelled parent context reached integration")
	}
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
		WorkerID:            "worker-runtime",
		SupervisorID:        "supervisor-runtime",
		LeaseDuration:       time.Minute,
		FinalizationTimeout: 5 * time.Second,
		Provider:            worker.ProviderConfig{BaseURL: "https://provider.invalid", APIKeyEnv: "MAR_TEST_KEY"},
		AgentProfile:        agent.Profile{Model: "test-model", BaseInstructions: "bounded coding worker"},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner.proofValid = func(processctl.TerminationProof) bool { return true }
	return runner
}

func TestWorkerProviderConfigStripsProviderOnlyFieldsOutsideProviderMode(t *testing.T) {
	provider := worker.ProviderConfig{
		BrainMode: worker.BrainProvider, BaseURL: "https://provider.invalid/v1",
		APIKeyEnv: "MAR_PROVIDER_SECRET", RequestTimeout: 17 * time.Second,
	}
	if got := workerProviderConfig(provider); !reflect.DeepEqual(got, provider) {
		t.Fatalf("provider mode lost compatibility config: got=%+v want=%+v", got, provider)
	}

	for _, mode := range []worker.BrainMode{worker.BrainWeb, worker.BrainHarness} {
		input := provider
		input.BrainMode = mode
		got := workerProviderConfig(input)
		if got.BrainMode != mode || got.BaseURL != "" || got.APIKeyEnv != "" || got.RequestTimeout != 0 {
			t.Fatalf("%s worker retained provider-only configuration: %+v", mode, got)
		}
	}
}

func TestWorkerCognitionConfigIsModeScoped(t *testing.T) {
	profile := agent.Profile{Model: "provider-model", ReasoningEffort: "high", BaseInstructions: "bounded instructions"}
	config := agent.Config{MaxTurns: 7, MaxToolCalls: 11, MaxTotalTokens: 9000, MaxDuration: 2 * time.Minute}

	if got := workerAgentProfile(worker.BrainProvider, profile); !reflect.DeepEqual(got, profile) {
		t.Fatalf("provider mode lost agent profile compatibility: got=%+v want=%+v", got, profile)
	}
	if got := workerAgentConfig(worker.BrainProvider, config); !reflect.DeepEqual(got, config) {
		t.Fatalf("provider mode lost agent config compatibility: got=%+v want=%+v", got, config)
	}

	webProfile := workerAgentProfile(worker.BrainWeb, profile)
	if webProfile.Model != "" || webProfile.ReasoningEffort != "" || webProfile.BaseInstructions != profile.BaseInstructions {
		t.Fatalf("web worker retained provider-specific profile fields: %+v", webProfile)
	}
	if got := workerAgentConfig(worker.BrainWeb, config); !reflect.DeepEqual(got, config) {
		t.Fatalf("web worker lost execution-budget config: got=%+v want=%+v", got, config)
	}

	if got := workerAgentProfile(worker.BrainHarness, profile); got != (agent.Profile{}) {
		t.Fatalf("external harness retained MAR agent profile: %+v", got)
	}
	if got := workerAgentConfig(worker.BrainHarness, config); !reflect.DeepEqual(got, agent.Config{}) {
		t.Fatalf("external harness retained MAR agent config: %+v", got)
	}
}

func TestTaskRunnerConfigAllowsWebBrainWithoutProviderCredentials(t *testing.T) {
	cfg := TaskRunnerConfig{
		WorkerID: "worker-runtime", SupervisorID: "supervisor-runtime", LeaseDuration: time.Minute,
		FinalizationTimeout: 5 * time.Second,
		Provider:            worker.ProviderConfig{BrainMode: worker.BrainWeb},
		AgentProfile:        agent.Profile{BaseInstructions: "bounded coding worker"},
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("web brain config should not require provider credentials: %v", err)
	}
	cfg.Provider = worker.ProviderConfig{BrainMode: worker.BrainProvider}
	if err := cfg.validate(); err == nil {
		t.Fatal("provider brain config must still require provider URL/API key env")
	}
}

func TestTaskRunnerConfigAllowsExternalHarnessWithoutAgentProfile(t *testing.T) {
	cfg := TaskRunnerConfig{
		WorkerID: "worker-runtime", SupervisorID: "supervisor-runtime", LeaseDuration: time.Minute,
		FinalizationTimeout: 5 * time.Second,
		Provider:            worker.ProviderConfig{BrainMode: worker.BrainHarness},
		HarnessExecutable:   `C:\tools\harness.exe`,
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("external harness should not require MAR agent profile/provider credentials: %v", err)
	}
	cfg.HarnessExecutable = "relative-harness.exe"
	if err := cfg.validate(); err == nil {
		t.Fatal("external harness executable must be absolute")
	}
}

func readyTaskAndWorkspace() (domain.Task, domain.Workspace) {
	task := domain.Task{ID: "task-1", State: domain.TaskWorkspaceReady, RunEpoch: 0}
	workspace := domain.Workspace{ID: "workspace-1", TaskID: task.ID, State: domain.WorkspaceReady, Path: `D:\MAR\test-workspace`}
	return task, workspace
}

func TestCompletedCandidateVerifiesBeforeRecordingTerminationAndIntegratesAfter(t *testing.T) {
	task, workspace := readyTaskAndWorkspace()
	svc := &fakeTaskService{task: task}
	workerProcess := &fakeWorkerProcess{service: svc, result: agent.Result{Status: agent.StatusCompletedCandidate, Turns: 3, ToolCalls: 5, Usage: model.Usage{InputTokens: 80, OutputTokens: 64, TotalTokens: 144}}}
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
	if got := verifier.request.ResourceSummary; got.AgentTurns != 3 || got.AgentToolCalls != 5 || got.ModelInputTokens != 80 || got.ModelOutputTokens != 64 || got.ModelTotalTokens != 144 {
		t.Fatalf("agent resource summary was not bound into verification result: %+v", got)
	}
}

func TestCompletedCandidateIntegratesAfterParentContextCancellationDuringFinalization(t *testing.T) {
	task, workspace := readyTaskAndWorkspace()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := &fakeTaskService{task: task, onConfirm: cancel}
	workerProcess := &fakeWorkerProcess{service: svc, result: agent.Result{Status: agent.StatusCompletedCandidate}}
	verifier := &fakeVerifier{service: svc, result: domain.TaskResult{Verdict: domain.ResultVerified}}
	integrator := &fakeIntegrator{service: svc, rejectCancelledContext: true}
	runner := testRunner(t, svc, workerProcess, verifier, integrator)

	outcome, err := runner.RunWorkspaceReady(ctx, task.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Verification == nil || outcome.Integration == nil || !integrator.called {
		t.Fatalf("expected verified candidate to integrate after parent cancellation: %+v", outcome)
	}
	want := []string{"begin", "worker", "verify", "confirm", "integrate"}
	if !reflect.DeepEqual(svc.events, want) {
		t.Fatalf("unexpected finalization ordering: got=%v want=%v", svc.events, want)
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

func TestWorkerBlockedPersistsBoundedDiagnostic(t *testing.T) {
	task, workspace := readyTaskAndWorkspace()
	svc := &fakeTaskService{task: task}
	workerProcess := &fakeWorkerProcess{service: svc, result: agent.Result{
		Status:  agent.StatusBlocked,
		Summary: "worker stopped safely",
		Blocker: "model protocol error:\nfinish_task is required for terminal completion/blocking",
	}}
	verifier := &fakeVerifier{service: svc}
	integrator := &fakeIntegrator{service: svc}
	runner := testRunner(t, svc, workerProcess, verifier, integrator)

	if _, err := runner.RunWorkspaceReady(context.Background(), task.ID, workspace); err != nil {
		t.Fatal(err)
	}
	if svc.task.State != domain.TaskBlocked || svc.attempt.AuthorityState != domain.AttemptPhysicallyTerminated {
		t.Fatalf("blocked worker did not finalize safely: task=%s authority=%s", svc.task.State, svc.attempt.AuthorityState)
	}
	want := "worker-blocked: model protocol error: finish_task is required for terminal completion/blocking"
	if svc.terminalStatus != want {
		t.Fatalf("blocked worker diagnostic was not persisted: got=%q want=%q", svc.terminalStatus, want)
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

func TestCancelledExecutionContextStillDurablyBlocksAndConfirmsTermination(t *testing.T) {
	task, workspace := readyTaskAndWorkspace()
	svc := &fakeTaskService{task: task, rejectCancelledContext: true}
	ctx, cancel := context.WithCancel(context.Background())
	workerProcess := &fakeWorkerProcess{service: svc, result: agent.Result{Status: agent.StatusCancelled}}
	workerProcess.onRun = cancel
	verifier := &fakeVerifier{service: svc}
	integrator := &fakeIntegrator{service: svc}
	runner := testRunner(t, svc, workerProcess, verifier, integrator)

	if _, err := runner.RunWorkspaceReady(ctx, task.ID, workspace); err != nil {
		t.Fatal(err)
	}
	want := []string{"begin", "worker", "transition:BLOCKED", "confirm"}
	if !reflect.DeepEqual(svc.events, want) {
		t.Fatalf("cancelled parent context lost durable finalization: got=%v want=%v", svc.events, want)
	}
	if svc.task.State != domain.TaskBlocked || svc.attempt.AuthorityState != domain.AttemptPhysicallyTerminated || integrator.called {
		t.Fatalf("cancelled execution context released unsafe state: task=%s authority=%s integrated=%v", svc.task.State, svc.attempt.AuthorityState, integrator.called)
	}
}

func TestAcceptanceT8WorkerCrashDurablyBlocksAfterPhysicalProof(t *testing.T) {
	task, workspace := readyTaskAndWorkspace()
	svc := &fakeTaskService{task: task}
	workerProcess := &fakeWorkerProcess{service: svc, err: errors.New("worker exited unexpectedly")}
	verifier := &fakeVerifier{service: svc}
	integrator := &fakeIntegrator{service: svc}
	runner := testRunner(t, svc, workerProcess, verifier, integrator)

	_, err := runner.RunWorkspaceReady(context.Background(), task.ID, workspace)
	if err == nil {
		t.Fatal("worker crash must surface an execution error")
	}
	want := []string{"begin", "worker", "transition:BLOCKED", "confirm"}
	if !reflect.DeepEqual(svc.events, want) {
		t.Fatalf("worker crash did not preserve coherent durable ordering: got=%v want=%v", svc.events, want)
	}
	if svc.task.State != domain.TaskBlocked || svc.attempt.AuthorityState != domain.AttemptPhysicallyTerminated || integrator.called {
		t.Fatalf("worker crash produced unsafe state: task=%s authority=%s integrated=%v", svc.task.State, svc.attempt.AuthorityState, integrator.called)
	}
}

func TestVerificationTerminationUnconfirmedRequiresRecoveryWithoutPhysicalConfirmation(t *testing.T) {
	task, workspace := readyTaskAndWorkspace()
	svc := &fakeTaskService{task: task}
	workerProcess := &fakeWorkerProcess{service: svc, result: agent.Result{Status: agent.StatusCompletedCandidate}}
	verifier := &fakeVerifier{service: svc, err: processctl.ErrSandboxTerminationUnconfirmed}
	integrator := &fakeIntegrator{service: svc}
	runner := testRunner(t, svc, workerProcess, verifier, integrator)

	_, err := runner.RunWorkspaceReady(context.Background(), task.ID, workspace)
	if !errors.Is(err, ErrRecoveryRequired) || !errors.Is(err, processctl.ErrSandboxTerminationUnconfirmed) {
		t.Fatalf("unconfirmed verification tree must require physical recovery, got %v", err)
	}
	want := []string{"begin", "worker", "verify", "transition:BLOCKED", "fence"}
	if !reflect.DeepEqual(svc.events, want) {
		t.Fatalf("verification uncertainty was incorrectly converted into physical confirmation: got=%v want=%v", svc.events, want)
	}
	if svc.attempt.AuthorityState != domain.AttemptLogicallyFenced || integrator.called {
		t.Fatalf("verification uncertainty released unsafe authority: authority=%s integrated=%v", svc.attempt.AuthorityState, integrator.called)
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
