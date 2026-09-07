package main

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mar/internal/service"
	"mar/internal/store"
)

func TestRemoteBridgeLifecyclePublishesCapabilityURLAndObservesMCPInitialization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := store.Open(t.TempDir() + `\mar.db`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	manager := newRemoteBridgeManager(ctx, service.NewTaskService(s), t.TempDir())
	manager.findTunnel = func() (string, error) { return `C:\fake\cloudflared.exe`, nil }
	var stopped atomic.Bool
	fakeDone := make(chan error)
	manager.startTunnel = func(_ context.Context, _ string, localURL string) (string, func() error, <-chan error, error) {
		return localURL, func() error { stopped.Store(true); return nil }, fakeDone, nil
	}
	defer manager.Close()

	state, err := manager.Start()
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "LINK_READY" || state.Initialized || state.PublicURL == "" || !strings.Contains(state.PublicURL, "/mcp/") {
		t.Fatalf("unexpected initial bridge state: %+v", state)
	}
	localEndpoint := manager.localEndpoint()
	if !strings.HasPrefix(localEndpoint, "http://127.0.0.1:") || !strings.Contains(localEndpoint, "/mcp/") {
		t.Fatalf("unexpected local bridge endpoint %q", localEndpoint)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "remote-bridge-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: localEndpoint, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		_ = session.Close()
		t.Fatal(err)
	}
	if len(listed.Tools) != 9 {
		_ = session.Close()
		t.Fatalf("remote bridge leaked or lost MCP tools: count=%d", len(listed.Tools))
	}
	_ = session.Close()
	state = manager.State()
	if state.Status != "CONNECTED" || !state.Initialized || !state.ToolsListed || state.LastSeenAt == nil || state.Requests < 2 {
		t.Fatalf("bridge did not record real MCP initialization/list-tools: %+v", state)
	}

	oldLocal := localEndpoint
	if err := manager.Stop(); err != nil {
		t.Fatal(err)
	}
	if !stopped.Load() {
		t.Fatal("stopping bridge did not revoke tunnel process")
	}
	state = manager.State()
	if state.Status != "READY_TO_START" || state.PublicURL != "" || state.Initialized {
		t.Fatalf("stopped bridge still looked connected: %+v", state)
	}
	probe, err := mcp.NewClient(&mcp.Implementation{Name: "revoked-link-test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: oldLocal, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err == nil {
		_ = probe.Close()
		t.Fatal("revoked remote MCP local endpoint remained reachable")
	}
}

func TestRemoteBridgeStateReportsMissingTunnelDependencyTruthfully(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := store.Open(t.TempDir() + `\mar.db`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	manager := newRemoteBridgeManager(ctx, service.NewTaskService(s), t.TempDir())
	manager.findTunnel = func() (string, error) { return "", context.Canceled }
	state := manager.State()
	if state.Status != "BRIDGE_RUNTIME_MISSING" || state.Dependency != "cloudflared" || state.LastError == "" {
		t.Fatalf("missing tunnel dependency was hidden: %+v", state)
	}
}
