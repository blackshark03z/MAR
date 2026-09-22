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
	root         string
	rootWritable bool
	readPaths    []string
	// writePaths may include a task-scoped Temp materialization directory for
	// generated executables; each path is still granted explicitly to LPAC.
	writePaths []string
	limits     processctl.Limits
	readyErr   error
}

func NewWindowsSandboxExecutor(root string, readPaths ...string) (*WindowsSandboxExecutor, error) {
	return newWindowsSandboxExecutor(root, true, processctl.Limits{}, nil, readPaths...)
}

func NewWindowsSandboxExecutorWithWritePaths(root string, writePaths []string, readPaths ...string) (*WindowsSandboxExecutor, error) {
	return newWindowsSandboxExecutor(root, true, processctl.Limits{}, writePaths, readPaths...)
}

func NewWindowsSandboxExecutorWithLimits(root string, limits processctl.Limits, readPaths ...string) (*WindowsSandboxExecutor, error) {
	return newWindowsSandboxExecutor(root, true, limits, nil, readPaths...)
}

func NewWindowsSandboxExecutorWithLimitsAndWritePaths(root string, limits processctl.Limits, writePaths []string, readPaths ...string) (*WindowsSandboxExecutor, error) {
	return newWindowsSandboxExecutor(root, true, limits, writePaths, readPaths...)
}

// NewWindowsReadOnlySandboxExecutorWithWritePaths creates an executor whose
// execution root is READ/EXEC only while retaining explicitly granted auxiliary
// write paths such as task-scoped Go temp/build caches.
func NewWindowsReadOnlySandboxExecutorWithWritePaths(root string, writePaths []string, readPaths ...string) (*WindowsSandboxExecutor, error) {
	return newWindowsSandboxExecutor(root, false, processctl.Limits{}, writePaths, readPaths...)
}

func NewWindowsReadOnlySandboxExecutorWithLimitsAndWritePaths(root string, limits processctl.Limits, writePaths []string, readPaths ...string) (*WindowsSandboxExecutor, error) {
	return newWindowsSandboxExecutor(root, false, limits, writePaths, readPaths...)
}

func newWindowsSandboxExecutor(root string, rootWritable bool, limits processctl.Limits, writePaths []string, readPaths ...string) (*WindowsSandboxExecutor, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	executor := &WindowsSandboxExecutor{
		root:         filepath.Clean(abs),
		rootWritable: rootWritable,
		readPaths:    append([]string(nil), readPaths...),
		writePaths: append([]string(nil), writePaths...),
		limits:     limits,
	}
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
	rootWritable := e.rootWritable
	result, err := processctl.RunSandboxedCommand(ctx, processctl.SandboxCommandSpec{
		TaskID:            taskID,
		OperationID:       spec.OperationID,
		WorkspaceRoot:     e.root,
		WorkspaceWritable: &rootWritable,
		ReadPaths:      append([]string(nil), e.readPaths...),
		WritePaths:     append([]string(nil), e.writePaths...),
		Path:           spec.Path,
		Args:           spec.Args,
		Dir:            spec.Dir,
		Env:            spec.Env,
		MaxOutputBytes: spec.MaxOutputBytes,
		Limits:         e.limits,
	})
	return ExecResult{Output: result.Output, ExitCode: result.ExitCode, OutputTruncated: result.OutputTruncated, CapturedBytes: result.CapturedBytes, TotalBytes: result.TotalBytes}, err
}
