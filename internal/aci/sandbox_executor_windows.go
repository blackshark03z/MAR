//go:build windows

package aci

import (
	"context"
	"errors"
	"path/filepath"

	"mar/internal/processctl"
)

// WindowsSandboxExecutor executes model-controlled commands inside a Windows
// LPAC (least-privileged AppContainer) and an existing MAR Job Object. LPAC
// opts out of broad ALL APPLICATION PACKAGES grants; task-scoped file access is
// supplied by a unique capability SID and network remains denied by default.
type WindowsSandboxExecutor struct {
	root      string
	readPaths []string
	limits    processctl.Limits
	readyErr  error
}

func NewWindowsSandboxExecutor(root string, readPaths ...string) (*WindowsSandboxExecutor, error) {
	return NewWindowsSandboxExecutorWithLimits(root, processctl.Limits{}, readPaths...)
}

func NewWindowsSandboxExecutorWithLimits(root string, limits processctl.Limits, readPaths ...string) (*WindowsSandboxExecutor, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	executor := &WindowsSandboxExecutor{root: filepath.Clean(abs), readPaths: append([]string(nil), readPaths...), limits: limits}
	executor.readyErr = processctl.CheckSandboxHostReady(context.Background(), executor.root)
	return executor, nil
}

func (e *WindowsSandboxExecutor) IsolationLevel() IsolationLevel {
	if e == nil || e.readyErr != nil {
		return IsolationTrustedHost
	}
	return IsolationEnforcedSandbox
}

func (e *WindowsSandboxExecutor) RequiresSanitizedEnvironment() bool { return true }

func (e *WindowsSandboxExecutor) Run(ctx context.Context, taskID string, spec ExecSpec) (ExecResult, error) {
	if e == nil {
		return ExecResult{ExitCode: -1}, errors.New("Windows sandbox executor is nil")
	}
	if e.readyErr != nil {
		return ExecResult{ExitCode: -1}, e.readyErr
	}
	result, err := processctl.RunSandboxedCommand(ctx, processctl.SandboxCommandSpec{
		TaskID:         taskID,
		OperationID:    spec.OperationID,
		WorkspaceRoot:  e.root,
		ReadPaths:      append([]string(nil), e.readPaths...),
		Path:           spec.Path,
		Args:           spec.Args,
		Dir:            spec.Dir,
		Env:            spec.Env,
		MaxOutputBytes: spec.MaxOutputBytes,
		Limits:         e.limits,
	})
	return ExecResult{Output: result.Output, ExitCode: result.ExitCode}, err
}
