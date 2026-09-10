//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	winjob "github.com/kolesnikovae/go-winjob"
)

// ownedCommand binds one infrastructure child process to a Windows Job Object.
// KILL_ON_JOB_CLOSE makes the OS terminate the child tree if the owning MAR
// process exits or is killed before normal cleanup can run.
type ownedCommand struct {
	cmd  *exec.Cmd
	job  *winjob.JobObject
	done chan struct{}

	mu  sync.Mutex
	err error
}

func startOwnedCommand(cmd *exec.Cmd) (*ownedCommand, error) {
	if cmd == nil {
		return nil, errors.New("owned command is required")
	}
	job, err := winjob.Start(cmd, winjob.LimitKillOnJobClose)
	if err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		return nil, fmt.Errorf("start owned process in Windows Job Object: %w", err)
	}
	contained, containErr := job.Contains(cmd.Process)
	if containErr != nil || !contained {
		_ = job.Terminate()
		_ = cmd.Wait()
		_ = job.Close()
		if containErr != nil {
			return nil, fmt.Errorf("verify owned process containment: %w", containErr)
		}
		return nil, errors.New("owned process started without expected Job Object containment")
	}
	p := &ownedCommand{cmd: cmd, job: job, done: make(chan struct{})}
	go p.wait()
	return p, nil
}

func (p *ownedCommand) wait() {
	err := p.cmd.Wait()
	p.mu.Lock()
	p.err = err
	job := p.job
	p.job = nil
	p.mu.Unlock()
	// Natural parent exit may still leave descendants. Closing the Job Object
	// is therefore part of normal cleanup as well as crash containment.
	if job != nil {
		_ = job.Close()
	}
	close(p.done)
}

func (p *ownedCommand) PID() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *ownedCommand) Done() <-chan struct{} { return p.done }

func (p *ownedCommand) Err() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *ownedCommand) Stop(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	job := p.job
	done := p.done
	p.mu.Unlock()
	if job != nil {
		if err := job.Terminate(); err != nil {
			select {
			case <-done:
				return nil
			default:
				return fmt.Errorf("terminate owned process Job Object: %w", err)
			}
		}
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
