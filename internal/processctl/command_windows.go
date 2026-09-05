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
	TaskID      string
	OperationID string
	Path        string
	Args        []string
	Dir         string
	Env         []string
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// RunContainedCommand executes a MAR control-plane command in a Windows Job
// Object. Daemon handle closure therefore kills the command tree, and normal
// return waits until the whole Job Object reports zero active processes.
func RunContainedCommand(ctx context.Context, spec CommandSpec) (string, error) {
	if spec.TaskID == "" || spec.OperationID == "" {
		return "", errors.New("task id and operation id are required")
	}
	if spec.Path == "" {
		return "", errors.New("command path is required")
	}
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	} else {
		cmd.Env = os.Environ()
	}
	var output lockedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	job, err := winjob.Start(cmd, winjob.LimitKillOnJobClose)
	if err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		return output.String(), fmt.Errorf("start contained command %s: %w", spec.OperationID, err)
	}
	contained, err := job.Contains(cmd.Process)
	if err != nil || !contained {
		_ = job.Terminate()
		_ = cmd.Wait()
		_ = job.Close()
		if err != nil {
			return output.String(), fmt.Errorf("verify contained command %s: %w", spec.OperationID, err)
		}
		return output.String(), fmt.Errorf("command %s escaped expected Job Object", spec.OperationID)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case waitErr := <-waitDone:
		if err := waitForNoActive(ctx, job); err != nil {
			return terminateContainedAfterError(job, waitDone, true, output.String(), spec.OperationID, err)
		}
		if err := job.Close(); err != nil {
			return output.String(), fmt.Errorf("close contained command job: %w", err)
		}
		if waitErr != nil {
			return output.String(), waitErr
		}
		return output.String(), nil
	case <-ctx.Done():
		return terminateContainedAfterError(job, waitDone, false, output.String(), spec.OperationID, ctx.Err())
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
