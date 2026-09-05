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
	ErrNotFound              = errors.New("not found")
	ErrIdempotencyConflict   = errors.New("idempotency key already belongs to a different goal contract")
	ErrProjectConflict       = errors.New("project id already exists with a different root")
	ErrStateConflict         = errors.New("task state changed or transition precondition failed")
	ErrStaleAttempt          = errors.New("execution attempt is stale or no longer authoritative")
	ErrPhysicalFenceRequired = errors.New("previous mutation-capable attempt is not confirmed physically terminated")
)

const latestSchemaVersion = 3

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
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version;").Scan(&version); err != nil {
		return fmt.Errorf("read sqlite schema version: %w", err)
	}
	for version < latestSchemaVersion {
		next := version + 1
		if err := s.applyMigration(ctx, next); err != nil {
			return err
		}
		version = next
	}
	if version > latestSchemaVersion {
		return fmt.Errorf("sqlite schema version %d is newer than supported %d", version, latestSchemaVersion)
	}
	return nil
}

func (s *SQLite) applyMigration(ctx context.Context, version int) error {
	var script string
	switch version {
	case 1:
		script = `
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
	case 2:
		script = `
ALTER TABLE tasks ADD COLUMN run_epoch INTEGER NOT NULL DEFAULT 0;

CREATE TABLE execution_attempts (
    attempt_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    run_epoch INTEGER NOT NULL,
    worker_id TEXT NOT NULL,
    supervisor_id TEXT NOT NULL,
    authority_state TEXT NOT NULL,
    started_at TEXT NOT NULL,
    heartbeat_at TEXT NOT NULL,
    lease_deadline TEXT NOT NULL,
    terminated_at TEXT,
    terminal_status TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(task_id) REFERENCES tasks(id),
    UNIQUE(task_id, run_epoch)
);

CREATE INDEX idx_attempts_task_authority ON execution_attempts(task_id, authority_state);
`
	case 3:
		script = `
CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL UNIQUE,
    project_id TEXT NOT NULL,
    path TEXT NOT NULL UNIQUE,
    base_revision TEXT NOT NULL,
    head_revision TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    failure TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    removed_at TEXT,
    FOREIGN KEY(task_id) REFERENCES tasks(id),
    FOREIGN KEY(project_id) REFERENCES projects(id)
);
CREATE INDEX idx_workspaces_project_state ON workspaces(project_id, state);
`
	default:
		return fmt.Errorf("unknown migration version %d", version)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", version, err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, script); err != nil {
		return fmt.Errorf("apply migration %d: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d;", version)); err != nil {
		return fmt.Errorf("mark migration %d: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", version, err)
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

	existing, err := getTaskWithQueryer(ctx, tx, `SELECT id, idempotency_key, contract_json, contract_hash, state, run_epoch, created_at, updated_at FROM tasks WHERE idempotency_key = ?`, task.IdempotencyKey)
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
	return getTaskWithQueryer(ctx, s.db, `SELECT id, idempotency_key, contract_json, contract_hash, state, run_epoch, created_at, updated_at FROM tasks WHERE id = ?`, id)
}

func (s *SQLite) OrchestratorTransition(ctx context.Context, taskID string, from, to domain.TaskState, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET state = ?, updated_at = ?
WHERE id = ?
  AND state = ?
  AND NOT EXISTS (
      SELECT 1 FROM execution_attempts a
      WHERE a.task_id = tasks.id
        AND a.authority_state != ?
  )`,
		string(to), now.UTC().Format(time.RFC3339Nano), taskID, string(from), string(domain.AttemptPhysicallyTerminated),
	)
	if err != nil {
		return fmt.Errorf("orchestrator transition: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("orchestrator transition rows: %w", err)
	}
	if rows != 1 {
		return ErrStateConflict
	}
	return nil
}

func (s *SQLite) BeginAttempt(ctx context.Context, taskID, attemptID, workerID, supervisorID string, now, leaseDeadline time.Time) (domain.ExecutionAttempt, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.ExecutionAttempt{}, fmt.Errorf("begin attempt transaction: %w", err)
	}
	defer tx.Rollback()

	var state string
	var currentEpoch int64
	if err := tx.QueryRowContext(ctx, `SELECT state, run_epoch FROM tasks WHERE id = ?`, taskID).Scan(&state, &currentEpoch); errors.Is(err, sql.ErrNoRows) {
		return domain.ExecutionAttempt{}, ErrNotFound
	} else if err != nil {
		return domain.ExecutionAttempt{}, fmt.Errorf("read task before attempt: %w", err)
	}
	if domain.TaskState(state) != domain.TaskWorkspaceReady {
		return domain.ExecutionAttempt{}, ErrStateConflict
	}

	var mutationCapable int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM execution_attempts
WHERE task_id = ? AND authority_state != ?`, taskID, string(domain.AttemptPhysicallyTerminated)).Scan(&mutationCapable); err != nil {
		return domain.ExecutionAttempt{}, fmt.Errorf("check physical fencing: %w", err)
	}
	if mutationCapable != 0 {
		return domain.ExecutionAttempt{}, ErrPhysicalFenceRequired
	}

	epoch := currentEpoch + 1
	res, err := tx.ExecContext(ctx, `
UPDATE tasks SET run_epoch = ?, state = ?, updated_at = ?
WHERE id = ? AND run_epoch = ? AND state = ?`,
		epoch, string(domain.TaskRunning), now.UTC().Format(time.RFC3339Nano), taskID, currentEpoch, string(domain.TaskWorkspaceReady),
	)
	if err != nil {
		return domain.ExecutionAttempt{}, fmt.Errorf("advance task epoch: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil || rows != 1 {
		return domain.ExecutionAttempt{}, ErrStateConflict
	}

	attempt := domain.ExecutionAttempt{
		ID:             attemptID,
		TaskID:         taskID,
		RunEpoch:       epoch,
		WorkerID:       workerID,
		SupervisorID:   supervisorID,
		AuthorityState: domain.AttemptActive,
		StartedAt:      now.UTC(),
		HeartbeatAt:    now.UTC(),
		LeaseDeadline:  leaseDeadline.UTC(),
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO execution_attempts(
    attempt_id, task_id, run_epoch, worker_id, supervisor_id, authority_state,
    started_at, heartbeat_at, lease_deadline, terminal_status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '')`,
		attempt.ID, attempt.TaskID, attempt.RunEpoch, attempt.WorkerID, attempt.SupervisorID,
		string(attempt.AuthorityState), attempt.StartedAt.Format(time.RFC3339Nano),
		attempt.HeartbeatAt.Format(time.RFC3339Nano), attempt.LeaseDeadline.Format(time.RFC3339Nano),
	)
	if err != nil {
		return domain.ExecutionAttempt{}, fmt.Errorf("insert execution attempt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.ExecutionAttempt{}, fmt.Errorf("commit execution attempt: %w", err)
	}
	return attempt, nil
}

func (s *SQLite) HeartbeatAttempt(ctx context.Context, taskID, attemptID string, epoch int64, now, leaseDeadline time.Time) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE execution_attempts
SET heartbeat_at = ?, lease_deadline = ?
WHERE attempt_id = ? AND task_id = ? AND run_epoch = ? AND authority_state = ?
  AND EXISTS (SELECT 1 FROM tasks WHERE id = ? AND run_epoch = ?)`,
		now.UTC().Format(time.RFC3339Nano), leaseDeadline.UTC().Format(time.RFC3339Nano),
		attemptID, taskID, epoch, string(domain.AttemptActive), taskID, epoch,
	)
	if err != nil {
		return fmt.Errorf("heartbeat attempt: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStaleAttempt
	}
	return nil
}

func (s *SQLite) LogicalFenceAttempt(ctx context.Context, taskID, attemptID string, epoch int64, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE execution_attempts
SET authority_state = ?, heartbeat_at = ?
WHERE attempt_id = ? AND task_id = ? AND run_epoch = ? AND authority_state = ?
  AND EXISTS (SELECT 1 FROM tasks WHERE id = ? AND run_epoch = ?)`,
		string(domain.AttemptLogicallyFenced), now.UTC().Format(time.RFC3339Nano),
		attemptID, taskID, epoch, string(domain.AttemptActive), taskID, epoch,
	)
	if err != nil {
		return fmt.Errorf("logical fence attempt: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStaleAttempt
	}
	return nil
}

func (s *SQLite) ConfirmAttemptTerminated(ctx context.Context, taskID, attemptID string, epoch int64, terminalStatus string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE execution_attempts
SET authority_state = ?, terminated_at = ?, terminal_status = ?
WHERE attempt_id = ? AND task_id = ? AND run_epoch = ? AND authority_state != ?`,
		string(domain.AttemptPhysicallyTerminated), now.UTC().Format(time.RFC3339Nano), terminalStatus,
		attemptID, taskID, epoch, string(domain.AttemptPhysicallyTerminated),
	)
	if err != nil {
		return fmt.Errorf("confirm attempt terminated: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 1 {
		return nil
	}
	attempt, getErr := s.GetAttempt(ctx, attemptID)
	if getErr == nil && attempt.TaskID == taskID && attempt.RunEpoch == epoch && attempt.AuthorityState == domain.AttemptPhysicallyTerminated {
		return nil
	}
	if getErr != nil {
		return getErr
	}
	return ErrStaleAttempt
}

func (s *SQLite) RecoverTaskToWorkspaceReady(ctx context.Context, taskID string, from domain.TaskState, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE tasks SET state = ?, updated_at = ?
WHERE id = ? AND state = ?
  AND NOT EXISTS (
      SELECT 1 FROM execution_attempts a
      WHERE a.task_id = tasks.id AND a.authority_state != ?
  )`,
		string(domain.TaskWorkspaceReady), now.UTC().Format(time.RFC3339Nano), taskID, string(from), string(domain.AttemptPhysicallyTerminated),
	)
	if err != nil {
		return fmt.Errorf("recover task to workspace ready: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrPhysicalFenceRequired
	}
	return nil
}

func (s *SQLite) TransitionTaskForAttempt(ctx context.Context, taskID, attemptID string, epoch int64, from, to domain.TaskState, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET state = ?, updated_at = ?
WHERE id = ? AND state = ? AND run_epoch = ?
  AND EXISTS (
      SELECT 1 FROM execution_attempts a
      WHERE a.attempt_id = ? AND a.task_id = tasks.id AND a.run_epoch = ? AND a.authority_state = ?
  )`,
		string(to), now.UTC().Format(time.RFC3339Nano), taskID, string(from), epoch,
		attemptID, epoch, string(domain.AttemptActive),
	)
	if err != nil {
		return fmt.Errorf("attempt task transition: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 1 {
		return nil
	}

	task, getErr := s.GetTask(ctx, taskID)
	if getErr != nil {
		return getErr
	}
	if task.RunEpoch != epoch {
		return ErrStaleAttempt
	}
	attempt, getErr := s.GetAttempt(ctx, attemptID)
	if getErr != nil || attempt.RunEpoch != epoch || attempt.AuthorityState != domain.AttemptActive {
		return ErrStaleAttempt
	}
	return ErrStateConflict
}

func (s *SQLite) GetAttempt(ctx context.Context, attemptID string) (domain.ExecutionAttempt, error) {
	var a domain.ExecutionAttempt
	var authority, started, heartbeat, lease string
	var terminated sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT attempt_id, task_id, run_epoch, worker_id, supervisor_id, authority_state,
       started_at, heartbeat_at, lease_deadline, terminated_at, terminal_status
FROM execution_attempts WHERE attempt_id = ?`, attemptID).Scan(
		&a.ID, &a.TaskID, &a.RunEpoch, &a.WorkerID, &a.SupervisorID, &authority,
		&started, &heartbeat, &lease, &terminated, &a.TerminalStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ExecutionAttempt{}, ErrNotFound
	}
	if err != nil {
		return domain.ExecutionAttempt{}, fmt.Errorf("get execution attempt: %w", err)
	}
	a.AuthorityState = domain.AttemptAuthorityState(authority)
	if a.StartedAt, err = time.Parse(time.RFC3339Nano, started); err != nil {
		return domain.ExecutionAttempt{}, fmt.Errorf("parse attempt started_at: %w", err)
	}
	if a.HeartbeatAt, err = time.Parse(time.RFC3339Nano, heartbeat); err != nil {
		return domain.ExecutionAttempt{}, fmt.Errorf("parse attempt heartbeat_at: %w", err)
	}
	if a.LeaseDeadline, err = time.Parse(time.RFC3339Nano, lease); err != nil {
		return domain.ExecutionAttempt{}, fmt.Errorf("parse attempt lease_deadline: %w", err)
	}
	if terminated.Valid {
		tm, parseErr := time.Parse(time.RFC3339Nano, terminated.String)
		if parseErr != nil {
			return domain.ExecutionAttempt{}, fmt.Errorf("parse attempt terminated_at: %w", parseErr)
		}
		a.TerminatedAt = &tm
	}
	return a, nil
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
		&task.RunEpoch,
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
