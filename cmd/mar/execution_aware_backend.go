package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"mar/internal/domain"
	"mar/internal/mcpedge"
	"mar/internal/model"
)

var errExecutionRuntimeUnavailable = errors.New("execution runtime is unavailable")

type executionAwareBackend struct {
	mcpedge.Backend
	readiness func(context.Context) (bool, string)
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
