package main

import (
	"context"
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
	openAITunnelInstallURL   = "https://github.com/openai/tunnel-client/releases/latest"
	openAITunnelHealthEvery  = time.Second
	openAITunnelProbeTimeout = 2 * time.Second
	openAITunnelStopTimeout  = 5 * time.Second
)

type openAITunnelState struct {
	Provider           string     `json:"provider"`
	Transport          string     `json:"transport"`
	Status             string     `json:"status"`
	Configured         bool       `json:"configured"`
	Running            bool       `json:"running"`
	Healthy            bool       `json:"healthy"`
	Ready              bool       `json:"ready"`
	Connected          bool       `json:"connected"`
	Identifier         string     `json:"identifier,omitempty"`
	ProfileName        string     `json:"profile_name"`
	APIKeyEnv          string     `json:"api_key_env"`
	AuthConfigured     bool       `json:"auth_configured"`
	ClientFound        bool       `json:"client_found"`
	ClientPath         string     `json:"client_path,omitempty"`
	InstallURL         string     `json:"install_url"`
	LocalTarget        string     `json:"local_target,omitempty"`
	AdminBaseURL       string     `json:"admin_base_url,omitempty"`
	DesiredRunning     bool       `json:"desired_running"`
	PID                int        `json:"pid,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	ConnectedSince     *time.Time `json:"connected_since,omitempty"`
	LastActivityAt     *time.Time `json:"last_activity_at,omitempty"`
	LastSuccessAt      *time.Time `json:"last_success_at,omitempty"`
	LastHealthAt       *time.Time `json:"last_health_at,omitempty"`
	LastError          string     `json:"last_error,omitempty"`
	DiagnosticsSummary string     `json:"diagnostics_summary,omitempty"`
	NextAction         string     `json:"next_action"`
}

type tunnelClientProcess interface {
	PID() int
	Done() <-chan struct{}
	Err() error
	Stop(context.Context) error
}

type openAITunnelManager struct {
	ctx      context.Context
	backend  mcpedge.Backend
	dataRoot string

	mu                 sync.Mutex
	config             store.OpenAITunnelConfig
	server             *http.Server
	listener           net.Listener
	localTarget        string
	process            tunnelClientProcess
	starting           bool
	stopping           bool
	terminationUnknown bool
	startEpoch         uint64
	startCancel        context.CancelFunc
	clientPath         string
	startedAt          time.Time
	connectedSince     time.Time
	lastActivityAt     time.Time
	lastSuccessAt      time.Time
	lastHealthAt       time.Time
	healthy            bool
	ready              bool
	lastError          string
	diagnosticsSummary string
	adminDiscovered    string
	healthWake         chan struct{}

	findClient   func(store.OpenAITunnelConfig, string) (string, error)
	runCommand   func(context.Context, string, ...string) (string, error)
	startProcess func(string, []string, func(string)) (tunnelClientProcess, error)
	probe        func(context.Context, string, string) (bool, string)
}

func newOpenAITunnelManager(ctx context.Context, backend mcpedge.Backend, dataRoot string) *openAITunnelManager {
	m := &openAITunnelManager{ctx: ctx, backend: backend, dataRoot: filepath.Clean(dataRoot), healthWake: make(chan struct{}, 1)}
	m.findClient = findTunnelClient
	m.runCommand = runTunnelClientCommand
	m.startProcess = startManagedTunnelClient
	m.probe = probeTunnelClientAdmin
	go m.healthLoop()
	return m
}

func (m *openAITunnelManager) Configure(config store.OpenAITunnelConfig) error {
	config.TunnelID = strings.TrimSpace(config.TunnelID)
	config.ProfileName = strings.TrimSpace(config.ProfileName)
	config.APIKeyEnv = strings.TrimSpace(config.APIKeyEnv)
	config.ClientPath = strings.TrimSpace(config.ClientPath)
	config.AdminBaseURL = strings.TrimRight(strings.TrimSpace(config.AdminBaseURL), "/")
	if err := config.Validate(); err != nil {
		return err
	}
	if err := m.ensureLocalServer(); err != nil {
		return err
	}
	m.mu.Lock()
	changed := m.config.TunnelID != config.TunnelID || m.config.ProfileName != config.ProfileName || m.config.APIKeyEnv != config.APIKeyEnv || m.config.ClientPath != config.ClientPath || m.config.AdminBaseURL != config.AdminBaseURL
	if changed && (m.process != nil || m.starting) {
		m.mu.Unlock()
		return errors.New("stop the active OpenAI tunnel before changing its connection configuration")
	}
	m.config = config
	if changed && m.process == nil {
		m.healthy = false
		m.ready = false
		m.connectedSince = time.Time{}
		m.lastError = ""
		m.diagnosticsSummary = ""
		m.adminDiscovered = ""
	}
	m.mu.Unlock()
	m.signalHealthRefresh()
	return nil
}

func (m *openAITunnelManager) State() openAITunnelState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stateLocked()
}

func (m *openAITunnelManager) stateLocked() openAITunnelState {
	config := m.config
	clientPath, clientErr := m.findClient(config, m.dataRoot)
	if m.clientPath != "" {
		clientPath = m.clientPath
		clientErr = nil
	}
	running := m.process != nil
	connected := running && !m.stopping && !m.terminationUnknown && m.healthy && m.ready
	state := openAITunnelState{
		Provider: "openai", Transport: "secure-mcp-tunnel", Configured: config.TunnelID != "", Running: running,
		Healthy: m.healthy, Ready: m.ready, Connected: connected, Identifier: config.TunnelID, ProfileName: config.ProfileName,
		APIKeyEnv: config.APIKeyEnv, AuthConfigured: strings.TrimSpace(os.Getenv(config.APIKeyEnv)) != "", ClientFound: clientErr == nil,
		ClientPath: clientPath, InstallURL: openAITunnelInstallURL, LocalTarget: m.localTarget,
		AdminBaseURL: firstNonEmpty(config.AdminBaseURL, m.adminDiscovered), DesiredRunning: config.DesiredRunning,
		LastError: m.lastError, DiagnosticsSummary: m.diagnosticsSummary,
	}
	if running {
		state.PID = m.process.PID()
	}
	copyTime := func(value time.Time) *time.Time {
		if value.IsZero() {
			return nil
		}
		v := value
		return &v
	}
	state.StartedAt = copyTime(m.startedAt)
	state.ConnectedSince = copyTime(m.connectedSince)
	state.LastActivityAt = copyTime(m.lastActivityAt)
	state.LastSuccessAt = copyTime(m.lastSuccessAt)
	state.LastHealthAt = copyTime(m.lastHealthAt)
	switch {
	case config.TunnelID == "":
		state.Status, state.NextAction = "MISCONFIGURED", "Nhập tunnel ID đã tạo trong OpenAI Platform."
	case clientErr != nil:
		state.Status, state.NextAction = "MISCONFIGURED", "Cài tunnel-client từ bản chính thức mới nhất hoặc chọn đúng đường dẫn file."
		if state.LastError == "" {
			state.LastError = clientErr.Error()
		}
	case !state.AuthConfigured:
		state.Status, state.NextAction = "MISCONFIGURED", "Đặt runtime API key trong biến "+config.APIKeyEnv+" rồi mở lại MAR Console."
	case m.starting:
		state.Status, state.NextAction = "CONNECTING", "MAR đang chạy Doctor và mở tunnel outbound."
	case m.stopping:
		state.Status, state.NextAction = "DISCONNECTING", "MAR đang dừng tunnel-client do mình sở hữu."
	case m.terminationUnknown:
		state.Status, state.NextAction = "DEGRADED", "Chưa xác nhận tunnel-client đã dừng; thử Dừng lại hoặc kiểm tra tiến trình trước khi restart."
	case connected:
		state.Status, state.NextAction = "CONNECTED", "Sao chép tunnel ID vào màn hình kết nối được OpenAI hỗ trợ."
	case running && m.lastError != "":
		state.Status, state.NextAction = "DEGRADED", "Tunnel vẫn còn tiến trình sau lỗi lifecycle; chẩn đoán trước khi thử lại."
	case running && (m.lastHealthAt.IsZero() || firstNonEmpty(config.AdminBaseURL, m.adminDiscovered) == ""):
		state.Status, state.NextAction = "CONNECTING", "Tunnel đang chạy; chờ readiness hoặc MCP request đầu tiên."
	case running:
		state.Status, state.NextAction = "DEGRADED", "Mở Chi tiết, chọn Chẩn đoán và kiểm tra health/readiness của tunnel-client."
	case m.lastError != "":
		state.Status, state.NextAction = "ERROR", "Chọn Chẩn đoán, sửa lỗi được báo rồi khởi động lại GPT tunnel."
	default:
		state.Status, state.NextAction = "DISCONNECTED", "Bắt đầu GPT tunnel."
	}
	return state
}

func (m *openAITunnelManager) Start() (openAITunnelState, error) {
	if err := m.ensureLocalServer(); err != nil {
		m.setError(err)
		return m.State(), err
	}
	m.mu.Lock()
	if m.process != nil || m.starting {
		state := m.stateLocked()
		m.mu.Unlock()
		return state, nil
	}
	config := m.config
	m.startEpoch++
	epoch := m.startEpoch
	startCtx, startCancel := context.WithCancel(m.ctx)
	m.startCancel = startCancel
	m.starting = true
	m.lastError = ""
	m.mu.Unlock()
	defer func() {
		startCancel()
		m.mu.Lock()
		if m.startEpoch == epoch {
			m.starting = false
			m.startCancel = nil
		}
		m.mu.Unlock()
	}()
	if config.TunnelID == "" {
		return m.failStart(epoch, errors.New("OpenAI tunnel ID is missing"))
	}
	clientPath, err := m.findClient(config, m.dataRoot)
	if err != nil {
		return m.failStart(epoch, err)
	}
	if strings.TrimSpace(os.Getenv(config.APIKeyEnv)) == "" {
		return m.failStart(epoch, fmt.Errorf("control-plane authentication is missing from environment variable %s", config.APIKeyEnv))
	}
	m.mu.Lock()
	localTarget := m.localTarget
	m.clientPath = clientPath
	m.mu.Unlock()
	if _, err := m.runCommandWithTimeout(startCtx, 30*time.Second, clientPath, "init", "--force", "--sample", "sample_mcp_with_dcr", "--profile", config.ProfileName, "--tunnel-id", config.TunnelID, "--mcp-server-url", localTarget); err != nil {
		return m.failStart(epoch, fmt.Errorf("initialize tunnel-client profile: %w", err))
	}
	diagnostic, err := m.runCommandWithTimeout(startCtx, 45*time.Second, clientPath, "doctor", "--profile", config.ProfileName, "--explain")
	diagnostic = redactTunnelOutput(diagnostic, os.Getenv(config.APIKeyEnv))
	m.mu.Lock()
	m.diagnosticsSummary = diagnostic
	m.mu.Unlock()
	if err != nil {
		return m.failStart(epoch, fmt.Errorf("tunnel-client doctor failed: %w", err))
	}
	if err := startCtx.Err(); err != nil {
		return m.failStart(epoch, fmt.Errorf("tunnel start cancelled: %w", err))
	}
	process, err := m.startProcess(clientPath, []string{"run", "--profile", config.ProfileName}, m.observeProcessOutput)
	if err != nil {
		return m.failStart(epoch, fmt.Errorf("start tunnel-client: %w", err))
	}
	now := time.Now().UTC()
	m.mu.Lock()
	if m.startEpoch != epoch || !m.starting {
		m.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), openAITunnelStopTimeout)
		defer cancel()
		_ = process.Stop(ctx)
		return m.State(), errors.New("tunnel start was superseded or stopped")
	}
	m.process = process
	m.starting = false
	m.startCancel = nil
	m.startedAt = now
	m.connectedSince = time.Time{}
	m.healthy = false
	m.ready = false
	m.lastHealthAt = time.Time{}
	m.lastError = ""
	m.mu.Unlock()
	startCancel()
	go m.watchProcess(process)
	m.signalHealthRefresh()
	return m.State(), nil
}

func (m *openAITunnelManager) failStart(epoch uint64, err error) (openAITunnelState, error) {
	m.mu.Lock()
	safeErr := newRedactedTunnelError(err, os.Getenv(m.config.APIKeyEnv), "")
	if m.startEpoch == epoch {
		m.starting = false
		m.lastError = safeErr.Error()
	}
	m.mu.Unlock()
	return m.State(), safeErr
}

func (m *openAITunnelManager) Stop() error {
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return errors.New("OpenAI tunnel is already stopping")
	}
	process := m.process
	startCancel := m.startCancel
	m.startEpoch++
	m.startCancel = nil
	m.starting = false
	m.stopping = process != nil
	m.healthy = false
	m.ready = false
	m.connectedSince = time.Time{}
	if process == nil {
		m.startedAt = time.Time{}
		m.lastError = ""
		m.terminationUnknown = false
	}
	m.mu.Unlock()
	if startCancel != nil {
		startCancel()
	}
	if process == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), openAITunnelStopTimeout)
	defer cancel()
	if err := process.Stop(ctx); err != nil && !errors.Is(err, os.ErrProcessDone) {
		m.mu.Lock()
		safeErr := newRedactedTunnelError(fmt.Errorf("stop tunnel-client: %w", err), os.Getenv(m.config.APIKeyEnv), "")
		m.stopping = false
		m.terminationUnknown = true
		m.lastError = safeErr.Error()
		m.mu.Unlock()
		return safeErr
	}
	m.mu.Lock()
	if m.process == process {
		m.process = nil
	}
	m.stopping = false
	m.terminationUnknown = false
	m.startedAt = time.Time{}
	m.lastError = ""
	m.mu.Unlock()
	return nil
}

func (m *openAITunnelManager) Restart() (openAITunnelState, error) {
	if err := m.Stop(); err != nil {
		return m.State(), err
	}
	return m.Start()
}

func (m *openAITunnelManager) Diagnose(ctx context.Context) (openAITunnelState, error) {
	m.mu.Lock()
	config := m.config
	localTarget := m.localTarget
	m.mu.Unlock()
	if config.TunnelID == "" {
		err := errors.New("OpenAI tunnel ID is missing")
		m.setError(err)
		return m.State(), err
	}
	clientPath, err := m.findClient(config, m.dataRoot)
	if err != nil {
		m.setError(err)
		return m.State(), err
	}
	if strings.TrimSpace(os.Getenv(config.APIKeyEnv)) == "" {
		err = fmt.Errorf("control-plane authentication is missing from environment variable %s", config.APIKeyEnv)
		m.setError(err)
		return m.State(), err
	}
	if output, initErr := m.runCommand(ctx, clientPath, "init", "--force", "--sample", "sample_mcp_with_dcr", "--profile", config.ProfileName, "--tunnel-id", config.TunnelID, "--mcp-server-url", localTarget); initErr != nil {
		redacted := redactTunnelOutput(output, os.Getenv(config.APIKeyEnv))
		safeErr := newRedactedTunnelError(initErr, os.Getenv(config.APIKeyEnv), redacted)
		m.setError(safeErr)
		return m.State(), safeErr
	}
	output, err := m.runCommand(ctx, clientPath, "doctor", "--profile", config.ProfileName, "--explain")
	output = redactTunnelOutput(output, os.Getenv(config.APIKeyEnv))
	m.mu.Lock()
	m.diagnosticsSummary = output
	if err == nil {
		m.lastError = ""
	}
	m.mu.Unlock()
	if err != nil {
		err = newRedactedTunnelError(fmt.Errorf("tunnel-client doctor failed: %w", err), os.Getenv(config.APIKeyEnv), "")
		m.setError(err)
	}
	return m.State(), err
}

func (m *openAITunnelManager) Close() error {
	var errs []error
	if err := m.Stop(); err != nil {
		errs = append(errs, err)
	}
	m.mu.Lock()
	server := m.server
	listener := m.listener
	m.server = nil
	m.listener = nil
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
	return errors.Join(errs...)
}

func (m *openAITunnelManager) ensureLocalServer() error {
	m.mu.Lock()
	if m.server != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	token, err := newRemoteBridgeToken()
	if err != nil {
		return err
	}
	handler, err := mcpedge.NewRemoteHTTPHandler(m.backend, mcpedge.RemoteHTTPOptions{
		PathToken: token, AllowedOriginHosts: []string{"openai.com", "chatgpt.com"}, Observe: m.observeMCP,
	})
	if err != nil {
		return fmt.Errorf("build OpenAI tunnel local MCP handler: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for OpenAI tunnel local MCP: %w", err)
	}
	serve := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		clone := r.Clone(r.Context())
		clonedURL := *r.URL
		clonedURL.Path = "/mcp/" + token
		clone.URL = &clonedURL
		handler.ServeHTTP(w, clone)
	})
	server := &http.Server{Handler: serve, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	m.mu.Lock()
	if m.server != nil {
		m.mu.Unlock()
		_ = listener.Close()
		return nil
	}
	m.server = server
	m.listener = listener
	m.localTarget = "http://" + listener.Addr().String() + "/mcp"
	m.mu.Unlock()
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.setError(fmt.Errorf("OpenAI tunnel local MCP server exited: %w", err))
		}
	}()
	return nil
}

func (m *openAITunnelManager) observeMCP(event mcpedge.RemoteHTTPEvent) {
	now := event.At.UTC()
	m.mu.Lock()
	m.lastActivityAt = now
	m.lastSuccessAt = now
	m.healthy = true
	m.ready = true
	if m.connectedSince.IsZero() {
		m.connectedSince = now
	}
	m.mu.Unlock()
}

func (m *openAITunnelManager) observeProcessOutput(line string) {
	m.mu.Lock()
	apiKeyEnv := m.config.APIKeyEnv
	m.mu.Unlock()
	redacted := redactTunnelOutput(line, os.Getenv(apiKeyEnv))
	lower := strings.ToLower(redacted)
	if !strings.Contains(lower, "admin") && !strings.Contains(lower, "healthz") && !strings.Contains(lower, "readyz") && !strings.Contains(lower, "metrics") {
		return
	}
	match := loopbackURLPattern.FindString(redacted)
	if match == "" {
		return
	}
	u, err := url.Parse(match)
	if err != nil {
		return
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	m.mu.Lock()
	if m.config.AdminBaseURL == "" {
		m.adminDiscovered = strings.TrimRight(u.String(), "/")
	}
	m.mu.Unlock()
	m.signalHealthRefresh()
}

func (m *openAITunnelManager) watchProcess(process tunnelClientProcess) {
	select {
	case <-process.Done():
		m.mu.Lock()
		if m.process != process {
			m.mu.Unlock()
			return
		}
		m.process = nil
		m.stopping = false
		m.terminationUnknown = false
		m.healthy = false
		m.ready = false
		m.connectedSince = time.Time{}
		err := process.Err()
		if err == nil {
			err = errors.New("tunnel-client exited")
		}
		m.lastError = redactTunnelOutput(err.Error(), os.Getenv(m.config.APIKeyEnv))
		m.mu.Unlock()
	case <-m.ctx.Done():
		_ = m.Stop()
	}
}

func (m *openAITunnelManager) healthLoop() {
	ticker := time.NewTicker(openAITunnelHealthEvery)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		case <-m.healthWake:
		}
		m.refreshHealth()
	}
}

func (m *openAITunnelManager) refreshHealth() {
	m.mu.Lock()
	if m.process == nil || m.stopping || m.terminationUnknown {
		m.mu.Unlock()
		return
	}
	base := firstNonEmpty(m.config.AdminBaseURL, m.adminDiscovered)
	m.mu.Unlock()
	if base == "" {
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, openAITunnelProbeTimeout)
	healthy, healthDetail := m.probe(ctx, base, "/healthz")
	cancel()
	ctx, cancel = context.WithTimeout(m.ctx, openAITunnelProbeTimeout)
	ready, readyDetail := m.probe(ctx, base, "/readyz")
	cancel()
	now := time.Now().UTC()
	m.mu.Lock()
	if m.process == nil {
		m.mu.Unlock()
		return
	}
	m.lastHealthAt = now
	m.healthy = healthy
	m.ready = ready
	if ready {
		m.lastSuccessAt = now
		if m.connectedSince.IsZero() {
			m.connectedSince = now
		}
	} else {
		m.connectedSince = time.Time{}
	}
	if !healthy || !ready {
		m.lastError = joinTunnelProbeDetails(healthDetail, readyDetail)
	} else if strings.Contains(m.lastError, "healthz") || strings.Contains(m.lastError, "readyz") {
		m.lastError = ""
	}
	m.mu.Unlock()
}

func joinTunnelProbeDetails(details ...string) string {
	parts := make([]string, 0, len(details))
	for _, detail := range details {
		if detail = strings.TrimSpace(detail); detail != "" {
			parts = append(parts, detail)
		}
	}
	return strings.Join(parts, "; ")
}

func (m *openAITunnelManager) signalHealthRefresh() {
	select {
	case m.healthWake <- struct{}{}:
	default:
	}
}

func (m *openAITunnelManager) setError(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	m.lastError = redactTunnelOutput(err.Error(), os.Getenv(m.config.APIKeyEnv))
	m.mu.Unlock()
}

func (m *openAITunnelManager) runCommandWithTimeout(parent context.Context, timeout time.Duration, executable string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return m.runCommand(ctx, executable, args...)
}

func findTunnelClient(config store.OpenAITunnelConfig, dataRoot string) (string, error) {
	if config.ClientPath != "" {
		info, err := os.Stat(config.ClientPath)
		if err != nil || info.IsDir() {
			return "", fmt.Errorf("configured tunnel-client was not found at %s", config.ClientPath)
		}
		return filepath.Clean(config.ClientPath), nil
	}
	managed := filepath.Join(dataRoot, "runtime", "tunnel-client.exe")
	if info, err := os.Stat(managed); err == nil && !info.IsDir() {
		return filepath.Clean(managed), nil
	}
	if path, err := exec.LookPath("tunnel-client"); err == nil {
		if absolute, absErr := filepath.Abs(path); absErr == nil {
			return filepath.Clean(absolute), nil
		}
		return filepath.Clean(path), nil
	}
	return "", errors.New("tunnel-client not found")
}

func runTunnelClientCommand(ctx context.Context, executable string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	return text, err
}

type execTunnelProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func startManagedTunnelClient(executable string, args []string, onOutput func(string)) (tunnelClientProcess, error) {
	cmd := exec.Command(executable, args...)
	writer := &tunnelOutputWriter{onLine: onOutput}
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &execTunnelProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		writer.Flush()
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		close(p.done)
	}()
	return p, nil
}

func (p *execTunnelProcess) PID() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
func (p *execTunnelProcess) Done() <-chan struct{} { return p.done }
func (p *execTunnelProcess) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}
func (p *execTunnelProcess) Stop(ctx context.Context) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		_ = p.cmd.Process.Kill()
	}
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		select {
		case <-p.done:
			return nil
		case <-time.After(time.Second):
			return ctx.Err()
		}
	}
}

type tunnelOutputWriter struct {
	mu     sync.Mutex
	carry  string
	onLine func(string)
}

func (w *tunnelOutputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.carry += string(p)
	for {
		index := strings.IndexByte(w.carry, '\n')
		if index < 0 {
			if len(w.carry) > 8192 {
				w.emit(w.carry[:8192])
				w.carry = w.carry[8192:]
			}
			break
		}
		w.emit(strings.TrimSpace(w.carry[:index]))
		w.carry = w.carry[index+1:]
	}
	return len(p), nil
}
func (w *tunnelOutputWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.TrimSpace(w.carry) != "" {
		w.emit(strings.TrimSpace(w.carry))
		w.carry = ""
	}
}
func (w *tunnelOutputWriter) emit(line string) {
	if w.onLine != nil && line != "" {
		w.onLine(line)
	}
}

func probeTunnelClientAdmin(ctx context.Context, baseURL, path string) (bool, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+path, nil)
	if err != nil {
		return false, path + ": " + err.Error()
	}
	resp, err := (&http.Client{Timeout: openAITunnelProbeTimeout}).Do(req)
	if err != nil {
		return false, strings.TrimPrefix(path, "/") + ": " + err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			detail = resp.Status
		}
		return false, strings.TrimPrefix(path, "/") + ": " + detail
	}
	return true, ""
}

var (
	loopbackURLPattern = regexp.MustCompile(`https?://(?:127\.0\.0\.1|localhost|\[::1\]):[0-9]+`)
	openAIKeyPattern   = regexp.MustCompile(`(?i)\b(?:sk|sess)-[A-Za-z0-9_-]{8,}`)
	bearerPattern      = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._-]{8,}`)
)

func redactTunnelOutput(value, secret string) string {
	value = strings.TrimSpace(value)
	if secret = strings.TrimSpace(secret); secret != "" {
		value = strings.ReplaceAll(value, secret, "[redacted]")
	}
	value = openAIKeyPattern.ReplaceAllString(value, "[redacted]")
	value = bearerPattern.ReplaceAllString(value, "${1}[redacted]")
	if len(value) > 4096 {
		value = value[:4096] + "…"
	}
	return value
}

type redactedTunnelError struct {
	message string
	cause   error
}

func (e *redactedTunnelError) Error() string { return e.message }
func (e *redactedTunnelError) Unwrap() error { return e.cause }

func newRedactedTunnelError(err error, secret, detail string) error {
	if err == nil {
		return nil
	}
	message := redactTunnelOutput(err.Error(), secret)
	if detail = redactTunnelOutput(detail, secret); detail != "" && !strings.Contains(message, detail) {
		message = strings.TrimSpace(message + ": " + detail)
	}
	return &redactedTunnelError{message: message, cause: err}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
