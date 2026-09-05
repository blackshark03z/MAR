package resourcegov

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type fakeSensor struct {
	mu       sync.Mutex
	snapshot Snapshot
	err      error
}

func (f *fakeSensor) Snapshot(context.Context) (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot, f.err
}

func testConfig() Config {
	return Config{
		MaxCPUPercent:           80,
		MaxMemoryLoadPercent:    80,
		MaxIOPressurePercent:    90,
		MinFreeRAMBytes:         100,
		MinFreeDiskBytes:        1000,
		MaxMARDiskBytes:         10_000,
		MaxHeavyJobs:            2,
		MaxHeavyJobsPerProject:  1,
		MaxHeavyJobsInteractive: 1,
	}
}

func healthySnapshot() Snapshot {
	return Snapshot{
		CPUPercent:        10,
		CPUKnown:          true,
		MemoryLoadPercent: 20,
		TotalRAMBytes:     10_000,
		AvailableRAMBytes: 8_000,
		FreeDiskBytes:     20_000,
		TotalDiskBytes:    100_000,
		MARDiskUsedBytes:  1_000,
	}
}

func claim(id, project string, heavy bool) Claim {
	class := WorkloadModelWait
	if heavy {
		class = WorkloadBuild
	}
	return Claim{ID: id, ProjectID: project, Class: class, Heavy: heavy}
}

func TestDiskReserveDeniesBeforeHostExhaustion(t *testing.T) {
	s := &fakeSensor{snapshot: healthySnapshot()}
	s.snapshot.FreeDiskBytes = 1500
	g, err := New(s, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	c := claim("a", "p", true)
	c.DiskBytes = 600
	lease, decision, err := g.TryAcquire(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if lease != nil || decision.Allowed || !hasReason(decision, DenyHostDiskReserve) {
		t.Fatalf("expected hard disk-reserve denial, decision=%+v", decision)
	}
}

func TestMARDiskBudgetIncludesReservations(t *testing.T) {
	s := &fakeSensor{snapshot: healthySnapshot()}
	cfg := testConfig()
	cfg.MaxMARDiskBytes = 2_000
	g, _ := New(s, cfg)
	first := claim("a", "p1", false)
	first.DiskBytes = 400
	lease, decision, err := g.TryAcquire(context.Background(), first)
	if err != nil || !decision.Allowed {
		t.Fatalf("first claim: lease=%v decision=%+v err=%v", lease, decision, err)
	}
	defer lease.Release()
	second := claim("b", "p2", false)
	second.DiskBytes = 700
	_, decision, err = g.TryAcquire(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed || !hasReason(decision, DenyMARDiskBudget) {
		t.Fatalf("expected MAR disk budget denial: %+v", decision)
	}
}

func TestMemoryPressureBlocksHeavyButAllowsLightweightWork(t *testing.T) {
	s := &fakeSensor{snapshot: healthySnapshot()}
	s.snapshot.MemoryLoadPercent = 95
	g, _ := New(s, testConfig())
	if _, decision, _ := g.TryAcquire(context.Background(), claim("heavy", "p1", true)); decision.Allowed || !hasReason(decision, DenyMemoryPressure) {
		t.Fatalf("heavy work should be denied: %+v", decision)
	}
	lease, decision, err := g.TryAcquire(context.Background(), claim("light", "p1", false))
	if err != nil || !decision.Allowed || lease == nil {
		t.Fatalf("light work should remain admissible: lease=%v decision=%+v err=%v", lease, decision, err)
	}
	lease.Release()
}

func TestCPUAndIOPressureOnlyThrottleHeavyAdmission(t *testing.T) {
	s := &fakeSensor{snapshot: healthySnapshot()}
	s.snapshot.CPUPercent = 95
	s.snapshot.IOPressureKnown = true
	s.snapshot.IOPressurePercent = 95
	g, _ := New(s, testConfig())
	_, decision, _ := g.TryAcquire(context.Background(), claim("heavy", "p1", true))
	if decision.Allowed || !hasReason(decision, DenyCPUPressure) || !hasReason(decision, DenyIOPressure) {
		t.Fatalf("heavy work should be pressure-throttled: %+v", decision)
	}
	lease, decision, _ := g.TryAcquire(context.Background(), claim("light", "p1", false))
	if !decision.Allowed || lease == nil {
		t.Fatalf("lightweight work should pass CPU/I/O pressure: %+v", decision)
	}
	lease.Release()
}

func TestHeavyCapacityAndProjectFairness(t *testing.T) {
	s := &fakeSensor{snapshot: healthySnapshot()}
	cfg := testConfig()
	cfg.MaxHeavyJobs = 3
	cfg.MaxHeavyJobsPerProject = 1
	cfg.MaxHeavyJobsInteractive = 0
	g, _ := New(s, cfg)
	lease, decision, _ := g.TryAcquire(context.Background(), claim("a", "project-a", true))
	if !decision.Allowed {
		t.Fatalf("first heavy claim denied: %+v", decision)
	}
	defer lease.Release()
	_, sameProject, _ := g.TryAcquire(context.Background(), claim("b", "project-a", true))
	if sameProject.Allowed || !hasReason(sameProject, DenyProjectHeavyCapacity) {
		t.Fatalf("same project should be capped: %+v", sameProject)
	}
	other, otherDecision, _ := g.TryAcquire(context.Background(), claim("c", "project-b", true))
	if !otherDecision.Allowed || other == nil {
		t.Fatalf("other project should receive fair heavy capacity: %+v", otherDecision)
	}
	other.Release()
}

func TestInteractiveUseReducesHeavyCapacity(t *testing.T) {
	s := &fakeSensor{snapshot: healthySnapshot()}
	s.snapshot.UserInteractive = true
	cfg := testConfig()
	cfg.MaxHeavyJobs = 3
	cfg.MaxHeavyJobsPerProject = 3
	cfg.MaxHeavyJobsInteractive = 1
	g, _ := New(s, cfg)
	first, d, _ := g.TryAcquire(context.Background(), claim("a", "p", true))
	if !d.Allowed {
		t.Fatalf("first interactive heavy claim should fit: %+v", d)
	}
	defer first.Release()
	_, d, _ = g.TryAcquire(context.Background(), claim("b", "p", true))
	if d.Allowed || !hasReason(d, DenyInteractiveCapacity) {
		t.Fatalf("second interactive heavy claim should wait: %+v", d)
	}
}

func TestReleaseIsIdempotentAndReturnsCapacity(t *testing.T) {
	s := &fakeSensor{snapshot: healthySnapshot()}
	cfg := testConfig()
	cfg.MaxHeavyJobs = 1
	cfg.MaxHeavyJobsPerProject = 1
	cfg.MaxHeavyJobsInteractive = 0
	g, _ := New(s, cfg)
	lease, d, _ := g.TryAcquire(context.Background(), claim("a", "p1", true))
	if !d.Allowed {
		t.Fatal(d)
	}
	if _, d, _ := g.TryAcquire(context.Background(), claim("b", "p2", true)); d.Allowed {
		t.Fatal("second heavy claim should be denied while lease is active")
	}
	lease.Release()
	lease.Release()
	second, d, _ := g.TryAcquire(context.Background(), claim("b", "p2", true))
	if !d.Allowed || second == nil {
		t.Fatalf("capacity not returned after release: %+v", d)
	}
	second.Release()
}

func TestConcurrentHeavyAdmissionNeverExceedsCapacity(t *testing.T) {
	s := &fakeSensor{snapshot: healthySnapshot()}
	cfg := testConfig()
	cfg.MaxHeavyJobs = 2
	cfg.MaxHeavyJobsPerProject = 2
	cfg.MaxHeavyJobsInteractive = 0
	g, _ := New(s, cfg)

	const n = 12
	var wg sync.WaitGroup
	wg.Add(n)
	leases := make(chan *Lease, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			l, d, err := g.TryAcquire(context.Background(), claim(fmtID(i), "p", true))
			if err == nil && d.Allowed && l != nil {
				leases <- l
			}
		}(i)
	}
	wg.Wait()
	close(leases)
	count := 0
	for l := range leases {
		count++
		defer l.Release()
	}
	if count != 2 {
		t.Fatalf("expected exactly 2 admitted heavy claims, got %d", count)
	}
}

func TestSensorFailureFailsClosed(t *testing.T) {
	s := &fakeSensor{err: errors.New("sensor unavailable")}
	g, _ := New(s, testConfig())
	lease, _, err := g.TryAcquire(context.Background(), claim("a", "p", true))
	if err == nil || lease != nil {
		t.Fatalf("sensor failure must not admit work: lease=%v err=%v", lease, err)
	}
}

func hasReason(d Decision, want DenialReason) bool {
	for _, got := range d.Reasons {
		if got == want {
			return true
		}
	}
	return false
}

func fmtID(i int) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if i < len(digits) {
		return "claim-" + string(digits[i])
	}
	return "claim-x"
}
