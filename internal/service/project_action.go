package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"mar/internal/domain"
)

const (
	defaultFastCommandTimeout = 120 * time.Second
	maxFastCommandTimeout     = 10 * time.Minute
	defaultFastOutputBytes    = 256 << 10
	maxFastOutputBytes        = 1 << 20
)

type ProjectWriteRequest struct {
	ProjectID      string
	Path           string
	ExpectedSHA256 string
	Content        string
}

type ProjectWriteResult struct {
	ProjectID    string `json:"project_id"`
	Path         string `json:"path"`
	BeforeSHA256 string `json:"before_sha256,omitempty"`
	AfterSHA256  string `json:"after_sha256"`
	Created      bool   `json:"created"`
	Bytes        int    `json:"bytes"`
}

type ProjectPatchRequest struct {
	ProjectID      string
	Path           string
	ExpectedSHA256 string
	Search         string
	Replacement    string
	ExpectedCount  int
}

type ProjectPatchResult struct {
	ProjectID    string `json:"project_id"`
	Path         string `json:"path"`
	BeforeSHA256 string `json:"before_sha256"`
	AfterSHA256  string `json:"after_sha256"`
	Replacements int    `json:"replacements"`
}

type ProjectFSActionResult struct {
	ProjectID   string `json:"project_id"`
	Operation   string `json:"operation"`
	Path        string `json:"path"`
	Destination string `json:"destination,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
}

type ProjectCommandResult struct {
	ProjectID       string   `json:"project_id"`
	Executable      string   `json:"executable"`
	Args            []string `json:"args,omitempty"`
	Cwd             string   `json:"cwd"`
	Output          string   `json:"output"`
	ExitCode        int      `json:"exit_code"`
	TimedOut        bool     `json:"timed_out,omitempty"`
	OutputTruncated bool     `json:"output_truncated,omitempty"`
}

type ProjectGitActionResult struct {
	ProjectID string   `json:"project_id"`
	Operation string   `json:"operation"`
	Paths     []string `json:"paths,omitempty"`
	Revision  string   `json:"revision,omitempty"`
	Remote    string   `json:"remote,omitempty"`
	Branch    string   `json:"branch,omitempty"`
	Output    string   `json:"output,omitempty"`
}

func (s *TaskService) WriteProjectFile(ctx context.Context, req ProjectWriteRequest) (ProjectWriteResult, error) {
	project, policy, err := s.fastProject(ctx, req.ProjectID)
	if err != nil {
		return ProjectWriteResult{}, err
	}
	if !policy.LocalFileWrite {
		return ProjectWriteResult{}, errors.New("project local_file_write policy is disabled")
	}
	if strings.TrimSpace(req.Path) == "" {
		return ProjectWriteResult{}, errors.New("write path is required")
	}
	payload := []byte(req.Content)
	if int64(len(payload)) > maxProjectReadBytes {
		return ProjectWriteResult{}, errors.New("write content exceeds fast-path text size limit")
	}
	if !utf8.Valid(payload) || strings.IndexByte(req.Content, 0) >= 0 {
		return ProjectWriteResult{}, errors.New("write content must be UTF-8 text")
	}
	target, err := safeProjectWriteTarget(project, req.Path)
	if err != nil {
		return ProjectWriteResult{}, err
	}
	expected := strings.TrimSpace(req.ExpectedSHA256)
	created := false
	beforeHex := ""
	switch {
	case strings.EqualFold(expected, "ABSENT"):
		if _, err := os.Lstat(target); err == nil {
			return ProjectWriteResult{}, errors.New("write target already exists; expected ABSENT")
		} else if !errors.Is(err, os.ErrNotExist) {
			return ProjectWriteResult{}, fmt.Errorf("inspect write target: %w", err)
		}
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return ProjectWriteResult{}, fmt.Errorf("create write target: %w", err)
		}
		if _, err := f.Write(payload); err != nil {
			_ = f.Close()
			_ = os.Remove(target)
			return ProjectWriteResult{}, fmt.Errorf("write new target: %w", err)
		}
		if err := f.Close(); err != nil {
			return ProjectWriteResult{}, fmt.Errorf("close new target: %w", err)
		}
		created = true
	default:
		expected = strings.ToLower(expected)
		if len(expected) != 64 {
			return ProjectWriteResult{}, errors.New("expected_sha256 must be ABSENT or an exact SHA-256 hex digest")
		}
		info, err := os.Stat(target)
		if err != nil {
			return ProjectWriteResult{}, fmt.Errorf("inspect write target: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() > maxProjectReadBytes {
			return ProjectWriteResult{}, errors.New("write target must be one bounded regular text file")
		}
		current, err := os.ReadFile(target)
		if err != nil {
			return ProjectWriteResult{}, fmt.Errorf("read write target: %w", err)
		}
		if !utf8.Valid(current) || strings.IndexByte(string(current), 0) >= 0 {
			return ProjectWriteResult{}, errors.New("write target must be UTF-8 text")
		}
		before := sha256.Sum256(current)
		beforeHex = hex.EncodeToString(before[:])
		if beforeHex != expected {
			return ProjectWriteResult{}, fmt.Errorf("write revision mismatch: expected %s got %s", expected, beforeHex)
		}
		if err := os.WriteFile(target, payload, info.Mode().Perm()); err != nil {
			return ProjectWriteResult{}, fmt.Errorf("replace write target: %w", err)
		}
	}
	after := sha256.Sum256(payload)
	rel, _ := filepath.Rel(project.Root, target)
	return ProjectWriteResult{
		ProjectID:    project.ID,
		Path:         filepath.ToSlash(rel),
		BeforeSHA256: beforeHex,
		AfterSHA256:  hex.EncodeToString(after[:]),
		Created:      created,
		Bytes:        len(payload),
	}, nil
}

func safeProjectWriteTarget(project domain.Project, requestedPath string) (string, error) {
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" {
		return "", errors.New("write path is required")
	}
	base := filepath.Base(requestedPath)
	if base == "." || base == ".." || base == string(filepath.Separator) || strings.TrimSpace(base) == "" {
		return "", errors.New("write path must name a file")
	}
	parentRequest := filepath.Dir(requestedPath)
	parent, err := safeProjectReadTarget(project, parentRequest)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return "", errors.New("write parent must be an existing project directory")
	}
	target := filepath.Join(parent, base)
	if _, err := os.Lstat(target); err == nil {
		return safeProjectReadTarget(project, requestedPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect write target: %w", err)
	}
	return target, nil
}

func (s *TaskService) CreateProjectDirectory(ctx context.Context, projectID, path string) (ProjectFSActionResult, error) {
	project, policy, err := s.fastProject(ctx, projectID)
	if err != nil {
		return ProjectFSActionResult{}, err
	}
	if !policy.LocalFileWrite {
		return ProjectFSActionResult{}, errors.New("project local_file_write policy is disabled")
	}
	clean, err := cleanFastGitPath(path)
	if err != nil || clean == "." {
		return ProjectFSActionResult{}, errors.New("mkdir path must be one project-relative directory")
	}
	target, err := safeProjectWriteTarget(project, filepath.FromSlash(clean))
	if err != nil {
		return ProjectFSActionResult{}, err
	}
	if _, err := os.Lstat(target); err == nil {
		return ProjectFSActionResult{}, errors.New("mkdir target already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ProjectFSActionResult{}, fmt.Errorf("inspect mkdir target: %w", err)
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		return ProjectFSActionResult{}, fmt.Errorf("create project directory: %w", err)
	}
	return ProjectFSActionResult{ProjectID: project.ID, Operation: "mkdir", Path: clean}, nil
}

func (s *TaskService) RemoveProjectFile(ctx context.Context, projectID, path, expectedSHA256 string) (ProjectFSActionResult, error) {
	project, policy, err := s.fastProject(ctx, projectID)
	if err != nil {
		return ProjectFSActionResult{}, err
	}
	if !policy.LocalFileWrite {
		return ProjectFSActionResult{}, errors.New("project local_file_write policy is disabled")
	}
	clean, target, digest, err := fastProjectFileIdentity(project, path)
	if err != nil {
		return ProjectFSActionResult{}, err
	}
	if err := requireFastFileSHA256(expectedSHA256, digest); err != nil {
		return ProjectFSActionResult{}, err
	}
	if err := os.Remove(target); err != nil {
		return ProjectFSActionResult{}, fmt.Errorf("remove project file: %w", err)
	}
	return ProjectFSActionResult{ProjectID: project.ID, Operation: "remove", Path: clean, SHA256: digest}, nil
}

func (s *TaskService) RenameProjectFile(ctx context.Context, projectID, path, destination, expectedSHA256 string) (ProjectFSActionResult, error) {
	project, policy, err := s.fastProject(ctx, projectID)
	if err != nil {
		return ProjectFSActionResult{}, err
	}
	if !policy.LocalFileWrite {
		return ProjectFSActionResult{}, errors.New("project local_file_write policy is disabled")
	}
	clean, source, digest, err := fastProjectFileIdentity(project, path)
	if err != nil {
		return ProjectFSActionResult{}, err
	}
	if err := requireFastFileSHA256(expectedSHA256, digest); err != nil {
		return ProjectFSActionResult{}, err
	}
	destinationClean, err := cleanFastGitPath(destination)
	if err != nil || destinationClean == "." {
		return ProjectFSActionResult{}, errors.New("rename destination must be one project-relative file path")
	}
	destinationTarget, err := safeProjectWriteTarget(project, filepath.FromSlash(destinationClean))
	if err != nil {
		return ProjectFSActionResult{}, err
	}
	if _, err := os.Lstat(destinationTarget); err == nil {
		return ProjectFSActionResult{}, errors.New("rename destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ProjectFSActionResult{}, fmt.Errorf("inspect rename destination: %w", err)
	}
	if err := os.Rename(source, destinationTarget); err != nil {
		return ProjectFSActionResult{}, fmt.Errorf("rename project file: %w", err)
	}
	return ProjectFSActionResult{ProjectID: project.ID, Operation: "rename", Path: clean, Destination: destinationClean, SHA256: digest}, nil
}

func fastProjectFileIdentity(project domain.Project, path string) (string, string, string, error) {
	clean, err := cleanFastGitPath(path)
	if err != nil || clean == "." {
		return "", "", "", errors.New("file path must be one project-relative file")
	}
	lexical := filepath.Join(project.Root, filepath.FromSlash(clean))
	lexicalInfo, err := os.Lstat(lexical)
	if err != nil {
		return "", "", "", fmt.Errorf("inspect project file: %w", err)
	}
	if lexicalInfo.Mode()&os.ModeSymlink != 0 {
		return "", "", "", errors.New("fast-path filesystem actions reject symlink files")
	}
	target, err := safeProjectReadTarget(project, filepath.FromSlash(clean))
	if err != nil {
		return "", "", "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", "", "", fmt.Errorf("inspect project file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxProjectReadBytes {
		return "", "", "", errors.New("fast-path filesystem actions require one bounded regular file")
	}
	payload, err := os.ReadFile(target)
	if err != nil {
		return "", "", "", fmt.Errorf("read project file identity: %w", err)
	}
	digest := sha256.Sum256(payload)
	return clean, target, hex.EncodeToString(digest[:]), nil
}

func requireFastFileSHA256(expected, actual string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if len(expected) != 64 {
		return errors.New("expected_sha256 must be an exact SHA-256 hex digest")
	}
	if expected != actual {
		return fmt.Errorf("file revision mismatch: expected %s got %s", expected, actual)
	}
	return nil
}

func (s *TaskService) ApplyProjectPatch(ctx context.Context, req ProjectPatchRequest) (ProjectPatchResult, error) {
	project, policy, err := s.fastProject(ctx, req.ProjectID)
	if err != nil {
		return ProjectPatchResult{}, err
	}
	if !policy.LocalFileWrite {
		return ProjectPatchResult{}, errors.New("project local_file_write policy is disabled")
	}
	if strings.TrimSpace(req.Path) == "" {
		return ProjectPatchResult{}, errors.New("patch path is required")
	}
	expected := strings.ToLower(strings.TrimSpace(req.ExpectedSHA256))
	if len(expected) != 64 {
		return ProjectPatchResult{}, errors.New("expected_sha256 must be an exact SHA-256 hex digest")
	}
	if req.Search == "" {
		return ProjectPatchResult{}, errors.New("patch search text must not be empty")
	}
	if req.ExpectedCount <= 0 {
		req.ExpectedCount = 1
	}
	target, err := safeProjectReadTarget(project, req.Path)
	if err != nil {
		return ProjectPatchResult{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return ProjectPatchResult{}, fmt.Errorf("inspect patch target: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxProjectReadBytes {
		return ProjectPatchResult{}, errors.New("patch target must be one bounded regular text file")
	}
	payload, err := os.ReadFile(target)
	if err != nil {
		return ProjectPatchResult{}, fmt.Errorf("read patch target: %w", err)
	}
	if !utf8.Valid(payload) || strings.IndexByte(string(payload), 0) >= 0 {
		return ProjectPatchResult{}, errors.New("patch target must be UTF-8 text")
	}
	before := sha256.Sum256(payload)
	beforeHex := hex.EncodeToString(before[:])
	if beforeHex != expected {
		return ProjectPatchResult{}, fmt.Errorf("patch revision mismatch: expected %s got %s", expected, beforeHex)
	}
	text := string(payload)
	count := strings.Count(text, req.Search)
	if count != req.ExpectedCount {
		return ProjectPatchResult{}, fmt.Errorf("patch search count mismatch: expected %d got %d", req.ExpectedCount, count)
	}
	updated := strings.Replace(text, req.Search, req.Replacement, req.ExpectedCount)
	if err := os.WriteFile(target, []byte(updated), info.Mode().Perm()); err != nil {
		return ProjectPatchResult{}, fmt.Errorf("write patch target: %w", err)
	}
	after := sha256.Sum256([]byte(updated))
	rel, _ := filepath.Rel(project.Root, target)
	return ProjectPatchResult{ProjectID: project.ID, Path: filepath.ToSlash(rel), BeforeSHA256: beforeHex, AfterSHA256: hex.EncodeToString(after[:]), Replacements: req.ExpectedCount}, nil
}

func (s *TaskService) RunProjectCommand(ctx context.Context, projectID, executable string, args []string, cwd string, timeoutSeconds, maxOutputBytes int) (ProjectCommandResult, error) {
	project, policy, err := s.fastProject(ctx, projectID)
	if err != nil {
		return ProjectCommandResult{}, err
	}
	if !policy.LocalFileWrite {
		return ProjectCommandResult{}, errors.New("project local_file_write policy is disabled")
	}
	if !policy.NetworkAllowed {
		return ProjectCommandResult{}, errors.New("trusted-owner run requires project network_allowed because host execution is not network-sandboxed")
	}
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return ProjectCommandResult{}, errors.New("executable is required")
	}
	if strings.TrimSpace(cwd) == "" {
		cwd = "."
	}
	runDir, err := safeProjectReadTarget(project, cwd)
	if err != nil {
		return ProjectCommandResult{}, err
	}
	info, err := os.Stat(runDir)
	if err != nil || !info.IsDir() {
		return ProjectCommandResult{}, errors.New("cwd must be an existing project directory")
	}
	timeout := defaultFastCommandTimeout
	if timeoutSeconds > 0 {
		timeout = time.Duration(timeoutSeconds) * time.Second
	}
	if timeout > maxFastCommandTimeout {
		return ProjectCommandResult{}, errors.New("timeout exceeds trusted-owner fast-path limit")
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = defaultFastOutputBytes
	}
	if maxOutputBytes > maxFastOutputBytes {
		maxOutputBytes = maxFastOutputBytes
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executable, args...)
	cmd.Dir = runDir
	cmd.Env = os.Environ()
	buf := &boundedActionOutput{max: maxOutputBytes}
	cmd.Stdout, cmd.Stderr = buf, buf
	runErr := cmd.Run()
	exitCode := 0
	timedOut := false
	if runErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			exitCode, timedOut = -1, true
		} else if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			return ProjectCommandResult{}, fmt.Errorf("run project command: %w", runErr)
		}
	}
	rel, _ := filepath.Rel(project.Root, runDir)
	return ProjectCommandResult{ProjectID: project.ID, Executable: executable, Args: append([]string(nil), args...), Cwd: filepath.ToSlash(rel), Output: buf.String(), ExitCode: exitCode, TimedOut: timedOut, OutputTruncated: buf.Truncated()}, nil
}

func (s *TaskService) StageProjectPaths(ctx context.Context, projectID string, paths []string) (ProjectGitActionResult, error) {
	project, policy, err := s.fastProject(ctx, projectID)
	if err != nil {
		return ProjectGitActionResult{}, err
	}
	if !policy.LocalGitWrite {
		return ProjectGitActionResult{}, errors.New("project local_git_write policy is disabled")
	}
	if len(paths) == 0 || len(paths) > 128 {
		return ProjectGitActionResult{}, errors.New("git_stage requires 1..128 project-relative paths")
	}
	clean := make([]string, 0, len(paths))
	for _, path := range paths {
		value, err := cleanFastGitPath(path)
		if err != nil {
			return ProjectGitActionResult{}, err
		}
		clean = append(clean, value)
	}
	args := append([]string{"add", "--"}, clean...)
	out, err := runProjectGit(ctx, project.Root, args...)
	if err != nil {
		return ProjectGitActionResult{}, err
	}
	return ProjectGitActionResult{ProjectID: project.ID, Operation: "git_stage", Paths: clean, Output: boundFastText(out)}, nil
}

func (s *TaskService) CommitProject(ctx context.Context, projectID, message string) (ProjectGitActionResult, error) {
	project, policy, err := s.fastProject(ctx, projectID)
	if err != nil {
		return ProjectGitActionResult{}, err
	}
	if !policy.LocalGitWrite {
		return ProjectGitActionResult{}, errors.New("project local_git_write policy is disabled")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return ProjectGitActionResult{}, errors.New("commit message is required")
	}
	out, err := runProjectGit(ctx, project.Root, "commit", "-m", message)
	if err != nil {
		return ProjectGitActionResult{}, err
	}
	head, err := runProjectGit(ctx, project.Root, "rev-parse", "HEAD")
	if err != nil {
		return ProjectGitActionResult{}, err
	}
	return ProjectGitActionResult{ProjectID: project.ID, Operation: "git_commit", Revision: strings.TrimSpace(head), Output: boundFastText(out)}, nil
}

func (s *TaskService) PushProject(ctx context.Context, projectID, remote string) (ProjectGitActionResult, error) {
	project, policy, err := s.fastProject(ctx, projectID)
	if err != nil {
		return ProjectGitActionResult{}, err
	}
	if !policy.LocalGitWrite {
		return ProjectGitActionResult{}, errors.New("project local_git_write policy is disabled")
	}
	if !policy.RemoteGitWrite {
		return ProjectGitActionResult{}, errors.New("project RemoteGitWrite policy is disabled")
	}
	if !policy.NetworkAllowed {
		return ProjectGitActionResult{}, errors.New("project network_allowed policy is disabled")
	}
	remote = strings.TrimSpace(remote)
	if remote == "" {
		remote = "origin"
	}
	if !validFastRemote(remote) {
		return ProjectGitActionResult{}, errors.New("remote name contains unsupported characters")
	}
	branchRaw, err := runProjectGit(ctx, project.Root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ProjectGitActionResult{}, err
	}
	branch := strings.TrimSpace(branchRaw)
	if branch == "" || branch == "HEAD" {
		return ProjectGitActionResult{}, errors.New("git_push requires a named current branch")
	}
	out, err := runProjectGit(ctx, project.Root, "push", remote, branch)
	if err != nil {
		return ProjectGitActionResult{}, err
	}
	return ProjectGitActionResult{ProjectID: project.ID, Operation: "git_push", Remote: remote, Branch: branch, Output: boundFastText(out)}, nil
}

func (s *TaskService) fastProject(ctx context.Context, projectID string) (domain.Project, domain.ProjectPolicy, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.Project{}, domain.ProjectPolicy{}, errors.New("project_id is required")
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return domain.Project{}, domain.ProjectPolicy{}, err
	}
	policy, err := s.store.GetProjectPolicy(ctx, projectID)
	if err != nil {
		return domain.Project{}, domain.ProjectPolicy{}, err
	}
	return project, policy, nil
}

func cleanFastGitPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return "", errors.New("git path must be project-relative")
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("git path escapes the registered project")
	}
	return filepath.ToSlash(clean), nil
}

func validFastRemote(remote string) bool {
	for _, r := range remote {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._-", r) {
			continue
		}
		return false
	}
	return remote != ""
}

func boundFastText(value string) string {
	if len(value) > maxFastOutputBytes {
		return value[:maxFastOutputBytes]
	}
	return value
}

type boundedActionOutput struct {
	mu        sync.Mutex
	buf       []byte
	max       int
	total     int64
	truncated bool
}

func (b *boundedActionOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total += int64(len(p))
	remaining := b.max - len(b.buf)
	if remaining > 0 {
		if len(p) < remaining {
			remaining = len(p)
		}
		b.buf = append(b.buf, p[:remaining]...)
	}
	if b.total > int64(b.max) {
		b.truncated = true
	}
	return len(p), nil
}

func (b *boundedActionOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.buf...))
}

func (b *boundedActionOutput) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}
