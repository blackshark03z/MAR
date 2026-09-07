package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const claudeDesktopPackageName = "mar-claude-desktop.mcpb"

type claudeDesktopManifest struct {
	ManifestVersion string `json:"manifest_version"`
	Name            string `json:"name"`
	DisplayName     string `json:"display_name"`
	Version         string `json:"version"`
	Description     string `json:"description"`
	Author          struct {
		Name string `json:"name"`
	} `json:"author"`
	Repository struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"repository"`
	Server struct {
		Type       string `json:"type"`
		EntryPoint string `json:"entry_point"`
		MCPConfig  struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcp_config"`
	} `json:"server"`
	Tools []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tools"`
	Compatibility struct {
		ClaudeDesktop string   `json:"claude_desktop"`
		Platforms     []string `json:"platforms"`
	} `json:"compatibility"`
}

func buildClaudeDesktopPackage(executable string, args []string) ([]byte, error) {
	executable = filepath.Clean(executable)
	if executable == "." || executable == "" {
		return nil, errors.New("MAR executable path is required")
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		return nil, fmt.Errorf("read MAR executable for Claude Desktop package: %w", err)
	}
	if len(binary) == 0 {
		return nil, errors.New("MAR executable is empty")
	}

	manifest := claudeDesktopManifest{
		ManifestVersion: "0.3",
		Name:            "mar-local-coding-runtime",
		DisplayName:     "MAR Local Coding Runtime",
		Version:         "1.0.0",
		Description:     "Connect Claude Desktop to the local MAR coding control plane over MCP stdio.",
	}
	manifest.Author.Name = "MAR"
	manifest.Repository.Type = "git"
	manifest.Repository.URL = "https://github.com/blackshark03z/MAR"
	manifest.Server.Type = "binary"
	manifest.Server.EntryPoint = "server/mar.exe"
	manifest.Server.MCPConfig.Command = "server/mar"
	manifest.Server.MCPConfig.Args = append([]string(nil), args...)
	manifest.Server.MCPConfig.Env = map[string]string{}
	manifest.Compatibility.ClaudeDesktop = ">=0.10.0"
	manifest.Compatibility.Platforms = []string{"win32"}
	for _, tool := range []struct{ name, description string }{
		{"submit", "Submit one bounded durable coding goal."},
		{"status", "Read durable task status."},
		{"steer", "Provide bounded non-contract steering."},
		{"input", "Provide requested owner input."},
		{"cancel", "Request durable task cancellation."},
		{"result", "Read the latest revision-bound task result."},
		{"inspect", "Inspect bounded task execution evidence."},
		{"brain_turn", "Read the pending Web-brain reasoning turn."},
		{"brain_respond", "Return the exact Web-brain turn response."},
	} {
		manifest.Tools = append(manifest.Tools, struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}{Name: tool.name, Description: tool.description})
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Claude Desktop manifest: %w", err)
	}

	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	if err := writeZipFile(zw, "manifest.json", manifestJSON); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := writeZipFile(zw, "server/mar.exe", binary); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("finalize Claude Desktop package: %w", err)
	}
	return out.Bytes(), nil
}

func writeZipFile(zw *zip.Writer, name string, content []byte) error {
	entry, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create %s in Claude Desktop package: %w", name, err)
	}
	if _, err := io.Copy(entry, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("write %s in Claude Desktop package: %w", name, err)
	}
	return nil
}
