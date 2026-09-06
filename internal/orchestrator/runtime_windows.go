//go:build windows

package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"mar/internal/aci"
	"mar/internal/agent"
	"mar/internal/effects"
	"mar/internal/integration"
	"mar/internal/processctl"
	"mar/internal/resourcegov"
	"mar/internal/scheduler"
	"mar/internal/service"
	"mar/internal/store"
	"mar/internal/verification"
	"mar/internal/worker"
	"mar/internal/workspace"
)

type RuntimeConfig struct {
	DataRoot             string
	Executable           string
	WorkerArguments      []string
	Provider             worker.ProviderConfig
	AgentProfile         agent.Profile
	AgentConfig          agent.Config
	VerificationProfiles []verification.Profile
	SandboxReadPaths     []string
	WorkerPathEntries    []string
	GoModuleCache        string
	CommandTimeout       time.Duration
	LeaseDuration        time.Duration
	WorkerStopTimeout    time.Duration
	WorkerProcessLimits  processctl.Limits
	ResourceGovernor     resourcegov.Config
	Scheduler            scheduler.Config
	Daemon               DaemonConfig
}

type Runtime struct {
	Service *service.TaskService
	Daemon  *Daemon
}

func NewRuntime(s *store.SQLite, cfg RuntimeConfig) (*Runtime, error) {
	if s == nil {
		return nil, errors.New("runtime requires SQLite store")
	}
	if cfg.DataRoot == "" || cfg.Executable == "" {
		return nil, errors.New("runtime requires data root and executable")
	}
	if cfg.CommandTimeout <= 0 {
		// Self-hosting verification frequently starts from a task-local cold build
		// cache. Keep individual commands bounded, but do not make the previous
		// two-minute ACI fallback the orchestration-wide acceptance policy.
		cfg.CommandTimeout = 5 * time.Minute
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = time.Minute
	}
	if cfg.WorkerStopTimeout <= 0 {
		cfg.WorkerStopTimeout = 10 * time.Second
	}
	if len(cfg.VerificationProfiles) == 0 {
		return nil, errors.New("runtime requires at least one verification profile")
	}
	readPaths, err := normalizeExistingDirs(cfg.SandboxReadPaths)
	if err != nil {
		return nil, err
	}
	pathEntries, err := normalizeExistingDirs(cfg.WorkerPathEntries)
	if err != nil {
		return nil, err
	}
	goModuleCache := ""
	if strings.TrimSpace(cfg.GoModuleCache) != "" {
		cachePaths, err := normalizeExistingDirs([]string{cfg.GoModuleCache})
		if err != nil {
			return nil, err
		}
		goModuleCache = cachePaths[0]
		if !coveredByReadGrant(goModuleCache, readPaths) {
			return nil, errors.New("shared Go module cache requires an explicit sandbox read grant")
		}
	}

	taskService := service.NewTaskService(s)
	profiles, err := verification.NewRegistry(cfg.VerificationProfiles...)
	if err != nil {
		return nil, err
	}
	preflight, err := NewPreflight(s, taskService, profiles)
	if err != nil {
		return nil, err
	}
	workspaceManager, err := workspace.NewManager(s, cfg.DataRoot)
	if err != nil {
		return nil, err
	}
	sensor, err := resourcegov.NewWindowsSensor(resourcegov.WindowsSensorConfig{
		DiskPath:                 cfg.DataRoot,
		MARRoots:                 []string{cfg.DataRoot},
		InteractiveIdleThreshold: 2 * time.Minute,
		DiskUsageCacheTTL:        5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	governor, err := resourcegov.New(sensor, cfg.ResourceGovernor)
	if err != nil {
		return nil, err
	}
	schedulerEngine, err := scheduler.New(s, governor, workspaceManager, cfg.Scheduler)
	if err != nil {
		return nil, err
	}
	effectManager := effects.New(s)
	sealer, err := verification.NewCandidateSealer(s, effectManager)
	if err != nil {
		return nil, err
	}
	verifier, err := verification.NewVerifier(s, sealer, profiles)
	if err != nil {
		return nil, err
	}
	integrationManager, err := integration.NewManager(s, verifier)
	if err != nil {
		return nil, err
	}
	processLimits, err := defaultWorkerProcessLimits(cfg.WorkerProcessLimits, cfg.ResourceGovernor, cfg.Daemon)
	if err != nil {
		return nil, err
	}
	// JobMemoryBytes is a hard per-worker ceiling, not an estimate of normal
	// resident usage. Reserving the entire hard cap here double-counts the OS
	// envelope and can permanently deny launch after tiny host-RAM fluctuations.
	// Admission uses the configured workload estimate; aggregate Job caps and
	// the live pressure monitor remain the hard safety boundaries.
	cfg.Daemon.ExecutionRAMReservation = executionRAMReservation(cfg.Daemon.ExecutionRAMReservation, cfg.Scheduler.WorkspaceRAMReservation)
	if cfg.Daemon.ExecutionDiskReservation == 0 {
		cfg.Daemon.ExecutionDiskReservation = cfg.Scheduler.WorkspaceDiskReservation
	}
	processRunner, err := worker.NewProcessRunner(taskService, processctl.NewSupervisor(), worker.ProcessConfig{
		Executable:    cfg.Executable,
		Arguments:     append([]string{}, cfg.WorkerArguments...),
		Environment:   workerEnvironment(os.Environ(), pathEntries),
		LeaseDuration: cfg.LeaseDuration,
		StopTimeout:   cfg.WorkerStopTimeout,
		ProcessLimits: processLimits,
	})
	if err != nil {
		return nil, err
	}
	runtimeFactory := func(workspacePath, taskID string) (verification.CommandRuntime, error) {
		executor, err := aci.NewWindowsSandboxExecutorWithLimits(workspacePath, processLimits, readPaths...)
		if err != nil {
			return nil, err
		}
		gitBroker, err := aci.NewContainedGitBroker()
		if err != nil {
			return nil, err
		}
		return aci.New(aci.Config{Root: workspacePath, TaskID: taskID, GitBroker: gitBroker, GoModuleCache: goModuleCache, CommandTimeout: cfg.CommandTimeout}, executor)
	}
	taskRunner, err := NewTaskRunner(taskService, processRunner, verifier, integrationManager, runtimeFactory, TaskRunnerConfig{
		WorkerID:              "mar-worker",
		SupervisorID:          "mar-daemon",
		LeaseDuration:         cfg.LeaseDuration,
		Provider:              cfg.Provider,
		AgentProfile:          cfg.AgentProfile,
		AgentConfig:           cfg.AgentConfig,
		SandboxReadPaths:      append([]string{}, readPaths...),
		GoModuleCache:         goModuleCache,
		CommandTimeout:        cfg.CommandTimeout,
		FinalizationTimeout:   cfg.WorkerStopTimeout,
		MemoryPressurePercent: cfg.ResourceGovernor.MaxMemoryLoadPercent,
	})
	if err != nil {
		return nil, err
	}
	daemon, err := NewDaemon(s, taskService, preflight, schedulerEngine, taskRunner, integrationManager, governor, cfg.Daemon)
	if err != nil {
		return nil, err
	}
	return &Runtime{Service: taskService, Daemon: daemon}, nil
}

func executionRAMReservation(explicit, configuredEstimate uint64) uint64 {
	if explicit != 0 {
		return explicit
	}
	return configuredEstimate
}

func defaultWorkerProcessLimits(explicit processctl.Limits, governor resourcegov.Config, daemon DaemonConfig) (processctl.Limits, error) {
	limits := explicit
	workers := daemon.withDefaults().MaxConcurrentWorkers
	if limits.CPUHardCapBasisPoints == 0 {
		globalBasisPoints := uint32(governor.MaxCPUPercent * 100)
		if globalBasisPoints == 0 {
			globalBasisPoints = 1
		}
		if globalBasisPoints > 10_000 {
			globalBasisPoints = 10_000
		}
		basisPoints := globalBasisPoints / uint32(workers)
		if basisPoints == 0 {
			basisPoints = 1
		}
		limits.CPUHardCapBasisPoints = basisPoints
	}
	if limits.JobMemoryBytes == 0 {
		_, available, err := resourcegov.WindowsPhysicalMemoryBytes()
		if err != nil {
			return processctl.Limits{}, fmt.Errorf("derive worker memory envelope: %w", err)
		}
		if available <= governor.MinFreeRAMBytes {
			return processctl.Limits{}, errors.New("available host RAM does not exceed configured MAR reserve")
		}
		limits.JobMemoryBytes = (available - governor.MinFreeRAMBytes) / uint64(workers)
		if limits.JobMemoryBytes == 0 {
			return processctl.Limits{}, errors.New("derived worker memory envelope is zero")
		}
	}
	if limits.MaxActiveProcesses == 0 {
		// Go builds legitimately fan out compiler/linker subprocesses. Scale the
		// containment ceiling with host parallelism rather than freezing a machine-
		// independent process count; the benchmark can tune this baseline later.
		count := goruntime.NumCPU() * 4
		if count < 16 {
			count = 16
		}
		limits.MaxActiveProcesses = uint32(count)
	}
	if err := limits.Validate(); err != nil {
		return processctl.Limits{}, err
	}
	return limits, nil
}

func normalizeExistingDirs(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{})
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			return nil, err
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			if err != nil {
				return nil, err
			}
			return nil, errors.New("runtime path grant is not a directory")
		}
		clean := filepath.Clean(resolved)
		key := strings.ToLower(clean)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out, nil
}

func coveredByReadGrant(path string, grants []string) bool {
	for _, grant := range grants {
		rel, err := filepath.Rel(grant, path)
		if err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func workerEnvironment(env, pathEntries []string) []string {
	out := append([]string{}, env...)
	if len(pathEntries) == 0 {
		return out
	}
	prefix := strings.Join(pathEntries, string(os.PathListSeparator))
	for i, item := range out {
		key, value, ok := strings.Cut(item, "=")
		if ok && strings.EqualFold(key, "PATH") {
			if value != "" {
				prefix += string(os.PathListSeparator) + value
			}
			out[i] = "PATH=" + prefix
			return out
		}
	}
	return append(out, "PATH="+prefix)
}
