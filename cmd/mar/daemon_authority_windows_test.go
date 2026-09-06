//go:build windows

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type authorityTestDaemon struct {
	entered chan struct{}
}

func (d *authorityTestDaemon) Run(ctx context.Context) error {
	select {
	case <-d.entered:
	default:
		close(d.entered)
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestDaemonAuthoritySerializesMultipleMCPProcessesAndAllowsTakeover(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mar.db")
	first := &authorityTestDaemon{entered: make(chan struct{})}
	second := &authorityTestDaemon{entered: make(chan struct{})}
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() { firstDone <- runDaemonAuthority(ctx1, dbPath, first) }()
	select {
	case <-first.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first MCP runtime never acquired daemon authority")
	}
	go func() { secondDone <- runDaemonAuthority(ctx2, dbPath, second) }()
	select {
	case <-second.entered:
		t.Fatal("second MCP runtime acquired daemon authority while first owner was alive")
	case <-time.After(350 * time.Millisecond):
	}

	cancel1()
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first daemon authority did not release after owner shutdown")
	}
	select {
	case <-second.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("standby MCP runtime did not take over daemon authority after release")
	}
	cancel2()
	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("second daemon authority did not shut down")
	}
}
