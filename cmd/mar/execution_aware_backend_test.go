package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"mar/internal/domain"
	"mar/internal/mcpedge"
	"mar/internal/model"
	"mar/internal/service"
)

type executionAwareDeltaFixture struct {
	mcpedge.Backend
	called          bool
	automaticCalled bool
	cursor          string
}

func (b *executionAwareDeltaFixture) CognitionDelta(_ context.Context, turn domain.WebTurn, cursor string) (service.CognitionDelta, error) {
	b.called = true
	b.cursor = cursor
	return service.CognitionDelta{
		Version: service.CognitionDeltaVersion,
		Mode:    "full",
		Cursor:  "next-cursor",
	}, nil
}

func (b *executionAwareDeltaFixture) AutomaticCognitionDelta(_ context.Context, _ domain.WebTurn) (service.CognitionDelta, error) {
	b.automaticCalled = true
	return service.CognitionDelta{Version: service.CognitionDeltaVersion, Mode: "delta", Cursor: "automatic-cursor"}, nil
}

type executionAwareNoDeltaFixture struct {
	mcpedge.Backend
}

type executionAwareRunFixture struct {
	mcpedge.Backend
	err error
}

func (f *executionAwareRunFixture) RunProjectCommand(context.Context, string, string, []string, string, int, int) (service.ProjectCommandResult, error) {
	return service.ProjectCommandResult{ExitCode: 0}, f.err
}

func (f *executionAwareRunFixture) RunProjectCommands(context.Context, string, []service.ProjectVerifyCommand) (service.ProjectCommandBatchResult, error) {
	return service.ProjectCommandBatchResult{Passed: true}, f.err
}

func TestExecutionAwareBackendPreservesBrainTurnCognitionDelta(t *testing.T) {
	underlying := &executionAwareDeltaFixture{}
	backend := executionAwareBackend{Backend: underlying}

	turn := domain.WebTurn{ID: "turn-1", TaskID: "task-1"}
	got, err := backend.CognitionDelta(context.Background(), turn, "prior-cursor")
	if err != nil {
		t.Fatal(err)
	}
	if !underlying.called || underlying.cursor != "prior-cursor" {
		t.Fatalf("brain_turn cognition delta was not forwarded: called=%v cursor=%q", underlying.called, underlying.cursor)
	}
	if got.Mode != "full" || got.Cursor != "next-cursor" {
		t.Fatalf("unexpected forwarded cognition delta: %+v", got)
	}
}

func TestExecutionAwareBackendPreservesAutomaticCognitionDelta(t *testing.T) {
	underlying := &executionAwareDeltaFixture{}
	backend := executionAwareBackend{Backend: underlying}
	got, err := backend.AutomaticCognitionDelta(context.Background(), domain.WebTurn{ID: "turn-auto", TaskID: "task-auto"})
	if err != nil {
		t.Fatal(err)
	}
	if !underlying.automaticCalled || got.Mode != "delta" || got.Cursor != "automatic-cursor" {
		t.Fatalf("automatic cognition delta was not forwarded: called=%v got=%+v", underlying.automaticCalled, got)
	}
}

func TestExecutionAwareBackendCognitionDeltaFailsClosedWhenWrappedBackendDoesNotSupportIt(t *testing.T) {
	backend := executionAwareBackend{Backend: &executionAwareNoDeltaFixture{}}
	_, err := backend.CognitionDelta(context.Background(), domain.WebTurn{ID: "turn-1", TaskID: "task-1"}, "")
	if !errors.Is(err, errCognitionDeltaUnavailable) {
		t.Fatalf("missing fail-closed cognition delta error: %v", err)
	}
	if !strings.Contains(err.Error(), "cognition delta") {
		t.Fatalf("cognition delta error must remain explicit: %v", err)
	}
	_, err = backend.AutomaticCognitionDelta(context.Background(), domain.WebTurn{ID: "turn-1", TaskID: "task-1"})
	if !errors.Is(err, errCognitionDeltaUnavailable) {
		t.Fatalf("missing fail-closed automatic cognition delta error: %v", err)
	}
}

func TestExecutionAwareBackendKeepsRespondWebTurnReadinessGate(t *testing.T) {
	backend := executionAwareBackend{readiness: func(context.Context) (bool, string) {
		return false, "mcp-stdio child exited"
	}}
	if _, _, err := backend.RespondWebTurn(context.Background(), "task", "turn", model.Message{Role: model.RoleAssistant, Content: "resume"}, "stop"); !errors.Is(err, errExecutionRuntimeUnavailable) {
		t.Fatalf("RespondWebTurn must remain fail-closed while execution is unavailable: %v", err)
	}
}

func TestExecutionAwareBackendTracksFastPathProcessStartHealth(t *testing.T) {
	health := newFastPathExecutionHealth()
	fixture := &executionAwareRunFixture{err: fmt.Errorf("%w: access denied", service.ErrProjectCommandStart)}
	backend := executionAwareBackend{Backend: fixture, fastPathHealth: health}

	if _, err := backend.RunProjectCommand(context.Background(), "mar", "cmd.exe", []string{"/c", "echo", "x"}, ".", 5, 1024); err == nil || !strings.Contains(err.Error(), "FAST_PATH_EXECUTION_UNAVAILABLE") {
		t.Fatalf("process-start failure was not classified for the remote caller: %v", err)
	}
	snapshot := health.snapshot()
	if snapshot.State != fastPathExecutionDegraded || snapshot.LastFailureAt == nil || !strings.Contains(snapshot.LastError, "access denied") {
		t.Fatalf("fast path health did not record degradation: %+v", snapshot)
	}

	fixture.err = errors.New("project local_file_write policy is disabled")
	if _, err := backend.RunProjectCommand(context.Background(), "mar", "cmd.exe", nil, ".", 5, 1024); err == nil {
		t.Fatal("expected policy error")
	}
	if health.snapshot().State != fastPathExecutionDegraded {
		t.Fatalf("ordinary policy error must not rewrite execution health: %+v", health.snapshot())
	}

	fixture.err = nil
	if _, err := backend.RunProjectCommands(context.Background(), "mar", []service.ProjectVerifyCommand{{Executable: "cmd.exe"}}); err != nil {
		t.Fatal(err)
	}
	snapshot = health.snapshot()
	if snapshot.State != fastPathExecutionHealthy || snapshot.LastSuccessAt == nil || snapshot.LastError != "" {
		t.Fatalf("successful process execution did not recover health: %+v", snapshot)
	}
}
