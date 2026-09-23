//go:build windows

package orchestrator

import (
	"os"
	"strings"
	"testing"

	"mar/internal/processctl"
	"mar/internal/resourcegov"
)

func TestWorkerEnvironmentIsFailClosedAndExplicit(t *testing.T) {
	env := []string{
		`SystemRoot=C:\Windows`,
		`TEMP=C:\Temp`,
		`PATH=C:\Host\Bin`,
		`MAR_AMBIENT_SECRET=must-not-cross`,
		`MAR_PROVIDER_KEY=provider-secret`,
	}
	got, err := workerEnvironment(env, []string{`D:\Go\bin`}, "MAR_PROVIDER_KEY", []string{"MAR_RUNTIME_E2E_WORKER=1"})
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string, len(got))
	for _, item := range got {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			t.Fatalf("invalid worker environment entry %q", item)
		}
		values[strings.ToUpper(key)] = value
	}
	if _, ok := values["MAR_AMBIENT_SECRET"]; ok {
		t.Fatalf("ambient secret crossed worker launch boundary: %v", got)
	}
	if values["MAR_PROVIDER_KEY"] != "provider-secret" {
		t.Fatalf("explicit provider key was not projected: %v", got)
	}
	if values["MAR_RUNTIME_E2E_WORKER"] != "1" {
		t.Fatalf("explicit worker extra was not projected: %v", got)
	}
	wantPath := `D:\Go\bin` + string(os.PathListSeparator) + `C:\Host\Bin`
	if values["PATH"] != wantPath {
		t.Fatalf("worker PATH mismatch: got=%q want=%q", values["PATH"], wantPath)
	}
	if values["SYSTEMROOT"] != `C:\Windows` || values["TEMP"] != `C:\Temp` {
		t.Fatalf("safe Windows runtime environment was not preserved: %v", got)
	}
}

func TestWorkerEnvironmentRejectsImplicitOverrides(t *testing.T) {
	if _, err := workerEnvironment([]string{`TEMP=C:\Temp`}, nil, "", []string{`TEMP=C:\Override`}); err == nil {
		t.Fatal("explicit extras must not override protected worker environment keys")
	}
	if _, err := workerEnvironment(nil, nil, "", []string{"BROKEN"}); err == nil {
		t.Fatal("malformed worker environment extra must fail closed")
	}
}

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
