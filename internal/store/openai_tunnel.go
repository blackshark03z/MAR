package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultOpenAITunnelProfile = "mar-openai"
	DefaultOpenAITunnelKeyEnv  = "CONTROL_PLANE_API_KEY"
)

var (
	tunnelIDPattern      = regexp.MustCompile(`^tunnel_[A-Za-z0-9_-]{8,}$`)
	tunnelProfilePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	environmentName      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

type OpenAITunnelConfig struct {
	TunnelID       string    `json:"tunnel_id,omitempty"`
	ProfileName    string    `json:"profile_name"`
	APIKeyEnv      string    `json:"api_key_env"`
	ClientPath     string    `json:"client_path,omitempty"`
	AdminBaseURL   string    `json:"admin_base_url,omitempty"`
	DesiredRunning bool      `json:"desired_running"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func DefaultOpenAITunnelConfig() OpenAITunnelConfig {
	return OpenAITunnelConfig{ProfileName: DefaultOpenAITunnelProfile, APIKeyEnv: DefaultOpenAITunnelKeyEnv, UpdatedAt: time.Now().UTC()}
}

func (c OpenAITunnelConfig) Validate() error {
	c.TunnelID = strings.TrimSpace(c.TunnelID)
	c.ProfileName = strings.TrimSpace(c.ProfileName)
	c.APIKeyEnv = strings.TrimSpace(c.APIKeyEnv)
	c.ClientPath = strings.TrimSpace(c.ClientPath)
	c.AdminBaseURL = strings.TrimSpace(c.AdminBaseURL)
	if c.TunnelID != "" && !tunnelIDPattern.MatchString(c.TunnelID) {
		return errors.New("OpenAI tunnel ID must start with tunnel_ and contain at least 8 identifier characters")
	}
	if !tunnelProfilePattern.MatchString(c.ProfileName) {
		return errors.New("OpenAI tunnel profile name is invalid")
	}
	if !environmentName.MatchString(c.APIKeyEnv) {
		return errors.New("OpenAI tunnel API-key environment variable name is invalid")
	}
	if c.ClientPath != "" && !filepath.IsAbs(c.ClientPath) {
		return errors.New("tunnel-client path must be absolute when provided")
	}
	if c.AdminBaseURL != "" {
		u, err := url.Parse(c.AdminBaseURL)
		if err != nil || !strings.EqualFold(u.Scheme, "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return errors.New("tunnel-client admin URL must be a clean loopback HTTP base URL")
		}
		host := strings.Trim(u.Hostname(), "[]")
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return errors.New("tunnel-client admin URL must use loopback")
		}
	}
	if c.UpdatedAt.IsZero() {
		return errors.New("OpenAI tunnel updated_at is required")
	}
	return nil
}

func (s *SQLite) EnsureOpenAITunnelConfig(ctx context.Context) (OpenAITunnelConfig, error) {
	config, err := s.GetOpenAITunnelConfig(ctx)
	if err == nil {
		return config, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return OpenAITunnelConfig{}, err
	}
	config = DefaultOpenAITunnelConfig()
	if err := s.UpsertOpenAITunnelConfig(ctx, config); err != nil {
		return OpenAITunnelConfig{}, err
	}
	return config, nil
}

func (s *SQLite) UpsertOpenAITunnelConfig(ctx context.Context, c OpenAITunnelConfig) error {
	c.TunnelID = strings.TrimSpace(c.TunnelID)
	c.ProfileName = strings.TrimSpace(c.ProfileName)
	c.APIKeyEnv = strings.TrimSpace(c.APIKeyEnv)
	c.ClientPath = strings.TrimSpace(c.ClientPath)
	c.AdminBaseURL = strings.TrimRight(strings.TrimSpace(c.AdminBaseURL), "/")
	if err := c.Validate(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO openai_tunnel_config(singleton_id, tunnel_id, profile_name, api_key_env, client_path, admin_base_url, desired_running, updated_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(singleton_id) DO UPDATE SET
  tunnel_id=excluded.tunnel_id,
  profile_name=excluded.profile_name,
  api_key_env=excluded.api_key_env,
  client_path=excluded.client_path,
  admin_base_url=excluded.admin_base_url,
  desired_running=excluded.desired_running,
  updated_at=excluded.updated_at`,
		c.TunnelID, c.ProfileName, c.APIKeyEnv, c.ClientPath, c.AdminBaseURL, c.DesiredRunning, c.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert OpenAI tunnel config: %w", err)
	}
	return nil
}

func (s *SQLite) GetOpenAITunnelConfig(ctx context.Context) (OpenAITunnelConfig, error) {
	var c OpenAITunnelConfig
	var desired int
	var updated string
	err := s.db.QueryRowContext(ctx, `
SELECT tunnel_id, profile_name, api_key_env, client_path, admin_base_url, desired_running, updated_at
FROM openai_tunnel_config WHERE singleton_id = 1`).Scan(
		&c.TunnelID, &c.ProfileName, &c.APIKeyEnv, &c.ClientPath, &c.AdminBaseURL, &desired, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return OpenAITunnelConfig{}, ErrNotFound
	}
	if err != nil {
		return OpenAITunnelConfig{}, fmt.Errorf("get OpenAI tunnel config: %w", err)
	}
	c.DesiredRunning = desired != 0
	c.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return OpenAITunnelConfig{}, fmt.Errorf("parse OpenAI tunnel updated_at: %w", err)
	}
	return c, nil
}
