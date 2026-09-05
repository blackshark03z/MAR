package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mar/internal/domain"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrIdempotencyConflict = errors.New("idempotency key already belongs to a different goal contract")
	ErrProjectConflict     = errors.New("project id already exists with a different root")
)

type SQLite struct {
	db *sql.DB
}

func Open(path string) (*SQLite, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// MAR V1 has one authoritative coordination writer. Keep one SQLite
	// connection so connection-scoped PRAGMAs and transaction ordering are
	// deterministic. Metadata throughput is tiny compared with agent/tool work.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA synchronous=FULL;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure sqlite (%s): %w", pragma, err)
		}
	}

	s := &SQLite{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLite) Close() error { return s.db.Close() }

func (s *SQLite) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    root TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    project_id TEXT NOT NULL,
    contract_json BLOB NOT NULL,
    contract_hash TEXT NOT NULL,
    state TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(project_id) REFERENCES projects(id)
);

CREATE INDEX IF NOT EXISTS idx_tasks_project_state ON tasks(project_id, state);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}
	return nil
}

func (s *SQLite) RegisterProject(ctx context.Context, p domain.Project) (domain.Project, bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects(id, root, created_at) VALUES (?, ?, ?)`,
		p.ID, p.Root, p.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return domain.Project{}, false, fmt.Errorf("insert project: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return domain.Project{}, false, fmt.Errorf("project rows affected: %w", err)
	}
	if rows == 1 {
		return p, true, nil
	}

	existing, err := s.GetProject(ctx, p.ID)
	if err != nil {
		return domain.Project{}, false, err
	}
	if filepath.Clean(existing.Root) != filepath.Clean(p.Root) {
		return domain.Project{}, false, ErrProjectConflict
	}
	return existing, false, nil
}

func (s *SQLite) GetProject(ctx context.Context, id string) (domain.Project, error) {
	var p domain.Project
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, root, created_at FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.Root, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Project{}, ErrNotFound
	}
	if err != nil {
		return domain.Project{}, fmt.Errorf("get project: %w", err)
	}
	p.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.Project{}, fmt.Errorf("parse project created_at: %w", err)
	}
	return p, nil
}

// SubmitTask is idempotent on idempotency_key. The same key + same contract
// returns the original task; the same key + different contract is rejected.
func (s *SQLite) SubmitTask(ctx context.Context, task domain.Task) (domain.Task, bool, error) {
	payload, err := task.Contract.CanonicalJSON()
	if err != nil {
		return domain.Task{}, false, fmt.Errorf("serialize goal contract: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.Task{}, false, fmt.Errorf("begin submit transaction: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO tasks(
    id, idempotency_key, project_id, contract_json, contract_hash, state, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.IdempotencyKey,
		task.Contract.ProjectID,
		payload,
		task.ContractHash,
		string(task.State),
		task.CreatedAt.UTC().Format(time.RFC3339Nano),
		task.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return domain.Task{}, false, fmt.Errorf("insert task: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return domain.Task{}, false, fmt.Errorf("task rows affected: %w", err)
	}

	if rows == 1 {
		if err := tx.Commit(); err != nil {
			return domain.Task{}, false, fmt.Errorf("commit task submit: %w", err)
		}
		return task, true, nil
	}

	existing, err := getTaskWithQueryer(ctx, tx, `SELECT id, idempotency_key, contract_json, contract_hash, state, created_at, updated_at FROM tasks WHERE idempotency_key = ?`, task.IdempotencyKey)
	if err != nil {
		return domain.Task{}, false, err
	}
	if existing.ContractHash != task.ContractHash {
		return domain.Task{}, false, ErrIdempotencyConflict
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, false, fmt.Errorf("commit idempotent submit read: %w", err)
	}
	return existing, false, nil
}

func (s *SQLite) GetTask(ctx context.Context, id string) (domain.Task, error) {
	return getTaskWithQueryer(ctx, s.db, `SELECT id, idempotency_key, contract_json, contract_hash, state, created_at, updated_at FROM tasks WHERE id = ?`, id)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getTaskWithQueryer(ctx context.Context, q queryer, query string, arg string) (domain.Task, error) {
	var task domain.Task
	var payload []byte
	var state string
	var created, updated string
	err := q.QueryRowContext(ctx, query, arg).Scan(
		&task.ID,
		&task.IdempotencyKey,
		&payload,
		&task.ContractHash,
		&state,
		&created,
		&updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, ErrNotFound
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("get task: %w", err)
	}
	if err := json.Unmarshal(payload, &task.Contract); err != nil {
		return domain.Task{}, fmt.Errorf("decode goal contract: %w", err)
	}
	task.State = domain.TaskState(state)
	task.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.Task{}, fmt.Errorf("parse task created_at: %w", err)
	}
	task.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return domain.Task{}, fmt.Errorf("parse task updated_at: %w", err)
	}
	return task, nil
}
