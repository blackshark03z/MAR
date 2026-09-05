//go:build windows

package contextengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"mar/internal/processctl"
)

const gitOutputTruncationMarker = "...[MAR output truncated]..."

type GitRepository struct {
	gitPath        string
	maxOutputBytes int
}

func NewGitRepository(maxOutputBytes int) (*GitRepository, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("Git executable is required for context repository snapshot: %w", err)
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = 8 << 20
	}
	return &GitRepository{gitPath: gitPath, maxOutputBytes: maxOutputBytes}, nil
}

func (g *GitRepository) Snapshot(ctx context.Context, root string) (RepositorySnapshot, error) {
	if g == nil || g.gitPath == "" {
		return RepositorySnapshot{}, errors.New("Git context repository is not initialized")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return RepositorySnapshot{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return RepositorySnapshot{}, fmt.Errorf("resolve Git context root: %w", err)
	}

	revision, err := g.runText(ctx, root, "revision", "rev-parse", "--verify", "HEAD")
	if err != nil {
		return RepositorySnapshot{}, err
	}
	tracked, err := g.runPaths(ctx, root, "tracked", "ls-files", "--cached", "-z")
	if err != nil {
		return RepositorySnapshot{}, err
	}
	untracked, err := g.runPaths(ctx, root, "untracked", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return RepositorySnapshot{}, err
	}
	modified, err := g.runPaths(ctx, root, "modified", "diff", "--name-only", "-z", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all")
	if err != nil {
		return RepositorySnapshot{}, err
	}
	staged, err := g.runPaths(ctx, root, "staged", "diff", "--cached", "--name-only", "-z", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all")
	if err != nil {
		return RepositorySnapshot{}, err
	}

	status := make(map[string]string, len(tracked)+len(untracked))
	for _, path := range tracked {
		status[path] = "clean"
	}
	for _, path := range untracked {
		status[path] = "untracked"
	}
	for _, path := range modified {
		status[path] = mergeGitStatus(status[path], "modified")
	}
	for _, path := range staged {
		status[path] = mergeGitStatus(status[path], "staged")
	}
	files := make([]RepositoryFile, 0, len(status))
	for path, state := range status {
		files = append(files, RepositoryFile{Path: filepath.ToSlash(path), Status: state})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return RepositorySnapshot{Revision: strings.TrimSpace(revision), Files: files}, nil
}

func mergeGitStatus(current, next string) string {
	if current == "" || current == "clean" {
		return next
	}
	if current == next || strings.Contains(current, next) {
		return current
	}
	if current == "untracked" {
		return current
	}
	parts := []string{current, next}
	sort.Strings(parts)
	return strings.Join(parts, "+")
}

func (g *GitRepository) runText(ctx context.Context, root, operation string, args ...string) (string, error) {
	output, err := g.run(ctx, root, operation, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (g *GitRepository) runPaths(ctx context.Context, root, operation string, args ...string) ([]string, error) {
	output, err := g.run(ctx, root, operation, args...)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(output, "\x00")
	paths := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, value := range parts {
		if value == "" {
			continue
		}
		clean, err := cleanRepositoryPath(value)
		if err != nil {
			return nil, fmt.Errorf("Git context returned unsafe path %q: %w", value, err)
		}
		clean = filepath.ToSlash(clean)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		paths = append(paths, clean)
	}
	sort.Strings(paths)
	return paths, nil
}

func (g *GitRepository) run(ctx context.Context, root, operation string, operationArgs ...string) (string, error) {
	env, err := gitContextEnvironment(root, g.gitPath)
	if err != nil {
		return "", err
	}
	args := []string{
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
		TaskID:         "context-snapshot",
		OperationID:    "context-git-" + operation,
		Path:           g.gitPath,
		Args:           args,
		Dir:            root,
		Env:            env,
		MaxOutputBytes: g.maxOutputBytes,
	})
	if strings.Contains(output, gitOutputTruncationMarker) {
		return "", fmt.Errorf("Git context %s output exceeded configured bound", operation)
	}
	if runErr != nil {
		return "", fmt.Errorf("Git context %s failed: %w: %s", operation, runErr, strings.TrimSpace(output))
	}
	return output, nil
}

func gitContextEnvironment(root, gitPath string) ([]string, error) {
	systemRoot := strings.TrimSpace(os.Getenv("SystemRoot"))
	if systemRoot == "" {
		systemRoot = strings.TrimSpace(os.Getenv("WINDIR"))
	}
	if systemRoot == "" {
		return nil, errors.New("Windows SystemRoot is required for Git context snapshot")
	}
	profileRoot := filepath.Join(root, ".mar", "runtime", "context-git-profile")
	tempRoot := filepath.Join(root, ".mar", "runtime", "context-git-tmp")
	for _, dir := range []string{profileRoot, tempRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	pathValue := strings.Join([]string{filepath.Dir(gitPath), filepath.Join(systemRoot, "System32"), systemRoot}, string(os.PathListSeparator))
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
