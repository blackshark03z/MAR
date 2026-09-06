//go:build windows

package processctl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// ExclusiveFileLease is a process-scoped Windows file handle opened with no
// sharing. The kernel releases the lease automatically if the owning process
// exits, including abrupt termination.
type ExclusiveFileLease struct {
	handle windows.Handle
}

func TryAcquireExclusiveFileLease(path string) (*ExclusiveFileLease, bool, error) {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return nil, false, errors.New("exclusive lease path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, false, fmt.Errorf("prepare exclusive lease directory: %w", err)
	}
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, false, err
	}
	handle, err := windows.CreateFile(
		ptr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("acquire exclusive lease %s: %w", path, err)
	}
	return &ExclusiveFileLease{handle: handle}, true, nil
}

func (l *ExclusiveFileLease) Close() error {
	if l == nil || l.handle == 0 || l.handle == windows.InvalidHandle {
		return nil
	}
	handle := l.handle
	l.handle = 0
	return windows.CloseHandle(handle)
}
