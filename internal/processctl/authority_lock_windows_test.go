//go:build windows

package processctl

import (
	"path/filepath"
	"testing"
)

func TestExclusiveFileLeaseAllowsOnlyOneDaemonAuthorityAndReleasesOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mar.db.daemon.lock")
	first, acquired, err := TryAcquireExclusiveFileLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired || first == nil {
		t.Fatal("first daemon authority lease was not acquired")
	}
	defer first.Close()

	second, acquired, err := TryAcquireExclusiveFileLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if acquired || second != nil {
		if second != nil {
			second.Close()
		}
		t.Fatal("second daemon authority lease must be denied while first is held")
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, acquired, err := TryAcquireExclusiveFileLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired || third == nil {
		t.Fatal("daemon authority lease was not reacquired after owner release")
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
}
