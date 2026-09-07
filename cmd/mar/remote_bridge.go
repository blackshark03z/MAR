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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"mar/internal/mcpedge"
)

type remoteBridgeState struct {
	Status        string     `json:"status"`
	Provider      string     `json:"provider"`
	PublicURL     string     `json:"public_url,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
	Initialized   bool       `json:"initialized"`
	ToolsListed   bool       `json:"tools_listed"`
	Requests      int64      `json:"requests"`
	Dependency    string     `json:"dependency,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	TemporaryLink bool       `json:"temporary_link"`
}

type remoteTunnelStartFunc func(context.Context, string, string) (string, func() error, <-chan error, error)

type remoteBridgeManager struct {
	ctx      context.Context
	backend  mcpedge.Backend
	dataRoot string

	mu          sync.Mutex
	server      *http.Server
	listener    net.Listener
	publicURL   string
	localURL    string
	startedAt   time.Time
	lastSeenAt  time.Time
	initialized bool
	toolsListed bool
	requests    int64
	lastError   string
	tunnelStop  func() error
	tunnelDone  <-chan error

	findTunnel  func() (string, error)
	startTunnel remoteTunnelStartFunc
}

func newRemoteBridgeManager(ctx context.Context, backend mcpedge.Backend, dataRoot string) *remoteBridgeManager {
	m := &remoteBridgeManager{ctx: ctx, backend: backend, dataRoot: filepath.Clean(dataRoot)}
	m.findTunnel = m.findCloudflared
	m.startTunnel = startCloudflaredQuickTunnel
	return m
}

func (m *remoteBridgeManager) State() remoteBridgeState {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := remoteBridgeState{Provider: "cloudflare-quick-tunnel", Initialized: m.initialized, ToolsListed: m.toolsListed, Requests: m.requests, TemporaryLink: true, LastError: m.lastError}
	if m.publicURL != "" && m.server != nil {
		state.Status = "LINK_READY"
		if m.initialized {
			state.Status = "CONNECTED"
		}
		state.PublicURL = m.publicURL
		started := m.startedAt
		state.StartedAt = &started
		if !m.lastSeenAt.IsZero() {
			last := m.lastSeenAt
			state.LastSeenAt = &last
		}
		return state
	}
	if path, err := m.findTunnel(); err != nil {
		state.Status = "BRIDGE_RUNTIME_MISSING"
		state.Dependency = "cloudflared"
		if state.LastError == "" {
			state.LastError = err.Error()
		}
	} else {
		state.Status = "READY_TO_START"
		state.Dependency = path
	}
	return state
}

func (m *remoteBridgeManager) Start() (remoteBridgeState, error) {
	m.mu.Lock()
	if m.publicURL != "" && m.server != nil {
		state := m.stateLocked()
		m.mu.Unlock()
		return state, nil
	}
	m.lastError = ""
	m.mu.Unlock()

	cloudflared, err := m.findTunnel()
	if err != nil {
		m.setError(err)
		return m.State(), err
	}
	token, err := newRemoteBridgeToken()
	if err != nil {
		m.setError(err)
		return m.State(), err
	}
	handler, err := mcpedge.NewRemoteHTTPHandler(m.backend, mcpedge.RemoteHTTPOptions{
		PathToken:          token,
		AllowedOriginHosts: []string{"claude.ai"},
		Observe:            m.observe,
	})
	if err != nil {
		m.setError(err)
		return m.State(), err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		m.setError(err)
		return m.State(), err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	localURL := "http://" + listener.Addr().String()
	serveDone := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveDone <- err
	}()

	baseURL, stopTunnel, tunnelDone, err := m.startTunnel(m.ctx, cloudflared, localURL)
	if err != nil {
		_ = server.Shutdown(context.Background())
		_ = listener.Close()
		<-serveDone
		m.setError(err)
		return m.State(), err
	}
	if err := waitRemoteBridgeReady(m.ctx, baseURL, token); err != nil {
		if stopTunnel != nil {
			_ = stopTunnel()
		}
		_ = server.Shutdown(context.Background())
		_ = listener.Close()
		<-serveDone
		m.setError(err)
		return m.State(), err
	}
	publicURL := strings.TrimRight(baseURL, "/") + "/mcp/" + token
	now := time.Now().UTC()

	m.mu.Lock()
	m.server = server
	m.listener = listener
	m.publicURL = publicURL
	m.localURL = localURL + "/mcp/" + token
	m.startedAt = now
	m.lastSeenAt = time.Time{}
	m.initialized = false
	m.toolsListed = false
	m.requests = 0
	m.tunnelStop = stopTunnel
	m.tunnelDone = tunnelDone
	m.lastError = ""
	m.mu.Unlock()

	go m.watchTunnel(tunnelDone, serveDone)
	return m.State(), nil
}

func (m *remoteBridgeManager) Stop() error {
	m.mu.Lock()
	server := m.server
	stopTunnel := m.tunnelStop
	m.server = nil
	m.listener = nil
	m.publicURL = ""
	m.localURL = ""
	m.tunnelStop = nil
	m.tunnelDone = nil
	m.initialized = false
	m.toolsListed = false
	m.requests = 0
	m.lastSeenAt = time.Time{}
	m.mu.Unlock()

	var errs []error
	if stopTunnel != nil {
		if err := stopTunnel(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, err)
		}
	}
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (m *remoteBridgeManager) Close() error { return m.Stop() }

func (m *remoteBridgeManager) localEndpoint() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.localURL
}

func (m *remoteBridgeManager) stateLocked() remoteBridgeState {
	state := remoteBridgeState{Status: "LINK_READY", Provider: "cloudflare-quick-tunnel", PublicURL: m.publicURL, Initialized: m.initialized, ToolsListed: m.toolsListed, Requests: m.requests, TemporaryLink: true, LastError: m.lastError}
	if m.initialized {
		state.Status = "CONNECTED"
	}
	if !m.startedAt.IsZero() {
		started := m.startedAt
		state.StartedAt = &started
	}
	if !m.lastSeenAt.IsZero() {
		last := m.lastSeenAt
		state.LastSeenAt = &last
	}
	return state
}

func (m *remoteBridgeManager) observe(event mcpedge.RemoteHTTPEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests++
	m.lastSeenAt = event.At
	if event.JSONRPCMethod == "initialize" {
		m.initialized = true
	}
	if event.JSONRPCMethod == "tools/list" {
		m.toolsListed = true
	}
}

func (m *remoteBridgeManager) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.lastError = err.Error()
	}
}

func (m *remoteBridgeManager) watchTunnel(tunnelDone <-chan error, serveDone <-chan error) {
	select {
	case err := <-tunnelDone:
		if err != nil {
			m.setError(fmt.Errorf("remote tunnel exited: %w", err))
		}
		_ = m.Stop()
	case err := <-serveDone:
		if err != nil {
			m.setError(fmt.Errorf("remote MCP HTTP server exited: %w", err))
		}
		_ = m.Stop()
	case <-m.ctx.Done():
		_ = m.Stop()
	}
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
	return "", errors.New("cloudflared is not installed; remote MCP quick link cannot start")
}

func newRemoteBridgeToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate remote MCP capability token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func waitRemoteBridgeReady(ctx context.Context, baseURL, token string) error {
	healthURL := strings.TrimRight(baseURL, "/") + "/health/" + token
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusNoContent {
				return nil
			}
			lastErr = fmt.Errorf("public bridge health returned HTTP %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("public bridge did not become reachable: %w", lastErr)
		case <-ticker.C:
		}
	}
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
