//go:build windows

package pathidentity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// ResolveExisting validates an existing trusted boundary path without walking
// or opening its absolute ancestor chain. MAR grants LPAC authority to exact
// workspace/cache roots while intentionally withholding access to ancestors
// such as D:\MAR. The trusted boundary itself must not be a symlink or other
// Windows reparse point.
func ResolveExisting(path string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if err := rejectReparsePoint(abs, info); err != nil {
		return "", err
	}
	return abs, nil
}

// ResolveWithin validates target relative to a trusted root and walks only
// from that root downward. Every component must be an ordinary path component;
// symlinks and junction/reparse points are rejected so a lexical in-root path
// cannot redirect outside the granted boundary.
func ResolveWithin(root, target string) (string, error) {
	root, err := ResolveExisting(root)
	if err != nil {
		return "", fmt.Errorf("resolve trusted root: %w", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !rootInfo.IsDir() {
		return "", errors.New("trusted root is not a directory")
	}
	target, err = filepath.Abs(strings.TrimSpace(target))
	if err != nil {
		return "", err
	}
	target = filepath.Clean(target)
	rel, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes trusted root")
	}
	if rel == "." {
		return root, nil
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if err := rejectReparsePoint(current, info); err != nil {
			return "", err
		}
	}
	return target, nil
}

func rejectReparsePoint(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink path is outside trusted path policy: %s", path)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("reparse-point path is outside trusted path policy: %s", path)
	}
	return nil
}
