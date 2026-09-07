package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mar/internal/domain"
	"mar/internal/store"
)

type ownerUIOptions struct {
	DBPath          string
	DataRoot        string
	Listen          string
	BrainMode       string
	ProviderBaseURL string
	APIKeyEnv       string
	Model           string
	Reasoning       string
	GoPath          string
	MaxWorkers      int
}

type ownerMCPClient interface {
	CallTool(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error)
}

type ownerUIBackend struct {
	db              *store.SQLite
	session         ownerMCPClient
	mcpMu           sync.Mutex
	brainMode       string
	providerBaseURL string
	apiKeyEnv       string
	model           string
	reasoning       string
}

type ownerProjectView struct {
	ID        string    `json:"id"`
	Root      string    `json:"root"`
	CreatedAt time.Time `json:"created_at"`
	Head      string    `json:"head,omitempty"`
	HeadError string    `json:"head_error,omitempty"`
}

type ownerSubmitRequest struct {
	IdempotencyKey      string   `json:"idempotency_key"`
	ProjectID           string   `json:"project_id"`
	BaseRevision        string   `json:"base_revision"`
	Goal                string   `json:"goal"`
	Acceptance          []string `json:"acceptance"`
	Boundaries          []string `json:"boundaries"`
	NonGoals            []string `json:"non_goals"`
	VerificationProfile string   `json:"verification_profile"`
	Priority            string   `json:"priority"`
	LocalFileWrite      bool     `json:"local_file_write"`
	LocalGitWrite       bool     `json:"local_git_write"`
	NetworkAllowed      bool     `json:"network_allowed"`
}

type ownerInputRequest struct {
	Message string `json:"message"`
}

func runOwnerUI(ctx context.Context, opts ownerUIOptions) error {
	if strings.TrimSpace(opts.Listen) == "" {
		opts.Listen = "127.0.0.1:8787"
	}
	if err := requireLoopbackListen(opts.Listen); err != nil {
		return err
	}
	if opts.MaxWorkers <= 0 {
		return errors.New("max-workers must be positive")
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{
		"mcp-stdio",
		"-db", opts.DBPath,
		"-data-root", opts.DataRoot,
		"-brain", opts.BrainMode,
		"-model", opts.Model,
		"-reasoning", opts.Reasoning,
		"-go", opts.GoPath,
		"-max-workers", fmt.Sprint(opts.MaxWorkers),
	}
	if strings.TrimSpace(opts.ProviderBaseURL) != "" {
		args = append(args, "-provider-base-url", opts.ProviderBaseURL)
	}
	if strings.TrimSpace(opts.APIKeyEnv) != "" {
		args = append(args, "-api-key-env", opts.APIKeyEnv)
	}
	cmd := exec.Command(executable, args...)
	cmd.Stderr = os.Stderr
	transport := &mcp.CommandTransport{Command: cmd, TerminateDuration: 15 * time.Second}
	client := mcp.NewClient(&mcp.Implementation{Name: "mar-owner-ui", Version: "1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect owner UI to MAR MCP runtime: %w", err)
	}
	defer session.Close()

	db, err := store.Open(opts.DBPath)
	if err != nil {
		return fmt.Errorf("open owner UI project registry: %w", err)
	}
	defer db.Close()

	backend := &ownerUIBackend{
		db:              db,
		session:         session,
		brainMode:       strings.ToLower(strings.TrimSpace(opts.BrainMode)),
		providerBaseURL: strings.TrimSpace(opts.ProviderBaseURL),
		apiKeyEnv:       strings.TrimSpace(opts.APIKeyEnv),
		model:           strings.TrimSpace(opts.Model),
		reasoning:       strings.TrimSpace(opts.Reasoning),
	}
	mux := backend.routes()
	listener, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		return fmt.Errorf("listen for owner UI: %w", err)
	}
	defer listener.Close()

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()
	fmt.Fprintf(os.Stdout, "MAR owner UI: http://%s\n", listener.Addr().String())

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		<-serveErr
		return ctx.Err()
	case err := <-serveErr:
		return err
	}
}

func requireLoopbackListen(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid UI listen address: %w", err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("owner UI must listen on loopback only in MAR V1")
	}
	return nil
}

func (b *ownerUIBackend) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", b.serveIndex)
	mux.HandleFunc("GET /api/runtime", b.serveRuntime)
	mux.HandleFunc("GET /api/projects", b.serveProjects)
	mux.HandleFunc("POST /api/tasks", b.submitTask)
	mux.HandleFunc("GET /api/tasks/{taskID}/status", b.proxyTaskRead("status"))
	mux.HandleFunc("GET /api/tasks/{taskID}/result", b.proxyTaskRead("result"))
	mux.HandleFunc("GET /api/tasks/{taskID}/inspect", b.proxyTaskRead("inspect"))
	mux.HandleFunc("GET /api/tasks/{taskID}/brain-turn", b.proxyTaskRead("brain_turn"))
	mux.HandleFunc("POST /api/tasks/{taskID}/cancel", b.cancelTask)
	mux.HandleFunc("POST /api/tasks/{taskID}/input", b.inputTask)
	return withOwnerUIHeaders(mux)
}

func withOwnerUIHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (b *ownerUIBackend) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(ownerUIHTML))
}

func (b *ownerUIBackend) serveRuntime(w http.ResponseWriter, _ *http.Request) {
	providerReady := strings.TrimSpace(b.providerBaseURL) != "" && strings.TrimSpace(b.apiKeyEnv) != "" && strings.TrimSpace(b.model) != "" && strings.TrimSpace(os.Getenv(b.apiKeyEnv)) != ""
	nextAction := "Provider brain is configured for autonomous task cognition from this UI."
	if b.brainMode == "web" {
		nextAction = "Connect an MCP-capable ChatGPT client before running a Web-brain task, or relaunch MAR UI in provider mode after configuring provider base URL, model, and API key environment."
	} else if !providerReady {
		nextAction = "Provider brain is not fully configured; relaunch MAR UI with provider base URL, model, and API key environment configured."
	}
	writeOwnerJSON(w, http.StatusOK, map[string]any{
		"brain_mode":                    b.brainMode,
		"model":                         b.model,
		"reasoning":                     b.reasoning,
		"provider_ready":                providerReady,
		"web_brain_requires_mcp_client": b.brainMode == "web",
		"brain_next_action":             nextAction,
	})
}

func (b *ownerUIBackend) serveProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := b.db.ListProjects(r.Context())
	if err != nil {
		writeOwnerError(w, http.StatusInternalServerError, err)
		return
	}
	views := make([]ownerProjectView, 0, len(projects))
	for _, project := range projects {
		view := ownerProjectView{ID: project.ID, Root: project.Root, CreatedAt: project.CreatedAt}
		head, err := gitProjectHead(r.Context(), project.Root)
		if err != nil {
			view.HeadError = err.Error()
		} else {
			view.Head = head
		}
		views = append(views, view)
	}
	writeOwnerJSON(w, http.StatusOK, map[string]any{"projects": views})
}

func gitProjectHead(ctx context.Context, root string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", filepath.Clean(root), "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read Git HEAD: %w", err)
	}
	head := strings.TrimSpace(string(out))
	if head == "" {
		return "", errors.New("Git HEAD is empty")
	}
	return head, nil
}

func (b *ownerUIBackend) submitTask(w http.ResponseWriter, r *http.Request) {
	var req ownerSubmitRequest
	if err := decodeOwnerJSON(r, &req); err != nil {
		writeOwnerError(w, http.StatusBadRequest, err)
		return
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.BaseRevision = strings.TrimSpace(req.BaseRevision)
	req.Goal = strings.TrimSpace(req.Goal)
	req.Acceptance = trimOwnerStrings(req.Acceptance)
	req.Boundaries = trimOwnerStrings(req.Boundaries)
	req.NonGoals = trimOwnerStrings(req.NonGoals)
	req.VerificationProfile = strings.TrimSpace(req.VerificationProfile)
	if req.VerificationProfile == "" {
		req.VerificationProfile = "go-standard"
	}
	if !slices.Contains([]string{"go-standard", "go-docs"}, req.VerificationProfile) {
		writeOwnerError(w, http.StatusBadRequest, errors.New("verification_profile must be go-standard or go-docs"))
		return
	}
	req.Priority = strings.ToUpper(strings.TrimSpace(req.Priority))
	if req.Priority == "" {
		req.Priority = "P2"
	}
	if !slices.Contains([]string{"P0", "P1", "P2", "P3"}, req.Priority) {
		writeOwnerError(w, http.StatusBadRequest, errors.New("priority must be P0, P1, P2, or P3"))
		return
	}
	if req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey); req.IdempotencyKey == "" {
		req.IdempotencyKey = newOwnerUIID("owner-ui")
	}
	contract := domain.GoalContract{
		Goal:         req.Goal,
		Acceptance:   req.Acceptance,
		Boundaries:   req.Boundaries,
		NonGoals:     req.NonGoals,
		ProjectID:    req.ProjectID,
		BaseRevision: req.BaseRevision,
		Authority: domain.Authority{
			LocalFileWrite: req.LocalFileWrite,
			LocalGitWrite:  req.LocalGitWrite,
			NetworkAllowed: req.NetworkAllowed,
			RemoteGitWrite: false,
			DeployAllowed:  false,
		},
		VerificationProfile: req.VerificationProfile,
		Priority:            req.Priority,
	}
	if err := contract.Validate(); err != nil {
		writeOwnerError(w, http.StatusBadRequest, err)
		return
	}
	result, err := b.callTool(r.Context(), "submit", map[string]any{"idempotency_key": req.IdempotencyKey, "contract": contract})
	if err != nil {
		writeOwnerError(w, http.StatusBadGateway, err)
		return
	}
	writeOwnerJSON(w, http.StatusCreated, result)
}

func (b *ownerUIBackend) proxyTaskRead(tool string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := strings.TrimSpace(r.PathValue("taskID"))
		if taskID == "" {
			writeOwnerError(w, http.StatusBadRequest, errors.New("task id is required"))
			return
		}
		result, err := b.callTool(r.Context(), tool, map[string]any{"task_id": taskID})
		if err != nil {
			writeOwnerError(w, http.StatusBadGateway, err)
			return
		}
		writeOwnerJSON(w, http.StatusOK, result)
	}
}

func (b *ownerUIBackend) cancelTask(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(r.PathValue("taskID"))
	if taskID == "" {
		writeOwnerError(w, http.StatusBadRequest, errors.New("task id is required"))
		return
	}
	result, err := b.callTool(r.Context(), "cancel", map[string]any{
		"task_id":         taskID,
		"idempotency_key": newOwnerUIID("cancel"),
		"reason":          "Owner requested cancellation from MAR owner UI.",
	})
	if err != nil {
		writeOwnerError(w, http.StatusBadGateway, err)
		return
	}
	writeOwnerJSON(w, http.StatusOK, result)
}

func (b *ownerUIBackend) inputTask(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(r.PathValue("taskID"))
	if taskID == "" {
		writeOwnerError(w, http.StatusBadRequest, errors.New("task id is required"))
		return
	}
	var req ownerInputRequest
	if err := decodeOwnerJSON(r, &req); err != nil {
		writeOwnerError(w, http.StatusBadRequest, err)
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		writeOwnerError(w, http.StatusBadRequest, errors.New("input message is required"))
		return
	}
	result, err := b.callTool(r.Context(), "input", map[string]any{
		"task_id":         taskID,
		"idempotency_key": newOwnerUIID("input"),
		"message":         req.Message,
	})
	if err != nil {
		writeOwnerError(w, http.StatusBadGateway, err)
		return
	}
	writeOwnerJSON(w, http.StatusOK, result)
}

func (b *ownerUIBackend) callTool(ctx context.Context, name string, args map[string]any) (any, error) {
	b.mcpMu.Lock()
	defer b.mcpMu.Unlock()
	result, err := b.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("%s returned no result", name)
	}
	if result.IsError {
		return nil, errors.New(ownerToolErrorText(result))
	}
	return result.StructuredContent, nil
}

func ownerToolErrorText(result *mcp.CallToolResult) string {
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok && strings.TrimSpace(text.Text) != "" {
			return strings.TrimSpace(text.Text)
		}
	}
	return "MAR MCP tool returned an error"
}

func decodeOwnerJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("decode JSON request: %w", err)
	}
	return nil
}

func trimOwnerStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func newOwnerUIID(prefix string) string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UTC().UnixNano())
}

func writeOwnerJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeOwnerError(w http.ResponseWriter, status int, err error) {
	writeOwnerJSON(w, status, map[string]any{"error": err.Error()})
}
