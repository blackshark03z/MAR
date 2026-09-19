//go:build windows

package aci

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mar/internal/processctl"
)

// ContainedGitBroker executes MAR-authored typed Git operations on the trusted
// daemon side. Model input is limited to validated paths, configured remote names,
// and validated branch names; raw Git commands, URLs and refspecs never cross this boundary.
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

func (b *ContainedGitBroker) ExecutablePath() string {
	if b == nil {
		return ""
	}
	return b.gitPath
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

func (b *ContainedGitBroker) RemoteRef(ctx context.Context, taskID, root, remote, branch string, maxOutputBytes int) (ExecResult, error) {
	remoteURL, err := b.validateRemoteTarget(ctx, root, remote, branch)
	if err != nil {
		return ExecResult{ExitCode: -1}, err
	}
	return b.runRemote(ctx, taskID, root, maxOutputBytes, remoteURL, []string{
		"ls-remote", "--heads", remote, "refs/heads/" + branch,
	})
}

func (b *ContainedGitBroker) PushHead(ctx context.Context, taskID, root, remote, branch string, maxOutputBytes int) (ExecResult, error) {
	remoteURL, err := b.validateRemoteTarget(ctx, root, remote, branch)
	if err != nil {
		return ExecResult{ExitCode: -1}, err
	}
	return b.runRemote(ctx, taskID, root, maxOutputBytes, remoteURL, []string{
		"push", "--porcelain", remote, "HEAD:refs/heads/" + branch,
	})
}

func (b *ContainedGitBroker) validateRemoteTarget(ctx context.Context, root, remote, branch string) (string, error) {
	if b == nil || b.gitPath == "" {
		return "", errors.New("typed Git broker is not initialized")
	}
	if err := validateRemoteName(remote); err != nil {
		return "", err
	}
	if err := validateRemoteBranch(branch); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, b.gitPath, "-C", root, "remote", "get-url", "--push", remote)
	cmd.Env = remoteGitEnvironment()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("configured Git remote %q is unavailable", remote)
	}
	remoteURL := strings.TrimSpace(string(out))
	if err := validateConfiguredRemoteAddress(remoteURL); err != nil {
		return "", fmt.Errorf("configured Git remote %q is not an allowed network remote: %w", remote, err)
	}
	return remoteURL, nil
}

func validateRemoteName(remote string) error {
	if remote == "" || len(remote) > 128 || strings.HasPrefix(remote, "-") {
		return errors.New("invalid Git remote name")
	}
	for _, r := range remote {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return errors.New("invalid Git remote name")
	}
	return nil
}

func validateRemoteBranch(branch string) error {
	if branch == "" || len(branch) > 240 || strings.HasPrefix(branch, "-") ||
		strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") ||
		strings.HasPrefix(branch, ".") || strings.HasSuffix(branch, ".") ||
		strings.Contains(branch, "..") || strings.Contains(branch, "//") ||
		strings.Contains(branch, "@{") || strings.HasSuffix(branch, ".lock") {
		return errors.New("invalid Git branch name")
	}
	for _, r := range branch {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-' || r == '/' {
			continue
		}
		return errors.New("invalid Git branch name")
	}
	return nil
}

func validateConfiguredRemoteAddress(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || filepath.IsAbs(raw) {
		return errors.New("remote must be a configured network remote, not a filesystem path")
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"):
		_, err := validateOutboundURL(raw)
		return err
	case strings.HasPrefix(lower, "ssh://"):
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return errors.New("invalid ssh remote URL")
		}
		return validateRemoteHost(u.Hostname())
	case strings.Contains(raw, "://"):
		return errors.New("unsupported Git remote URL scheme")
	default:
		colon := strings.Index(raw, ":")
		if colon <= 0 || strings.Contains(raw[:colon], "/") || strings.Contains(raw[:colon], "\\") {
			return errors.New("remote must use http(s), ssh, or scp-like SSH syntax")
		}
		host := raw[:colon]
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		return validateRemoteHost(host)
	}
}

func validateRemoteHost(host string) error {
	host = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(host, ".")))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".home.arpa") {
		return errors.New("remote host is local or special-use")
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicNetworkIP(ip) {
		return errors.New("remote host is private, local, or special-use")
	}
	return nil
}

func (b *ContainedGitBroker) runRemote(ctx context.Context, taskID, root string, maxOutputBytes int, remoteURL string, operationArgs []string) (ExecResult, error) {
	if taskID == "" || root == "" {
		return ExecResult{ExitCode: -1}, errors.New("task id and workspace root are required for remote Git broker")
	}
	args := []string{
		"-c", "core.hooksPath=NUL",
		"-c", "core.pager=cat",
		"-C", root,
	}
	args = append(args, operationArgs...)
	capture, runErr := processctl.RunContainedCommandDetailed(ctx, processctl.CommandSpec{
		TaskID:         taskID,
		OperationID:    "aci-git-remote",
		Path:           b.gitPath,
		Args:           args,
		Dir:            root,
		Env:            remoteGitEnvironment(),
		MaxOutputBytes: maxOutputBytes,
	})
	output := capture.Output
	if remoteURL != "" {
		output = strings.ReplaceAll(output, remoteURL, "<configured-remote>")
	}
	result := ExecResult{Output: output, ExitCode: 0, OutputTruncated: capture.OutputTruncated, CapturedBytes: capture.CapturedBytes, TotalBytes: capture.TotalBytes}
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

func remoteGitEnvironment() []string {
	blocked := map[string]struct{}{
		"git_terminal_prompt": {},
		"gcm_interactive":     {},
		"git_pager":           {},
		"pager":               {},
	}
	env := make([]string, 0, len(os.Environ())+4)
	for _, item := range os.Environ() {
		key := item
		if idx := strings.IndexByte(item, '='); idx >= 0 {
			key = item[:idx]
		}
		if _, skip := blocked[strings.ToLower(key)]; skip {
			continue
		}
		env = append(env, item)
	}
	return append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"GIT_PAGER=cat",
		"PAGER=cat",
	)
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
	capture, runErr := processctl.RunContainedCommandDetailed(ctx, processctl.CommandSpec{
		TaskID:         taskID,
		OperationID:    "aci-git-broker",
		Path:           b.gitPath,
		Args:           args,
		Dir:            root,
		Env:            env,
		MaxOutputBytes: maxOutputBytes,
	})
	result := ExecResult{Output: capture.Output, ExitCode: 0, OutputTruncated: capture.OutputTruncated, CapturedBytes: capture.CapturedBytes, TotalBytes: capture.TotalBytes}
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
