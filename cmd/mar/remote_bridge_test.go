package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
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

func TestRemoteBridgeExposesIndependentGPTAndClaudeLinks(t *testing.T) {
	requireLoopbackTCP(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := store.Open(t.TempDir() + `\mar.db`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	manager := newRemoteBridgeManager(ctx, service.NewTaskService(s), t.TempDir())
	manager.listenAddr = "127.0.0.1:0"
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
	gpt := connectorState(t, state, store.RemoteConnectorChatGPTWeb)
	claude := connectorState(t, state, store.RemoteConnectorClaudeWeb)
	if gpt.Status != "LINK_READY" || claude.Status != "LINK_READY" || gpt.PublicURL == "" || claude.PublicURL == "" || gpt.PublicURL == claude.PublicURL || len(state.Connectors) != 2 {
		t.Fatalf("independent connector state is wrong: %+v", state)
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
	if claude.Status != "IDLE" || !claude.Initialized || !claude.ToolsListed || claude.Requests < 2 || claude.LastSeenAt == nil || !claude.ActiveSessionsAvailable || claude.ActiveSessions != 0 {
		t.Fatalf("Claude telemetry should show observed activity but no active session after explicit close: %+v", claude)
	}
	gpt = connectorState(t, state, store.RemoteConnectorChatGPTWeb)
	if gpt.Status != "LINK_READY" || gpt.Requests != 0 || gpt.Initialized || gpt.ToolsListed {
		t.Fatalf("Claude traffic contaminated GPT telemetry: %+v", gpt)
	}
	if manager.localEndpointFor(store.RemoteConnectorChatGPTWeb) == "" || manager.localEndpointFor(store.RemoteConnectorChatGPTWeb) == manager.localEndpointFor(store.RemoteConnectorClaudeWeb) {
		t.Fatal("GPT and Claude capability endpoints are not independent")
	}

	gptClient := mcp.NewClient(&mcp.Implementation{Name: "chatgpt-like-test", Version: "1"}, nil)
	gptSession, err := gptClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: manager.localEndpointFor(store.RemoteConnectorChatGPTWeb), DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gptListed, err := gptSession.ListTools(ctx, &mcp.ListToolsParams{})
	_ = gptSession.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(gptListed.Tools) != 11 {
		t.Fatalf("GPT link leaked or lost MCP tools: count=%d", len(gptListed.Tools))
	}
	gpt = connectorState(t, manager.State(), store.RemoteConnectorChatGPTWeb)
	if gpt.Status != "IDLE" || !gpt.Initialized || !gpt.ToolsListed || gpt.Requests < 2 || gpt.LastSeenAt == nil || !gpt.ActiveSessionsAvailable || gpt.ActiveSessions != 0 {
		t.Fatalf("GPT fallback telemetry should show observed activity but no active session after explicit close: %+v", gpt)
	}

	if err := manager.StopTemporary(); err != nil {
		t.Fatal(err)
	}
	if !stopped.Load() {
		t.Fatal("stopping temporary bridge did not terminate tunnel")
	}
	state = manager.State()
	if connectorState(t, state, store.RemoteConnectorClaudeWeb).PublicURL != "" || connectorState(t, state, store.RemoteConnectorChatGPTWeb).PublicURL != "" {
		t.Fatalf("temporary URLs survived tunnel stop: %+v", state)
	}
	if manager.localEndpointFor(store.RemoteConnectorClaudeWeb) == "" || manager.localEndpointFor(store.RemoteConnectorChatGPTWeb) == "" {
		t.Fatal("stopping temporary tunnel incorrectly destroyed stable local bridge")
	}
}

func TestRemoteBridgeSerializesConcurrentTemporaryStartAndReportsConnecting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := store.Open(t.TempDir() + `\\mar.db`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	manager := newRemoteBridgeManager(ctx, service.NewTaskService(s), t.TempDir())
	makeRemoteBridgePassiveForTest(manager)
	manager.waitReady = func(context.Context, string, string) error { return nil }
	manager.findTunnel = func() (string, error) { return `C:\\fake\\cloudflared.exe`, nil }
	entered := make(chan struct{})
	release := make(chan struct{})
	var starts atomic.Int32
	manager.startTunnel = func(_ context.Context, _ string, localURL string) (string, func() error, <-chan error, error) {
		if starts.Add(1) == 1 {
			close(entered)
		}
		<-release
		return localURL, func() error { return nil }, make(chan error), nil
	}
	if err := manager.ConfigureProfiles(testRemoteProfiles(t)); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			if _, err := manager.StartTemporary(); err != nil {
				t.Errorf("StartTemporary: %v", err)
			}
		}()
	}
	<-entered
	state := manager.State()
	if connectorState(t, state, store.RemoteConnectorChatGPTWeb).Status != "CONNECTING" || connectorState(t, state, store.RemoteConnectorClaudeWeb).Status != "CONNECTING" {
		t.Fatalf("concurrent startup did not report CONNECTING: %+v", state)
	}
	close(release)
	wg.Wait()
	if starts.Load() != 1 {
		t.Fatalf("concurrent startup launched %d tunnels, want 1", starts.Load())
	}
}

func TestRemoteBridgeRetriesDeadTemporaryRouteWithinBoundedStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := store.Open(t.TempDir() + `\\mar.db`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	manager := newRemoteBridgeManager(ctx, service.NewTaskService(s), t.TempDir())
	makeRemoteBridgePassiveForTest(manager)
	manager.findTunnel = func() (string, error) { return `C:\\fake\\cloudflared.exe`, nil }
	var starts atomic.Int32
	var stops atomic.Int32
	manager.startTunnel = func(_ context.Context, _ string, localURL string) (string, func() error, <-chan error, error) {
		starts.Add(1)
		return localURL, func() error { stops.Add(1); return nil }, make(chan error), nil
	}
	var probes atomic.Int32
	manager.waitReady = func(_ context.Context, _, _ string) error {
		if probes.Add(1) == 1 {
			return errors.New("lookup dead.trycloudflare.com: no such host")
		}
		return nil
	}
	if err := manager.ConfigureProfiles(testRemoteProfiles(t)); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	state, err := manager.StartTemporary()
	if err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 2 || probes.Load() != 2 || stops.Load() != 1 {
		t.Fatalf("bounded retry counts wrong: starts=%d probes=%d stops=%d", starts.Load(), probes.Load(), stops.Load())
	}
	if connectorState(t, state, store.RemoteConnectorChatGPTWeb).Status != "LINK_READY" || connectorState(t, state, store.RemoteConnectorClaudeWeb).Status != "LINK_READY" {
		t.Fatalf("retry did not recover link: %+v", state)
	}
}

func TestRemoteBridgeStableProfileKeepsPersistentURLAndRevokesRotatedToken(t *testing.T) {
	requireLoopbackTCP(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := store.Open(t.TempDir() + `\mar.db`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	manager := newRemoteBridgeManager(ctx, service.NewTaskService(s), t.TempDir())
	manager.listenAddr = "127.0.0.1:0"
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
	makeRemoteBridgePassiveForTest(manager)
	manager.findTunnel = func() (string, error) { return "", context.Canceled }
	if err := manager.ConfigureProfiles(testRemoteProfiles(t)); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	state := manager.State()
	if state.Status != "BRIDGE_RUNTIME_MISSING" || state.Dependency != "cloudflared" || state.LastError == "" {
		t.Fatalf("missing tunnel dependency was hidden: %+v", state)
	}
	if connectorState(t, state, store.RemoteConnectorClaudeWeb).Status != "BRIDGE_RUNTIME_MISSING" || connectorState(t, state, store.RemoteConnectorChatGPTWeb).Status != "BRIDGE_RUNTIME_MISSING" {
		t.Fatalf("connector missing dependency was hidden: %+v", state)
	}
}
