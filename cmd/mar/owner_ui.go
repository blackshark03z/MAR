package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
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
	"mar/internal/service"
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
	svc             *service.TaskService
	session         ownerMCPClient
	mcpMu           sync.Mutex
	brainMode       string
	providerBaseURL string
	apiKeyEnv       string
	model           string
	reasoning       string
	executable      string
	dbPath          string
	dataRoot        string
	goPath          string
	maxWorkers      int
	sessionToken    string
}

type ownerProjectView struct {
	ID            string               `json:"id"`
	Root          string               `json:"root"`
	CreatedAt     time.Time            `json:"created_at"`
	Head          string               `json:"head,omitempty"`
	HeadError     string               `json:"head_error,omitempty"`
	Supported     bool                 `json:"supported"`
	SupportReason string               `json:"support_reason,omitempty"`
	Policy        domain.ProjectPolicy `json:"policy"`
}

type ownerProjectRequest struct {
	ID   string `json:"id,omitempty"`
	Root string `json:"root"`
}

type ownerProjectPolicyRequest struct {
	LocalFileWrite bool `json:"local_file_write"`
	LocalGitWrite  bool `json:"local_git_write"`
}

type ownerFeedbackRequest struct {
	IdempotencyKey string                      `json:"idempotency_key,omitempty"`
	Verdict        domain.OwnerFeedbackVerdict `json:"verdict"`
	Message        string                      `json:"message,omitempty"`
}

type ownerTaskView struct {
	ID                string                 `json:"id"`
	ProjectID         string                 `json:"project_id"`
	Goal              string                 `json:"goal"`
	State             domain.TaskState       `json:"state"`
	UpdatedAt         time.Time              `json:"updated_at"`
	NeedsAttention    bool                   `json:"needs_attention"`
	ResultVerdict     domain.ResultVerdict   `json:"result_verdict,omitempty"`
	IntegrationStatus string                 `json:"integration_status,omitempty"`
	CandidateRevision string                 `json:"candidate_revision,omitempty"`
	OwnerFeedback     *domain.OwnerFeedback  `json:"owner_feedback,omitempty"`
	Usage             domain.ResourceSummary `json:"usage"`
}

type ownerConnectionView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Transport string   `json:"transport"`
	Summary   string   `json:"summary"`
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
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
		svc:             service.NewTaskService(db),
		session:         session,
		brainMode:       strings.ToLower(strings.TrimSpace(opts.BrainMode)),
		providerBaseURL: strings.TrimSpace(opts.ProviderBaseURL),
		apiKeyEnv:       strings.TrimSpace(opts.APIKeyEnv),
		model:           strings.TrimSpace(opts.Model),
		reasoning:       strings.TrimSpace(opts.Reasoning),
		executable:      executable,
		dbPath:          opts.DBPath,
		dataRoot:        opts.DataRoot,
		goPath:          opts.GoPath,
		maxWorkers:      opts.MaxWorkers,
		sessionToken:    newOwnerUIID("session"),
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

const ownerSessionHeader = "X-MAR-Owner-Token"

func (b *ownerUIBackend) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", b.serveIndex)
	mux.HandleFunc("GET /api/runtime", b.serveRuntime)
	mux.HandleFunc("GET /api/projects", b.serveProjects)
	mux.HandleFunc("POST /api/projects", b.addProject)
	mux.HandleFunc("POST /api/projects/{projectID}/policy", b.updateProjectPolicy)
	mux.HandleFunc("GET /api/tasks", b.serveTasks)
	mux.HandleFunc("POST /api/tasks", b.submitTask)
	mux.HandleFunc("GET /api/tasks/{taskID}/status", b.proxyTaskRead("status"))
	mux.HandleFunc("GET /api/tasks/{taskID}/result", b.proxyTaskRead("result"))
	mux.HandleFunc("GET /api/tasks/{taskID}/inspect", b.proxyTaskRead("inspect"))
	mux.HandleFunc("GET /api/tasks/{taskID}/brain-turn", b.proxyTaskRead("brain_turn"))
	mux.HandleFunc("POST /api/tasks/{taskID}/cancel", b.cancelTask)
	mux.HandleFunc("POST /api/tasks/{taskID}/input", b.inputTask)
	mux.HandleFunc("GET /api/tasks/{taskID}/feedback", b.serveTaskFeedback)
	mux.HandleFunc("POST /api/tasks/{taskID}/feedback", b.recordTaskFeedback)
	return withOwnerUIHeaders(b.withOwnerRequestBoundary(mux))
}

func (b *ownerUIBackend) withOwnerRequestBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ownerHostAllowed(r.Host) {
			writeOwnerError(w, http.StatusForbidden, errors.New("owner UI request host is not loopback"))
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			contentType := strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0])
			if !strings.EqualFold(contentType, "application/json") {
				writeOwnerError(w, http.StatusUnsupportedMediaType, errors.New("owner UI mutations require application/json"))
				return
			}
			provided := r.Header.Get(ownerSessionHeader)
			if b.sessionToken == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(b.sessionToken)) != 1 {
				writeOwnerError(w, http.StatusForbidden, errors.New("owner UI session token is missing or invalid"))
				return
			}
			if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") || !ownerOriginAllowed(r) {
				writeOwnerError(w, http.StatusForbidden, errors.New("owner UI mutation origin is not authorized"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func ownerHostAllowed(rawHost string) bool {
	host := strings.TrimSpace(rawHost)
	if host == "" {
		return false
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func ownerOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	prefix := "http://"
	if !strings.HasPrefix(strings.ToLower(origin), prefix) {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(origin[len(prefix):], "/"), r.Host)
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
	html := strings.ReplaceAll(ownerUIHTML, "__MAR_OWNER_SESSION_TOKEN__", b.sessionToken)
	_, _ = w.Write([]byte(html))
}

func (b *ownerUIBackend) serveRuntime(w http.ResponseWriter, _ *http.Request) {
	providerReady := strings.TrimSpace(b.providerBaseURL) != "" && strings.TrimSpace(b.apiKeyEnv) != "" && strings.TrimSpace(b.model) != "" && strings.TrimSpace(os.Getenv(b.apiKeyEnv)) != ""
	nextAction := "Provider brain is configured for autonomous task cognition from this UI."
	if b.brainMode == "web" {
		nextAction = "Use an MCP-capable client. Local desktop clients can launch MAR over stdio; cloud ChatWeb clients require a supported remote MCP/tunnel path."
	} else if !providerReady {
		nextAction = "Provider brain is not fully configured; relaunch MAR UI with provider base URL, model, and API key environment configured."
	}
	stdioArgs := []string{"mcp-stdio", "-db", b.dbPath, "-data-root", b.dataRoot, "-brain", "web", "-model", "gpt-5.6-sol", "-reasoning", "high", "-go", b.goPath, "-max-workers", fmt.Sprint(b.maxWorkers)}
	connections := []ownerConnectionView{
		{ID: "claude-desktop", Name: "Claude Desktop / local MCP client", Status: "AVAILABLE_LOCAL", Transport: "stdio", Summary: "Runs MAR locally with the current OS user. Configure the client to launch this executable and arguments.", Command: b.executable, Args: stdioArgs},
		{ID: "chatgpt", Name: "ChatGPT", Status: "REMOTE_BRIDGE_REQUIRED", Transport: "remote-mcp", Summary: "This MAR build is local/stdio. ChatGPT cloud cannot be labeled connected until a supported remote MCP or secure tunnel path is configured and observed."},
	}
	providerStatus := "NOT_CONFIGURED"
	if providerReady {
		providerStatus = "READY"
	}
	connections = append(connections, ownerConnectionView{ID: "provider", Name: "Autonomous provider", Status: providerStatus, Transport: "provider-api", Summary: "Provider usage is autonomous when configured. API key values are never returned by the Console."})
	writeOwnerJSON(w, http.StatusOK, map[string]any{
		"brain_mode":                    b.brainMode,
		"model":                         b.model,
		"reasoning":                     b.reasoning,
		"provider_ready":                providerReady,
		"web_brain_requires_mcp_client": b.brainMode == "web",
		"brain_next_action":             nextAction,
		"connections":                   connections,
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
		view.Head, view.SupportReason = validateOwnerProjectRoot(r.Context(), project.Root)
		view.Supported = view.SupportReason == ""
		if !view.Supported {
			view.HeadError = view.SupportReason
		}
		if policy, policyErr := b.db.GetProjectPolicy(r.Context(), project.ID); policyErr == nil {
			view.Policy = policy
		} else {
			view.SupportReason = strings.TrimSpace(strings.Join([]string{view.SupportReason, "Project policy unavailable: " + policyErr.Error()}, "; "))
			view.Supported = false
		}
		views = append(views, view)
	}
	writeOwnerJSON(w, http.StatusOK, map[string]any{"projects": views})
}

func (b *ownerUIBackend) addProject(w http.ResponseWriter, r *http.Request) {
	if b.svc == nil {
		writeOwnerError(w, http.StatusServiceUnavailable, errors.New("project service is unavailable"))
		return
	}
	var req ownerProjectRequest
	if err := decodeOwnerJSON(r, &req); err != nil {
		writeOwnerError(w, http.StatusBadRequest, err)
		return
	}
	root, err := filepath.Abs(strings.TrimSpace(req.Root))
	if err != nil || strings.TrimSpace(req.Root) == "" {
		writeOwnerError(w, http.StatusBadRequest, errors.New("project root is required"))
		return
	}
	root = filepath.Clean(root)
	head, reason := validateOwnerProjectRoot(r.Context(), root)
	if reason != "" {
		writeOwnerError(w, http.StatusBadRequest, errors.New(reason))
		return
	}
	projects, err := b.db.ListProjects(r.Context())
	if err != nil {
		writeOwnerError(w, http.StatusInternalServerError, err)
		return
	}
	for _, existing := range projects {
		if strings.EqualFold(filepath.Clean(existing.Root), root) {
			policy, _ := b.db.GetProjectPolicy(r.Context(), existing.ID)
			writeOwnerJSON(w, http.StatusOK, map[string]any{"created": false, "project": ownerProjectView{ID: existing.ID, Root: existing.Root, CreatedAt: existing.CreatedAt, Head: head, Supported: true, Policy: policy}})
			return
		}
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = uniqueOwnerProjectID(projects, root)
	}
	project, created, err := b.svc.RegisterProject(r.Context(), id, root)
	if err != nil {
		writeOwnerError(w, http.StatusBadRequest, err)
		return
	}
	policy, err := b.svc.ProjectPolicy(r.Context(), project.ID)
	if err != nil {
		writeOwnerError(w, http.StatusInternalServerError, err)
		return
	}
	writeOwnerJSON(w, http.StatusCreated, map[string]any{"created": created, "project": ownerProjectView{ID: project.ID, Root: project.Root, CreatedAt: project.CreatedAt, Head: head, Supported: true, Policy: policy}})
}

func (b *ownerUIBackend) updateProjectPolicy(w http.ResponseWriter, r *http.Request) {
	if b.svc == nil {
		writeOwnerError(w, http.StatusServiceUnavailable, errors.New("project service is unavailable"))
		return
	}
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	var req ownerProjectPolicyRequest
	if projectID == "" || decodeOwnerJSON(r, &req) != nil {
		writeOwnerError(w, http.StatusBadRequest, errors.New("valid project id and policy JSON are required"))
		return
	}
	policy, err := b.svc.UpdateProjectPolicy(r.Context(), projectID, req.LocalFileWrite, req.LocalGitWrite)
	if err != nil {
		writeOwnerError(w, http.StatusBadRequest, err)
		return
	}
	writeOwnerJSON(w, http.StatusOK, map[string]any{"policy": policy})
}

func validateOwnerProjectRoot(ctx context.Context, root string) (string, string) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", "Project folder does not exist or is not a directory."
	}
	cmd := exec.CommandContext(ctx, "git", "-C", filepath.Clean(root), "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil || !strings.EqualFold(filepath.Clean(strings.TrimSpace(string(out))), filepath.Clean(root)) {
		return "", "Project folder must be the root of a readable Git repository."
	}
	if info, err := os.Stat(filepath.Join(root, "go.mod")); err != nil || info.IsDir() {
		return "", "MAR V1 currently supports Go module projects only; go.mod was not found at the project root."
	}
	head, err := gitProjectHead(ctx, root)
	if err != nil {
		return "", err.Error()
	}
	return head, ""
}

func uniqueOwnerProjectID(projects []domain.Project, root string) string {
	base := strings.ToLower(filepath.Base(filepath.Clean(root)))
	var clean strings.Builder
	lastDash := false
	for _, r := range base {
		allowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if allowed {
			clean.WriteRune(r)
			lastDash = false
		} else if !lastDash && clean.Len() > 0 {
			clean.WriteByte('-')
			lastDash = true
		}
	}
	candidate := strings.Trim(clean.String(), "-")
	if candidate == "" {
		candidate = "project"
	}
	used := make(map[string]struct{}, len(projects))
	for _, project := range projects {
		used[strings.ToLower(project.ID)] = struct{}{}
	}
	if _, exists := used[candidate]; !exists {
		return candidate
	}
	for n := 2; ; n++ {
		value := fmt.Sprintf("%s-%d", candidate, n)
		if _, exists := used[value]; !exists {
			return value
		}
	}
}

func (b *ownerUIBackend) serveTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := b.db.ListRecentTasks(r.Context(), 30)
	if err != nil {
		writeOwnerError(w, http.StatusInternalServerError, err)
		return
	}
	views := make([]ownerTaskView, 0, len(tasks))
	for _, task := range tasks {
		view := ownerTaskView{ID: task.ID, ProjectID: task.Contract.ProjectID, Goal: task.Contract.Goal, State: task.State, UpdatedAt: task.UpdatedAt}
		view.NeedsAttention = task.State == domain.TaskInputRequired || task.State == domain.TaskBlocked || task.State == domain.TaskFailed
		result, ok, resultErr := b.db.LatestTaskResult(r.Context(), task.ID)
		if resultErr != nil {
			writeOwnerError(w, http.StatusInternalServerError, fmt.Errorf("read durable result for %s: %w", task.ID, resultErr))
			return
		}
		if ok {
			view.ResultVerdict = result.Verdict
			view.IntegrationStatus = result.IntegrationStatus
			view.CandidateRevision = result.FinalRevision
			view.Usage = result.ResourceSummary
		}
		feedback, feedbackErr := b.db.ListOwnerFeedbackByTask(r.Context(), task.ID, 1)
		if feedbackErr != nil {
			writeOwnerError(w, http.StatusInternalServerError, fmt.Errorf("read owner feedback for %s: %w", task.ID, feedbackErr))
			return
		}
		if len(feedback) > 0 {
			copy := feedback[0]
			view.OwnerFeedback = &copy
		}
		views = append(views, view)
	}
	writeOwnerJSON(w, http.StatusOK, map[string]any{"tasks": views})
}

func (b *ownerUIBackend) serveTaskFeedback(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(r.PathValue("taskID"))
	if taskID == "" {
		writeOwnerError(w, http.StatusBadRequest, errors.New("task id is required"))
		return
	}
	feedback, err := b.db.ListOwnerFeedbackByTask(r.Context(), taskID, 20)
	if err != nil {
		writeOwnerError(w, http.StatusInternalServerError, err)
		return
	}
	writeOwnerJSON(w, http.StatusOK, map[string]any{"feedback": feedback})
}

func (b *ownerUIBackend) recordTaskFeedback(w http.ResponseWriter, r *http.Request) {
	if b.svc == nil {
		writeOwnerError(w, http.StatusServiceUnavailable, errors.New("feedback service is unavailable"))
		return
	}
	taskID := strings.TrimSpace(r.PathValue("taskID"))
	var req ownerFeedbackRequest
	if taskID == "" || decodeOwnerJSON(r, &req) != nil {
		writeOwnerError(w, http.StatusBadRequest, errors.New("valid task id and feedback JSON are required"))
		return
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		req.IdempotencyKey = newOwnerUIID("feedback")
	}
	feedback, created, err := b.svc.RecordOwnerFeedback(r.Context(), taskID, req.IdempotencyKey, req.Verdict, req.Message)
	if err != nil {
		writeOwnerError(w, http.StatusBadRequest, err)
		return
	}
	writeOwnerJSON(w, http.StatusCreated, map[string]any{"created": created, "feedback": feedback})
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
	if req.NetworkAllowed {
		writeOwnerError(w, http.StatusBadRequest, errors.New("network authority is not supported by MAR V1 runtime"))
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
