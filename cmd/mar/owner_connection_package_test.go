package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebStdioArgsDoNotPinModelOrReasoning(t *testing.T) {
	backend := &ownerUIBackend{
		dbPath:     `D:\MAR\.mar\mar.db`,
		dataRoot:   `D:\MAR\.mar`,
		goPath:     `D:\Go\bin\go.exe`,
		maxWorkers: 2,
	}
	args := backend.webStdioArgs()
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{"-model", "-reasoning"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("Web stdio path must delegate model/reasoning configuration to ChatWeb: %v", args)
		}
	}
}

func TestBuildClaudeDesktopPackageBindsCurrentRuntimeWithoutSecrets(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "mar.exe")
	if err := os.WriteFile(exe, []byte("MZ-test-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"mcp-stdio", "-db", `D:\MAR\.mar\mar.db`, "-data-root", `D:\MAR\.mar`, "-brain", "web"}
	payload, err := buildClaudeDesktopPackage(exe, args)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name] = content
	}
	if string(files["server/mar.exe"]) != "MZ-test-binary" {
		t.Fatal("Claude Desktop package did not contain the exact MAR executable")
	}
	var manifest claudeDesktopManifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ManifestVersion != "0.3" || manifest.Server.Type != "binary" || manifest.Server.MCPConfig.Command != "server/mar" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if strings.Join(manifest.Server.MCPConfig.Args, "|") != strings.Join(args, "|") {
		t.Fatalf("runtime args were not candidate-bound: got=%v want=%v", manifest.Server.MCPConfig.Args, args)
	}
	text := string(files["manifest.json"])
	for _, forbidden := range []string{"OPENAI_API_KEY=", "api_key_value", "secret"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			t.Fatalf("manifest leaked secret-shaped content %q", forbidden)
		}
	}
}

func TestOwnerUIClaudeDesktopPackageEndpointReturnsInstallableBundle(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "mar.exe")
	if err := os.WriteFile(exe, []byte("MZ-test-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	backend := &ownerUIBackend{
		executable:   exe,
		dbPath:       `D:\MAR\.mar\mar.db`,
		dataRoot:     `D:\MAR\.mar`,
		goPath:       `D:\Go\bin\go.exe`,
		maxWorkers:   2,
		sessionToken: "test-owner-token",
	}
	req := httptest.NewRequest(http.MethodPost, "/api/connections/claude-desktop/package", strings.NewReader(`{}`))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ownerSessionHeader, "test-owner-token")
	rec := httptest.NewRecorder()
	backend.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), claudeDesktopPackageName) {
		t.Fatalf("unexpected download header %q", rec.Header().Get("Content-Disposition"))
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil || len(zr.File) != 2 {
		t.Fatalf("invalid MCPB response: files=%d err=%v", len(zr.File), err)
	}
}
