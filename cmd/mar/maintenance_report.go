package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mar/internal/store"
)

type maintenancePathBytes struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Bytes int64  `json:"bytes"`
}

type maintenanceActivationSummary struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

type maintenanceStorageSnapshot struct {
	TotalBytes        int64                        `json:"total_bytes"`
	TopLevel          []maintenancePathBytes       `json:"top_level"`
	RuntimeTop        []maintenancePathBytes       `json:"runtime_top"`
	ActivationBackups maintenanceActivationSummary `json:"activation_backups"`
}

type maintenanceReport struct {
	GeneratedAt time.Time                  `json:"generated_at"`
	Since       time.Time                  `json:"since"`
	DataRoot    string                     `json:"data_root"`
	DBPath      string                     `json:"db_path"`
	DBFileBytes int64                      `json:"db_file_bytes"`
	Storage     maintenanceStorageSnapshot `json:"storage"`
	Database    store.MaintenanceDBStats   `json:"database"`
}

func runMaintenanceReport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("maintenance-report", flag.ContinueOnError)
	dataRoot := fs.String("data-root", ".mar", "MAR managed data root")
	dbPath := fs.String("db", "", "SQLite database path; defaults to <data-root>/mar.db")
	sinceDays := fs.Int("since-days", 14, "terminal-status lookback in days")
	top := fs.Int("top", 15, "maximum entries in each disk breakdown")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sinceDays < 1 || *sinceDays > 365 {
		return errors.New("since-days must be in [1,365]")
	}
	if *top < 1 || *top > 100 {
		return errors.New("top must be in [1,100]")
	}

	root, err := filepath.Abs(strings.TrimSpace(*dataRoot))
	if err != nil {
		return fmt.Errorf("resolve maintenance data root: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("stat maintenance data root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("maintenance data root must be a real directory")
	}

	resolvedDB := strings.TrimSpace(*dbPath)
	if resolvedDB == "" {
		resolvedDB = filepath.Join(root, "mar.db")
	}
	resolvedDB, err = filepath.Abs(resolvedDB)
	if err != nil {
		return fmt.Errorf("resolve maintenance database path: %w", err)
	}

	storage, err := buildMaintenanceStorageSnapshot(root, *top)
	if err != nil {
		return err
	}

	dbInfo, err := os.Stat(resolvedDB)
	if err != nil {
		return fmt.Errorf("stat maintenance database: %w", err)
	}

	s, err := store.Open(resolvedDB)
	if err != nil {
		return err
	}
	defer s.Close()

	now := time.Now().UTC()
	since := now.Add(-time.Duration(*sinceDays) * 24 * time.Hour)
	dbStats, err := s.MaintenanceDBStats(ctx, since)
	if err != nil {
		return err
	}

	return printJSON(maintenanceReport{
		GeneratedAt: now,
		Since:       since,
		DataRoot:    root,
		DBPath:      resolvedDB,
		DBFileBytes: dbInfo.Size(),
		Storage:     storage,
		Database:    dbStats,
	})
}

func buildMaintenanceStorageSnapshot(root string, top int) (maintenanceStorageSnapshot, error) {
	topLevel, total, err := measureDirectChildren(root, top)
	if err != nil {
		return maintenanceStorageSnapshot{}, err
	}
	runtimeTop := []maintenancePathBytes{}
	runtimeRoot := filepath.Join(root, "runtime")
	if info, err := os.Lstat(runtimeRoot); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		runtimeTop, _, err = measureDirectChildren(runtimeRoot, top)
		if err != nil {
			return maintenanceStorageSnapshot{}, err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return maintenanceStorageSnapshot{}, err
	}
	activations, err := measureActivationBackups(filepath.Join(root, "recovery"))
	if err != nil {
		return maintenanceStorageSnapshot{}, err
	}
	return maintenanceStorageSnapshot{
		TotalBytes:        total,
		TopLevel:          topLevel,
		RuntimeTop:        runtimeTop,
		ActivationBackups: activations,
	}, nil
}

func measureDirectChildren(root string, top int) ([]maintenancePathBytes, int64, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, 0, err
	}
	items := make([]maintenancePathBytes, 0, len(entries))
	var total int64
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, 0, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		size, err := maintenancePathSize(path, info)
		if err != nil {
			return nil, 0, err
		}
		kind := "file"
		if info.IsDir() {
			kind = "dir"
		}
		items = append(items, maintenancePathBytes{Name: entry.Name(), Kind: kind, Bytes: size})
		total += size
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Bytes == items[j].Bytes {
			return items[i].Name < items[j].Name
		}
		return items[i].Bytes > items[j].Bytes
	})
	if len(items) > top {
		items = items[:top]
	}
	return items, total, nil
}

func maintenancePathSize(path string, info fs.FileInfo) (int64, error) {
	if !info.IsDir() {
		if info.Mode().IsRegular() {
			return info.Size(), nil
		}
		return 0, nil
	}
	var total int64
	err := filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current != path && entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if entryInfo.Mode().IsRegular() {
			total += entryInfo.Size()
		}
		return nil
	})
	return total, err
}

func measureActivationBackups(recoveryRoot string) (maintenanceActivationSummary, error) {
	var result maintenanceActivationSummary
	info, err := os.Lstat(recoveryRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, errors.New("recovery root must be a real directory")
	}
	entries, err := os.ReadDir(recoveryRoot)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), "activation-") {
			continue
		}
		dir := filepath.Join(recoveryRoot, entry.Name())
		marker := filepath.Join(dir, "activation.json")
		markerInfo, err := os.Lstat(marker)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return result, err
		}
		if !markerInfo.Mode().IsRegular() || markerInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		dirInfo, err := os.Lstat(dir)
		if err != nil {
			return result, err
		}
		size, err := maintenancePathSize(dir, dirInfo)
		if err != nil {
			return result, err
		}
		result.Count++
		result.Bytes += size
	}
	return result, nil
}
