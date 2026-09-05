package aci

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cleanRelative(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) {
		return "", errors.New("path must be workspace-relative")
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes workspace")
	}
	first := strings.ToLower(strings.Split(clean, string(os.PathSeparator))[0])
	if first == ".git" {
		return "", errors.New("direct .git access is forbidden")
	}
	return clean, nil
}

func (r *Runtime) resolveExisting(rel string) (string, error) {
	clean, err := cleanRelative(rel)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(r.root, clean)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	if !inside(r.root, resolved) {
		return "", errors.New("resolved path escapes workspace")
	}
	return filepath.Clean(resolved), nil
}

func (r *Runtime) resolveExistingForWrite(rel string) (string, error) {
	clean, err := cleanRelative(rel)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(r.root, clean)
	if err := rejectSymlinkComponents(r.root, candidate); err != nil {
		return "", err
	}
	st, err := os.Stat(candidate)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("mutation target must be a regular file")
	}
	return filepath.Clean(candidate), nil
}

func (r *Runtime) resolveWriteTarget(rel string) (string, bool, string, error) {
	clean, err := cleanRelative(rel)
	if err != nil {
		return "", false, "", err
	}
	candidate := filepath.Join(r.root, clean)
	if !inside(r.root, candidate) {
		return "", false, "", errors.New("write target escapes workspace")
	}
	if err := ensureParentDirectoriesNoSymlink(r.root, filepath.Dir(candidate)); err != nil {
		return "", false, "", err
	}
	info, err := os.Lstat(candidate)
	if os.IsNotExist(err) {
		return filepath.Clean(candidate), false, "", nil
	}
	if err != nil {
		return "", false, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", false, "", errors.New("write target must be a regular non-symlink file")
	}
	b, err := os.ReadFile(candidate)
	if err != nil {
		return "", false, "", err
	}
	sum := sha256.Sum256(b)
	return filepath.Clean(candidate), true, hex.EncodeToString(sum[:]), nil
}

func ensureParentDirectoriesNoSymlink(root, parent string) error {
	rel, err := filepath.Rel(root, parent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("parent escapes workspace")
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("write parent contains symlink/non-directory: %s", current)
		}
	}
	return nil
}

func rejectSymlinkComponents(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("target escapes workspace")
	}
	current := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("mutation through symlink is forbidden: %s", current)
		}
	}
	return nil
}

func atomicWrite(path string, content []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".mar-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("atomic replace: %w", err)
	}
	return nil
}

func inside(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func (r *Runtime) relative(path string) string {
	rel, err := filepath.Rel(r.root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
