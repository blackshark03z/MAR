//go:build windows

package processctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	winjob "github.com/kolesnikovae/go-winjob"
)

// AttemptRef binds a physical process tree to one durable MAR execution attempt.
type AttemptRef struct {
	TaskID    string
	AttemptID string
	RunEpoch  int64
}

type Spec struct {
	Attempt AttemptRef
	Path    string
	Args    []string
	Dir     string
	Env     []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

type Supervisor struct{}

func NewSupervisor() *Supervisor { return &Supervisor{} }

// TerminationProof cannot be forged outside this package into a valid proof
// because all confirmation fields are private. Its only meaning is that the
// associated Job Object reported zero active processes before its handle closed.
type TerminationProof struct {
	attempt         AttemptRef
	confirmedAt     time.Time
	activeProcesses uint32
	valid           bool
}

func (p TerminationProof) Valid() bool             { return p.valid && p.activeProcesses == 0 }
func (p TerminationProof) Attempt() AttemptRef     { return p.attempt }
func (p TerminationProof) ConfirmedAt() time.Time  { return p.confirmedAt }
func (p TerminationProof) ActiveProcesses() uint32 { return p.activeProcesses }

type Tree struct {
	ref AttemptRef
	cmd *exec.Cmd
	job *winjob.JobObject

	waitDone chan struct{}
	waitMu   sync.Mutex
	waitErr  error

	opMu   sync.Mutex
	closed bool
	proof  TerminationProof
}

func (s *Supervisor) Start(spec Spec) (*Tree, error) {
	if spec.Attempt.TaskID == "" || spec.Attempt.AttemptID == "" || spec.Attempt.RunEpoch <= 0 {
		return nil, errors.New("valid task/attempt/run_epoch is required")
	}
	if spec.Path == "" {
		return nil, errors.New("process path is required")
	}

	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	} else {
		cmd.Env = os.Environ()
	}
	cmd.Stdin = spec.Stdin
	if spec.Stdout != nil {
		cmd.Stdout = spec.Stdout
	} else {
		cmd.Stdout = io.Discard
	}
	if spec.Stderr != nil {
		cmd.Stderr = spec.Stderr
	} else {
		cmd.Stderr = io.Discard
	}

	// Start creates the process suspended, assigns it to the Job Object, then
	// resumes it. We intentionally do NOT enable BREAKAWAY_OK/SILENT_BREAKAWAY.
	job, err := winjob.Start(cmd, winjob.LimitKillOnJobClose)
	if err != nil {
		// go-winjob may have created a suspended process before an assignment
		// failure. Kill/wait defensively so a failed Start cannot leak it.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		return nil, fmt.Errorf("start process in job object: %w", err)
	}

	contained, err := job.Contains(cmd.Process)
	if err != nil || !contained {
		_ = job.Terminate()
		_ = cmd.Wait()
		_ = job.Close()
		if err != nil {
			return nil, fmt.Errorf("verify process job containment: %w", err)
		}
		return nil, errors.New("process started without expected job containment")
	}

	t := &Tree{
		ref:      spec.Attempt,
		cmd:      cmd,
		job:      job,
		waitDone: make(chan struct{}),
	}
	go func() {
		err := cmd.Wait()
		t.waitMu.Lock()
		t.waitErr = err
		t.waitMu.Unlock()
		close(t.waitDone)
	}()
	return t, nil
}

func (t *Tree) Attempt() AttemptRef { return t.ref }
func (t *Tree) PID() int            { return t.cmd.Process.Pid }

func (t *Tree) Counters() (winjob.Counters, error) {
	t.opMu.Lock()
	defer t.opMu.Unlock()
	if t.closed {
		return winjob.Counters{}, errors.New("process tree is closed")
	}
	var c winjob.Counters
	if err := t.job.QueryCounters(&c); err != nil {
		return winjob.Counters{}, err
	}
	return c, nil
}

// TerminateAndConfirm terminates the entire Job Object tree and returns a valid
// proof only after Windows reports zero active processes. A timeout/error never
// creates a physical-termination proof.
func (t *Tree) TerminateAndConfirm(ctx context.Context) (TerminationProof, error) {
	t.opMu.Lock()
	defer t.opMu.Unlock()

	if t.proof.Valid() {
		return t.proof, nil
	}
	if t.closed {
		return TerminationProof{}, errors.New("process tree already closed without termination proof")
	}
	if err := t.job.Terminate(); err != nil {
		return TerminationProof{}, fmt.Errorf("terminate job object: %w", err)
	}
	if err := waitForNoActive(ctx, t.job); err != nil {
		return TerminationProof{}, err
	}

	// Parent wait is a process-resource cleanup signal. A non-zero exit is
	// expected after TerminateJobObject and does not invalidate physical proof.
	select {
	case <-t.waitDone:
	case <-ctx.Done():
		return TerminationProof{}, fmt.Errorf("wait parent after job termination: %w", ctx.Err())
	}

	proof := TerminationProof{
		attempt:         t.ref,
		confirmedAt:     time.Now().UTC(),
		activeProcesses: 0,
		valid:           true,
	}
	if err := t.job.Close(); err != nil {
		return TerminationProof{}, fmt.Errorf("close confirmed job object: %w", err)
	}
	t.closed = true
	t.proof = proof
	return proof, nil
}

// WaitAndConfirm is for natural process-tree completion. Parent exit alone is
// insufficient because descendants may still be running in the Job Object.
func (t *Tree) WaitAndConfirm(ctx context.Context) (TerminationProof, error) {
	t.opMu.Lock()
	defer t.opMu.Unlock()

	if t.proof.Valid() {
		return t.proof, nil
	}
	if t.closed {
		return TerminationProof{}, errors.New("process tree already closed without termination proof")
	}
	if err := waitForNoActive(ctx, t.job); err != nil {
		return TerminationProof{}, err
	}
	select {
	case <-t.waitDone:
	case <-ctx.Done():
		return TerminationProof{}, ctx.Err()
	}
	proof := TerminationProof{attempt: t.ref, confirmedAt: time.Now().UTC(), activeProcesses: 0, valid: true}
	if err := t.job.Close(); err != nil {
		return TerminationProof{}, err
	}
	t.closed = true
	t.proof = proof
	return proof, nil
}

func waitForNoActive(ctx context.Context, job *winjob.JobObject) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var c winjob.Counters
		if err := job.QueryCounters(&c); err != nil {
			return fmt.Errorf("query job counters: %w", err)
		}
		if c.ActiveProcesses == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("physical process-tree termination not confirmed: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// CloseUnverified is emergency cleanup only. It intentionally returns no proof.
// With KILL_ON_JOB_CLOSE, closing the handle asks Windows to terminate remaining
// processes, but MAR must not treat this call alone as confirmed termination.
func (t *Tree) CloseUnverified() error {
	t.opMu.Lock()
	defer t.opMu.Unlock()
	if t.closed {
		return nil
	}
	err := t.job.Close()
	t.closed = true
	return err
}

func (t *Tree) ParentWaitError() error {
	select {
	case <-t.waitDone:
		t.waitMu.Lock()
		defer t.waitMu.Unlock()
		return t.waitErr
	default:
		return errors.New("parent process has not exited")
	}
}
