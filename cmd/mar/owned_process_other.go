//go:build !windows

package main

import (
	"context"
	"errors"
	"os/exec"
	"sync"
)

type ownedCommand struct {
	cmd  *exec.Cmd
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func startOwnedCommand(cmd *exec.Cmd) (*ownedCommand, error) {
	if cmd == nil {
		return nil, errors.New("owned command is required")
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &ownedCommand{cmd: cmd, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		close(p.done)
	}()
	return p, nil
}

func (p *ownedCommand) PID() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
func (p *ownedCommand) Done() <-chan struct{} { return p.done }
func (p *ownedCommand) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}
func (p *ownedCommand) Stop(ctx context.Context) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	_ = p.cmd.Process.Kill()
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
