//go:build windows

package processctl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	winjob "github.com/kolesnikovae/go-winjob"
)

type CommandSpec struct {
	TaskID         string
	OperationID    string
	Path           string
	Args           []string
	Dir            string
	Env            []string
	MaxOutputBytes int
}

type lockedBuffer struct {
	mu        sync.Mutex
	b         bytes.Buffer
	max       int
	total     int64
	truncated bool
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	originalLen := len(p)
	b.total += int64(originalLen)
	if b.max <= 0 {
		b.max = 1 << 20
	}
	remaining := b.max - b.b.Len()
	if remaining <= 0 {
		b.truncated = true
		return originalLen, nil
	}
	if len(p) > remaining {
		_, _ = b.b.Write(p[:remaining])
		b.truncated = true
		return originalLen, nil
	}
	_, _ = b.b.Write(p)
	return originalLen, nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.truncated {
		return b.b.String()
	}
	return b.b.String() + "\n...[MAR output truncated]..."
}

func (b *lockedBuffer) CaptureStats() (capturedBytes, totalBytes int64, truncated bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int64(b.b.Len()), b.total, b.truncated
}

type CommandResult struct {
	Output          string
	CapturedBytes   int64
	TotalBytes      int64
	OutputTruncated bool
}

// RunContainedCommand preserves the legacy string result for callers that do
// not produce evidence. Evidence-producing callers use the detailed variant.
func RunContainedCommand(ctx context.Context, spec CommandSpec) (string, error) {
	result, err := RunContainedCommandDetailed(ctx, spec)
	return result.Output, err
}

// RunContainedCommandDetailed executes a MAR control-plane command in a Windows
// Job Object and reports whether its output capture ceiling discarded bytes.
func RunContainedCommandDetailed(ctx context.Context, spec CommandSpec) (CommandResult, error) {
	if spec.TaskID == "" || spec.OperationID == "" {
		return CommandResult{}, errors.New("task id and operation id are required")
	}
	if spec.Path == "" {
		return CommandResult{}, errors.New("command path is required")
	}
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	} else {
		cmd.Env = os.Environ()
	}
	output := &lockedBuffer{max: spec.MaxOutputBytes}
	cmd.Stdout = output
	cmd.Stderr = output
	snapshot := func(text string) CommandResult {
		captured, total, truncated := output.CaptureStats()
		return CommandResult{Output: text, CapturedBytes: captured, TotalBytes: total, OutputTruncated: truncated}
	}

	job, err := winjob.Start(cmd, winjob.LimitKillOnJobClose)
	if err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		return snapshot(output.String()), fmt.Errorf("start contained command %s: %w", spec.OperationID, err)
	}
	contained, err := job.Contains(cmd.Process)
	if err != nil || !contained {
		_ = job.Terminate()
		_ = cmd.Wait()
		_ = job.Close()
		if err != nil {
			return snapshot(output.String()), fmt.Errorf("verify contained command %s: %w", spec.OperationID, err)
		}
		return snapshot(output.String()), fmt.Errorf("command %s escaped expected Job Object", spec.OperationID)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case waitErr := <-waitDone:
		if err := waitForNoActive(ctx, job); err != nil {
			text, termErr := terminateContainedAfterError(job, waitDone, true, output.String(), spec.OperationID, err)
			return snapshot(text), termErr
		}
		if err := job.Close(); err != nil {
			return snapshot(output.String()), fmt.Errorf("close contained command job: %w", err)
		}
		if waitErr != nil {
			return snapshot(output.String()), waitErr
		}
		return snapshot(output.String()), nil
	case <-ctx.Done():
		text, termErr := terminateContainedAfterError(job, waitDone, false, output.String(), spec.OperationID, ctx.Err())
		return snapshot(text), termErr
	}
}

func terminateContainedAfterError(job *winjob.JobObject, waitDone <-chan error, parentAlreadyWaited bool, output, operationID string, cause error) (string, error) {
	if err := job.Terminate(); err != nil {
		_ = job.Close()
		return output, fmt.Errorf("%s: %w; terminate job: %v", operationID, cause, err)
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := waitForNoActive(cleanupCtx, job); err != nil {
		_ = job.Close()
		return output, fmt.Errorf("%s: %w; termination unconfirmed: %v", operationID, cause, err)
	}
	if !parentAlreadyWaited {
		select {
		case <-waitDone:
		case <-cleanupCtx.Done():
			_ = job.Close()
			return output, fmt.Errorf("%s: %w; parent wait unconfirmed: %v", operationID, cause, cleanupCtx.Err())
		}
	}
	if err := job.Close(); err != nil {
		return output, fmt.Errorf("%s: %w; close job: %v", operationID, cause, err)
	}
	return output, cause
}
