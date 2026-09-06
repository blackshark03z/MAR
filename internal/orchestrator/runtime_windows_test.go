//go:build windows

package orchestrator

import (
	"testing"

	"mar/internal/processctl"
	"mar/internal/resourcegov"
)

func TestDefaultWorkerProcessLimitsRespectExplicitConfiguration(t *testing.T) {
	explicit := processctl.Limits{CPUHardCapBasisPoints: 4_000, JobMemoryBytes: 512 << 20, MaxActiveProcesses: 7}
	got, err := defaultWorkerProcessLimits(explicit, resourcegov.Config{MaxCPUPercent: 85, MinFreeRAMBytes: 1}, DaemonConfig{MaxConcurrentWorkers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got != explicit {
		t.Fatalf("explicit worker process envelope was rewritten: got=%+v want=%+v", got, explicit)
	}
}

func TestExecutionRAMReservationUsesConfiguredEstimateInsteadOfHardJobCap(t *testing.T) {
	configuredEstimate := uint64(256 << 20)
	hardJobCap := uint64(8 << 30)
	if got := executionRAMReservation(0, configuredEstimate); got != configuredEstimate {
		t.Fatalf("default execution RAM reservation mismatch: got=%d want=%d", got, configuredEstimate)
	} else if got == hardJobCap {
		t.Fatal("execution admission incorrectly reserved the entire hard Job memory ceiling")
	}
	explicit := uint64(384 << 20)
	if got := executionRAMReservation(explicit, configuredEstimate); got != explicit {
		t.Fatalf("explicit execution RAM reservation was overwritten: got=%d want=%d", got, explicit)
	}
}

func TestDefaultWorkerProcessLimitsDeriveHostBoundedEnvelope(t *testing.T) {
	governor := resourcegov.Config{MaxCPUPercent: 85, MinFreeRAMBytes: 1 << 20}
	got, err := defaultWorkerProcessLimits(processctl.Limits{}, governor, DaemonConfig{MaxConcurrentWorkers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.CPUHardCapBasisPoints != 4_250 {
		t.Fatalf("per-worker CPU cap was not derived from aggregate governor budget: %+v", got)
	}
	if got.JobMemoryBytes == 0 || got.MaxActiveProcesses == 0 {
		t.Fatalf("host-derived hard envelope is incomplete: %+v", got)
	}
}
