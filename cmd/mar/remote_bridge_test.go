package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mar/internal/service"
	"mar/internal/store"
)

func testRemoteProfiles(t *testing.T) []store.RemoteConnectorProfile {
	t.Helper()
	now := time.Now().UTC()
	return []store.RemoteConnectorProfile{
		{ID: store.RemoteConnectorClaudeWeb, PathToken: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PreferredMode: store.RemoteConnectorModeTemporary, UpdatedAt: now},
		// A pre-v13 database may retain this retired row. The Claude bridge must
		// ignore it so there is no second GPT transport or status authority.
		{ID: store.RemoteConnectorChatGPTWeb, PathToken: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", PreferredMode: store.RemoteConnectorModeTemporary, UpdatedAt: now},
	}
}

func connectorState(t *testing.T, state remoteBridgeState, id string) remoteConnectorState {
	t.Helper()
	for _, connector := range state.Connectors {
		if connector.ID == id {
			return connector
		}
	}
	t.Fatalf("connector %s missing from %+v", id, state)
	return remoteConnectorState{}
}

func TestRemoteBridgeIsClaudeOnlyAndIgnoresLegacyChatGPTProfile(t *testing.T) {
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
	if err := manager.ConfigureProfiles(testRemoteProfiles(t)); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	state, err := manager.StartTemporary()
	if err != nil {
		t.Fatal(err)
	}
	claude := connectorState(t, state, store.RemoteConnectorClaudeWeb)
	if claude.Status != "LINK_READY" || claude.PublicURL == "" || len(state.Connectors) != 1 {
		t.Fatalf("Claude-only connector state is wrong: %+v", state)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "claude-like-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: manager.localEndpointFor(store.RemoteConnectorClaudeWeb), DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	_ = session.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 11 {
		t.Fatalf("remote bridge leaked or lost MCP tools: count=%d", len(listed.Tools))
	}

	state = manager.State()
	claude = connectorState(t, state, store.RemoteConnectorClaudeWeb)
	if claude.Status != "CONNECTED" || !claude.Initialized || !claude.ToolsListed || claude.Requests < 2 || claude.LastSeenAt == nil {
		t.Fatalf("Claude telemetry missing: %+v", claude)
	}
	if manager.localEndpointFor(store.RemoteConnectorChatGPTWeb) != "" {
		t.Fatal("legacy public ChatGPT MCP endpoint remained active")
	}

	if err := manager.StopTemporary(); err != nil {
		t.Fatal(err)
	}
	if !stopped.Load() {
		t.Fatal("stopping temporary bridge did not terminate tunnel")
	}
	state = manager.State()
	if connectorState(t, state, store.RemoteConnectorClaudeWeb).PublicURL != "" {
		t.Fatalf("temporary URLs survived tunnel stop: %+v", state)
	}
	if manager.localEndpointFor(store.RemoteConnectorClaudeWeb) == "" {
		t.Fatal("stopping temporary tunnel incorrectly destroyed stable local bridge")
	}
}

func TestRemoteBridgeStableProfileKeepsPersistentURLAndRevokesRotatedToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := store.Open(t.TempDir() + `\mar.db`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	manager := newRemoteBridgeManager(ctx, service.NewTaskService(s), t.TempDir())
	profiles := testRemoteProfiles(t)
	if err := manager.ConfigureProfiles(profiles); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	localBase := manager.State().LocalTarget
	profiles[0].StableBaseURL = localBase
	profiles[0].PreferredMode = store.RemoteConnectorModeStable
	if err := manager.ConfigureProfiles(profiles); err != nil {
		t.Fatal(err)
	}
	manager.refreshStableHealth()
	state := manager.State()
	claude := connectorState(t, state, store.RemoteConnectorClaudeWeb)
	if claude.Status != "LINK_READY" || !claude.RouteReady || claude.StableURL != localBase+"/mcp/"+profiles[0].PathToken {
		t.Fatalf("stable connector was not route-ready: %+v", claude)
	}
	oldEndpoint := manager.localEndpointFor(store.RemoteConnectorClaudeWeb)
	profiles[0].PathToken = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	profiles[0].UpdatedAt = time.Now().UTC()
	if err := manager.ConfigureProfiles(profiles); err != nil {
		t.Fatal(err)
	}
	newEndpoint := manager.localEndpointFor(store.RemoteConnectorClaudeWeb)
	if newEndpoint == oldEndpoint {
		t.Fatal("rotating connector token did not change stable endpoint")
	}
	probe, err := mcp.NewClient(&mcp.Implementation{Name: "revoked-stable-link", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: oldEndpoint, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err == nil {
		_ = probe.Close()
		t.Fatal("rotated stable capability path remained reachable")
	}
}

func TestSanitizeRemoteBridgeReadyErrorRedactsCapabilityToken(t *testing.T) {
	const token = "0123456789abcdef-secret-capability"
	err := &url.Error{Op: "Get", URL: "https://example.invalid/health/" + token, Err: errors.New("dial tcp: lookup example.invalid: no such host")}
	got := sanitizeRemoteBridgeReadyError(err, token)
	if strings.Contains(got, token) || strings.Contains(got, "/health/") {
		t.Fatalf("remote bridge diagnostic leaked capability URL/token: %q", got)
	}
	if !strings.Contains(got, "no such host") {
		t.Fatalf("remote bridge diagnostic lost useful network cause: %q", got)
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
	if err := manager.ConfigureProfiles(testRemoteProfiles(t)); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	state := manager.State()
	if state.Status != "BRIDGE_RUNTIME_MISSING" || state.Dependency != "cloudflared" || state.LastError == "" {
		t.Fatalf("missing tunnel dependency was hidden: %+v", state)
	}
	if connectorState(t, state, store.RemoteConnectorClaudeWeb).Status != "BRIDGE_RUNTIME_MISSING" {
		t.Fatalf("Claude missing dependency was hidden: %+v", state)
	}
}
