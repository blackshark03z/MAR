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
		providerBaseURL := fs.String("provider-base-url", os.Getenv("MAR_MODEL_BASE_URL"), "OpenAI-compatible model provider base URL")
		apiKeyEnv := fs.String("api-key-env", envOrDefault("MAR_MODEL_API_KEY_ENV", "OPENAI_API_KEY"), "environment variable containing the model provider API key")
		modelName := fs.String("model", os.Getenv("MAR_MODEL"), "agent model name")
		reasoning := fs.String("reasoning", envOrDefault("MAR_REASONING_EFFORT", "high"), "agent reasoning effort")
		goPath := fs.String("go", defaultGoExecutable(), "Go executable used by the built-in go-standard verification profile")
		maxWorkers := fs.Int("max-workers", 2, "maximum concurrent MAR worker processes")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runMCPRuntime(ctx, mcpRuntimeOptions{
			DBPath:          *dbPath,
			DataRoot:        *dataRoot,
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
	dataRoot, err := filepath.Abs(opts.DataRoot)
	if err != nil {
		return err
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
	sharedGoModCache := filepath.Join(dataRoot, "runtime", "gomodcache")
	if err := os.MkdirAll(sharedGoModCache, 0o755); err != nil {
		return fmt.Errorf("prepare shared Go module cache: %w", err)
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
			BaseURL:        opts.ProviderBaseURL,
			APIKeyEnv:      opts.APIKeyEnv,
			RequestTimeout: 2 * time.Minute,
		},
		AgentProfile: agent.Profile{
			Model:            opts.Model,
			ReasoningEffort:  opts.Reasoning,
			BaseInstructions: defaultWorkerInstructions,
		},
		VerificationProfiles: []verification.Profile{{
			ID: "go-standard",
			Commands: []verification.Command{
				{Name: goExecutable, Args: []string{"test", "-count=1", "-timeout", "180s", "./..."}, Cwd: "."},
				{Name: goExecutable, Args: []string{"vet", "./..."}, Cwd: "."},
				{Name: goExecutable, Args: []string{"build", "./..."}, Cwd: "."},
			},
		}},
		SandboxReadPaths:  []string{goRoot, sharedGoModCache},
		WorkerPathEntries: []string{goBin},
		GoModuleCache:     sharedGoModCache,
		LeaseDuration:     time.Minute,
		WorkerStopTimeout: 10 * time.Second,
		ResourceGovernor: resourcegov.Config{
			MaxCPUPercent:           85,
			MaxMemoryLoadPercent:    85,
			MaxIOPressurePercent:    90,
			MinFreeRAMBytes:         1 << 30,
			MinFreeDiskBytes:        2 << 30,
			MaxMARDiskBytes:         20 << 30,
			MaxHeavyJobs:            opts.MaxWorkers,
			MaxHeavyJobsPerProject:  1,
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
		err := runtime.Daemon.Run(runtimeCtx)
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
