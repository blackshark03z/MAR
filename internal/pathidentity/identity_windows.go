//go:build windows

package pathidentity

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// ResolveExisting returns the canonical identity of one existing path without
// enumerating its absolute ancestor chain. MAR's LPAC can have authority for an
// exact workspace/cache path while intentionally lacking directory-list access
// to ancestors such as D:\MAR. Opening the exact path and asking Windows for
// its final handle name still resolves symlinks/junctions, allowing callers to
// compare canonical root/target identities and fail closed on escapes.
func ResolveExisting(path string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]uint16, 512)
	for {
		n, callErr := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
		if n >= uint32(len(buf)) {
			buf = make([]uint16, int(n)+1)
			continue
		}
		if callErr != nil {
			return "", callErr
		}
		if n == 0 {
			return "", errors.New("Windows returned an empty final path identity")
		}
		return filepath.Clean(normalizeFinalPath(windows.UTF16ToString(buf[:n]))), nil
	}
}

func normalizeFinalPath(path string) string {
	if strings.HasPrefix(path, `\\?\UNC\`) {
		return `\\` + strings.TrimPrefix(path, `\\?\UNC\`)
	}
	return strings.TrimPrefix(path, `\\?\`)
}
