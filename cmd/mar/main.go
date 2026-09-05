package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"mar/internal/domain"
	"mar/internal/service"
	"mar/internal/store"
)

const defaultDB = ".mar/mar.db"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "init":
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		dbPath := fs.String("db", defaultDB, "SQLite database path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		s, err := store.Open(*dbPath)
		if err != nil {
			return err
		}
		defer s.Close()
		return printJSON(map[string]any{"status": "initialized", "db": *dbPath})

	case "project-add":
		fs := flag.NewFlagSet("project-add", flag.ContinueOnError)
		dbPath := fs.String("db", defaultDB, "SQLite database path")
		id := fs.String("id", "", "Project id")
		root := fs.String("root", "", "Project root")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		s, svc, err := openService(*dbPath)
		if err != nil {
			return err
		}
		defer s.Close()
		project, created, err := svc.RegisterProject(ctx, *id, *root)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"created": created, "project": project})

	case "submit":
		fs := flag.NewFlagSet("submit", flag.ContinueOnError)
		dbPath := fs.String("db", defaultDB, "SQLite database path")
		key := fs.String("key", "", "Idempotency key")
		file := fs.String("file", "", "Goal Contract JSON file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *file == "" {
			return errors.New("-file is required")
		}
		payload, err := os.ReadFile(filepath.Clean(*file))
		if err != nil {
			return fmt.Errorf("read goal contract: %w", err)
		}
		payload = bytes.TrimPrefix(payload, []byte{0xEF, 0xBB, 0xBF})
		var contract domain.GoalContract
		if err := json.Unmarshal(payload, &contract); err != nil {
			return fmt.Errorf("decode goal contract: %w", err)
		}
		s, svc, err := openService(*dbPath)
		if err != nil {
			return err
		}
		defer s.Close()
		task, created, err := svc.Submit(ctx, *key, contract)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"created": created, "task": task})

	case "status":
		fs := flag.NewFlagSet("status", flag.ContinueOnError)
		dbPath := fs.String("db", defaultDB, "SQLite database path")
		id := fs.String("task", "", "Task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		s, svc, err := openService(*dbPath)
		if err != nil {
			return err
		}
		defer s.Close()
		task, err := svc.Status(ctx, *id)
		if err != nil {
			return err
		}
		return printJSON(task)

	default:
		return usage()
	}
}

func openService(dbPath string) (*store.SQLite, *service.TaskService, error) {
	s, err := store.Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	return s, service.NewTaskService(s), nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func usage() error {
	return errors.New("usage: mar <init|project-add|submit|status> [options]")
}
