package testsupport

import (
	"net"
	"os/exec"
	"testing"
	"time"
)

// RequireLoopbackTCP keeps host-only integration tests active on normal MAR
// release runs while allowing the same package to execute inside the verifier's
// intentionally networkless LPAC. The skip is capability-probed, not driven by
// a mutable environment flag.
func RequireLoopbackTCP(t testing.TB) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback TCP is unavailable in this verification environment: %v", err)
	}
	defer listener.Close()

	accepted := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if conn != nil {
			_ = conn.Close()
		}
		accepted <- acceptErr
	}()

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 500*time.Millisecond)
	if err != nil {
		t.Skipf("loopback TCP connect is unavailable in this verification environment: %v", err)
	}
	_ = conn.Close()
	select {
	case acceptErr := <-accepted:
		if acceptErr != nil {
			t.Skipf("loopback TCP accept is unavailable in this verification environment: %v", acceptErr)
		}
	case <-time.After(time.Second):
		t.Skip("loopback TCP probe timed out in this verification environment")
	}
}

// RequireExecutable gates tests that deliberately exercise a host-installed
// development tool. The verifier's sanitized PATH intentionally omits tools
// that are not granted to the candidate LPAC; normal host release runs still
// execute these tests when the tool is installed.
func RequireExecutable(t testing.TB, name string) string {
	t.Helper()
	if name == "git" {
		inside, err := InAppContainer()
		if err != nil {
			t.Fatalf("query Git verification capability: %v", err)
		}
		if inside {
			t.Skip("host Git/MSYS fixtures require execution outside an AppContainer; sandbox Git remains broker-only")
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is unavailable in this verification environment: %v", name, err)
	}
	return path
}
