package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mar/internal/store"
)

type maintenanceCompactReport struct {
	GeneratedAt   time.Time                       `json:"generated_at"`
	Cutoff        time.Time                       `json:"cutoff"`
	Apply         bool                            `json:"apply"`
	Vacuum        bool                            `json:"vacuum"`
	DBPath        string                          `json:"db_path"`
	BeforeDBBytes int64                           `json:"before_db_bytes"`
	Plan          store.TerminalWebTurnCompaction `json:"plan"`
	Applied       store.TerminalWebTurnCompaction `json:"applied"`
	AfterDBBytes  int64                           `json:"after_db_bytes"`
}

func runMaintenanceCompact(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("maintenance-compact", flag.ContinueOnError)
	dataRoot := fs.String("data-root", ".mar", "MAR managed data root")
	dbPath := fs.String("db", "", "SQLite database path; defaults to <data-root>/mar.db")
	olderThanDays := fs.Int("older-than-days", 7, "compact terminal WebTurn requests older than this many days")
	apply := fs.Bool("apply", false, "apply compaction; default is dry-run")
	vacuum := fs.Bool("vacuum", false, "VACUUM after compaction; requires no active task states")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *olderThanDays < 1 || *olderThanDays > 3650 {
		return errors.New("older-than-days must be in [1,3650]")
	}
	if *vacuum && !*apply {
		return errors.New("vacuum requires apply")
	}
	root, err := filepath.Abs(strings.TrimSpace(*dataRoot))
	if err != nil {
		return err
	}
	resolvedDB := strings.TrimSpace(*dbPath)
	if resolvedDB == "" {
		resolvedDB = filepath.Join(root, "mar.db")
	}
	resolvedDB, err = filepath.Abs(resolvedDB)
	if err != nil {
		return err
	}
	before, err := os.Stat(resolvedDB)
	if err != nil {
		return err
	}
	s, err := store.Open(resolvedDB)
	if err != nil {
		return err
	}
	defer s.Close()

	now := time.Now().UTC()
	cutoff := now.Add(-time.Duration(*olderThanDays) * 24 * time.Hour)
	plan, err := s.TerminalWebTurnCompactionPlan(ctx, cutoff)
	if err != nil {
		return err
	}
	report := maintenanceCompactReport{GeneratedAt: now, Cutoff: cutoff, Apply: *apply, Vacuum: *vacuum, DBPath: resolvedDB, BeforeDBBytes: before.Size(), Plan: plan, AfterDBBytes: before.Size()}
	if !*apply {
		return printJSON(report)
	}
	if *vacuum {
		stats, err := s.MaintenanceDBStats(ctx, now.Add(-14*24*time.Hour))
		if err != nil {
			return err
		}
		for _, state := range stats.TaskStates {
			switch strings.ToUpper(strings.TrimSpace(state.Name)) {
			case "BLOCKED", "COMPLETE", "CANCELLED":
			default:
				if state.Count > 0 {
					return errors.New("vacuum refused while active task states exist")
				}
			}
		}
	}
	applied, err := s.CompactTerminalWebTurnRequests(ctx, cutoff)
	if err != nil {
		return err
	}
	report.Applied = applied
	if *vacuum {
		if err := s.Vacuum(ctx); err != nil {
			return err
		}
	}
	after, err := os.Stat(resolvedDB)
	if err != nil {
		return err
	}
	report.AfterDBBytes = after.Size()
	return printJSON(report)
}
