package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"mar/internal/agent"
	"mar/internal/domain"
	"mar/internal/mcpedge"
	"mar/internal/orchestrator"
	"mar/internal/processctl"
	"mar/internal/resourcegov"
	"mar/internal/scheduler"
	"mar/internal/service"
	"mar/internal/store"
	"mar/internal/verification"
	"mar/internal/worker"
)

const defaultDB = ".mar/mar.db"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "init":
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		dbPath := fs.String("db", defaultDB, "SQLite database path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		s, err := store.Open(*dbPath)
		if err != nil {
			return err
		}
		defer s.Close()
		return printJSON(map[string]any{"status": "initialized", "db": *dbPath})

	case "project-add":
		fs := flag.NewFlagSet("project-add", flag.ContinueOnError)
		dbPath := fs.String("db", defaultDB, "SQLite database path")
		id := fs.String("id", "", "Project id")
		root := fs.String("root", "", "Project root")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		s, svc, err := openService(*dbPath)
		if err != nil {
			return err
		}
		defer s.Close()
		project, created, err := svc.RegisterProject(ctx, *id, *root)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"created": created, "project": project})

	case "submit":
		fs := flag.NewFlagSet("submit", flag.ContinueOnError)
		dbPath := fs.String("db", defaultDB, "SQLite database path")
		key := fs.String("key", "", "Idempotency key")
		file := fs.String("file", "", "Goal Contract JSON file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *file == "" {
			return errors.New("-file is required")
		}
		payload, err := os.ReadFile(filepath.Clean(*file))
		if err != nil {
			return fmt.Errorf("read goal contract: %w", err)
		}
		payload = bytes.TrimPrefix(payload, []byte{0xEF, 0xBB, 0xBF})
		var contract domain.GoalContract
		if err := json.Unmarshal(payload, &contract); err != nil {
			return fmt.Errorf("decode goal contract: %w", err)
		}
		s, svc, err := openService(*dbPath)
		if err != nil {
			return err
		}
		defer s.Close()
		task, created, err := svc.Submit(ctx, *key, contract)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"created": created, "task": task})

	case "status":
		fs := flag.NewFlagSet("status", flag.ContinueOnError)
		dbPath := fs.String("db", defaultDB, "SQLite database path")
		id := fs.String("task", "", "Task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		s, svc, err := openService(*dbPath)
		if err != nil {
			return err
		}
		defer s.Close()
		task, err := svc.Status(ctx, *id)
		if err != nil {
			return err
		}
		return printJSON(task)

	case "mcp-stdio":
		fs := flag.NewFlagSet("mcp-stdio", flag.ContinueOnError)
		dbPath := fs.String("db", defaultDB, "SQLite database path")
		dataRoot := fs.String("data-root", ".mar", "MAR managed data root")
		brainMode := fs.String("brain", envOrDefault("MAR_BRAIN_MODE", string(worker.BrainProvider)), "coding brain mode: provider or web")
		providerBaseURL := fs.String("provider-base-url", os.Getenv("MAR_MODEL_BASE_URL"), "OpenAI-compatible model provider base URL (provider brain mode)")
		apiKeyEnv := fs.String("api-key-env", envOrDefault("MAR_MODEL_API_KEY_ENV", "OPENAI_API_KEY"), "environment variable containing the model provider API key (provider brain mode)")
		modelName := fs.String("model", os.Getenv("MAR_MODEL"), "agent model name; web mode defaults to gpt-5.6-sol")
		reasoning := fs.String("reasoning", envOrDefault("MAR_REASONING_EFFORT", "high"), "agent reasoning effort")
		goPath := fs.String("go", defaultGoExecutable(), "Go executable used by the built-in go-standard verification profile")
		maxWorkers := fs.Int("max-workers", 2, "maximum concurrent MAR worker processes")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runMCPRuntime(ctx, mcpRuntimeOptions{
			DBPath:          *dbPath,
			DataRoot:        *dataRoot,
			BrainMode:       *brainMode,
			ProviderBaseURL: *providerBaseURL,
			APIKeyEnv:       *apiKeyEnv,
			Model:           *modelName,
			Reasoning:       *reasoning,
			GoPath:          *goPath,
			MaxWorkers:      *maxWorkers,
		})

	case "worker-run":
		return worker.RunChild(ctx, os.Stdin, os.Stdout)

	case "sandbox-host-check":
		fs := flag.NewFlagSet("sandbox-host-check", flag.ContinueOnError)
		workspace := fs.String("workspace", ".", "Workspace used for the AppContainer readiness probe")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := processctl.CheckSandboxHostReady(ctx, *workspace); err != nil {
			return err
		}
		return printJSON(map[string]any{"sandbox_host_ready": true, "workspace": *workspace})

	case "sandbox-host-prepare":
		fs := flag.NewFlagSet("sandbox-host-prepare", flag.ContinueOnError)
		workspace := fs.String("workspace", ".", "Workspace used to verify the AppContainer prerequisite after preparation")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := processctl.PrepareSandboxHost(); err != nil {
			return err
		}
		if err := processctl.CheckSandboxHostReady(ctx, *workspace); err != nil {
			return err
		}
		return printJSON(map[string]any{"sandbox_host_ready": true, "prepared": true, "workspace": *workspace})

	default:
		return usage()
	}
}

type mcpRuntimeOptions struct {
	DBPath          string
	DataRoot        string
	BrainMode       string
	ProviderBaseURL string
	APIKeyEnv       string
	Model           string
	Reasoning       string
	GoPath          string
	MaxWorkers      int
}

const defaultWorkerInstructions = `You are the bounded MAR coding worker for one immutable Goal Contract. Work only inside the assigned task workspace and granted authority. Inspect relevant context before editing. Use the provided coding tools for reads, writes, Git inspection and allowed commands. Never push, deploy, widen the Goal Contract, or mutate authoritative integration state. Checkpoint meaningful progress. Finish only with finish_task using completed_candidate, blocked, cancelled, or budget_exhausted; completed_candidate means ready for MAR verification, not verified or integrated.`

func runMCPRuntime(ctx context.Context, opts mcpRuntimeOptions) error {
	if opts.MaxWorkers <= 0 {
		return errors.New("max-workers must be positive")
	}
	brainMode := worker.BrainMode(strings.ToLower(strings.TrimSpace(opts.BrainMode)))
	if brainMode == "" {
		brainMode = worker.BrainProvider
	}
	switch brainMode {
	case worker.BrainProvider:
		if strings.TrimSpace(opts.ProviderBaseURL) == "" || strings.TrimSpace(opts.APIKeyEnv) == "" || strings.TrimSpace(opts.Model) == "" {
			return errors.New("provider brain mode requires provider-base-url, api-key-env and model")
		}
	case worker.BrainWeb:
		if strings.TrimSpace(opts.Model) == "" {
			opts.Model = "gpt-5.6-sol"
		}
	default:
		return errors.New("brain mode must be provider or web")
	}
	dataRoot, err := filepath.Abs(opts.DataRoot)
	if err != nil {
		return err
	}
	if err := prepareRuntimeDataRoot(dataRoot); err != nil {
		return fmt.Errorf("prepare MAR data root: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	goExecutable, err := resolveExecutable(opts.GoPath)
	if err != nil {
		return fmt.Errorf("resolve Go verification toolchain: %w", err)
	}
	goRoot := filepath.Dir(filepath.Dir(goExecutable))
	goBin := filepath.Dir(goExecutable)
	goModuleProxyDir, err := resolveGoModuleProxyDir(goExecutable, dataRoot)
	if err != nil {
		return fmt.Errorf("resolve read-only Go module proxy seed: %w", err)
	}
	s, err := store.Open(opts.DBPath)
	if err != nil {
		return err
	}
	defer s.Close()
	runtime, err := orchestrator.NewRuntime(s, orchestrator.RuntimeConfig{
		DataRoot:   dataRoot,
		Executable: executable,
		Provider: worker.ProviderConfig{
			BrainMode:      brainMode,
			BaseURL:        opts.ProviderBaseURL,
			APIKeyEnv:      opts.APIKeyEnv,
			RequestTimeout: 2 * time.Minute,
		},
		AgentProfile: agent.Profile{
			Model:            opts.Model,
			ReasoningEffort:  opts.Reasoning,
			BaseInstructions: defaultWorkerInstructions,
		},
		VerificationProfiles: builtinVerificationProfiles(goExecutable),
		SandboxReadPaths:     []string{goRoot, goModuleProxyDir},
		WorkerPathEntries:    []string{goBin},
		GoModuleCache:        goModuleProxyDir,
		LeaseDuration:        time.Minute,
		WorkerStopTimeout:    10 * time.Second,
		ResourceGovernor: resourcegov.Config{
			MaxCPUPercent:           85,
			MaxMemoryLoadPercent:    85,
			MaxIOPressurePercent:    90,
			MinFreeRAMBytes:         1 << 30,
			MinFreeDiskBytes:        2 << 30,
			MaxMARDiskBytes:         20 << 30,
			MaxHeavyJobs:            opts.MaxWorkers,
			MaxHeavyJobsPerProject:  opts.MaxWorkers,
			MaxHeavyJobsInteractive: 1,
		},
		Scheduler: scheduler.Config{
			AgingInterval:            5 * time.Minute,
			WorkspaceRAMReservation:  256 << 20,
			WorkspaceDiskReservation: 256 << 20,
		},
		Daemon: orchestrator.DaemonConfig{
			PollInterval:         250 * time.Millisecond,
			ControlPollInterval:  200 * time.Millisecond,
			MaxConcurrentWorkers: opts.MaxWorkers,
			MaxPreflightPerTick:  8,
			ErrorSink: func(err error) {
				fmt.Fprintln(os.Stderr, "daemon:", err)
			},
		},
	})
	if err != nil {
		return err
	}

	runtimeCtx, cancelRuntime := context.WithCancel(ctx)
	defer cancelRuntime()
	daemonDone := make(chan error, 1)
	go func() {
		err := runDaemonAuthority(runtimeCtx, opts.DBPath, runtime.Daemon)
		daemonDone <- err
		if err != nil && !errors.Is(err, context.Canceled) {
			cancelRuntime()
		}
	}()

	mcpErr := mcpedge.RunStdio(runtimeCtx, runtime.Service)
	if mcpErr == nil {
		// Stdio EOF is a client disconnect, not permission to kill a mutation-
		// capable task. Let already-active workers reach a bounded terminal point.
		if err := waitForActiveWorkers(ctx, runtime.Daemon); err != nil {
			cancelRuntime()
			<-daemonDone
			return err
		}
	}
	cancelRuntime()
	daemonErr := <-daemonDone
	if mcpErr != nil && !errors.Is(mcpErr, context.Canceled) {
		return mcpErr
	}
	if daemonErr != nil && !errors.Is(daemonErr, context.Canceled) {
		return daemonErr
	}
	return nil
}

type daemonAuthorityRunner interface {
	Run(context.Context) error
}

func runDaemonAuthority(ctx context.Context, dbPath string, daemon daemonAuthorityRunner) error {
	if daemon == nil {
		return errors.New("daemon authority requires a daemon")
	}
	if strings.TrimSpace(dbPath) == "" {
		return errors.New("daemon authority requires a database path")
	}
	lockPath, err := filepath.Abs(dbPath + ".daemon.lock")
	if err != nil {
		return err
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		lease, acquired, err := processctl.TryAcquireExclusiveFileLease(lockPath)
		if err != nil {
			return err
		}
		if acquired {
			defer lease.Close()
			return daemon.Run(ctx)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitForActiveWorkers(ctx context.Context, daemon *orchestrator.Daemon) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for daemon.ActiveCount() != 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}

func prepareRuntimeDataRoot(path string) error {
	return os.MkdirAll(path, 0o755)
}

func builtinVerificationProfiles(goExecutable string) []verification.Profile {
	return []verification.Profile{
		goStandardVerificationProfile(goExecutable),
		goDocsVerificationProfile(goExecutable),
	}
}

func goStandardVerificationProfile(goExecutable string) verification.Profile {
	return verification.Profile{
		ID: "go-standard",
		Commands: []verification.Command{
			{Name: goExecutable, Args: []string{"test", "-p", "1", "-count=1", "-timeout", "180s", "./..."}, Cwd: "."},
			{Name: goExecutable, Args: []string{"vet", "-p", "1", "./..."}, Cwd: "."},
			{Name: goExecutable, Args: []string{"build", "-p", "1", "./..."}, Cwd: "."},
		},
	}
}

func goDocsVerificationProfile(goExecutable string) verification.Profile {
	return verification.Profile{
		ID: "go-docs",
		Commands: []verification.Command{
			{Name: goExecutable, Args: []string{"test", "-p", "1", "-count=1", "-run", "^$", "-timeout", "180s", "./..."}, Cwd: "."},
			{Name: goExecutable, Args: []string{"vet", "-p", "1", "./..."}, Cwd: "."},
			{Name: goExecutable, Args: []string{"build", "-p", "1", "./..."}, Cwd: "."},
		},
	}
}

func resolveGoModuleProxyDir(goExecutable, dataRoot string) (string, error) {
	cmd := exec.Command(goExecutable, "env", "GOMODCACHE")
	out, err := cmd.Output()
	if err == nil {
		root := strings.TrimSpace(string(out))
		if root != "" {
			download := filepath.Join(root, "cache", "download")
			if info, statErr := os.Stat(download); statErr == nil && info.IsDir() {
				abs, absErr := filepath.Abs(download)
				if absErr != nil {
					return "", absErr
				}
				return filepath.Clean(abs), nil
			}
		}
	}
	// Offline verification cannot manufacture dependencies that are absent from
	// every local cache. Keep an empty MAR-owned proxy directory as a bounded,
	// read-only fallback; Go then fails explicitly if a required module is not
	// preseeded instead of gaining network or shared-cache write authority.
	fallback := filepath.Join(dataRoot, "runtime", "gomodproxy")
	if err := os.MkdirAll(fallback, 0o755); err != nil {
		return "", err
	}
	return filepath.Clean(fallback), nil
}

func defaultGoExecutable() string {
	portable := filepath.Join(".mar", "runtime", "go-portable", "go", "bin", "go.exe")
	if abs, err := filepath.Abs(portable); err == nil {
		if _, statErr := os.Stat(abs); statErr == nil {
			return abs
		}
	}
	return "go"
}

func resolveExecutable(name string) (string, error) {
	resolved, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func openService(dbPath string) (*store.SQLite, *service.TaskService, error) {
	s, err := store.Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	return s, service.NewTaskService(s), nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func usage() error {
	return errors.New("usage: mar <init|project-add|submit|status|mcp-stdio|sandbox-host-check|sandbox-host-prepare> [options]")
}
