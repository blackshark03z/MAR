package retention

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const activationMetadataFile = "activation.json"

type Result struct {
	RemovedActivationBackups []string `json:"removed_activation_backups"`
	RemovedStagingFiles      []string `json:"removed_staging_files"`
	RemovedHistoricalScratch []string `json:"removed_historical_scratch"`
	ClearedRebuildableCaches []string `json:"cleared_rebuildable_caches"`
	FreedBytes               int64    `json:"freed_bytes"`
}

var pressureRebuildableCacheDirs = []string{
	filepath.Join("runtime", "go-build-cache"),
	filepath.Join("runtime", "gocache"),
	filepath.Join("runtime", "diag-gocache"),
}

type activationMetadata struct {
	CreatedAtUTC   string `json:"created_at_utc"`
	TargetRevision string `json:"target_revision"`
	TargetRelease  string `json:"target_release"`
}

type activationBackup struct {
	name      string
	path      string
	createdAt time.Time
	size      int64
}

func Prune(dataRoot string, keepActivation int) (Result, error) {
	if keepActivation < 1 {
		return Result{}, errors.New("keep activation backups must be at least 1")
	}
	root, err := cleanRoot(dataRoot)
	if err != nil {
		return Result{}, err
	}
	result := Result{}
	if err := pruneActivationBackups(root, keepActivation, &result); err != nil {
		return Result{}, err
	}
	if err := pruneStaging(root, &result); err != nil {
		return Result{}, err
	}
	if err := pruneHistoricalRuntimeScratch(root, &result); err != nil {
		return Result{}, err
	}
	if err := pruneHistoricalDataRootScratch(root, &result); err != nil {
		return Result{}, err
	}
	if err := pruneLegacyRuntimeExecutables(root, &result); err != nil {
		return Result{}, err
	}
	if err := pruneLegacyRecoveryScratch(root, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

// PrunePressureCaches removes only explicitly classified rebuildable cache
// contents. Cache roots themselves are preserved so Windows ACLs granted to
// sandboxed workers remain intact. Module caches and evidence/history are not
// pressure-pruned because they may be required for offline execution or are not
// yet classified as safely reproducible.
func PrunePressureCaches(dataRoot string) (Result, error) {
	root, err := cleanRoot(dataRoot)
	if err != nil {
		return Result{}, err
	}
	result := Result{}
	for _, relative := range pressureRebuildableCacheDirs {
		if err := pruneCacheContents(root, relative, &result); err != nil {
			return Result{}, err
		}
	}
	sort.Strings(result.ClearedRebuildableCaches)
	return result, nil
}

func cleanRoot(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("MAR data root is required")
	}
	root, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve MAR data root: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("stat MAR data root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("MAR data root must be a real directory")
	}
	return filepath.Clean(root), nil
}

func pruneActivationBackups(root string, keep int, result *Result) error {
	recovery := filepath.Join(root, "recovery")
	ok, err := realOptionalDir(recovery)
	if err != nil || !ok {
		return err
	}
	entries, err := os.ReadDir(recovery)
	if err != nil {
		return fmt.Errorf("read recovery root: %w", err)
	}
	backups := make([]activationBackup, 0)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "activation-") || entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		dir := filepath.Join(recovery, entry.Name())
		if !isDirectChild(recovery, dir) {
			continue
		}
		marker := filepath.Join(dir, activationMetadataFile)
		meta, valid, err := readActivationMetadata(marker)
		if err != nil {
			return err
		}
		if !valid {
			continue
		}
		size, safe, err := safeTreeSize(dir)
		if err != nil {
			return err
		}
		if !safe {
			continue
		}
		backups = append(backups, activationBackup{name: entry.Name(), path: dir, createdAt: meta, size: size})
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].createdAt.Equal(backups[j].createdAt) {
			return backups[i].name > backups[j].name
		}
		return backups[i].createdAt.After(backups[j].createdAt)
	})
	for i := len(backups) - 1; i >= keep; i-- {
		backup := backups[i]
		if !isDirectChild(recovery, backup.path) {
			return errors.New("activation backup escaped recovery root")
		}
		info, err := os.Lstat(backup.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		if err := os.RemoveAll(backup.path); err != nil {
			return fmt.Errorf("remove activation backup %s: %w", backup.name, err)
		}
		result.RemovedActivationBackups = append(result.RemovedActivationBackups, backup.name)
		result.FreedBytes += backup.size
	}
	return nil
}

func readActivationMetadata(path string) (time.Time, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return time.Time{}, false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false, err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	var meta activationMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return time.Time{}, false, nil
	}
	if strings.TrimSpace(meta.CreatedAtUTC) == "" || strings.TrimSpace(meta.TargetRevision) == "" || strings.TrimSpace(meta.TargetRelease) == "" {
		return time.Time{}, false, nil
	}
	created, err := time.Parse(time.RFC3339Nano, meta.CreatedAtUTC)
	if err != nil {
		return time.Time{}, false, nil
	}
	return created.UTC(), true, nil
}

func pruneHistoricalRuntimeScratch(root string, result *Result) error {
	runtimeRoot := filepath.Join(root, "runtime")
	ok, err := realOptionalDir(runtimeRoot)
	if err != nil || !ok {
		return err
	}
	entries, err := os.ReadDir(runtimeRoot)
	if err != nil {
		return fmt.Errorf("read runtime root: %w", err)
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(runtimeRoot, name)
		if !isDirectChild(runtimeRoot, path) {
			continue
		}
		if strings.HasPrefix(name, "owner_realuse_") {
			if err := removeHistoricalScratchPath(root, path, filepath.ToSlash(filepath.Join("runtime", name)), result); err != nil {
				return err
			}
			continue
		}
		if name == "audit-20260915" {
			if err := pruneHistoricalAudit20260915(root, path, result); err != nil {
				return err
			}
		}
	}
	sort.Strings(result.RemovedHistoricalScratch)
	return nil
}

func pruneLegacyRecoveryScratch(root string, result *Result) error {
	recoveryRoot := filepath.Join(root, "recovery")
	ok, err := realOptionalDir(recoveryRoot)
	if err != nil || !ok {
		return err
	}
	prefixes := []string{
		"cads-bench-purge-",
		"cads-purge-",
		"activate-requalification",
		"python-support-",
		"activation-20260918-",
		"activate-retention",
		"owner-reconcile-",
		"index-refresh-",
	}
	entries, err := os.ReadDir(recoveryRoot)
	if err != nil {
		return fmt.Errorf("read recovery root for historical scratch: %w", err)
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		name := entry.Name()
		matched := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(name, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		path := filepath.Join(recoveryRoot, name)
		if !isDirectChild(recoveryRoot, path) {
			continue
		}
		if _, err := os.Lstat(filepath.Join(path, activationMetadataFile)); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := removeHistoricalScratchPath(root, path, filepath.ToSlash(filepath.Join("recovery", name)), result); err != nil {
			return err
		}
	}
	sort.Strings(result.RemovedHistoricalScratch)
	return nil
}

func pruneLegacyRuntimeExecutables(root string, result *Result) error {
	runtimeRoot := filepath.Join(root, "runtime")
	ok, err := realOptionalDir(runtimeRoot)
	if err != nil || !ok {
		return err
	}
	keep := map[string]bool{
		"mar-v1-stable.exe": true,
		"tunnel-client.exe": true,
		"cloudflared.exe":   true,
	}
	entries, err := os.ReadDir(runtimeRoot)
	if err != nil {
		return fmt.Errorf("read runtime root for legacy executables: %w", err)
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if keep[lower] || !legacyRuntimeExecutableName(lower) {
			continue
		}
		path := filepath.Join(runtimeRoot, name)
		if !isDirectChild(runtimeRoot, path) {
			continue
		}
		if err := removeHistoricalScratchPath(root, path, filepath.ToSlash(filepath.Join("runtime", name)), result); err != nil {
			return err
		}
	}
	sort.Strings(result.RemovedHistoricalScratch)
	return nil
}

func legacyRuntimeExecutableName(lower string) bool {
	if lower == "mar.exe" || lower == "owner_web_client.exe" {
		return true
	}
	if strings.HasPrefix(lower, "mar-") && (strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".exe~")) {
		return true
	}
	if strings.HasPrefix(lower, "mcp_") && strings.HasSuffix(lower, ".exe") {
		return true
	}
	if strings.HasPrefix(lower, "selfhost_") && strings.HasSuffix(lower, ".exe") {
		return true
	}
	return strings.HasPrefix(lower, "tunnel-client-v") && strings.HasSuffix(lower, ".exe")
}

func pruneHistoricalDataRootScratch(root string, result *Result) error {
	allow := map[string][]string{
		"selfdev": {
			"w",
			filepath.Join("runtime", "cloudflared.exe"),
		},
		"remote-full-goal-final": {
			filepath.Join("data", "w"),
			filepath.Join("data", "runtime"),
			"mar.exe",
			"remote-client.exe",
			"remote-owner-driver.exe",
		},
		"selfhost-v4": {
			"w",
		},
		"final-uat-f01731e-v2": {
			"w",
		},
		"owner-uat-final": {
			filepath.Join("data", "runtime"),
			filepath.Join("runtime", "mar-owner-uat.exe"),
		},
		"remote-full-goal-7c847ee": {
			filepath.Join("data", "runtime"),
			"mar.exe",
		},
	}
	for rootName, relatives := range allow {
		historicalRoot := filepath.Join(root, rootName)
		ok, err := realOptionalDir(historicalRoot)
		if err != nil || !ok {
			if err != nil {
				return err
			}
			continue
		}
		for _, relative := range relatives {
			path := filepath.Join(historicalRoot, relative)
			label := filepath.ToSlash(filepath.Join(rootName, relative))
			if err := removeHistoricalScratchPath(root, path, label, result); err != nil {
				return err
			}
		}
	}
	for _, rootName := range []string{
		"candidate-v1.2-final",
		"candidate-v1.2-webwait",
		"candidate-v1.2-metrics",
		"verify",
		"fastpath-git-convenience",
		"fastpath-apply-verify",
		"research-worktrees",
	} {
		path := filepath.Join(root, rootName)
		if err := removeHistoricalScratchPath(root, path, filepath.ToSlash(rootName), result); err != nil {
			return err
		}
	}
	stagingRoot := filepath.Join(root, "staging")
	if ok, err := realOptionalDir(stagingRoot); err != nil {
		return err
	} else if ok {
		entries, err := os.ReadDir(stagingRoot)
		if err != nil {
			return fmt.Errorf("read historical staging root: %w", err)
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasPrefix(name, "requalification") && !strings.HasPrefix(name, "slice-") {
				continue
			}
			path := filepath.Join(stagingRoot, name)
			if err := removeHistoricalScratchPath(root, path, filepath.ToSlash(filepath.Join("staging", name)), result); err != nil {
				return err
			}
		}
	}
	sort.Strings(result.RemovedHistoricalScratch)
	return nil
}

func pruneHistoricalAudit20260915(root, auditRoot string, result *Result) error {
	ok, err := realOptionalDir(auditRoot)
	if err != nil || !ok {
		return err
	}
	entries, err := os.ReadDir(auditRoot)
	if err != nil {
		return fmt.Errorf("read historical audit root: %w", err)
	}
	allowedDirs := map[string]bool{
		"candidate":              true,
		"temp-recovery-qualify":  true,
		"temp-retention":         true,
		"temp-retention-q1":      true,
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(auditRoot, entry.Name())
		if !isDirectChild(auditRoot, path) {
			continue
		}
		if entry.IsDir() && allowedDirs[entry.Name()] {
			if err := removeHistoricalScratchPath(root, path, filepath.ToSlash(filepath.Join("runtime", "audit-20260915", entry.Name())), result); err != nil {
				return err
			}
			continue
		}
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".test.exe") {
			if err := removeHistoricalScratchPath(root, path, filepath.ToSlash(filepath.Join("runtime", "audit-20260915", entry.Name())), result); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeHistoricalScratchPath(root, path, label string, result *Result) error {
	if !isWithinRoot(root, path) {
		return errors.New("historical scratch path escaped MAR data root")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	var size int64
	if info.IsDir() {
		var safe bool
		size, safe, err = safeTreeSize(path)
		if err != nil {
			return err
		}
		if !safe {
			return nil
		}
	} else if info.Mode().IsRegular() {
		size = info.Size()
	} else {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove historical scratch %s: %w", label, err)
	}
	result.RemovedHistoricalScratch = append(result.RemovedHistoricalScratch, label)
	result.FreedBytes += size
	return nil
}

func pruneStaging(root string, result *Result) error {
	staging := filepath.Join(root, "runtime", "staging")
	ok, err := realOptionalDir(staging)
	if err != nil || !ok {
		return err
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return fmt.Errorf("read runtime staging: %w", err)
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !allowedStagingName(entry.Name()) {
			continue
		}
		path := filepath.Join(staging, entry.Name())
		if !isDirectChild(staging, path) {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		size := info.Size()
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove staging file %s: %w", entry.Name(), err)
		}
		result.RemovedStagingFiles = append(result.RemovedStagingFiles, entry.Name())
		result.FreedBytes += size
	}
	sort.Strings(result.RemovedStagingFiles)
	return nil
}

func allowedStagingName(name string) bool {
	lower := strings.ToLower(name)
	return (strings.HasPrefix(lower, "mar-") && strings.HasSuffix(lower, ".exe")) ||
		(strings.HasPrefix(lower, "release-manifest-") && strings.HasSuffix(lower, ".json"))
}

func pruneCacheContents(root, relative string, result *Result) error {
	cacheRoot := filepath.Join(root, relative)
	if !isWithinRoot(root, cacheRoot) {
		return errors.New("rebuildable cache path escaped MAR data root")
	}
	ok, err := realOptionalDir(cacheRoot)
	if err != nil || !ok {
		return err
	}
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		return fmt.Errorf("read rebuildable cache %s: %w", relative, err)
	}
	var freed int64
	removedAny := false
	for _, entry := range entries {
		candidate := filepath.Join(cacheRoot, entry.Name())
		if !isDirectChild(cacheRoot, candidate) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := os.Lstat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		var size int64
		if info.IsDir() {
			var safe bool
			size, safe, err = safeTreeSize(candidate)
			if err != nil {
				return err
			}
			if !safe {
				continue
			}
		} else if info.Mode().IsRegular() {
			size = info.Size()
		} else {
			continue
		}
		if err := os.RemoveAll(candidate); err != nil {
			return fmt.Errorf("remove rebuildable cache entry %s: %w", candidate, err)
		}
		freed += size
		removedAny = true
	}
	if removedAny {
		result.ClearedRebuildableCaches = append(result.ClearedRebuildableCaches, filepath.ToSlash(relative))
		result.FreedBytes += freed
	}
	return nil
}

func isWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != "." && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func realOptionalDir(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("retention root must not be a symlink")
	}
	if !info.IsDir() {
		return false, errors.New("retention root must be a directory")
	}
	return true, nil
}

func safeTreeSize(root string) (int64, bool, error) {
	var total int64
	safe := true
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root && entry.Type()&os.ModeSymlink != 0 {
			safe = false
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			safe = false
			return nil
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	return total, safe, nil
}

func isDirectChild(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return filepath.Dir(rel) == "."
}
