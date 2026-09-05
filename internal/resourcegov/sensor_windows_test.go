//go:build windows

package resourcegov

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
