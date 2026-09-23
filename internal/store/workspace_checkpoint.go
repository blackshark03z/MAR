package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"mar/internal/domain"
)

func (s *SQLite) ListWorkspaceCheckpointRecoveryCandidates(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT task_id
FROM workspaces
WHERE state = ?
ORDER BY updated_at ASC, task_id ASC
LIMIT ?`, string(domain.WorkspaceCheckpointing), limit)
	if err != nil {
		return nil, fmt.Errorf("list workspace checkpoint recovery candidates: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil, err
		}
		out = append(out, taskID)
	}
	return out, rows.Err()
}

func (s *SQLite) ListRehydratedCheckpointRefReleaseCandidates(ctx context.Context, limit int) ([]domain.WorkspaceCheckpoint, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT checkpoint_id, task_id, project_id, snapshot_revision, ref_name, state
FROM workspace_checkpoints
WHERE state = ? AND ref_name LIKE 'refs/mar/checkpoints/%'
ORDER BY COALESCE(rehydrated_at, created_at) ASC, checkpoint_id ASC
LIMIT ?`, string(domain.WorkspaceCheckpointRehydrated), limit)
	if err != nil {
		return nil, fmt.Errorf("list rehydrated checkpoint ref release candidates: %w", err)
	}
	defer rows.Close()
	var out []domain.WorkspaceCheckpoint
	for rows.Next() {
		var c domain.WorkspaceCheckpoint
		var state string
		if err := rows.Scan(&c.ID, &c.TaskID, &c.ProjectID, &c.SnapshotRevision, &c.RefName, &state); err != nil {
			return nil, err
		}
		c.State = domain.WorkspaceCheckpointState(state)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLite) FinishWorkspaceCheckpointRefRelease(ctx context.Context, checkpointID, expectedRef string) error {
	expectedRef = strings.TrimSpace(expectedRef)
	if strings.TrimSpace(checkpointID) == "" || !strings.HasPrefix(expectedRef, "refs/mar/checkpoints/") {
		return ErrStateConflict
	}
	releasedRef := "released:" + expectedRef
	res, err := s.db.ExecContext(ctx, `UPDATE workspace_checkpoints SET ref_name = ? WHERE checkpoint_id = ? AND state = ? AND ref_name = ?`,
		releasedRef, checkpointID, string(domain.WorkspaceCheckpointRehydrated), expectedRef)
	if err != nil {
		return fmt.Errorf("finalize workspace checkpoint ref release: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	return nil
}

func (s *SQLite) ListBlockedWorkspaceCheckpointCandidates(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id
FROM tasks t
JOIN workspaces w ON w.task_id = t.id
WHERE t.state = ?
  AND w.state = ?
  AND w.head_revision <> ''
  AND NOT EXISTS (
      SELECT 1 FROM execution_attempts a
      WHERE a.task_id = t.id AND a.authority_state <> ?
  )
  AND NOT EXISTS (
      SELECT 1
      FROM task_results tr
      WHERE tr.task_id = t.id
        AND tr.version = (SELECT MAX(latest.version) FROM task_results latest WHERE latest.task_id = t.id)
        AND tr.verdict = ?
        AND tr.integration_status = 'BLOCKED'
  )
ORDER BY t.updated_at ASC, t.id ASC
LIMIT ?`,
		string(domain.TaskBlocked),
		string(domain.WorkspaceReady),
		string(domain.AttemptPhysicallyTerminated),
		string(domain.ResultVerified),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list blocked workspace checkpoint candidates: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil, err
		}
		out = append(out, taskID)
	}
	return out, rows.Err()
}

func (s *SQLite) BeginWorkspaceCheckpoint(ctx context.Context, taskID string, now time.Time) (domain.Workspace, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.Workspace{}, err
	}
	defer tx.Rollback()

	workspace, err := getWorkspaceWithQueryer(ctx, tx, `SELECT id, task_id, project_id, path, base_revision, head_revision, state, failure, created_at, updated_at, removed_at FROM workspaces WHERE task_id = ?`, taskID)
	if err != nil {
		return domain.Workspace{}, err
	}
	var taskState string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, taskID).Scan(&taskState); err != nil {
		return domain.Workspace{}, err
	}
	if domain.TaskState(taskState) != domain.TaskBlocked || workspace.State != domain.WorkspaceReady || workspace.HeadRevision == "" {
		return domain.Workspace{}, ErrWorkspaceRemovalUnsafe
	}
	var unsafe int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_attempts WHERE task_id = ? AND authority_state <> ?`,
		taskID, string(domain.AttemptPhysicallyTerminated)).Scan(&unsafe); err != nil {
		return domain.Workspace{}, err
	}
	if unsafe != 0 {
		return domain.Workspace{}, ErrPhysicalFenceRequired
	}
	var protected int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM task_results tr
WHERE tr.task_id = ?
  AND tr.version = (SELECT MAX(latest.version) FROM task_results latest WHERE latest.task_id = ?)
  AND tr.verdict = ?
  AND tr.integration_status = 'BLOCKED'`,
		taskID, taskID, string(domain.ResultVerified)).Scan(&protected); err != nil {
		return domain.Workspace{}, err
	}
	if protected != 0 {
		return domain.Workspace{}, ErrWorkspaceRemovalUnsafe
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE workspaces SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		string(domain.WorkspaceCheckpointing), stamp, workspace.ID, string(domain.WorkspaceReady))
	if err != nil {
		return domain.Workspace{}, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.Workspace{}, ErrStateConflict
	}
	workspace.State = domain.WorkspaceCheckpointing
	workspace.UpdatedAt = now.UTC()
	if err := tx.Commit(); err != nil {
		return domain.Workspace{}, err
	}
	return workspace, nil
}

func (s *SQLite) AbortWorkspaceCheckpoint(ctx context.Context, taskID string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE workspaces SET state = ?, updated_at = ? WHERE task_id = ? AND state = ?`,
		string(domain.WorkspaceReady), now.UTC().Format(time.RFC3339Nano), taskID, string(domain.WorkspaceCheckpointing))
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	return nil
}

func (s *SQLite) RecordWorkspaceCheckpoint(ctx context.Context, checkpoint domain.WorkspaceCheckpoint) (domain.WorkspaceCheckpoint, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.WorkspaceCheckpoint{}, err
	}
	defer tx.Rollback()
	var workspaceState string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM workspaces WHERE id = ? AND task_id = ?`, checkpoint.WorkspaceID, checkpoint.TaskID).Scan(&workspaceState); err != nil {
		return domain.WorkspaceCheckpoint{}, err
	}
	if domain.WorkspaceState(workspaceState) != domain.WorkspaceCheckpointing {
		return domain.WorkspaceCheckpoint{}, ErrStateConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM workspace_checkpoints WHERE task_id = ?`, checkpoint.TaskID).Scan(&checkpoint.Version); err != nil {
		return domain.WorkspaceCheckpoint{}, err
	}
	checkpoint.State = domain.WorkspaceCheckpointCaptured
	checkpoint.CreatedAt = checkpoint.CreatedAt.UTC()
	_, err = tx.ExecContext(ctx, `
INSERT INTO workspace_checkpoints(
 checkpoint_id, task_id, workspace_id, project_id, version, original_head,
 snapshot_revision, ref_name, status_hash, dirty, state, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		checkpoint.ID, checkpoint.TaskID, checkpoint.WorkspaceID, checkpoint.ProjectID, checkpoint.Version,
		checkpoint.OriginalHead, checkpoint.SnapshotRevision, checkpoint.RefName, checkpoint.StatusHash,
		boolInt(checkpoint.Dirty), string(checkpoint.State), checkpoint.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return domain.WorkspaceCheckpoint{}, fmt.Errorf("insert workspace checkpoint: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.WorkspaceCheckpoint{}, err
	}
	return checkpoint, nil
}

func (s *SQLite) FinishWorkspaceCheckpoint(ctx context.Context, taskID, checkpointID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE workspace_checkpoints SET state = ?, compacted_at = ? WHERE checkpoint_id = ? AND task_id = ? AND state = ?`,
		string(domain.WorkspaceCheckpointCompacted), stamp, checkpointID, taskID, string(domain.WorkspaceCheckpointCaptured))
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	res, err = tx.ExecContext(ctx, `UPDATE workspaces SET state = ?, updated_at = ? WHERE task_id = ? AND state = ?`,
		string(domain.WorkspaceCheckpointed), stamp, taskID, string(domain.WorkspaceCheckpointing))
	if err != nil {
		return err
	}
	rows, _ = res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	return tx.Commit()
}

func (s *SQLite) LatestWorkspaceCheckpoint(ctx context.Context, taskID string) (domain.WorkspaceCheckpoint, bool, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT checkpoint_id, task_id, workspace_id, project_id, version, original_head,
       snapshot_revision, ref_name, status_hash, dirty, state, created_at, compacted_at, rehydrated_at
FROM workspace_checkpoints
WHERE task_id = ?
ORDER BY version DESC
LIMIT 1`, taskID)
	var c domain.WorkspaceCheckpoint
	var dirty int
	var state, created string
	var compacted, rehydrated sql.NullString
	err := row.Scan(&c.ID, &c.TaskID, &c.WorkspaceID, &c.ProjectID, &c.Version, &c.OriginalHead,
		&c.SnapshotRevision, &c.RefName, &c.StatusHash, &dirty, &state, &created, &compacted, &rehydrated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceCheckpoint{}, false, nil
	}
	if err != nil {
		return domain.WorkspaceCheckpoint{}, false, err
	}
	c.Dirty = dirty != 0
	c.State = domain.WorkspaceCheckpointState(state)
	if c.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return domain.WorkspaceCheckpoint{}, false, err
	}
	if compacted.Valid {
		tm, parseErr := time.Parse(time.RFC3339Nano, compacted.String)
		if parseErr != nil {
			return domain.WorkspaceCheckpoint{}, false, parseErr
		}
		c.CompactedAt = &tm
	}
	if rehydrated.Valid {
		tm, parseErr := time.Parse(time.RFC3339Nano, rehydrated.String)
		if parseErr != nil {
			return domain.WorkspaceCheckpoint{}, false, parseErr
		}
		c.RehydratedAt = &tm
	}
	return c, true, nil
}

func (s *SQLite) RecoverBlockedCheckpointToWaitingResource(ctx context.Context, taskID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var workspaceState string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM workspaces WHERE task_id = ?`, taskID).Scan(&workspaceState); err != nil {
		return err
	}
	if domain.WorkspaceState(workspaceState) != domain.WorkspaceCheckpointed {
		return ErrStateConflict
	}
	var unsafe int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_attempts WHERE task_id = ? AND authority_state <> ?`,
		taskID, string(domain.AttemptPhysicallyTerminated)).Scan(&unsafe); err != nil {
		return err
	}
	if unsafe != 0 {
		return ErrPhysicalFenceRequired
	}
	res, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		string(domain.TaskWaitingResource), now.UTC().Format(time.RFC3339Nano), taskID, string(domain.TaskBlocked))
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	return tx.Commit()
}

func (s *SQLite) BeginWorkspaceRehydrate(ctx context.Context, taskID string, now time.Time) (domain.Workspace, domain.WorkspaceCheckpoint, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.Workspace{}, domain.WorkspaceCheckpoint{}, err
	}
	defer tx.Rollback()
	workspace, err := getWorkspaceWithQueryer(ctx, tx, `SELECT id, task_id, project_id, path, base_revision, head_revision, state, failure, created_at, updated_at, removed_at FROM workspaces WHERE task_id = ?`, taskID)
	if err != nil {
		return domain.Workspace{}, domain.WorkspaceCheckpoint{}, err
	}
	if workspace.State != domain.WorkspaceCheckpointed {
		return domain.Workspace{}, domain.WorkspaceCheckpoint{}, ErrStateConflict
	}
	var taskState string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, taskID).Scan(&taskState); err != nil {
		return domain.Workspace{}, domain.WorkspaceCheckpoint{}, err
	}
	if domain.TaskState(taskState) != domain.TaskWaitingResource {
		return domain.Workspace{}, domain.WorkspaceCheckpoint{}, ErrStateConflict
	}
	checkpoint, ok, err := latestWorkspaceCheckpointTx(ctx, tx, taskID)
	if err != nil {
		return domain.Workspace{}, domain.WorkspaceCheckpoint{}, err
	}
	if !ok || checkpoint.State != domain.WorkspaceCheckpointCompacted {
		return domain.Workspace{}, domain.WorkspaceCheckpoint{}, ErrStateConflict
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE workspaces SET state = ?, head_revision = ?, updated_at = ? WHERE id = ? AND state = ?`,
		string(domain.WorkspacePreparing), checkpoint.SnapshotRevision, stamp, workspace.ID, string(domain.WorkspaceCheckpointed))
	if err != nil {
		return domain.Workspace{}, domain.WorkspaceCheckpoint{}, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.Workspace{}, domain.WorkspaceCheckpoint{}, ErrStateConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workspace_checkpoints SET state = ? WHERE checkpoint_id = ? AND state = ?`,
		string(domain.WorkspaceCheckpointRehydrating), checkpoint.ID, string(domain.WorkspaceCheckpointCompacted)); err != nil {
		return domain.Workspace{}, domain.WorkspaceCheckpoint{}, err
	}
	workspace.State = domain.WorkspacePreparing
	workspace.HeadRevision = checkpoint.SnapshotRevision
	workspace.UpdatedAt = now.UTC()
	checkpoint.State = domain.WorkspaceCheckpointRehydrating
	if err := tx.Commit(); err != nil {
		return domain.Workspace{}, domain.WorkspaceCheckpoint{}, err
	}
	return workspace, checkpoint, nil
}

func (s *SQLite) FinishWorkspaceRehydrate(ctx context.Context, taskID, checkpointID, headRevision string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE workspaces SET state = ?, head_revision = ?, failure = '', updated_at = ? WHERE task_id = ? AND state = ? AND head_revision = ?`,
		string(domain.WorkspaceReady), headRevision, stamp, taskID, string(domain.WorkspacePreparing), headRevision)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	res, err = tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		string(domain.TaskWorkspaceReady), stamp, taskID, string(domain.TaskWaitingResource))
	if err != nil {
		return err
	}
	rows, _ = res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	res, err = tx.ExecContext(ctx, `UPDATE workspace_checkpoints SET state = ?, rehydrated_at = ? WHERE checkpoint_id = ? AND task_id = ? AND state = ?`,
		string(domain.WorkspaceCheckpointRehydrated), stamp, checkpointID, taskID, string(domain.WorkspaceCheckpointRehydrating))
	if err != nil {
		return err
	}
	rows, _ = res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	return tx.Commit()
}

func latestWorkspaceCheckpointTx(ctx context.Context, tx *sql.Tx, taskID string) (domain.WorkspaceCheckpoint, bool, error) {
	row := tx.QueryRowContext(ctx, `
SELECT checkpoint_id, task_id, workspace_id, project_id, version, original_head,
       snapshot_revision, ref_name, status_hash, dirty, state, created_at, compacted_at, rehydrated_at
FROM workspace_checkpoints WHERE task_id = ? ORDER BY version DESC LIMIT 1`, taskID)
	var c domain.WorkspaceCheckpoint
	var dirty int
	var state, created string
	var compacted, rehydrated sql.NullString
	err := row.Scan(&c.ID, &c.TaskID, &c.WorkspaceID, &c.ProjectID, &c.Version, &c.OriginalHead,
		&c.SnapshotRevision, &c.RefName, &c.StatusHash, &dirty, &state, &created, &compacted, &rehydrated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceCheckpoint{}, false, nil
	}
	if err != nil {
		return domain.WorkspaceCheckpoint{}, false, err
	}
	c.Dirty = dirty != 0
	c.State = domain.WorkspaceCheckpointState(state)
	if c.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return domain.WorkspaceCheckpoint{}, false, err
	}
	if compacted.Valid {
		tm, parseErr := time.Parse(time.RFC3339Nano, compacted.String)
		if parseErr != nil {
			return domain.WorkspaceCheckpoint{}, false, parseErr
		}
		c.CompactedAt = &tm
	}
	if rehydrated.Valid {
		tm, parseErr := time.Parse(time.RFC3339Nano, rehydrated.String)
		if parseErr != nil {
			return domain.WorkspaceCheckpoint{}, false, parseErr
		}
		c.RehydratedAt = &tm
	}
	return c, true, nil
}
