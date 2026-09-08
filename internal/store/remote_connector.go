package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	RemoteConnectorClaudeWeb  = "claude-web"
	RemoteConnectorChatGPTWeb = "chatgpt-web"

	RemoteConnectorModeTemporary = "temporary"
	RemoteConnectorModeStable    = "stable"
)

type RemoteConnectorProfile struct {
	ID            string    `json:"id"`
	StableBaseURL string    `json:"stable_base_url,omitempty"`
	PathToken     string    `json:"-"`
	PreferredMode string    `json:"preferred_mode"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (p RemoteConnectorProfile) Validate() error {
	if p.ID != RemoteConnectorClaudeWeb && p.ID != RemoteConnectorChatGPTWeb {
		return errors.New("remote connector id must be claude-web or chatgpt-web")
	}
	if len(strings.TrimSpace(p.PathToken)) < 32 {
		return errors.New("remote connector path token must be at least 32 characters")
	}
	if p.PreferredMode != RemoteConnectorModeTemporary && p.PreferredMode != RemoteConnectorModeStable {
		return errors.New("remote connector preferred mode must be temporary or stable")
	}
	if p.UpdatedAt.IsZero() {
		return errors.New("remote connector updated_at is required")
	}
	return nil
}

func (s *SQLite) UpsertRemoteConnectorProfile(ctx context.Context, p RemoteConnectorProfile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO remote_connector_profiles(connector_id, stable_base_url, path_token, preferred_mode, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(connector_id) DO UPDATE SET
  stable_base_url=excluded.stable_base_url,
  path_token=excluded.path_token,
  preferred_mode=excluded.preferred_mode,
  updated_at=excluded.updated_at`,
		p.ID, strings.TrimSpace(p.StableBaseURL), strings.TrimSpace(p.PathToken), p.PreferredMode, p.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert remote connector profile: %w", err)
	}
	return nil
}

func (s *SQLite) GetRemoteConnectorProfile(ctx context.Context, id string) (RemoteConnectorProfile, error) {
	var p RemoteConnectorProfile
	var updated string
	err := s.db.QueryRowContext(ctx, `
SELECT connector_id, stable_base_url, path_token, preferred_mode, updated_at
FROM remote_connector_profiles WHERE connector_id = ?`, strings.TrimSpace(id)).Scan(
		&p.ID, &p.StableBaseURL, &p.PathToken, &p.PreferredMode, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RemoteConnectorProfile{}, ErrNotFound
	}
	if err != nil {
		return RemoteConnectorProfile{}, fmt.Errorf("get remote connector profile: %w", err)
	}
	p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return RemoteConnectorProfile{}, fmt.Errorf("parse remote connector updated_at: %w", err)
	}
	return p, nil
}

func (s *SQLite) ListRemoteConnectorProfiles(ctx context.Context) ([]RemoteConnectorProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT connector_id, stable_base_url, path_token, preferred_mode, updated_at
FROM remote_connector_profiles ORDER BY connector_id`)
	if err != nil {
		return nil, fmt.Errorf("list remote connector profiles: %w", err)
	}
	defer rows.Close()
	var out []RemoteConnectorProfile
	for rows.Next() {
		var p RemoteConnectorProfile
		var updated string
		if err := rows.Scan(&p.ID, &p.StableBaseURL, &p.PathToken, &p.PreferredMode, &updated); err != nil {
			return nil, fmt.Errorf("scan remote connector profile: %w", err)
		}
		p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("parse remote connector updated_at: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate remote connector profiles: %w", err)
	}
	return out, nil
}
