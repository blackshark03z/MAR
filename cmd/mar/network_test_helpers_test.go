package main

import (
	"net"
	"net/http"
	"testing"

	"mar/internal/testsupport"
)

type passiveTestListener struct{}

func (*passiveTestListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (*passiveTestListener) Close() error              { return nil }
func (*passiveTestListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
}

func makeOpenAITunnelPassiveForTest(m *openAITunnelManager) {
	m.mu.Lock()
	m.server = &http.Server{}
	m.listener = nil
	m.localTarget = "http://127.0.0.1:1/mcp"
	m.mu.Unlock()
}

func makeRemoteBridgePassiveForTest(m *remoteBridgeManager) {
	m.mu.Lock()
	m.server = &http.Server{}
	m.listener = &passiveTestListener{}
	m.localBaseURL = "http://127.0.0.1:1"
	m.mu.Unlock()
}

func requireLoopbackTCP(t testing.TB) {
	t.Helper()
	testsupport.RequireLoopbackTCP(t)
}

func requireGitTool(t testing.TB) string {
	t.Helper()
	return testsupport.RequireExecutable(t, "git")
}
