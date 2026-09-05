//go:build windows

package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mar/internal/aci"
	"mar/internal/contextengine"
	"mar/internal/domain"
	"mar/internal/model"
	"mar/internal/service"
	"mar/internal/store"
)

func TestLoopExecutesRealCodingACIWriteAndGitStatus(t *testing.T) {
	root := t.TempDir()
	runAgentGit(t, root, "init")

	executor, err := aci.NewWindowsSandboxExecutor(root)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := aci.NewContainedGitBroker()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := aci.New(aci.Config{
		Root:                  root,
		TaskID:                "task-agent-aci-integration",
		MaxCommandOutputBytes: 32 << 10,
		CommandTimeout:        15 * time.Second,
		GitBroker:             broker,
	}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.SelfHostingSafe() {
		t.Skip("Windows LPAC host prerequisite is not prepared; real autonomous ACI integration remains fail-closed")
	}

	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(40, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "aci-write", Name: "write_file", Arguments: `{"path":"agent.txt","expected_sha256":"ABSENT","content":"written-by-agent\n"}`}}}),
		assistantResponse(40, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "aci-status", Name: "git_status", Arguments: `{}`}}}),
		assistantResponse(40, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "aci-finish", Name: finishToolName, Arguments: `{"status":"completed_candidate","summary":"ACI mutation and status inspection complete"}`}}}),
	}}
	loop, err := New(gateway, runtime, fakeContextBuilder{pack: contextengine.Pack{Revision: "rev-aci", Bytes: 128}}, &fakeAttemptAuthority{}, &fakeCheckpointStore{}, Profile{
		Model:            "test-model",
		ReasoningEffort:  "high",
		BaseInstructions: "You are the MAR coding worker.",
	}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := loop.Run(context.Background(), RunRequest{
		TaskID:    "task-agent-aci-integration",
		AttemptID: "attempt-agent-aci-integration",
		RunEpoch:  1,
		Root:      root,
		Contract: domain.GoalContract{
			Goal:                "create agent.txt and inspect Git status",
			Acceptance:          []string{"agent.txt exists"},
			ProjectID:           "project-agent-aci",
			BaseRevision:        "rev-aci",
			Authority:           domain.Authority{LocalFileWrite: true},
			VerificationProfile: "test",
			Priority:            "P2",
		},
		ExpectedRevision: "rev-aci",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCompletedCandidate || result.ToolCalls != 3 {
		t.Fatalf("unexpected real ACI loop result: %+v", result)
	}
	content, err := os.ReadFile(filepath.Join(root, "agent.txt"))
	if err != nil || string(content) != "written-by-agent\n" {
		t.Fatalf("real ACI mutation missing: content=%q err=%v", content, err)
	}
	if len(gateway.requests) != 3 {
		t.Fatalf("unexpected model request count: %d", len(gateway.requests))
	}
	thirdMessages := gateway.requests[2].Messages
	if len(thirdMessages) == 0 {
		t.Fatal("missing Git status observation")
	}
	last := thirdMessages[len(thirdMessages)-1]
	if last.Role != model.RoleTool || last.ToolCallID != "aci-status" || !strings.Contains(last.Content, "agent.txt") {
		t.Fatalf("typed Git broker observation did not return to model: %+v", last)
	}
}

func TestLoopUsesDurableAttemptAuthorityBeforeRealACIMutation(t *testing.T) {
	root := t.TempDir()
	runAgentGit(t, root, "init")

	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.NewTaskService(db)
	ctx := context.Background()
	project, _, err := svc.RegisterProject(ctx, "project-agent-durable", root)
	if err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "create durable.txt",
		Acceptance:          []string{"durable.txt exists"},
		ProjectID:           project.ID,
		BaseRevision:        "rev-durable",
		Authority:           domain.Authority{LocalFileWrite: true},
		VerificationProfile: "test",
		Priority:            "P2",
	}
	task, _, err := svc.Submit(ctx, "agent-durable-fence", contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	attempt, err := svc.BeginAttempt(ctx, task.ID, "worker-agent", "supervisor-agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	executor, err := aci.NewWindowsSandboxExecutor(root)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := aci.NewContainedGitBroker()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := aci.New(aci.Config{Root: root, TaskID: task.ID, GitBroker: broker, CommandTimeout: 15 * time.Second}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.SelfHostingSafe() {
		t.Skip("Windows LPAC host prerequisite is not prepared; durable fencing integration remains fail-closed")
	}

	gateway := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(30, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "durable-write", Name: "write_file", Arguments: `{"path":"durable.txt","expected_sha256":"ABSENT","content":"durable\n"}`}}}),
		assistantResponse(30, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "durable-finish", Name: finishToolName, Arguments: `{"status":"completed_candidate","summary":"durable mutation ready"}`}}}),
	}}
	loop, err := New(gateway, runtime, fakeContextBuilder{pack: contextengine.Pack{Revision: contract.BaseRevision}}, svc, svc, Profile{Model: "test-model", BaseInstructions: "You are the MAR coding worker."}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	runReq := RunRequest{TaskID: task.ID, AttemptID: attempt.ID, RunEpoch: attempt.RunEpoch, Root: root, Contract: contract, ExpectedRevision: contract.BaseRevision}
	result, err := loop.Run(ctx, runReq)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCompletedCandidate {
		t.Fatalf("active durable attempt failed to run: %+v", result)
	}
	durableTask, err := svc.Status(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if durableTask.State != domain.TaskRunning {
		t.Fatalf("completed_candidate bypassed authoritative verification: state=%s", durableTask.State)
	}
	if got, err := os.ReadFile(filepath.Join(root, "durable.txt")); err != nil || string(got) != "durable\n" {
		t.Fatalf("active durable attempt did not mutate through ACI: content=%q err=%v", got, err)
	}

	if err := svc.LogicalFenceAttempt(ctx, task.ID, attempt.ID, attempt.RunEpoch); err != nil {
		t.Fatal(err)
	}
	secondGateway := &scriptedGateway{responses: []model.TurnResponse{assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "must-not-run", Name: "write_file", Arguments: `{}`}}})}}
	secondLoop, err := New(secondGateway, runtime, fakeContextBuilder{pack: contextengine.Pack{Revision: contract.BaseRevision}}, svc, svc, Profile{Model: "test-model", BaseInstructions: "You are the MAR coding worker."}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := secondLoop.Run(ctx, runReq)
	if err != nil {
		t.Fatal(err)
	}
	if secondResult.Status != StatusCancelled || len(secondGateway.requests) != 0 {
		t.Fatalf("logically fenced durable attempt reached model/tools: result=%+v requests=%d", secondResult, len(secondGateway.requests))
	}
}

func TestLoopPersistsAndResumesSemanticCheckpointAcrossReplacementAttempt(t *testing.T) {
	root := t.TempDir()
	runAgentGit(t, root, "init")

	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.NewTaskService(db)
	ctx := context.Background()
	project, _, err := svc.RegisterProject(ctx, "project-agent-resume", root)
	if err != nil {
		t.Fatal(err)
	}
	contract := domain.GoalContract{
		Goal:                "resume semantic coding state",
		Acceptance:          []string{"replacement attempt receives prior semantic checkpoint"},
		ProjectID:           project.ID,
		BaseRevision:        "rev-resume",
		Authority:           domain.Authority{LocalFileWrite: true},
		VerificationProfile: "test",
		Priority:            "P2",
	}
	task, _, err := svc.Submit(ctx, "agent-resume-checkpoint", contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.TaskState{domain.TaskPreflight, domain.TaskWaitingResource, domain.TaskWorkspaceReady} {
		if err := svc.AdvancePreExecution(ctx, task.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	attemptA, err := svc.BeginAttempt(ctx, task.ID, "worker-a", "supervisor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	executor, err := aci.NewWindowsSandboxExecutor(root)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := aci.NewContainedGitBroker()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := aci.New(aci.Config{Root: root, TaskID: task.ID, GitBroker: broker, CommandTimeout: 15 * time.Second}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.SelfHostingSafe() {
		t.Skip("Windows LPAC host prerequisite is not prepared; semantic resume integration remains fail-closed")
	}

	gatewayA := &scriptedGateway{responses: []model.TurnResponse{
		assistantResponse(25, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "checkpoint-a", Name: checkpointToolName, Arguments: `{"completed_work":["inspected failing serializer"],"current_hypothesis":"serializer drops metadata","changed_areas":["serializer.go"],"verification_status":"failure reproduced","blockers":[],"remaining_work":["patch serializer","run tests"],"next_action":"patch serializer metadata copy","critical_evidence_refs":["serializer_test failure"]}`}}}),
		assistantResponse(25, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "finish-a", Name: finishToolName, Arguments: `{"status":"blocked","summary":"simulate worker replacement after checkpoint","blocker":"test replacement"}`}}}),
	}}
	loopA, err := New(gatewayA, runtime, fakeContextBuilder{pack: contextengine.Pack{Revision: contract.BaseRevision}}, svc, svc, Profile{Model: "test-model", BaseInstructions: "You are the MAR coding worker."}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	requestA := RunRequest{TaskID: task.ID, AttemptID: attemptA.ID, RunEpoch: attemptA.RunEpoch, Root: root, Contract: contract, ExpectedRevision: contract.BaseRevision}
	resultA, err := loopA.Run(ctx, requestA)
	if err != nil {
		t.Fatal(err)
	}
	if resultA.Status != StatusBlocked {
		t.Fatalf("attempt A did not checkpoint before simulated replacement: %+v", resultA)
	}
	checkpoint, ok, err := svc.LatestValidCheckpoint(ctx, task.ID)
	if err != nil || !ok || checkpoint.Payload.NextAction != "patch serializer metadata copy" {
		t.Fatalf("durable checkpoint missing before replacement: ok=%v checkpoint=%+v err=%v", ok, checkpoint, err)
	}

	if err := svc.LogicalFenceAttempt(ctx, task.ID, attemptA.ID, attemptA.RunEpoch); err != nil {
		t.Fatal(err)
	}
	if err := db.ConfirmAttemptTerminated(ctx, task.ID, attemptA.ID, attemptA.RunEpoch, "test-confirmed-terminated", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecoverForReplacement(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	attemptB, err := svc.BeginAttempt(ctx, task.ID, "worker-b", "supervisor-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	gatewayB := &scriptedGateway{responses: []model.TurnResponse{assistantResponse(20, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "finish-b", Name: finishToolName, Arguments: `{"status":"completed_candidate","summary":"resume seed received"}`}}})}}
	loopB, err := New(gatewayB, runtime, fakeContextBuilder{pack: contextengine.Pack{Revision: contract.BaseRevision}}, svc, svc, Profile{Model: "test-model", BaseInstructions: "You are the MAR coding worker."}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	requestB := RunRequest{TaskID: task.ID, AttemptID: attemptB.ID, RunEpoch: attemptB.RunEpoch, Root: root, Contract: contract, ExpectedRevision: contract.BaseRevision}
	resultB, err := loopB.Run(ctx, requestB)
	if err != nil {
		t.Fatal(err)
	}
	if resultB.Status != StatusCompletedCandidate || resultB.ResumeCheckpointID != checkpoint.ID || resultB.ResumeCheckpointVersion != checkpoint.Version {
		t.Fatalf("replacement attempt did not bind prior checkpoint: %+v", resultB)
	}
	if len(gatewayB.requests) != 1 || len(gatewayB.requests[0].Messages) != 2 {
		t.Fatalf("replacement replayed transcript instead of bounded resume seed: %+v", gatewayB.requests)
	}
	initial := gatewayB.requests[0].Messages[1].Content
	if !strings.Contains(initial, "patch serializer metadata copy") || !strings.Contains(initial, "UNTRUSTED_REPOSITORY_CONTEXT_JSON") {
		t.Fatalf("replacement prompt missing semantic checkpoint + fresh context: %s", initial)
	}
}

func runAgentGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
