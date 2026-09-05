package resourcegov

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type WorkloadClass string

const (
	WorkloadSearch          WorkloadClass = "SEARCH_CONTEXT"
	WorkloadModelWait       WorkloadClass = "MODEL_WAIT"
	WorkloadStaticAnalysis  WorkloadClass = "STATIC_ANALYSIS"
	WorkloadUnitTest        WorkloadClass = "UNIT_TEST"
	WorkloadBuild           WorkloadClass = "BUILD"
	WorkloadBrowser         WorkloadClass = "BROWSER_UI"
	WorkloadIntegrationTest WorkloadClass = "INTEGRATION_TEST"
)

type Claim struct {
	ID        string
	ProjectID string
	Class     WorkloadClass
	RAMBytes  uint64
	DiskBytes uint64
	Heavy     bool
	Priority  int
}

func (c Claim) validate() error {
	if c.ID == "" {
		return errors.New("resource claim id is required")
	}
	if c.ProjectID == "" {
		return errors.New("resource claim project id is required")
	}
	if c.Class == "" {
		return errors.New("resource claim workload class is required")
	}
	return nil
}

type Snapshot struct {
	ObservedAt        time.Time
	CPUPercent        float64
	CPUKnown          bool
	MemoryLoadPercent float64
	TotalRAMBytes     uint64
	AvailableRAMBytes uint64
	FreeDiskBytes     uint64
	TotalDiskBytes    uint64
	MARDiskUsedBytes  uint64
	UserInteractive   bool
	IOPressurePercent float64
	IOPressureKnown   bool
}

type Sensor interface {
	Snapshot(context.Context) (Snapshot, error)
}

type Config struct {
	MaxCPUPercent           float64
	MaxMemoryLoadPercent    float64
	MaxIOPressurePercent    float64
	MinFreeRAMBytes         uint64
	MinFreeDiskBytes        uint64
	MaxMARDiskBytes         uint64
	MaxHeavyJobs            int
	MaxHeavyJobsPerProject  int
	MaxHeavyJobsInteractive int
}

func (c Config) validate() error {
	if c.MaxCPUPercent <= 0 || c.MaxCPUPercent > 100 {
		return errors.New("max CPU percent must be in (0,100]")
	}
	if c.MaxMemoryLoadPercent <= 0 || c.MaxMemoryLoadPercent > 100 {
		return errors.New("max memory load percent must be in (0,100]")
	}
	if c.MaxIOPressurePercent < 0 || c.MaxIOPressurePercent > 100 {
		return errors.New("max I/O pressure percent must be in [0,100]")
	}
	if c.MaxHeavyJobs <= 0 {
		return errors.New("max heavy jobs must be positive")
	}
	if c.MaxHeavyJobsPerProject <= 0 {
		return errors.New("max heavy jobs per project must be positive")
	}
	if c.MaxHeavyJobsInteractive < 0 {
		return errors.New("interactive heavy-job limit cannot be negative")
	}
	return nil
}

type DenialReason string

const (
	DenyDuplicateClaim       DenialReason = "DUPLICATE_CLAIM"
	DenyCPUPressure          DenialReason = "CPU_PRESSURE"
	DenyMemoryPressure       DenialReason = "MEMORY_PRESSURE"
	DenyMemoryReserve        DenialReason = "MEMORY_RESERVE"
	DenyIOPressure           DenialReason = "IO_PRESSURE"
	DenyHostDiskReserve      DenialReason = "HOST_DISK_RESERVE"
	DenyMARDiskBudget        DenialReason = "MAR_DISK_BUDGET"
	DenyHeavyCapacity        DenialReason = "HEAVY_CAPACITY"
	DenyProjectHeavyCapacity DenialReason = "PROJECT_HEAVY_CAPACITY"
	DenyInteractiveCapacity  DenialReason = "INTERACTIVE_HEAVY_CAPACITY"
)

type Decision struct {
	Allowed                bool
	Reasons                []DenialReason
	Snapshot               Snapshot
	ActiveClaims           int
	ActiveHeavyJobs        int
	ProjectActiveHeavyJobs int
	ReservedRAMBytes       uint64
	ReservedDiskBytes      uint64
}

type Governor struct {
	sensor Sensor
	cfg    Config

	mu     sync.Mutex
	active map[string]Claim
}

func New(sensor Sensor, cfg Config) (*Governor, error) {
	if sensor == nil {
		return nil, errors.New("resource sensor is required")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Governor{sensor: sensor, cfg: cfg, active: make(map[string]Claim)}, nil
}

func (g *Governor) TryAcquire(ctx context.Context, claim Claim) (*Lease, Decision, error) {
	if err := claim.validate(); err != nil {
		return nil, Decision{}, err
	}
	snapshot, err := g.sensor.Snapshot(ctx)
	if err != nil {
		return nil, Decision{}, fmt.Errorf("resource snapshot: %w", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	decision := g.evaluateLocked(snapshot, claim)
	if !decision.Allowed {
		return nil, decision, nil
	}
	g.active[claim.ID] = claim
	return &Lease{governor: g, claimID: claim.ID}, decision, nil
}

func (g *Governor) Evaluate(ctx context.Context, claim Claim) (Decision, error) {
	if err := claim.validate(); err != nil {
		return Decision{}, err
	}
	snapshot, err := g.sensor.Snapshot(ctx)
	if err != nil {
		return Decision{}, fmt.Errorf("resource snapshot: %w", err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.evaluateLocked(snapshot, claim), nil
}

func (g *Governor) evaluateLocked(snapshot Snapshot, claim Claim) Decision {
	decision := Decision{Allowed: true, Snapshot: snapshot, ActiveClaims: len(g.active)}

	if _, exists := g.active[claim.ID]; exists {
		decision.Reasons = append(decision.Reasons, DenyDuplicateClaim)
	}

	for _, active := range g.active {
		decision.ReservedRAMBytes += active.RAMBytes
		decision.ReservedDiskBytes += active.DiskBytes
		if active.Heavy {
			decision.ActiveHeavyJobs++
			if active.ProjectID == claim.ProjectID {
				decision.ProjectActiveHeavyJobs++
			}
		}
	}

	// Physical reserves are hard regardless of workload class. Reservations are
	// deliberately conservative: an admitted task may not have reached its peak
	// yet, so MAR keeps its estimated remaining headroom reserved.
	requiredRAM := saturatingAdd(g.cfg.MinFreeRAMBytes, saturatingAdd(decision.ReservedRAMBytes, claim.RAMBytes))
	if snapshot.AvailableRAMBytes < requiredRAM {
		decision.Reasons = append(decision.Reasons, DenyMemoryReserve)
	}
	requiredDisk := saturatingAdd(g.cfg.MinFreeDiskBytes, saturatingAdd(decision.ReservedDiskBytes, claim.DiskBytes))
	if snapshot.FreeDiskBytes < requiredDisk {
		decision.Reasons = append(decision.Reasons, DenyHostDiskReserve)
	}
	if g.cfg.MaxMARDiskBytes > 0 {
		projected := saturatingAdd(snapshot.MARDiskUsedBytes, saturatingAdd(decision.ReservedDiskBytes, claim.DiskBytes))
		if projected > g.cfg.MaxMARDiskBytes {
			decision.Reasons = append(decision.Reasons, DenyMARDiskBudget)
		}
	}

	if claim.Heavy {
		if snapshot.CPUKnown && snapshot.CPUPercent >= g.cfg.MaxCPUPercent {
			decision.Reasons = append(decision.Reasons, DenyCPUPressure)
		}
		if snapshot.MemoryLoadPercent >= g.cfg.MaxMemoryLoadPercent {
			decision.Reasons = append(decision.Reasons, DenyMemoryPressure)
		}
		if snapshot.IOPressureKnown && g.cfg.MaxIOPressurePercent > 0 && snapshot.IOPressurePercent >= g.cfg.MaxIOPressurePercent {
			decision.Reasons = append(decision.Reasons, DenyIOPressure)
		}
		if decision.ActiveHeavyJobs >= g.cfg.MaxHeavyJobs {
			decision.Reasons = append(decision.Reasons, DenyHeavyCapacity)
		}
		if decision.ProjectActiveHeavyJobs >= g.cfg.MaxHeavyJobsPerProject {
			decision.Reasons = append(decision.Reasons, DenyProjectHeavyCapacity)
		}
		if snapshot.UserInteractive && g.cfg.MaxHeavyJobsInteractive > 0 && decision.ActiveHeavyJobs >= g.cfg.MaxHeavyJobsInteractive {
			decision.Reasons = append(decision.Reasons, DenyInteractiveCapacity)
		}
	}

	decision.Allowed = len(decision.Reasons) == 0
	return decision
}

func (g *Governor) Active() []Claim {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Claim, 0, len(g.active))
	for _, claim := range g.active {
		out = append(out, claim)
	}
	return out
}

func (g *Governor) release(id string) {
	g.mu.Lock()
	delete(g.active, id)
	g.mu.Unlock()
}

type Lease struct {
	governor *Governor
	claimID  string
	once     sync.Once
}

func (l *Lease) Release() {
	if l == nil || l.governor == nil {
		return
	}
	l.once.Do(func() { l.governor.release(l.claimID) })
}

func saturatingAdd(a, b uint64) uint64 {
	if ^uint64(0)-a < b {
		return ^uint64(0)
	}
	return a + b
}
