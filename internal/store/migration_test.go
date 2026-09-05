package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationFromPreVersionedSlice1Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := `
CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    root TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);
CREATE TABLE tasks (
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
CREATE INDEX idx_tasks_project_state ON tasks(project_id, state);
`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id, root, created_at) VALUES ('p1', 'C:\\repo', '2026-09-05T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	contractJSON := `{"goal":"legacy","acceptance":["migrate"],"boundaries":[],"non_goals":[],"project_id":"p1","base_revision":"abc","authority":{"local_file_write":true,"local_git_write":true,"network_allowed":false,"remote_git_write":false,"deploy_allowed":false},"verification_profile":"legacy","priority":"P2"}`
	if _, err := db.Exec(`INSERT INTO tasks(id, idempotency_key, project_id, contract_json, contract_hash, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"task-legacy", "legacy-key", "p1", contractJSON, "hash", "SUBMITTED", "2026-09-05T00:00:00Z", "2026-09-05T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var version int
	if err := s.db.QueryRow(`PRAGMA user_version;`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != latestSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", latestSchemaVersion, version)
	}
	task, err := s.GetTask(context.Background(), "task-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if task.RunEpoch != 0 {
		t.Fatalf("legacy task should migrate with run_epoch=0, got %d", task.RunEpoch)
	}
}
