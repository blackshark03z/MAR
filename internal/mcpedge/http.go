package mcpedge

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const remoteMCPMaxRequestBytes int64 = 1 << 20

const RemoteMCPSessionTimeout = 30 * time.Minute

type RemoteHTTPEvent struct {
	At            time.Time `json:"at"`
	HTTPMethod    string    `json:"http_method"`
	JSONRPCMethod string    `json:"jsonrpc_method,omitempty"`
	SessionID     string    `json:"session_id,omitempty"`
	Host          string    `json:"host,omitempty"`
	Origin        string    `json:"origin,omitempty"`
}

type RemoteHTTPOptions struct {
	PathToken          string
	AllowedOriginHosts []string
	Observe            func(RemoteHTTPEvent)
	Stateless          bool
}

func NewRemoteHTTPHandler(backend Backend, opts RemoteHTTPOptions) (http.Handler, error) {
	if backend == nil {
		return nil, errors.New("remote MCP backend is required")
	}
	token := strings.TrimSpace(opts.PathToken)
	if len(token) < 32 || strings.ContainsAny(token, "/\\?#") {
		return nil, errors.New("remote MCP path token must be at least 32 URL-safe characters")
	}
	server, err := NewServer(backend)
	if err != nil {
		return nil, err
	}
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		JSONResponse:                 true,
		Stateless:                    opts.Stateless,
		SessionTimeout:               RemoteMCPSessionTimeout,
		DisableLocalhostProtection:   true, // secret-path + Origin guard below; tunnel forwards the public Host to loopback.
		MaxRequestBodyBytes:          remoteMCPMaxRequestBytes,
		PropagateRequestCancellation: true,
	})
	expectedPath := "/mcp/" + token
	healthPath := "/health/" + token
	allowedOrigins := normalizeOriginHosts(opts.AllowedOriginHosts)
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"claude.ai"}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if subtle.ConstantTimeCompare([]byte(r.URL.Path), []byte(healthPath)) == 1 {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.URL.Path), []byte(expectedPath)) != 1 {
			http.NotFound(w, r)
			return
		}
		if !remoteOriginAllowed(r.Header.Get("Origin"), allowedOrigins) {
			http.Error(w, "remote MCP origin is not authorized", http.StatusForbidden)
			return
		}

		rpcMethod, ok := inspectRemoteRPCMethod(w, r)
		if !ok {
			return
		}
		if opts.Observe != nil {
			opts.Observe(RemoteHTTPEvent{
				At:            time.Now().UTC(),
				HTTPMethod:    r.Method,
				JSONRPCMethod: rpcMethod,
				SessionID:     strings.TrimSpace(r.Header.Get("Mcp-Session-Id")),
				Host:          r.Host,
				Origin:        strings.TrimSpace(r.Header.Get("Origin")),
			})
		}
		streamable.ServeHTTP(w, r)
	}), nil
}

func normalizeOriginHosts(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.TrimPrefix(value, ".")
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func remoteOriginAllowed(raw string, allowed []string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		// Remote MCP connectors are server-to-server and normally omit Origin.
		// If a browser-style Origin is present it must match the explicit allowlist.
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	for _, allowedHost := range allowed {
		if host == allowedHost || strings.HasSuffix(host, "."+allowedHost) {
			return true
		}
	}
	return false
}

func inspectRemoteRPCMethod(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Method != http.MethodPost || r.Body == nil {
		return "", true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, remoteMCPMaxRequestBytes+1))
	if err != nil {
		http.Error(w, "read remote MCP request", http.StatusBadRequest)
		return "", false
	}
	if int64(len(body)) > remoteMCPMaxRequestBytes {
		http.Error(w, "remote MCP request too large", http.StatusRequestEntityTooLarge)
		return "", false
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	var envelope struct {
		Method string `json:"method"`
	}
	if len(bytes.TrimSpace(body)) != 0 {
		_ = json.Unmarshal(body, &envelope)
	}
	return strings.TrimSpace(envelope.Method), true
}
