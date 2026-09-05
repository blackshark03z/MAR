//go:build windows

package aci

import (
	"context"
	"errors"
	"os/exec"

	"mar/internal/processctl"
)

// ContainedHostExecutor provides process-tree containment only. It is NOT a
// filesystem/network security sandbox and therefore reports TRUSTED_HOST.
type ContainedHostExecutor struct{}

func NewContainedHostExecutor() *ContainedHostExecutor { return &ContainedHostExecutor{} }

func (e *ContainedHostExecutor) IsolationLevel() IsolationLevel { return IsolationTrustedHost }

func (e *ContainedHostExecutor) Run(ctx context.Context, taskID string, spec ExecSpec) (ExecResult, error) {
	output, err := processctl.RunContainedCommand(ctx, processctl.CommandSpec{
		TaskID:         taskID,
		OperationID:    spec.OperationID,
		Path:           spec.Path,
		Args:           spec.Args,
		Dir:            spec.Dir,
		Env:            spec.Env,
		MaxOutputBytes: spec.MaxOutputBytes,
	})
	result := ExecResult{Output: output, ExitCode: 0}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, err
	}
	result.ExitCode = -1
	return result, err
}
