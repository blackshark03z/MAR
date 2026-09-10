//go:build windows

package main

import (
	"os/exec"
	"testing"
	"time"
)

func TestOwnedCommandJobCloseKillsChild(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "ping -n 30 127.0.0.1 >nul")
	p, err := startOwnedCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	job := p.job
	p.mu.Unlock()
	if job == nil {
		t.Fatal("owned command has no Windows Job Object")
	}
	// Simulate abrupt owner-process teardown: Windows closes the Job Object
	// handle even when MAR cannot run its normal Stop path.
	if err := job.Close(); err != nil {
		t.Fatalf("close owned Job Object: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("owned child PID %d survived Job Object close", p.PID())
	}
}
