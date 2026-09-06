//go:build windows

package aci

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"mar/internal/processctl"
)

// ContainedGitBroker executes only MAR-authored, read-only Git inspection
// operations on the trusted daemon side. Model input is limited to already
// validated workspace-relative diff paths; raw Git commands never cross this
// boundary.
type ContainedGitBroker struct {
	gitPath string
}

func NewContainedGitBroker() (*ContainedGitBroker, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("Git executable is required for typed Git broker: %w", err)
	}
	return &ContainedGitBroker{gitPath: gitPath}, nil
}

func (b *ContainedGitBroker) Status(ctx context.Context, taskID, root string, maxOutputBytes int) (ExecResult, error) {
	return b.run(ctx, taskID, root, maxOutputBytes, []string{
		"status",
		"--porcelain=v1",
		"--branch",
		"--untracked-files=all",
		"--ignore-submodules=all",
	})
}

func (b *ContainedGitBroker) Diff(ctx context.Context, taskID, root string, paths []string, maxOutputBytes int) (ExecResult, error) {
	args := []string{
		"diff",
		"--no-ext-diff",
		"--no-textconv",
		"--ignore-submodules=all",
		"--",
	}
	args = append(args, paths...)
	return b.run(ctx, taskID, root, maxOutputBytes, args)
}

func (b *ContainedGitBroker) run(ctx context.Context, taskID, root string, maxOutputBytes int, operationArgs []string) (ExecResult, error) {
	if b == nil || b.gitPath == "" {
		return ExecResult{ExitCode: -1}, errors.New("typed Git broker is not initialized")
	}
	if taskID == "" || root == "" {
		return ExecResult{ExitCode: -1}, errors.New("task id and workspace root are required for typed Git broker")
	}
	env, err := gitBrokerEnvironment(root, b.gitPath)
	if err != nil {
		return ExecResult{ExitCode: -1}, err
	}
	args := []string{
		"-c", "core.autocrlf=false",
		"-c", "core.eol=lf",
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=NUL",
		"-c", "core.excludesFile=NUL",
		"-c", "core.attributesFile=NUL",
		"-c", "credential.helper=",
		"-c", "core.pager=cat",
		"-C", root,
	}
	args = append(args, operationArgs...)
	output, runErr := processctl.RunContainedCommand(ctx, processctl.CommandSpec{
		TaskID:         taskID,
		OperationID:    "aci-git-broker",
		Path:           b.gitPath,
		Args:           args,
		Dir:            root,
		Env:            env,
		MaxOutputBytes: maxOutputBytes,
	})
	result := ExecResult{Output: output, ExitCode: 0}
	if runErr == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, runErr
	}
	result.ExitCode = -1
	return result, runErr
}

func gitBrokerEnvironment(root, gitPath string) ([]string, error) {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = os.Getenv("WINDIR")
	}
	if systemRoot == "" {
		return nil, errors.New("Windows SystemRoot is required for typed Git broker")
	}
	profileRoot := filepath.Join(root, ".mar", "runtime", "git-profile")
	tempRoot := filepath.Join(root, ".mar", "runtime", "git-tmp")
	for _, dir := range []string{profileRoot, tempRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	pathValue := filepath.Dir(gitPath) + string(os.PathListSeparator) + filepath.Join(systemRoot, "System32") + string(os.PathListSeparator) + systemRoot
	return []string{
		"SystemRoot=" + systemRoot,
		"WINDIR=" + systemRoot,
		"ComSpec=" + filepath.Join(systemRoot, "System32", "cmd.exe"),
		"PATH=" + pathValue,
		"PATHEXT=.COM;.EXE;.BAT;.CMD",
		"USERPROFILE=" + profileRoot,
		"HOME=" + profileRoot,
		"TEMP=" + tempRoot,
		"TMP=" + tempRoot,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_PAGER=cat",
		"PAGER=cat",
	}, nil
}
