package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"mar/internal/domain"
	"mar/internal/mcpedge"
	"mar/internal/model"
	"mar/internal/service"
)

var (
	errExecutionRuntimeUnavailable = errors.New("execution runtime is unavailable")
	errCognitionDeltaUnavailable   = errors.New("cognition delta is unavailable for wrapped backend")
)

const (
	fastPathExecutionUnknown  = "UNKNOWN"
	fastPathExecutionHealthy  = "HEALTHY"
	fastPathExecutionDegraded = "DEGRADED"
)

type fastPathExecutionHealthSnapshot struct {
	State         string     `json:"state"`
	Detail        string     `json:"detail"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

type fastPathExecutionHealth struct {
	mu            sync.RWMutex
	state         string
	lastSuccessAt time.Time
	lastFailureAt time.Time
	lastError     string
}

func newFastPathExecutionHealth() *fastPathExecutionHealth {
	return &fastPathExecutionHealth{state: fastPathExecutionUnknown}
}

func (h *fastPathExecutionHealth) recordSuccess() {
	if h == nil {
		return
	}
	now := time.Now().UTC()
	h.mu.Lock()
	h.state = fastPathExecutionHealthy
	h.lastSuccessAt = now
	h.lastError = ""
	h.mu.Unlock()
}

func (h *fastPathExecutionHealth) recordFailure(err error) {
	if h == nil || err == nil {
		return
	}
	now := time.Now().UTC()
	h.mu.Lock()
	h.state = fastPathExecutionDegraded
	h.lastFailureAt = now
	h.lastError = err.Error()
	h.mu.Unlock()
}

func (h *fastPathExecutionHealth) snapshot() fastPathExecutionHealthSnapshot {
	if h == nil {
		return fastPathExecutionHealthSnapshot{State: fastPathExecutionUnknown, Detail: "No Fast Path execution telemetry is available."}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	state := h.state
	if state == "" {
		state = fastPathExecutionUnknown
	}
	result := fastPathExecutionHealthSnapshot{State: state, LastError: h.lastError}
	switch state {
	case fastPathExecutionHealthy:
		result.Detail = "Fast Path process execution succeeded on the latest observed action/run."
	case fastPathExecutionDegraded:
		result.Detail = "Fast Path could not start a resolved host process on the latest observed action/run."
	default:
		result.Detail = "No action/run process-start outcome has been observed in this runtime yet."
	}
	if !h.lastSuccessAt.IsZero() {
		value := h.lastSuccessAt
		result.LastSuccessAt = &value
	}
	if !h.lastFailureAt.IsZero() {
		value := h.lastFailureAt
		result.LastFailureAt = &value
	}
	return result
}

type cognitionDeltaProvider interface {
	CognitionDelta(context.Context, domain.WebTurn, string) (service.CognitionDelta, error)
}

type automaticCognitionDeltaProvider interface {
	AutomaticCognitionDelta(context.Context, domain.WebTurn) (service.CognitionDelta, error)
}

type executionAwareBackend struct {
	mcpedge.Backend
	readiness      func(context.Context) (bool, string)
	fastPathHealth *fastPathExecutionHealth
}

func (b executionAwareBackend) requireExecution(ctx context.Context) error {
	if b.readiness == nil {
		return errExecutionRuntimeUnavailable
	}
	ready, detail := b.readiness(ctx)
	if ready {
		return nil
	}
	if detail = strings.TrimSpace(detail); detail != "" {
		return fmt.Errorf("%w: %s", errExecutionRuntimeUnavailable, detail)
	}
	return errExecutionRuntimeUnavailable
}

func (b executionAwareBackend) Submit(ctx context.Context, key string, contract domain.GoalContract) (domain.Task, bool, error) {
	if err := b.requireExecution(ctx); err != nil {
		return domain.Task{}, false, err
	}
	return b.Backend.Submit(ctx, key, contract)
}

func (b executionAwareBackend) RespondWebTurn(ctx context.Context, taskID, turnID string, message model.Message, finishReason string) (domain.WebTurn, bool, error) {
	if err := b.requireExecution(ctx); err != nil {
		return domain.WebTurn{}, false, err
	}
	return b.Backend.RespondWebTurn(ctx, taskID, turnID, message, finishReason)
}

func (b executionAwareBackend) observeFastPathExecution(err error) error {
	if err == nil {
		b.fastPathHealth.recordSuccess()
		return nil
	}
	if errors.Is(err, service.ErrProjectCommandStart) {
		b.fastPathHealth.recordFailure(err)
		return fmt.Errorf("FAST_PATH_EXECUTION_UNAVAILABLE: %w", err)
	}
	return err
}

func (b executionAwareBackend) RunProjectCommand(ctx context.Context, projectID, executable string, args []string, cwd string, timeoutSeconds, maxOutputBytes int) (service.ProjectCommandResult, error) {
	result, err := b.Backend.RunProjectCommand(ctx, projectID, executable, args, cwd, timeoutSeconds, maxOutputBytes)
	if classified := b.observeFastPathExecution(err); classified != nil {
		return service.ProjectCommandResult{}, classified
	}
	return result, nil
}

func (b executionAwareBackend) RunProjectCommands(ctx context.Context, projectID string, commands []service.ProjectVerifyCommand) (service.ProjectCommandBatchResult, error) {
	result, err := b.Backend.RunProjectCommands(ctx, projectID, commands)
	if classified := b.observeFastPathExecution(err); classified != nil {
		return service.ProjectCommandBatchResult{}, classified
	}
	return result, nil
}

func (b executionAwareBackend) CognitionDelta(ctx context.Context, current domain.WebTurn, cursor string) (service.CognitionDelta, error) {
	provider, ok := b.Backend.(cognitionDeltaProvider)
	if !ok || provider == nil {
		return service.CognitionDelta{}, errCognitionDeltaUnavailable
	}
	return provider.CognitionDelta(ctx, current, cursor)
}

func (b executionAwareBackend) AutomaticCognitionDelta(ctx context.Context, current domain.WebTurn) (service.CognitionDelta, error) {
	provider, ok := b.Backend.(automaticCognitionDeltaProvider)
	if !ok || provider == nil {
		return service.CognitionDelta{}, errCognitionDeltaUnavailable
	}
	return provider.AutomaticCognitionDelta(ctx, current)
}
