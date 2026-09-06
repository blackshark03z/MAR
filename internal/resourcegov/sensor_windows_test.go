//go:build windows

package resourcegov

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPressureSnapshotBoundsSlowMARDiskScanAndFailsSafe(t *testing.T) {
	root := t.TempDir()
	sensor, err := NewWindowsSensor(WindowsSensorConfig{
		DiskPath:                 root,
		MARRoots:                 []string{root},
		InteractiveIdleThreshold: 2 * time.Minute,
		DiskUsageCacheTTL:        0,
	})
	if err != nil {
		t.Fatal(err)
	}
	releaseScan := make(chan struct{})
	defer close(releaseScan)
	sensor.marDiskUsageFn = func(context.Context) (uint64, error) {
		// Deliberately ignore cancellation to model a filesystem call such as
		// WalkDir/d.Info that cannot be interrupted by the callback context.
		<-releaseScan
		return 123, nil
	}
	started := time.Now()
	snapshot, err := sensor.PressureSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("slow MAR disk scan blocked pressure observation: %v", elapsed)
	}
	if snapshot.FreeDiskBytes == 0 || snapshot.TotalDiskBytes == 0 {
		t.Fatalf("pressure snapshot lost fast host disk reading: %+v", snapshot)
	}
	if snapshot.MARDiskUsedBytes != ^uint64(0) {
		t.Fatalf("unknown MAR disk scan did not fail safe: %+v", snapshot)
	}
	governor, err := New(sensor, Config{MaxCPUPercent: 100, MaxMemoryLoadPercent: 100, MaxIOPressurePercent: 100, MinFreeRAMBytes: 1, MinFreeDiskBytes: 1, MaxMARDiskBytes: 1 << 30, MaxHeavyJobs: 1, MaxHeavyJobsPerProject: 1, MaxHeavyJobsInteractive: 1})
	if err != nil {
		t.Fatal(err)
	}
	decisionStarted := time.Now()
	decision, err := governor.Pressure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(decisionStarted); elapsed > 2*time.Second {
		t.Fatalf("non-cooperative MAR disk scan blocked pressure decision: %v", elapsed)
	}
	if !containsDenialReason(decision.Reasons, DenyMARDiskBudget) {
		t.Fatalf("scan uncertainty suppressed disk emergency decision: %+v", decision)
	}
}

func containsDenialReason(reasons []DenialReason, want DenialReason) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func TestWindowsSensorReportsHostAndMARDiskUsage(t *testing.T) {
	root := t.TempDir()
	payload := make([]byte, 4096)
	if err := os.WriteFile(filepath.Join(root, "artifact.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	sensor, err := NewWindowsSensor(WindowsSensorConfig{
		DiskPath:                 root,
		MARRoots:                 []string{root},
		InteractiveIdleThreshold: 2 * time.Minute,
		DiskUsageCacheTTL:        0,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := sensor.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalRAMBytes == 0 || first.AvailableRAMBytes == 0 {
		t.Fatalf("invalid RAM snapshot: %+v", first)
	}
	if first.TotalDiskBytes == 0 || first.FreeDiskBytes == 0 {
		t.Fatalf("invalid disk snapshot: %+v", first)
	}
	if first.MARDiskUsedBytes < uint64(len(payload)) {
		t.Fatalf("MAR disk usage did not include test payload: %+v", first)
	}

	time.Sleep(60 * time.Millisecond)
	second, err := sensor.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !second.CPUKnown {
		t.Fatal("second CPU sample should have a delta")
	}
	if second.CPUPercent < 0 || second.CPUPercent > 100 {
		t.Fatalf("CPU percent out of range: %v", second.CPUPercent)
	}
}
