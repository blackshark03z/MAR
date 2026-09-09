package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"mar/internal/mcpedge"
	"mar/internal/store"
)

const (
	remoteBridgeListen           = "127.0.0.1:8788"
	remoteConnectedWindow        = 45 * time.Second
	remoteStableProbeEvery       = 5 * time.Second
	remoteStableProbeTimeout     = 3 * time.Second
	remoteQuickProbeAttemptLimit = 15 * time.Second
	remoteQuickStartAttempts     = 3
	remoteSessionTelemetryLimit  = 256
)

type remoteConnectorState struct {
	ID                      string     `json:"id"`
	Status                  string     `json:"status"`
	PreferredMode           string     `json:"preferred_mode"`
	PublicURL               string     `json:"public_url,omitempty"`
	StableBaseURL           string     `json:"stable_base_url,omitempty"`
	StableURL               string     `json:"stable_url,omitempty"`
	TemporaryURL            string     `json:"temporary_url,omitempty"`
	LocalTarget             string     `json:"local_target,omitempty"`
	LastSeenAt              *time.Time `json:"last_seen_at,omitempty"`
	LastHealthAt            *time.Time `json:"last_health_at,omitempty"`
	Initialized             bool       `json:"initialized"`
	ToolsListed             bool       `json:"tools_listed"`
	Requests                int64      `json:"requests"`
	ActiveSessions          int        `json:"active_sessions,omitempty"`
	ActiveSessionsAvailable bool       `json:"active_sessions_available"`
	ActiveSessionsReason    string     `json:"active_sessions_reason,omitempty"`
	RouteReady              bool       `json:"route_ready"`
	LastError               string     `json:"last_error,omitempty"`
}

type remoteBridgeState struct {
	Status        string                 `json:"status"`
	Provider      string                 `json:"provider"`
	PublicURL     string                 `json:"public_url,omitempty"` // compatibility: Claude active URL.
	StartedAt     *time.Time             `json:"started_at,omitempty"`
	LastSeenAt    *time.Time             `json:"last_seen_at,omitempty"`
	Initialized   bool                   `json:"initialized"`
	ToolsListed   bool                   `json:"tools_listed"`
	Requests      int64                  `json:"requests"`
	Dependency    string                 `json:"dependency,omitempty"`
	LastError     string                 `json:"last_error,omitempty"`
	TemporaryLink bool                   `json:"temporary_link"`
	LocalTarget   string                 `json:"local_target,omitempty"`
	Connectors    []remoteConnectorState `json:"connectors"`
}

type remoteTunnelStartFunc func(context.Context, string, string) (string, func() error, <-chan error, error)

type remoteConnectorTelemetry struct {
	lastSeenAt                     time.Time
	initialized                    bool
	toolsListed                    bool
	requests                       int64
	sessions                       map[string]time.Time
	sessionTrackingSeen            bool
	sessionTrackingUnreliableUntil time.Time
	stableReady                    bool
	lastHealthAt                   time.Time
	stableError                    string
}

type remoteBridgeManager struct {
	ctx      context.Context
	backend  mcpedge.Backend
	dataRoot string

	lifecycleMu    sync.Mutex
	mu             sync.Mutex
	server         *http.Server
	listener       net.Listener
	listenAddr     string // tests may use an ephemeral loopback port; production defaults to remoteBridgeListen.
	localBaseURL   string
	routes         map[string]http.Handler
	profiles       map[string]store.RemoteConnectorProfile
	telemetry      map[string]*remoteConnectorTelemetry
	quickBaseURL   string
	starting       bool
	startedAt      time.Time
	lastError      string
	tunnelStop     func() error
	tunnelDone     <-chan error
	healthWake     chan struct{}
	healthLoopOnce sync.Once

	findTunnel  func() (string, error)
	startTunnel remoteTunnelStartFunc
	waitReady   func(context.Context, string, string) error
}

func newRemoteBridgeManager(ctx context.Context, backend mcpedge.Backend, dataRoot string) *remoteBridgeManager {
	m := &remoteBridgeManager{
		ctx: ctx, backend: backend, dataRoot: filepath.Clean(dataRoot),
		routes: make(map[string]http.Handler), profiles: make(map[string]store.RemoteConnectorProfile),
		telemetry: make(map[string]*remoteConnectorTelemetry), healthWake: make(chan struct{}, 1),
	}
	m.findTunnel = m.findCloudflared
	m.startTunnel = startCloudflaredQuickTunnel
	m.waitReady = waitRemoteBridgeReady
	return m
}

func (m *remoteBridgeManager) ConfigureProfiles(profiles []store.RemoteConnectorProfile) error {
	if len(profiles) == 0 {
		return errors.New("remote connector profiles are required")
	}
	newProfiles := make(map[string]store.RemoteConnectorProfile, len(profiles))
	newRoutes := make(map[string]http.Handler, len(profiles)*2)
	for _, profile := range profiles {
		// GPT and Claude share the same hardened Streamable HTTP bridge engine but
		// keep independent capability paths and telemetry. OpenAI Secure MCP Tunnel
		// remains available as an optional transport; it is not required for MCP Link.
		if profile.ID != store.RemoteConnectorClaudeWeb && profile.ID != store.RemoteConnectorChatGPTWeb {
			continue
		}
		if err := profile.Validate(); err != nil {
			return err
		}
		profile.StableBaseURL = strings.TrimRight(strings.TrimSpace(profile.StableBaseURL), "/")
		connectorID := profile.ID
		allowed := []string{"claude.ai"}
		if connectorID == store.RemoteConnectorChatGPTWeb {
			allowed = []string{"openai.com", "chatgpt.com"}
		}
		handler, err := mcpedge.NewRemoteHTTPHandler(m.backend, mcpedge.RemoteHTTPOptions{
			PathToken: profile.PathToken, AllowedOriginHosts: allowed,
			Observe: func(event mcpedge.RemoteHTTPEvent) { m.observe(connectorID, event) },
		})
		if err != nil {
			return fmt.Errorf("build %s remote MCP handler: %w", connectorID, err)
		}
		newProfiles[connectorID] = profile
		newRoutes["/mcp/"+profile.PathToken] = handler
		newRoutes["/health/"+profile.PathToken] = handler
	}
	if len(newProfiles) == 0 {
		return errors.New("at least one remote connector profile is required")
	}

	m.mu.Lock()
	for id, profile := range newProfiles {
		previous, existed := m.profiles[id]
		if !existed || previous.PathToken != profile.PathToken || previous.StableBaseURL != profile.StableBaseURL || previous.PreferredMode != profile.PreferredMode {
			m.telemetry[id] = &remoteConnectorTelemetry{}
		} else if m.telemetry[id] == nil {
			m.telemetry[id] = &remoteConnectorTelemetry{}
		}
	}
	m.profiles = newProfiles
	m.routes = newRoutes
	m.mu.Unlock()

	if err := m.ensureLocalServer(); err != nil {
		m.setError(err)
		return err
	}
	m.healthLoopOnce.Do(func() { go m.stableHealthLoop() })
	m.signalHealthRefresh()
	return nil
}

func (m *remoteBridgeManager) State() remoteBridgeState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stateLocked()
}

func (m *remoteBridgeManager) stateLocked() remoteBridgeState {
	state := remoteBridgeState{Provider: "streamable-http", TemporaryLink: m.quickBaseURL != "", LocalTarget: m.localBaseURL, LastError: m.lastError}
	if !m.startedAt.IsZero() {
		started := m.startedAt
		state.StartedAt = &started
	}
	ids := []string{store.RemoteConnectorChatGPTWeb, store.RemoteConnectorClaudeWeb}
	for _, id := range ids {
		profile, ok := m.profiles[id]
		if !ok {
			continue
		}
		telemetry := m.telemetry[id]
		if telemetry == nil {
			telemetry = &remoteConnectorTelemetry{}
		}
		connector := m.connectorStateLocked(profile, telemetry)
		state.Connectors = append(state.Connectors, connector)
		state.Requests += connector.Requests
		state.Initialized = state.Initialized || connector.Initialized
		state.ToolsListed = state.ToolsListed || connector.ToolsListed
		if connector.LastSeenAt != nil && (state.LastSeenAt == nil || connector.LastSeenAt.After(*state.LastSeenAt)) {
			last := *connector.LastSeenAt
			state.LastSeenAt = &last
		}
		if id == store.RemoteConnectorClaudeWeb {
			state.PublicURL = connector.PublicURL
		}
	}
	state.Status = "READY_TO_START"
	for _, connector := range state.Connectors {
		switch connector.Status {
		case "CONNECTED":
			state.Status = "CONNECTED"
			return state
		case "CONNECTING":
			if state.Status == "READY_TO_START" {
				state.Status = "CONNECTING"
			}
		case "IDLE":
			if state.Status != "CONNECTED" {
				state.Status = "IDLE"
			}
		case "LINK_READY":
			if state.Status != "IDLE" {
				state.Status = "LINK_READY"
			}
		case "ROUTE_OFFLINE", "STABLE_URL_REQUIRED":
			if state.Status == "READY_TO_START" {
				state.Status = connector.Status
			}
		}
	}
	if len(state.Connectors) == 0 {
		state.Status = "NOT_CONFIGURED"
	}
	if m.quickBaseURL == "" {
		if path, err := m.findTunnel(); err != nil {
			state.Dependency = "cloudflared"
			if state.LastError == "" {
				state.LastError = err.Error()
			}
			for i := range state.Connectors {
				if state.Connectors[i].PreferredMode == store.RemoteConnectorModeTemporary && state.Connectors[i].Status == "READY_TO_START" {
					state.Connectors[i].Status = "BRIDGE_RUNTIME_MISSING"
					state.Connectors[i].LastError = err.Error()
				}
			}
			if state.Status == "READY_TO_START" {
				state.Status = "BRIDGE_RUNTIME_MISSING"
			}
		} else {
			state.Dependency = path
		}
	}
	return state
}

func pruneRemoteSessionTelemetry(telemetry *remoteConnectorTelemetry, now time.Time) {
	if telemetry == nil {
		return
	}
	for sessionID, lastSeen := range telemetry.sessions {
		if now.Sub(lastSeen) > mcpedge.RemoteMCPSessionTimeout {
			delete(telemetry.sessions, sessionID)
		}
	}
	if !telemetry.sessionTrackingUnreliableUntil.IsZero() && !now.Before(telemetry.sessionTrackingUnreliableUntil) {
		telemetry.sessionTrackingUnreliableUntil = time.Time{}
	}
}

func (m *remoteBridgeManager) connectorStateLocked(profile store.RemoteConnectorProfile, telemetry *remoteConnectorTelemetry) remoteConnectorState {
	now := time.Now().UTC()
	pruneRemoteSessionTelemetry(telemetry, now)
	sessionsAvailable := telemetry.sessionTrackingSeen && telemetry.sessionTrackingUnreliableUntil.IsZero()
	state := remoteConnectorState{
		ID: profile.ID, PreferredMode: profile.PreferredMode, StableBaseURL: profile.StableBaseURL,
		LocalTarget: m.localBaseURL, Initialized: telemetry.initialized, ToolsListed: telemetry.toolsListed,
		Requests: telemetry.requests, LastError: telemetry.stableError, ActiveSessionsAvailable: sessionsAvailable,
	}
	switch {
	case !telemetry.sessionTrackingSeen:
		state.ActiveSessionsReason = "NOT_OBSERVED"
	case !sessionsAvailable:
		state.ActiveSessionsReason = "CARDINALITY_LIMIT"
	default:
		state.ActiveSessions = len(telemetry.sessions)
	}
	if !telemetry.lastSeenAt.IsZero() {
		last := telemetry.lastSeenAt
		state.LastSeenAt = &last
	}
	if !telemetry.lastHealthAt.IsZero() {
		checked := telemetry.lastHealthAt
		state.LastHealthAt = &checked
	}
	if profile.StableBaseURL != "" {
		state.StableURL = connectorPublicURL(profile.StableBaseURL, profile.PathToken)
	}
	if m.quickBaseURL != "" {
		state.TemporaryURL = connectorPublicURL(m.quickBaseURL, profile.PathToken)
	}

	if profile.PreferredMode == store.RemoteConnectorModeStable {
		state.PublicURL = state.StableURL
		if profile.StableBaseURL == "" {
			state.Status = "STABLE_URL_REQUIRED"
			return state
		}
		state.RouteReady = telemetry.stableReady
		if !telemetry.stableReady {
			if telemetry.lastHealthAt.IsZero() {
				state.Status = "CHECKING"
			} else {
				state.Status = "ROUTE_OFFLINE"
			}
			return state
		}
	} else {
		state.PublicURL = state.TemporaryURL
		state.RouteReady = m.quickBaseURL != ""
		if m.quickBaseURL == "" {
			if m.starting {
				state.Status = "CONNECTING"
			} else {
				state.Status = "READY_TO_START"
			}
			return state
		}
	}

	if telemetry.initialized {
		if state.ActiveSessionsAvailable {
			if state.ActiveSessions > 0 {
				state.Status = "CONNECTED"
			} else {
				state.Status = "IDLE"
			}
			return state
		}
		if !telemetry.lastSeenAt.IsZero() && time.Since(telemetry.lastSeenAt) <= remoteConnectedWindow {
			state.Status = "CONNECTED"
		} else {
			state.Status = "IDLE"
		}
		return state
	}
	state.Status = "LINK_READY"
	return state
}

func (m *remoteBridgeManager) Profile(id string) (store.RemoteConnectorProfile, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profiles[id]
	return p, ok
}

func (m *remoteBridgeManager) ensureLocalServer() error {
	m.mu.Lock()
	if m.server != nil && m.listener != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	listenAddr := strings.TrimSpace(m.listenAddr)
	if listenAddr == "" {
		listenAddr = remoteBridgeListen
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen for stable remote MCP bridge on %s: %w", listenAddr, err)
	}
	server := &http.Server{Handler: http.HandlerFunc(m.serveRemoteHTTP), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	serveDone := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveDone <- err
	}()

	m.mu.Lock()
	if m.server != nil {
		m.mu.Unlock()
		_ = listener.Close()
		return nil
	}
	m.server = server
	m.listener = listener
	m.localBaseURL = "http://" + listener.Addr().String()
	m.lastError = ""
	m.mu.Unlock()
	go m.watchServer(serveDone)
	return nil
}

func (m *remoteBridgeManager) serveRemoteHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	handler := m.routes[r.URL.Path]
	m.mu.Unlock()
	if handler == nil {
		http.NotFound(w, r)
		return
	}
	handler.ServeHTTP(w, r)
}

func (m *remoteBridgeManager) Start() (remoteBridgeState, error) { return m.StartTemporary() }

func (m *remoteBridgeManager) StartTemporary() (remoteBridgeState, error) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if err := m.ensureLocalServer(); err != nil {
		m.setError(err)
		return m.State(), err
	}
	m.mu.Lock()
	if m.quickBaseURL != "" {
		state := m.stateLocked()
		m.mu.Unlock()
		return state, nil
	}
	localURL := m.localBaseURL
	var probeToken string
	for _, id := range []string{store.RemoteConnectorChatGPTWeb, store.RemoteConnectorClaudeWeb} {
		if p, ok := m.profiles[id]; ok {
			probeToken = p.PathToken
			break
		}
	}
	m.lastError = ""
	m.starting = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.starting = false
		m.mu.Unlock()
	}()
	if probeToken == "" {
		return m.State(), errors.New("no remote connector profile is configured")
	}

	cloudflared, err := m.findTunnel()
	if err != nil {
		m.setError(err)
		return m.State(), err
	}
	var baseURL string
	var stopTunnel func() error
	var tunnelDone <-chan error
	var lastErr error
	for attempt := 1; attempt <= remoteQuickStartAttempts; attempt++ {
		baseURL, stopTunnel, tunnelDone, err = m.startTunnel(m.ctx, cloudflared, localURL)
		if err == nil {
			probeCtx, cancel := context.WithTimeout(m.ctx, remoteQuickProbeAttemptLimit)
			ready := m.waitReady
			if ready == nil {
				ready = waitRemoteBridgeReady
			}
			err = ready(probeCtx, baseURL, probeToken)
			cancel()
		}
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
		if stopTunnel != nil {
			_ = stopTunnel()
		}
		baseURL, stopTunnel, tunnelDone = "", nil, nil
		if m.ctx.Err() != nil {
			break
		}
	}
	if lastErr != nil || baseURL == "" {
		if lastErr == nil {
			lastErr = errors.New("temporary remote MCP route did not start")
		}
		err = fmt.Errorf("temporary remote MCP link failed after %d bounded attempts: %s", remoteQuickStartAttempts, sanitizeRemoteBridgeReadyError(lastErr, probeToken))
		m.setError(err)
		return m.State(), err
	}
	now := time.Now().UTC()
	m.mu.Lock()
	m.quickBaseURL = strings.TrimRight(baseURL, "/")
	m.startedAt = now
	m.tunnelStop = stopTunnel
	m.tunnelDone = tunnelDone
	for id, profile := range m.profiles {
		if profile.PreferredMode == store.RemoteConnectorModeTemporary {
			m.telemetry[id] = &remoteConnectorTelemetry{}
		}
	}
	m.lastError = ""
	m.mu.Unlock()
	go m.watchTunnel(tunnelDone)
	return m.State(), nil
}

func (m *remoteBridgeManager) StopTemporary() error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.Lock()
	stopTunnel := m.tunnelStop
	m.quickBaseURL = ""
	m.tunnelStop = nil
	m.tunnelDone = nil
	for id, profile := range m.profiles {
		if profile.PreferredMode == store.RemoteConnectorModeTemporary {
			m.telemetry[id] = &remoteConnectorTelemetry{}
		}
	}
	m.mu.Unlock()
	if stopTunnel != nil {
		if err := stopTunnel(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	return nil
}

func (m *remoteBridgeManager) Stop() error {
	var errs []error
	if err := m.StopTemporary(); err != nil {
		errs = append(errs, err)
	}
	m.mu.Lock()
	server := m.server
	listener := m.listener
	m.server = nil
	m.listener = nil
	m.localBaseURL = ""
	m.mu.Unlock()
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if listener != nil {
		_ = listener.Close()
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (m *remoteBridgeManager) Close() error { return m.Stop() }

func (m *remoteBridgeManager) localEndpoint() string {
	return m.localEndpointFor(store.RemoteConnectorClaudeWeb)
}
func (m *remoteBridgeManager) localEndpointFor(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	profile, ok := m.profiles[id]
	if !ok || m.localBaseURL == "" {
		return ""
	}
	return m.localBaseURL + "/mcp/" + profile.PathToken
}

func (m *remoteBridgeManager) observe(connectorID string, event mcpedge.RemoteHTTPEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	telemetry := m.telemetry[connectorID]
	if telemetry == nil {
		telemetry = &remoteConnectorTelemetry{}
		m.telemetry[connectorID] = telemetry
	}
	at := event.At.UTC()
	if event.At.IsZero() {
		at = time.Now().UTC()
	}
	telemetry.requests++
	telemetry.lastSeenAt = at
	pruneRemoteSessionTelemetry(telemetry, at)
	if sessionID := strings.TrimSpace(event.SessionID); sessionID != "" {
		telemetry.sessionTrackingSeen = true
		if telemetry.sessions == nil {
			telemetry.sessions = make(map[string]time.Time)
		}
		if event.HTTPMethod == http.MethodDelete {
			delete(telemetry.sessions, sessionID)
		} else if _, exists := telemetry.sessions[sessionID]; exists {
			telemetry.sessions[sessionID] = at
		} else if len(telemetry.sessions) < remoteSessionTelemetryLimit {
			telemetry.sessions[sessionID] = at
		} else {
			telemetry.sessionTrackingUnreliableUntil = at.Add(mcpedge.RemoteMCPSessionTimeout)
		}
	}
	if event.JSONRPCMethod == "initialize" {
		telemetry.initialized = true
	}
	if event.JSONRPCMethod == "tools/list" {
		telemetry.toolsListed = true
	}
}

func (m *remoteBridgeManager) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.lastError = err.Error()
	}
}

func (m *remoteBridgeManager) watchTunnel(tunnelDone <-chan error) {
	select {
	case err := <-tunnelDone:
		if err != nil {
			m.setError(fmt.Errorf("remote tunnel exited: %w", err))
		}
		_ = m.StopTemporary()
	case <-m.ctx.Done():
		_ = m.StopTemporary()
	}
}

func (m *remoteBridgeManager) watchServer(serveDone <-chan error) {
	select {
	case err := <-serveDone:
		if err != nil {
			m.setError(fmt.Errorf("remote MCP HTTP server exited: %w", err))
		}
	case <-m.ctx.Done():
	}
}

func (m *remoteBridgeManager) stableHealthLoop() {
	ticker := time.NewTicker(remoteStableProbeEvery)
	defer ticker.Stop()
	for {
		m.refreshStableHealth()
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		case <-m.healthWake:
		}
	}
}

func (m *remoteBridgeManager) signalHealthRefresh() {
	select {
	case m.healthWake <- struct{}{}:
	default:
	}
}

func (m *remoteBridgeManager) refreshStableHealth() {
	m.mu.Lock()
	profiles := make([]store.RemoteConnectorProfile, 0, len(m.profiles))
	for _, p := range m.profiles {
		if p.StableBaseURL != "" {
			profiles = append(profiles, p)
		}
	}
	m.mu.Unlock()
	for _, p := range profiles {
		ctx, cancel := context.WithTimeout(m.ctx, remoteStableProbeTimeout)
		err := probeRemoteBridgeReady(ctx, p.StableBaseURL, p.PathToken)
		cancel()
		now := time.Now().UTC()
		m.mu.Lock()
		telemetry := m.telemetry[p.ID]
		if telemetry == nil {
			telemetry = &remoteConnectorTelemetry{}
			m.telemetry[p.ID] = telemetry
		}
		telemetry.lastHealthAt = now
		telemetry.stableReady = err == nil
		telemetry.stableError = ""
		if err != nil {
			telemetry.stableError = sanitizeRemoteBridgeReadyError(err, p.PathToken)
		}
		m.mu.Unlock()
	}
}

func connectorPublicURL(baseURL, token string) string {
	return strings.TrimRight(baseURL, "/") + "/mcp/" + token
}

func probeRemoteBridgeReady(ctx context.Context, baseURL, token string) error {
	healthURL := strings.TrimRight(baseURL, "/") + "/health/" + token
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: remoteStableProbeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("public bridge health returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (m *remoteBridgeManager) findCloudflared() (string, error) {
	managed := filepath.Join(m.dataRoot, "runtime", "cloudflared.exe")
	if path, err := filepath.Abs(managed); err == nil {
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			return filepath.Clean(path), nil
		}
	}
	if path, err := exec.LookPath("cloudflared"); err == nil {
		if abs, absErr := filepath.Abs(path); absErr == nil {
			return filepath.Clean(abs), nil
		}
		return filepath.Clean(path), nil
	}
	return "", errors.New("cloudflared is not installed; temporary remote MCP link cannot start")
}

func newRemoteBridgeToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate remote MCP capability token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

const remoteBridgeReadyTimeout = 90 * time.Second

func waitRemoteBridgeReady(ctx context.Context, baseURL, token string) error {
	deadline := time.NewTimer(remoteBridgeReadyTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		lastErr = probeRemoteBridgeReady(probeCtx, baseURL, token)
		cancel()
		if lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("public bridge did not become reachable within %s: %s", remoteBridgeReadyTimeout, sanitizeRemoteBridgeReadyError(lastErr, token))
		case <-ticker.C:
		}
	}
}

func sanitizeRemoteBridgeReadyError(err error, token string) string {
	if err == nil {
		return "unknown public route error"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		err = urlErr.Err
	}
	text := err.Error()
	if token != "" {
		text = strings.ReplaceAll(text, token, "[redacted]")
	}
	return text
}

var quickTunnelURLPattern = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

func startCloudflaredQuickTunnel(ctx context.Context, executable, localURL string) (string, func() error, <-chan error, error) {
	cmd := exec.CommandContext(ctx, executable, "tunnel", "--no-autoupdate", "--url", localURL)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return "", nil, nil, fmt.Errorf("start cloudflared quick tunnel: %w", err)
	}
	urlCh := make(chan string, 1)
	readURLs := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			if value := quickTunnelURLPattern.FindString(strings.ToLower(scanner.Text())); value != "" {
				select {
				case urlCh <- value:
				default:
				}
			}
		}
	}
	go readURLs(stdout)
	go readURLs(stderr)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case value := <-urlCh:
		stop := func() error {
			if cmd.Process == nil {
				return nil
			}
			return cmd.Process.Kill()
		}
		return value, stop, done, nil
	case err := <-done:
		if err == nil {
			err = errors.New("cloudflared exited before publishing a tunnel URL")
		}
		return "", nil, nil, err
	case <-timer.C:
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", nil, nil, errors.New("timed out waiting for cloudflared quick tunnel URL")
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", nil, nil, ctx.Err()
	}
}
