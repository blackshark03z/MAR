//go:build !windows

package pathidentity

import (
	"errors"
	"path/filepath"
	"strings"
)

func ResolveExisting(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func ResolveWithin(root, target string) (string, error) {
	root, err := ResolveExisting(root)
	if err != nil {
		return "", err
	}
	target, err = ResolveExisting(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes trusted root")
	}
	return target, nil
}
