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

func TestSchema17SharedPathMigrationPreservesRowsAndAllowsSharedPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema17.db")
	db, err := sql.Open("sqlite", path)
	if err != nil { t.Fatal(err) }
	db.SetMaxOpenConns(1)
	defer db.Close()
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil { t.Fatal(err) }
	s := &SQLite{db: db}
	for version := 1; version <= 16; version++ {
		if err := s.applyMigration(context.Background(), version); err != nil { t.Fatalf("prepare schema %d: %v", version, err) }
	}
	now := "2026-09-22T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO projects(id, root, created_at) VALUES ('p', 'repo', ?)`, now); err != nil { t.Fatal(err) }
	contract := `{"goal":"g","acceptance":["a"],"boundaries":[],"non_goals":[],"project_id":"p","base_revision":"abc","authority":{"local_file_write":false,"local_git_write":false,"network_allowed":false,"remote_git_write":false,"deploy_allowed":false},"verification_profile":"go-docs","priority":"high"}`
	for _, id := range []string{"task-a", "task-b"} {
		if _, err := db.Exec(`INSERT INTO tasks(id,idempotency_key,project_id,contract_json,contract_hash,state,run_epoch,created_at,updated_at) VALUES(?,?,?,?,?,?,0,?,?)`, id, "key-"+id, "p", contract, "hash-"+id, "WAITING_RESOURCE", now, now); err != nil { t.Fatal(err) }
	}
	if _, err := db.Exec(`INSERT INTO workspaces(id,task_id,project_id,path,base_revision,head_revision,state,failure,created_at,updated_at) VALUES('ws-a','task-a','p','shared-path','abc','abc','READY','',?,?)`, now, now); err != nil { t.Fatal(err) }
	if _, err := db.Exec(`INSERT INTO workspace_checkpoints(checkpoint_id,task_id,workspace_id,project_id,version,original_head,snapshot_revision,ref_name,status_hash,dirty,state,created_at) VALUES('cp-a','task-a','ws-a','p',1,'abc','abc','refs/mar/checkpoints/a','hash',0,'CAPTURED',?)`, now); err != nil { t.Fatal(err) }
	if err := s.applyMigration(context.Background(), 17); err != nil { t.Fatalf("migration 17 failed: %v", err) }
	var gotPath, gotState string
	if err := db.QueryRow(`SELECT path,state FROM workspaces WHERE task_id='task-a'`).Scan(&gotPath, &gotState); err != nil { t.Fatal(err) }
	if gotPath != "shared-path" || gotState != "READY" { t.Fatalf("workspace row changed during migration: path=%q state=%q", gotPath, gotState) }
	if _, err := db.Exec(`INSERT INTO workspaces(id,task_id,project_id,path,base_revision,head_revision,state,failure,created_at,updated_at) VALUES('ws-b','task-b','p','shared-path','abc','abc','READY','',?,?)`, now, now); err != nil { t.Fatalf("schema 17 must allow SharedPath reuse: %v", err) }
	var checkpointWorkspace string
	if err := db.QueryRow(`SELECT workspace_id FROM workspace_checkpoints WHERE checkpoint_id='cp-a'`).Scan(&checkpointWorkspace); err != nil || checkpointWorkspace != "ws-a" { t.Fatalf("checkpoint FK row lost: workspace=%q err=%v", checkpointWorkspace, err) }
	rows, err := db.Query(`PRAGMA foreign_key_check;`); if err != nil { t.Fatal(err) }; defer rows.Close()
	if rows.Next() { t.Fatal("migration 17 introduced foreign-key violation") }
}
