//go:build windows

package processctl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	winjob "github.com/kolesnikovae/go-winjob"
	"golang.org/x/sys/windows"
)

const recoveryMarkerSchema = "mar-attempt-job-recovery-v1"

type attemptRecoveryMarker struct {
	Schema  string     `json:"schema"`
	Attempt AttemptRef `json:"attempt"`
	JobName string     `json:"job_name"`
	Stage   string     `json:"stage"`
	ArmedAt time.Time  `json:"armed_at"`
}

func attemptRecoveryKey(ref AttemptRef) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", ref.TaskID, ref.AttemptID, ref.RunEpoch)))
	return hex.EncodeToString(sum[:16])
}

func attemptJobName(ref AttemptRef) string {
	return "MAR-attempt-" + attemptRecoveryKey(ref)
}

func (s *Supervisor) markerPath(ref AttemptRef) string {
	if s == nil || s.recoveryRoot == "" {
		return ""
	}
	return filepath.Join(s.recoveryRoot, attemptRecoveryKey(ref)+".json")
}

func (s *Supervisor) writeAssignedMarker(ref AttemptRef, jobName string) error {
	path := s.markerPath(ref)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("prepare attempt recovery marker root: %w", err)
	}
	marker := attemptRecoveryMarker{
		Schema:  recoveryMarkerSchema,
		Attempt: ref,
		JobName: jobName,
		Stage:   "ASSIGNED",
		ArmedAt: time.Now().UTC(),
	}
	payload, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("encode attempt recovery marker: %w", err)
	}
	tmp := path + fmt.Sprintf(".tmp-%d", os.Getpid())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create attempt recovery marker: %w", err)
	}
	writeErr := error(nil)
	if _, err := f.Write(append(payload, '\n')); err != nil {
		writeErr = err
	} else if err := f.Sync(); err != nil {
		writeErr = err
	}
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write attempt recovery marker: %w", writeErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close attempt recovery marker: %w", closeErr)
	}
	if _, err := os.Stat(path); err == nil {
		existing, readErr := s.readAssignedMarker(ref)
		_ = os.Remove(tmp)
		if readErr == nil && existing.JobName == jobName {
			return nil
		}
		return errors.New("attempt recovery marker already exists with incompatible identity")
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmp)
		return fmt.Errorf("inspect existing attempt recovery marker: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("promote attempt recovery marker: %w", err)
	}
	return nil
}

func (s *Supervisor) readAssignedMarker(ref AttemptRef) (attemptRecoveryMarker, error) {
	path := s.markerPath(ref)
	if path == "" {
		return attemptRecoveryMarker{}, os.ErrNotExist
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return attemptRecoveryMarker{}, err
	}
	var marker attemptRecoveryMarker
	if err := json.Unmarshal(payload, &marker); err != nil {
		return attemptRecoveryMarker{}, fmt.Errorf("decode attempt recovery marker: %w", err)
	}
	if marker.Schema != recoveryMarkerSchema || marker.Stage != "ASSIGNED" || marker.Attempt != ref || marker.JobName != attemptJobName(ref) {
		return attemptRecoveryMarker{}, errors.New("attempt recovery marker identity is invalid")
	}
	return marker, nil
}

// startContainedJob preserves the legacy anonymous-job path when no trusted
// recovery root is configured. Production uses a deterministic named Job
// Object. The recovery marker is written only after the suspended worker has
// been assigned to that Job Object, and before the worker is resumed. A crash
// before the marker therefore remains fail-closed; a marker never certifies an
// uncontained process.
func (s *Supervisor) startContainedJob(cmd *exec.Cmd, ref AttemptRef, limits Limits) (*winjob.JobObject, error) {
	if s == nil || s.recoveryRoot == "" {
		return winjob.Start(cmd, windowsJobLimits(limits)...)
	}
	jobName := attemptJobName(ref)
	job, err := winjob.Create(jobName, windowsJobLimits(limits)...)
	if err != nil {
		return nil, fmt.Errorf("create named attempt job object: %w", err)
	}
	cleanup := func() {
		_ = job.Terminate()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = job.Close()
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = new(syscall.SysProcAttr)
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	if err := cmd.Start(); err != nil {
		_ = job.Close()
		return nil, fmt.Errorf("start suspended worker: %w", err)
	}
	if err := job.Assign(cmd.Process); err != nil {
		cleanup()
		return nil, fmt.Errorf("assign worker to named job object: %w", err)
	}
	if err := s.writeAssignedMarker(ref, jobName); err != nil {
		cleanup()
		return nil, err
	}
	if err := winjob.Resume(cmd); err != nil {
		cleanup()
		return nil, fmt.Errorf("resume contained worker: %w", err)
	}
	return job, nil
}

// RecoverTermination returns a valid proof only for the new recovery protocol:
// a durable ASSIGNED marker must bind the exact attempt to its deterministic
// named Job Object. Marker absence/corruption is not evidence of termination.
func (s *Supervisor) RecoverTermination(ctx context.Context, ref AttemptRef) (TerminationProof, bool, error) {
	if s == nil || s.recoveryRoot == "" {
		return TerminationProof{}, false, nil
	}
	marker, err := s.readAssignedMarker(ref)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return TerminationProof{}, false, nil
		}
		return TerminationProof{}, false, err
	}
	job, err := winjob.Open(marker.JobName)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			// A marker is written only after assignment and before resume. With
			// KILL_ON_JOB_CLOSE, disappearance of that exact named kernel job
			// after the daemon handle was lost means the contained tree has
			// completed the kill/destroy lifecycle. This is not PID absence.
			return TerminationProof{attempt: ref, confirmedAt: time.Now().UTC(), activeProcesses: 0, valid: true}, true, nil
		}
		return TerminationProof{}, false, fmt.Errorf("open named attempt job object: %w", err)
	}
	defer job.Close()
	if err := job.QueryLimits(); err != nil {
		return TerminationProof{}, false, fmt.Errorf("query named attempt job limits: %w", err)
	}
	if !winjob.LimitKillOnJobClose.IsSet(job) {
		return TerminationProof{}, false, errors.New("named attempt job lacks KILL_ON_JOB_CLOSE")
	}
	var counters winjob.Counters
	if err := job.QueryCounters(&counters); err != nil {
		return TerminationProof{}, false, fmt.Errorf("query named attempt job counters: %w", err)
	}
	if counters.ActiveProcesses > 0 {
		if err := job.Terminate(); err != nil {
			return TerminationProof{}, false, fmt.Errorf("terminate recovered attempt job: %w", err)
		}
	}
	if err := waitForNoActive(ctx, job); err != nil {
		return TerminationProof{}, false, err
	}
	return TerminationProof{attempt: ref, confirmedAt: time.Now().UTC(), activeProcesses: 0, valid: true}, true, nil
}
