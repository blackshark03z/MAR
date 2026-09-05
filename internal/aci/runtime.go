package aci

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type IsolationLevel string

const (
	IsolationTrustedHost     IsolationLevel = "TRUSTED_HOST"
	IsolationEnforcedSandbox IsolationLevel = "ENFORCED_SANDBOX"
)

type ExecSpec struct {
	OperationID    string
	Path           string
	Args           []string
	Dir            string
	Env            []string
	MaxOutputBytes int
}

type ExecResult struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

type Executor interface {
	IsolationLevel() IsolationLevel
	Run(ctx context.Context, taskID string, spec ExecSpec) (ExecResult, error)
}

type GitBroker interface {
	Status(ctx context.Context, taskID, root string, maxOutputBytes int) (ExecResult, error)
	Diff(ctx context.Context, taskID, root string, paths []string, maxOutputBytes int) (ExecResult, error)
}

type sandboxEnvironmentPolicy interface {
	RequiresSanitizedEnvironment() bool
}

type Config struct {
	Root                         string
	TaskID                       string
	MaxReadBytes                 int
	MaxWriteBytes                int
	MaxSearchResults             int
	MaxSearchFileBytes           int64
	MaxCommandOutputBytes        int
	CommandTimeout               time.Duration
	AllowTrustedCommandExecution bool
	GitBroker                    GitBroker
	GoModuleCache                string
}

type Runtime struct {
	root          string
	taskID        string
	cfg           Config
	executor      Executor
	gitBroker     GitBroker
	goModuleCache string
}

type ReadResult struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	SHA256    string `json:"sha256"`
}

type SearchMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type SearchResult struct {
	Query     string        `json:"query"`
	Matches   []SearchMatch `json:"matches"`
	Truncated bool          `json:"truncated"`
}

type MutationResult struct {
	Path       string `json:"path"`
	BeforeHash string `json:"before_hash,omitempty"`
	AfterHash  string `json:"after_hash"`
	Created    bool   `json:"created"`
}

type Command struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
	Cwd  string   `json:"cwd,omitempty"`
}

func New(cfg Config, executor Executor) (*Runtime, error) {
	if strings.TrimSpace(cfg.Root) == "" || strings.TrimSpace(cfg.TaskID) == "" {
		return nil, errors.New("workspace root and task id are required")
	}
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("workspace root is not a directory")
	}
	if cfg.MaxReadBytes <= 0 {
		cfg.MaxReadBytes = 64 << 10
	}
	if cfg.MaxWriteBytes <= 0 {
		cfg.MaxWriteBytes = 2 << 20
	}
	if cfg.MaxSearchResults <= 0 {
		cfg.MaxSearchResults = 100
	}
	if cfg.MaxSearchFileBytes <= 0 {
		cfg.MaxSearchFileBytes = 1 << 20
	}
	if cfg.MaxCommandOutputBytes <= 0 {
		cfg.MaxCommandOutputBytes = 256 << 10
	}
	if cfg.CommandTimeout <= 0 {
		cfg.CommandTimeout = 2 * time.Minute
	}
	goModuleCache := ""
	if strings.TrimSpace(cfg.GoModuleCache) != "" {
		cache, err := filepath.Abs(cfg.GoModuleCache)
		if err != nil {
			return nil, fmt.Errorf("resolve shared Go module cache: %w", err)
		}
		cache, err = filepath.EvalSymlinks(cache)
		if err != nil {
			return nil, fmt.Errorf("resolve shared Go module cache identity: %w", err)
		}
		info, err := os.Stat(cache)
		if err != nil || !info.IsDir() {
			if err != nil {
				return nil, fmt.Errorf("stat shared Go module cache: %w", err)
			}
			return nil, errors.New("shared Go module cache is not a directory")
		}
		goModuleCache = filepath.Clean(cache)
	}
	return &Runtime{root: filepath.Clean(root), taskID: cfg.TaskID, cfg: cfg, executor: executor, gitBroker: cfg.GitBroker, goModuleCache: goModuleCache}, nil
}

func (r *Runtime) Root() string { return r.root }

func (r *Runtime) SelfHostingSafe() bool {
	return r.executor != nil && r.executor.IsolationLevel() == IsolationEnforcedSandbox && r.gitBroker != nil
}

func (r *Runtime) ReadFile(rel string, startLine, endLine int) (ReadResult, error) {
	if startLine <= 0 {
		startLine = 1
	}
	if endLine > 0 && endLine < startLine {
		return ReadResult{}, errors.New("end_line must be >= start_line")
	}
	path, err := r.resolveExisting(rel)
	if err != nil {
		return ReadResult{}, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return ReadResult{}, err
	}
	if !st.Mode().IsRegular() {
		return ReadResult{}, errors.New("read_file requires a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return ReadResult{}, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ReadResult{}, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ReadResult{}, err
	}

	var b strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), int(r.cfg.MaxSearchFileBytes))
	lineNo := 0
	lastIncluded := startLine - 1
	truncated := false
	for scanner.Scan() {
		lineNo++
		if lineNo < startLine {
			continue
		}
		if endLine > 0 && lineNo > endLine {
			break
		}
		line := scanner.Text() + "\n"
		if b.Len()+len(line) > r.cfg.MaxReadBytes {
			remaining := r.cfg.MaxReadBytes - b.Len()
			if remaining > 0 {
				b.WriteString(line[:min(remaining, len(line))])
			}
			truncated = true
			break
		}
		b.WriteString(line)
		lastIncluded = lineNo
	}
	if err := scanner.Err(); err != nil {
		return ReadResult{}, err
	}
	return ReadResult{
		Path:      r.relative(path),
		StartLine: startLine,
		EndLine:   lastIncluded,
		Text:      b.String(),
		Truncated: truncated,
		SHA256:    hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func (r *Runtime) SearchText(query string, maxResults int) (SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResult{}, errors.New("search query is required")
	}
	if maxResults <= 0 || maxResults > r.cfg.MaxSearchResults {
		maxResults = r.cfg.MaxSearchResults
	}
	result := SearchResult{Query: query}
	errStop := errors.New("search result limit reached")
	err := filepath.WalkDir(r.root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel := r.relative(path)
		if d.IsDir() {
			base := strings.ToLower(d.Name())
			if path != r.root && (base == ".git" || base == ".mar") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > r.cfg.MaxSearchFileBytes {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64<<10), int(r.cfg.MaxSearchFileBytes))
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if strings.Contains(line, query) {
				result.Matches = append(result.Matches, SearchMatch{Path: rel, Line: lineNo, Text: boundText(line, 500)})
				if len(result.Matches) >= maxResults {
					result.Truncated = true
					return errStop
				}
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return SearchResult{}, err
	}
	sort.Slice(result.Matches, func(i, j int) bool {
		if result.Matches[i].Path == result.Matches[j].Path {
			return result.Matches[i].Line < result.Matches[j].Line
		}
		return result.Matches[i].Path < result.Matches[j].Path
	})
	return result, nil
}

func (r *Runtime) WriteFile(rel, expectedSHA256 string, content []byte) (MutationResult, error) {
	if len(content) > r.cfg.MaxWriteBytes {
		return MutationResult{}, errors.New("write exceeds configured byte limit")
	}
	path, exists, beforeHash, err := r.resolveWriteTarget(rel)
	if err != nil {
		return MutationResult{}, err
	}
	if expectedSHA256 == "ABSENT" {
		if exists {
			return MutationResult{}, errors.New("file already exists")
		}
	} else {
		if !exists {
			return MutationResult{}, errors.New("file does not exist for hash-bound write")
		}
		if !strings.EqualFold(expectedSHA256, beforeHash) {
			return MutationResult{}, errors.New("file revision/hash precondition failed")
		}
	}
	if err := atomicWrite(path, content); err != nil {
		return MutationResult{}, err
	}
	after := sha256.Sum256(content)
	return MutationResult{Path: r.relative(path), BeforeHash: beforeHash, AfterHash: hex.EncodeToString(after[:]), Created: !exists}, nil
}

func (r *Runtime) ReplaceExact(rel, expectedSHA256, search, replacement string, expectedCount int) (MutationResult, error) {
	if expectedCount <= 0 || search == "" {
		return MutationResult{}, errors.New("search text and positive expected_count are required")
	}
	path, err := r.resolveExistingForWrite(rel)
	if err != nil {
		return MutationResult{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return MutationResult{}, err
	}
	if len(b) > r.cfg.MaxWriteBytes {
		return MutationResult{}, errors.New("file exceeds configured mutation byte limit")
	}
	before := sha256.Sum256(b)
	beforeHash := hex.EncodeToString(before[:])
	if !strings.EqualFold(expectedSHA256, beforeHash) {
		return MutationResult{}, errors.New("file revision/hash precondition failed")
	}
	count := strings.Count(string(b), search)
	if count != expectedCount {
		return MutationResult{}, fmt.Errorf("expected %d exact match(es), found %d", expectedCount, count)
	}
	updated := []byte(strings.Replace(string(b), search, replacement, expectedCount))
	if len(updated) > r.cfg.MaxWriteBytes {
		return MutationResult{}, errors.New("updated file exceeds configured mutation byte limit")
	}
	if err := atomicWrite(path, updated); err != nil {
		return MutationResult{}, err
	}
	after := sha256.Sum256(updated)
	return MutationResult{Path: r.relative(path), BeforeHash: beforeHash, AfterHash: hex.EncodeToString(after[:])}, nil
}

func (r *Runtime) GitStatus(ctx context.Context) (ExecResult, error) {
	if r.gitBroker == nil {
		return ExecResult{}, errors.New("typed Git broker is not configured")
	}
	commandCtx, cancel := context.WithTimeout(ctx, r.cfg.CommandTimeout)
	defer cancel()
	return r.gitBroker.Status(commandCtx, r.taskID, r.root, r.cfg.MaxCommandOutputBytes)
}

func (r *Runtime) GitDiff(ctx context.Context, paths []string) (ExecResult, error) {
	if r.gitBroker == nil {
		return ExecResult{}, errors.New("typed Git broker is not configured")
	}
	cleanPaths := make([]string, 0, len(paths))
	for _, rel := range paths {
		clean, err := cleanRelative(rel)
		if err != nil {
			return ExecResult{}, err
		}
		cleanPaths = append(cleanPaths, clean)
	}
	commandCtx, cancel := context.WithTimeout(ctx, r.cfg.CommandTimeout)
	defer cancel()
	return r.gitBroker.Diff(commandCtx, r.taskID, r.root, cleanPaths, r.cfg.MaxCommandOutputBytes)
}

func (r *Runtime) RunCommand(ctx context.Context, cmd Command) (ExecResult, error) {
	return r.runCommand(ctx, cmd)
}

func (r *Runtime) runCommand(ctx context.Context, cmd Command) (ExecResult, error) {
	if r.executor == nil {
		return ExecResult{}, errors.New("command executor is not configured")
	}
	if r.executor.IsolationLevel() != IsolationEnforcedSandbox && !r.cfg.AllowTrustedCommandExecution {
		return ExecResult{}, errors.New("command execution requires enforced sandbox")
	}
	if err := validateCommand(cmd); err != nil {
		return ExecResult{}, err
	}
	cwd := r.root
	if rawCwd := strings.TrimSpace(cmd.Cwd); rawCwd != "" && filepath.Clean(rawCwd) != "." {
		resolved, err := r.resolveExisting(rawCwd)
		if err != nil {
			return ExecResult{}, err
		}
		st, err := os.Stat(resolved)
		if err != nil || !st.IsDir() {
			return ExecResult{}, errors.New("command cwd must be an existing directory")
		}
		cwd = resolved
	}
	path, err := exec.LookPath(cmd.Name)
	if err != nil {
		return ExecResult{}, fmt.Errorf("resolve command %q: %w", cmd.Name, err)
	}
	env, err := r.commandEnvironment(path)
	if err != nil {
		return ExecResult{}, err
	}
	if strings.EqualFold(filepath.Base(path), "go.exe") || strings.EqualFold(filepath.Base(path), "go") {
		cacheRoot := filepath.Join(r.root, ".mar", "go")
		if err := os.MkdirAll(filepath.Join(cacheRoot, "build"), 0o755); err != nil {
			return ExecResult{}, err
		}
		modCache := r.goModuleCache
		if modCache == "" {
			modCache = filepath.Join(cacheRoot, "mod")
			if err := os.MkdirAll(modCache, 0o755); err != nil {
				return ExecResult{}, err
			}
		}
		if err := os.MkdirAll(filepath.Join(cacheRoot, "tmp"), 0o755); err != nil {
			return ExecResult{}, err
		}
		env = append(env,
			"GOCACHE="+filepath.Join(cacheRoot, "build"),
			"GOMODCACHE="+modCache,
			"GOTMPDIR="+filepath.Join(cacheRoot, "tmp"),
			"GOPROXY=off",
			"GOSUMDB=off",
			"GOENV=off",
			"GOTOOLCHAIN=local",
			"GOROOT="+filepath.Dir(filepath.Dir(path)),
		)
	}
	commandCtx, cancel := context.WithTimeout(ctx, r.cfg.CommandTimeout)
	defer cancel()
	return r.executor.Run(commandCtx, r.taskID, ExecSpec{
		OperationID:    "aci-" + shortHash(strings.Join(append([]string{cmd.Name}, cmd.Args...), "\x00")),
		Path:           path,
		Args:           append([]string(nil), cmd.Args...),
		Dir:            cwd,
		Env:            env,
		MaxOutputBytes: r.cfg.MaxCommandOutputBytes,
	})
}

func (r *Runtime) commandEnvironment(commandPath string) ([]string, error) {
	requiresSanitized := r.executor != nil && r.executor.IsolationLevel() == IsolationEnforcedSandbox
	if policy, ok := r.executor.(sandboxEnvironmentPolicy); ok && policy.RequiresSanitizedEnvironment() {
		requiresSanitized = true
	}
	if !requiresSanitized {
		return os.Environ(), nil
	}

	systemRoot := strings.TrimSpace(os.Getenv("SystemRoot"))
	if systemRoot == "" {
		systemRoot = strings.TrimSpace(os.Getenv("WINDIR"))
	}
	if systemRoot == "" {
		return nil, errors.New("Windows SystemRoot is required for sandboxed command execution")
	}
	profileRoot := filepath.Join(r.root, ".mar", "runtime", "profile")
	tempRoot := filepath.Join(r.root, ".mar", "runtime", "tmp")
	appData := filepath.Join(profileRoot, "AppData", "Roaming")
	localAppData := filepath.Join(profileRoot, "AppData", "Local")
	for _, dir := range []string{profileRoot, tempRoot, appData, localAppData} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	pathValue := strings.Join([]string{
		filepath.Dir(commandPath),
		filepath.Join(systemRoot, "System32"),
		systemRoot,
	}, string(os.PathListSeparator))
	return []string{
		"SystemRoot=" + systemRoot,
		"WINDIR=" + systemRoot,
		"ComSpec=" + filepath.Join(systemRoot, "System32", "cmd.exe"),
		"PATH=" + pathValue,
		"PATHEXT=.COM;.EXE;.BAT;.CMD",
		"USERPROFILE=" + profileRoot,
		"HOME=" + profileRoot,
		"APPDATA=" + appData,
		"LOCALAPPDATA=" + localAppData,
		"TEMP=" + tempRoot,
		"TMP=" + tempRoot,
		"PWD=" + r.root,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
	}, nil
}

func validateCommand(cmd Command) error {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(cmd.Name)))
	if name == "" {
		return errors.New("command name is required")
	}
	switch name {
	case "git", "git.exe":
		return errors.New("Git is available only through typed git_status/git_diff tools")
	case "go", "go.exe":
		if len(cmd.Args) == 0 {
			return errors.New("go subcommand is required")
		}
		sub := strings.ToLower(cmd.Args[0])
		if sub != "test" && sub != "vet" && sub != "build" && sub != "fmt" {
			return fmt.Errorf("go subcommand %q is not allowed by coding ACI", sub)
		}
		for _, arg := range cmd.Args[1:] {
			lower := strings.ToLower(arg)
			if strings.HasPrefix(lower, "-exec") || strings.HasPrefix(lower, "-toolexec") || lower == "-o" || strings.HasPrefix(lower, "-o=") {
				return fmt.Errorf("go argument %q is not allowed by coding ACI", arg)
			}
		}
	case "gofmt", "gofmt.exe":
		for _, arg := range cmd.Args {
			if strings.HasPrefix(arg, "-") && arg != "-w" && arg != "-d" {
				return fmt.Errorf("gofmt argument %q is not allowed", arg)
			}
		}
	default:
		return fmt.Errorf("command %q is not allowed by coding ACI", cmd.Name)
	}
	return nil
}

func boundText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
