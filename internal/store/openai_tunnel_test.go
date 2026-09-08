package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenAITunnelConfigPersistsWithoutSecretValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mar.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	config := OpenAITunnelConfig{
		TunnelID: "tunnel_0123456789abcdef", ProfileName: "mar-openai", APIKeyEnv: "MAR_TEST_TUNNEL_KEY",
		ClientPath: `C:\tools\tunnel-client.exe`, AdminBaseURL: "http://127.0.0.1:9090", DesiredRunning: true, UpdatedAt: time.Now().UTC().Round(0),
	}
	if err := db.UpsertOpenAITunnelConfig(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.GetOpenAITunnelConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.TunnelID != config.TunnelID || got.ProfileName != config.ProfileName || got.APIKeyEnv != config.APIKeyEnv || got.ClientPath != config.ClientPath || got.AdminBaseURL != config.AdminBaseURL || !got.DesiredRunning || !got.UpdatedAt.Equal(config.UpdatedAt) {
		t.Fatalf("config did not survive restart: got=%+v want=%+v", got, config)
	}
}

func TestOpenAITunnelConfigRejectsUnsafeInputs(t *testing.T) {
	base := DefaultOpenAITunnelConfig()
	for name, mutate := range map[string]func(*OpenAITunnelConfig){
		"invalid tunnel":        func(c *OpenAITunnelConfig) { c.TunnelID = "not-a-tunnel" },
		"shell profile":         func(c *OpenAITunnelConfig) { c.ProfileName = "mar;delete" },
		"secret instead of env": func(c *OpenAITunnelConfig) { c.APIKeyEnv = "sk-secret" },
		"relative executable":   func(c *OpenAITunnelConfig) { c.ClientPath = `tools\tunnel-client.exe` },
		"remote admin":          func(c *OpenAITunnelConfig) { c.AdminBaseURL = "http://example.com:9090" },
	} {
		t.Run(name, func(t *testing.T) {
			config := base
			mutate(&config)
			if err := config.Validate(); err == nil {
				t.Fatalf("unsafe config accepted: %+v", config)
			}
		})
	}
}
