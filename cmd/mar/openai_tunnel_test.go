package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mar/internal/service"
	"mar/internal/store"
)

type fakeTunnelProcess struct {
	mu      sync.Mutex
	done    chan struct{}
	err     error
	stopErr error
	stopped bool
}

func newFakeTunnelProcess() *fakeTunnelProcess     { return &fakeTunnelProcess{done: make(chan struct{})} }
func (p *fakeTunnelProcess) PID() int              { return 4242 }
func (p *fakeTunnelProcess) Done() <-chan struct{} { return p.done }
func (p *fakeTunnelProcess) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}
func (p *fakeTunnelProcess) finish(err error) {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.err = err
	p.stopped = true
	close(p.done)
	p.mu.Unlock()
}
func (p *fakeTunnelProcess) Stop(context.Context) error {
	p.mu.Lock()
	err := p.stopErr
	p.mu.Unlock()
	if err != nil {
		return err
	}
	p.finish(nil)
	return nil
}

func testOpenAITunnelManager(t *testing.T) (*openAITunnelManager, *fakeTunnelProcess, *[]string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := newOpenAITunnelManager(ctx, service.NewTaskService(db), t.TempDir())
	// Most lifecycle tests do not need an actual socket. Keep them executable in
	// MAR's networkless verifier LPAC while dedicated host integration tests below
	// still exercise the real loopback transport when that capability exists.
	makeOpenAITunnelPassiveForTest(m)
	t.Cleanup(func() { _ = m.Close() })
	process := newFakeTunnelProcess()
	var calls []string
	m.findClient = func(store.OpenAITunnelConfig, string) (string, error) { return `C:\fake\tunnel-client.exe`, nil }
	m.runCommand = func(_ context.Context, executable string, args ...string) (string, error) {
		calls = append(calls, executable+" "+strings.Join(args, " "))
		if len(args) > 0 && args[0] == "doctor" {
			return "doctor: profile ready", nil
		}
		return "", nil
	}
	m.startProcess = func(executable string, args []string, _ func(string)) (tunnelClientProcess, error) {
		calls = append(calls, executable+" "+strings.Join(args, " "))
		return process, nil
	}
	return m, process, &calls
}

func validOpenAITunnelConfig() store.OpenAITunnelConfig {
	return store.OpenAITunnelConfig{
		TunnelID: "tunnel_0123456789abcdef", ProfileName: "mar-openai", APIKeyEnv: "MAR_TUNNEL_TEST_KEY", UpdatedAt: time.Now().UTC(),
	}
}

func TestOpenAITunnelMissingConfigClientAndAuthFailClosed(t *testing.T) {
	t.Run("missing tunnel ID", func(t *testing.T) {
		m, _, _ := testOpenAITunnelManager(t)
		config := store.DefaultOpenAITunnelConfig()
		if err := m.Configure(config); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Start(); err == nil || m.State().Status != "MISCONFIGURED" {
			t.Fatalf("missing tunnel ID did not fail closed: state=%+v err=%v", m.State(), err)
		}
	})
	t.Run("missing client", func(t *testing.T) {
		m, _, _ := testOpenAITunnelManager(t)
		config := validOpenAITunnelConfig()
		t.Setenv(config.APIKeyEnv, "test-secret")
		m.findClient = func(store.OpenAITunnelConfig, string) (string, error) {
			return "", errors.New("tunnel-client not found")
		}
		if err := m.Configure(config); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Start(); err == nil || m.State().ClientFound {
			t.Fatalf("missing client was hidden: state=%+v err=%v", m.State(), err)
		}
	})
	t.Run("missing auth", func(t *testing.T) {
		m, _, _ := testOpenAITunnelManager(t)
		config := validOpenAITunnelConfig()
		if err := m.Configure(config); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Start(); err == nil || m.State().AuthConfigured {
			t.Fatalf("missing auth was hidden: state=%+v err=%v", m.State(), err)
		}
	})
}

func TestOpenAITunnelStartDoctorHealthReadinessStopAndCommandSafety(t *testing.T) {
	m, process, calls := testOpenAITunnelManager(t)
	config := validOpenAITunnelConfig()
	t.Setenv(config.APIKeyEnv, "unit-test-runtime-credential")
	config.AdminBaseURL = "http://127.0.0.1:1"
	m.probe = func(context.Context, string, string) (bool, string) { return true, "" }
	if err := m.Configure(config); err != nil {
		t.Fatal(err)
	}
	state, err := m.Start()
	if err != nil {
		t.Fatal(err)
	}
	if !state.Running || state.PID != 4242 {
		t.Fatalf("process did not start: %+v", state)
	}
	m.refreshHealth()
	state = m.State()
	if state.Status != "CONNECTED" || !state.Healthy || !state.Ready || state.LastSuccessAt == nil {
		t.Fatalf("health/readiness did not produce connected state: %+v", state)
	}
	joined := strings.Join(*calls, "\n")
	for _, required := range []string{"init --force --sample sample_mcp_with_dcr", "--tunnel-id " + config.TunnelID, "--mcp-server-url http://127.0.0.1:", "doctor --profile mar-openai --explain", "run --profile mar-openai"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing tunnel-client command %q in %s", required, joined)
		}
	}
	if strings.Contains(joined, "unit-test-runtime-credential") {
		t.Fatalf("runtime API key leaked into argv: %s", joined)
	}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	process.mu.Lock()
	stopped := process.stopped
	process.mu.Unlock()
	if !stopped || m.State().Running || m.State().Status != "DISCONNECTED" {
		t.Fatalf("stop did not terminate owned process: %+v", m.State())
	}
}

func TestOpenAITunnelActualLocalMCPActivityEstablishesConnectionWithoutAdminGuess(t *testing.T) {
	requireLoopbackTCP(t)
	m, _, _ := testOpenAITunnelManager(t)
	m.mu.Lock()
	m.server = nil
	m.localTarget = ""
	m.mu.Unlock()
	config := validOpenAITunnelConfig()
	t.Setenv(config.APIKeyEnv, "test-secret")
	if err := m.Configure(config); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	state := m.State()
	if state.Status != "CONNECTING" || state.Connected {
		t.Fatalf("expected CONNECTING before real MCP traffic: %+v", state)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "openai-tunnel-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: state.LocalTarget, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ListTools(context.Background(), &mcp.ListToolsParams{}); err != nil {
		t.Fatal(err)
	}
	_ = session.Close()
	state = m.State()
	if state.Status != "CONNECTED" || state.LastActivityAt == nil || state.LastSuccessAt == nil {
		t.Fatalf("real MCP activity did not establish connection truth: %+v", state)
	}
}

func TestOpenAITunnelCrashAutoRecoversDesiredStateWithoutStaleConnectionTruth(t *testing.T) {
	m, first, _ := testOpenAITunnelManager(t)
	config := validOpenAITunnelConfig()
	config.DesiredRunning = true
	config.AdminBaseURL = "http://127.0.0.1:1"
	t.Setenv(config.APIKeyEnv, "test-secret")
	m.recoveryDelay = func(int) time.Duration { return 0 }
	m.recoverySettle = 5 * time.Millisecond
	m.probe = func(context.Context, string, string) (bool, string) { return true, "" }
	second := newFakeTunnelProcess()
	starts := 0
	m.startProcess = func(string, []string, func(string)) (tunnelClientProcess, error) {
		starts++
		if starts == 1 {
			return first, nil
		}
		return second, nil
	}
	if err := m.Configure(config); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	m.refreshHealth()
	if !m.State().Connected {
		t.Fatalf("first process never became connected: %+v", m.State())
	}
	first.finish(errors.New("simulated crash"))
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state := m.State()
		if starts >= 2 && state.Running && !state.RecoveryInProgress {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	state := m.State()
	if starts != 2 || !state.Running || state.RecoveryAttempts != 1 || state.Identifier != config.TunnelID {
		t.Fatalf("desired-state recovery did not launch exactly one replacement: starts=%d state=%+v", starts, state)
	}
	if state.Connected && (state.StartedAt == nil || state.ConnectedSince == nil || state.ConnectedSince.Before(*state.StartedAt)) {
		t.Fatalf("recovery reused stale connected truth: %+v", state)
	}
	if !state.Connected {
		m.refreshHealth()
	}
	if !m.State().Connected {
		t.Fatalf("replacement did not establish fresh readiness: %+v", m.State())
	}
}

func TestOpenAITunnelAutoRecoveryExhaustsBoundedBudgetWithoutSpin(t *testing.T) {
	m, first, _ := testOpenAITunnelManager(t)
	config := validOpenAITunnelConfig()
	config.DesiredRunning = true
	t.Setenv(config.APIKeyEnv, "test-secret")
	m.recoveryDelay = func(int) time.Duration { return 0 }
	m.recoverySettle = time.Millisecond
	starts := 0
	m.startProcess = func(string, []string, func(string)) (tunnelClientProcess, error) {
		starts++
		if starts == 1 {
			return first, nil
		}
		return nil, errors.New("simulated restart failure")
	}
	if err := m.Configure(config); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	first.finish(errors.New("simulated crash"))
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state := m.State()
		if !state.RecoveryInProgress && state.RecoveryAttempts == openAITunnelRecoveryLimit && strings.Contains(state.LastError, "automatic tunnel recovery exhausted") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	state := m.State()
	if state.Running || state.Status != "ERROR" || state.RecoveryAttempts != openAITunnelRecoveryLimit || !strings.Contains(state.LastError, "automatic tunnel recovery exhausted") {
		t.Fatalf("recovery budget did not fail closed: starts=%d state=%+v", starts, state)
	}
	wantStarts := 1 + openAITunnelRecoveryLimit
	time.Sleep(25 * time.Millisecond)
	if starts != wantStarts {
		t.Fatalf("recovery spun after budget exhaustion: starts=%d want=%d", starts, wantStarts)
	}
}

func TestOpenAITunnelRestartRequiresFreshReadiness(t *testing.T) {
	m, first, _ := testOpenAITunnelManager(t)
	config := validOpenAITunnelConfig()
	t.Setenv(config.APIKeyEnv, "test-secret")
	config.AdminBaseURL = "http://127.0.0.1:1"
	m.probe = func(context.Context, string, string) (bool, string) { return true, "" }
	second := newFakeTunnelProcess()
	starts := 0
	m.startProcess = func(string, []string, func(string)) (tunnelClientProcess, error) {
		starts++
		if starts == 1 {
			return first, nil
		}
		return second, nil
	}
	if err := m.Configure(config); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	m.refreshHealth()
	firstState := m.State()
	if !firstState.Connected || firstState.LastSuccessAt == nil {
		t.Fatalf("first connection never became ready: %+v", firstState)
	}
	state, err := m.Restart()
	if err != nil {
		t.Fatal(err)
	}
	if starts != 2 {
		t.Fatalf("restart did not start a fresh process: state=%+v starts=%d", state, starts)
	}
	if state.Connected && (state.StartedAt == nil || state.ConnectedSince == nil || state.LastHealthAt == nil || state.ConnectedSince.Before(*state.StartedAt)) {
		t.Fatalf("restart reused stale readiness: first=%+v restarted=%+v", firstState, state)
	}
	if !state.Connected {
		m.refreshHealth()
	}
	if !m.State().Connected {
		t.Fatalf("fresh readiness did not reconnect: %+v", m.State())
	}
}

func TestOpenAITunnelRejectsIdentityChangeWhileActive(t *testing.T) {
	m, _, _ := testOpenAITunnelManager(t)
	config := validOpenAITunnelConfig()
	t.Setenv(config.APIKeyEnv, "test-secret")
	if err := m.Configure(config); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	changed := config
	changed.TunnelID = "tunnel_fedcba9876543210"
	if err := m.Configure(changed); err == nil {
		t.Fatal("active tunnel accepted a new identity without an explicit stop/restart")
	}
	state := m.State()
	if state.Identifier != config.TunnelID || !state.Running {
		t.Fatalf("rejected configuration changed active truth: %+v", state)
	}
}

func TestOpenAITunnelDoctorFailureBlocksProcessStart(t *testing.T) {
	m, _, _ := testOpenAITunnelManager(t)
	config := validOpenAITunnelConfig()
	secret := "unit-test-runtime-credential"
	t.Setenv(config.APIKeyEnv, secret)
	started := false
	m.runCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "doctor" {
			return "control plane rejected profile with " + secret, errors.New("doctor exit 1 exposed " + secret)
		}
		return "", nil
	}
	m.startProcess = func(string, []string, func(string)) (tunnelClientProcess, error) {
		started = true
		return newFakeTunnelProcess(), nil
	}
	if err := m.Configure(config); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(); err == nil || started {
		t.Fatalf("failed Doctor did not block run: started=%v err=%v", started, err)
	} else if strings.Contains(err.Error(), secret) {
		t.Fatalf("Doctor error leaked runtime credential: %v", err)
	}
	state := m.State()
	if state.Running || state.Status != "ERROR" || !strings.Contains(state.LastError, "doctor") || strings.Contains(state.LastError, secret) || strings.Contains(state.DiagnosticsSummary, secret) {
		t.Fatalf("Doctor failure was not actionable: %+v", state)
	}
}

func TestOpenAITunnelStopCancelsInFlightStart(t *testing.T) {
	m, _, _ := testOpenAITunnelManager(t)
	config := validOpenAITunnelConfig()
	t.Setenv(config.APIKeyEnv, "test-secret")
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	var once sync.Once
	m.runCommand = func(ctx context.Context, _ string, _ ...string) (string, error) {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		close(cancelled)
		return "", ctx.Err()
	}
	processStarted := false
	m.startProcess = func(string, []string, func(string)) (tunnelClientProcess, error) {
		processStarted = true
		return newFakeTunnelProcess(), nil
	}
	if err := m.Configure(config); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := m.Start()
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Start never entered tunnel-client init")
	}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel in-flight tunnel-client init")
	}
	select {
	case err := <-result:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("Start did not return cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled Start did not return")
	}
	state := m.State()
	if processStarted || state.Running || state.Status != "DISCONNECTED" || state.LastError != "" {
		t.Fatalf("Stop raced into a stale start or error: processStarted=%v state=%+v", processStarted, state)
	}
}

func TestOpenAITunnelStopFailureRetainsProcessAuthorityAndBlocksRestart(t *testing.T) {
	m, process, _ := testOpenAITunnelManager(t)
	config := validOpenAITunnelConfig()
	t.Setenv(config.APIKeyEnv, "test-secret")
	if err := m.Configure(config); err != nil {
		t.Fatal(err)
	}
	starts := 0
	m.startProcess = func(string, []string, func(string)) (tunnelClientProcess, error) {
		starts++
		return process, nil
	}
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	process.mu.Lock()
	process.stopErr = context.DeadlineExceeded
	process.mu.Unlock()
	if err := m.Stop(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop did not surface physical termination failure: %v", err)
	}
	state := m.State()
	if !state.Running || state.Connected || state.Status != "DEGRADED" || !strings.Contains(state.LastError, "stop tunnel-client") {
		t.Fatalf("Stop failure lost process authority or connection truth: %+v", state)
	}
	if _, err := m.Restart(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Restart did not fail closed on the retained process: %v", err)
	}
	if starts != 1 {
		t.Fatalf("Restart launched a second process after unconfirmed stop: starts=%d", starts)
	}
	process.mu.Lock()
	process.stopErr = nil
	process.mu.Unlock()
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestJoinTunnelProbeDetailsOmitsEmptySeparators(t *testing.T) {
	if got := joinTunnelProbeDetails("", "readyz returned 503"); got != "readyz returned 503" {
		t.Fatalf("unexpected probe detail: %q", got)
	}
}

func TestTunnelClientAdminProbeAndSecretRedaction(t *testing.T) {
	requireLoopbackTCP(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	ok, detail := probeTunnelClientAdmin(context.Background(), server.URL, "/healthz")
	if !ok || detail != "" {
		t.Fatalf("health parser failed: ok=%v detail=%q", ok, detail)
	}
	ok, detail = probeTunnelClientAdmin(context.Background(), server.URL, "/readyz")
	if ok || !strings.Contains(detail, "not ready") {
		t.Fatalf("readiness parser hid failure: ok=%v detail=%q", ok, detail)
	}
	secret := "unit-test-secret-value"
	redacted := redactTunnelOutput("header="+secret+" credential="+secret, secret)
	if strings.Contains(redacted, secret) || !strings.Contains(redacted, "[redacted]") {
		t.Fatalf("secret redaction failed: %q", redacted)
	}
}

func TestTunnelControlPlaneStallDetectionUsesUpstreamDeadline(t *testing.T) {
	now := time.Now().UTC()
	snapshot := &tunnelControlPlaneHealth{Component: "control-plane", Status: "ok", State: "polling", Details: tunnelControlPlaneDetails{CurrentPollAgeSeconds: 39, DeadlineSeconds: 35}}
	if stalled, detail := tunnelControlPlaneStalled(snapshot, now); stalled || detail != "" {
		t.Fatalf("valid long poll was misclassified as stalled: stalled=%v detail=%q", stalled, detail)
	}
	snapshot.Details.CurrentPollAgeSeconds = 41
	if stalled, detail := tunnelControlPlaneStalled(snapshot, now); !stalled || !strings.Contains(detail, "control-plane poll stalled") {
		t.Fatalf("deadline-exceeded poll was not detected: stalled=%v detail=%q", stalled, detail)
	}
	snapshot.State = "backoff"
	snapshot.Details.CurrentPollAgeSeconds = 300
	if stalled, detail := tunnelControlPlaneStalled(snapshot, now); stalled || detail != "" {
		t.Fatalf("upstream backoff must not be force-restarted: stalled=%v detail=%q", stalled, detail)
	}
	lastSuccess := now.Add(-41 * time.Second)
	snapshot.State = "idle"
	snapshot.Details.LastSuccess = &lastSuccess
	if stalled, detail := tunnelControlPlaneStalled(snapshot, now); !stalled || !strings.Contains(detail, "state=idle") {
		t.Fatalf("stale idle poller was not detected: stalled=%v detail=%q", stalled, detail)
	}
}

func TestTunnelClientControlPlaneProbeReadsV0015Health(t *testing.T) {
	requireLoopbackTCP(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/control-plane" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"schema_version\":1,\"component\":\"control-plane\",\"status\":\"ok\",\"state\":\"polling\",\"details\":{\"last_success\":\"2026-09-26T08:15:04Z\",\"consecutive_failures\":0,\"current_poll_age_seconds\":0.4,\"deadline_seconds\":35}}"))
	}))
	defer server.Close()

	snapshot := probeTunnelClientControlPlane(context.Background(), server.URL)
	if snapshot == nil {
		t.Fatal("v0.0.15 control-plane health was not parsed")
	}
	if snapshot.Component != "control-plane" || snapshot.State != "polling" || snapshot.Details.DeadlineSeconds != 35 || snapshot.Details.LastSuccess == nil {
		t.Fatalf("unexpected control-plane snapshot: %+v", snapshot)
	}
}

func TestOpenAITunnelStalledControlPlanePollUsesBoundedDesiredRecovery(t *testing.T) {
	m, first, _ := testOpenAITunnelManager(t)
	config := validOpenAITunnelConfig()
	config.DesiredRunning = true
	config.AdminBaseURL = "http://127.0.0.1:1"
	t.Setenv(config.APIKeyEnv, "test-secret")
	m.recoveryDelay = func(int) time.Duration { return 0 }
	m.recoverySettle = 5 * time.Millisecond
	m.probe = func(context.Context, string, string) (bool, string) { return true, "" }
	probeCalls := 0
	m.probeControlPlane = func(context.Context, string) *tunnelControlPlaneHealth {
		probeCalls++
		age := 1.0
		if probeCalls == 1 {
			age = 41
		}
		return &tunnelControlPlaneHealth{Component: "control-plane", Status: "ok", State: "polling", Details: tunnelControlPlaneDetails{CurrentPollAgeSeconds: age, DeadlineSeconds: 35}}
	}
	second := newFakeTunnelProcess()
	starts := 0
	m.startProcess = func(string, []string, func(string)) (tunnelClientProcess, error) {
		starts++
		if starts == 1 {
			return first, nil
		}
		return second, nil
	}
	if err := m.Configure(config); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	m.refreshHealth()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state := m.State()
		if starts == 2 && state.Running && !state.RecoveryInProgress {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	state := m.State()
	first.mu.Lock()
	firstStopped := first.stopped
	first.mu.Unlock()
	if !firstStopped || starts != 2 || !state.Running || state.RecoveryAttempts != 1 {
		t.Fatalf("stalled poll did not use exactly one bounded replacement: firstStopped=%v starts=%d state=%+v", firstStopped, starts, state)
	}
	if state.Identifier != config.TunnelID {
		t.Fatalf("stalled recovery changed tunnel identity: state=%+v", state)
	}
}

func TestOpenAITunnelConfigureRejectsNonLoopbackAdminAndInvalidTunnelID(t *testing.T) {
	base := validOpenAITunnelConfig()
	cases := []store.OpenAITunnelConfig{base, base}
	cases[0].TunnelID = "not-a-tunnel"
	cases[1].AdminBaseURL = "http://192.168.1.2:9000"
	for _, config := range cases {
		if err := config.Validate(); err == nil {
			t.Fatalf("unsafe config accepted: %+v", config)
		}
	}
	if reflect.DeepEqual(cases[0], cases[1]) {
		t.Fatal("test cases unexpectedly collapsed")
	}
}
