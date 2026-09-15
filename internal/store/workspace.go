package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"mar/internal/domain"
)

var (
	ErrWorkspaceConflict      = errors.New("workspace already exists with different immutable identity")
	ErrWorkspaceRemovalUnsafe = errors.New("workspace removal is not safe for current task/attempt state")
)

func (s *SQLite) BeginWorkspace(ctx context.Context, workspace domain.Workspace) (domain.Workspace, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.Workspace{}, false, fmt.Errorf("begin workspace transaction: %w", err)
	}
	defer tx.Rollback()

	var taskState, projectID string
	if err := tx.QueryRowContext(ctx, `SELECT state, project_id FROM tasks WHERE id = ?`, workspace.TaskID).Scan(&taskState, &projectID); errors.Is(err, sql.ErrNoRows) {
		return domain.Workspace{}, false, ErrNotFound
	} else if err != nil {
		return domain.Workspace{}, false, fmt.Errorf("read task for workspace: %w", err)
	}
	if domain.TaskState(taskState) != domain.TaskWaitingResource || projectID != workspace.ProjectID {
		return domain.Workspace{}, false, ErrStateConflict
	}

	res, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO workspaces(
    id, task_id, project_id, path, base_revision, head_revision, state, failure, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, '', ?, '', ?, ?)`,
		workspace.ID,
		workspace.TaskID,
		workspace.ProjectID,
		workspace.Path,
		workspace.BaseRevision,
		string(domain.WorkspacePreparing),
		workspace.CreatedAt.UTC().Format(time.RFC3339Nano),
		workspace.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return domain.Workspace{}, false, fmt.Errorf("insert workspace: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return domain.Workspace{}, false, err
	}
	if rows == 1 {
		workspace.State = domain.WorkspacePreparing
		if err := tx.Commit(); err != nil {
			return domain.Workspace{}, false, err
		}
		return workspace, true, nil
	}

	existing, err := getWorkspaceWithQueryer(ctx, tx, `SELECT id, task_id, project_id, path, base_revision, head_revision, state, failure, created_at, updated_at, removed_at FROM workspaces WHERE task_id = ?`, workspace.TaskID)
	if err != nil {
		return domain.Workspace{}, false, err
	}
	if existing.ProjectID != workspace.ProjectID || !samePath(existing.Path, workspace.Path) || existing.BaseRevision != workspace.BaseRevision {
		return domain.Workspace{}, false, ErrWorkspaceConflict
	}
	if err := tx.Commit(); err != nil {
		return domain.Workspace{}, false, err
	}
	return existing, false, nil
}

func (s *SQLite) GetWorkspaceByTask(ctx context.Context, taskID string) (domain.Workspace, error) {
	return getWorkspaceWithQueryer(ctx, s.db, `SELECT id, task_id, project_id, path, base_revision, head_revision, state, failure, created_at, updated_at, removed_at FROM workspaces WHERE task_id = ?`, taskID)
}

func (s *SQLite) ListTerminalWorkspaceRemovalCandidates(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id
FROM tasks t
JOIN workspaces w ON w.task_id = t.id
WHERE w.state IN (?, ?, ?)
  AND NOT EXISTS (
      SELECT 1 FROM execution_attempts a
      WHERE a.task_id = t.id AND a.authority_state <> 'PHYSICALLY_TERMINATED'
  )
  AND (
      t.state IN (?, ?)
      OR (
          t.state = ?
          AND EXISTS (
              SELECT 1
              FROM task_results tr
              WHERE tr.task_id = t.id
                AND tr.version = (SELECT MAX(latest.version) FROM task_results latest WHERE latest.task_id = t.id)
                AND tr.verdict = ?
                AND tr.integration_status = 'INTEGRATED'
                AND tr.workspace_disposition = 'RETAINED'
                AND tr.final_revision = w.head_revision
          )
      )
  )
ORDER BY t.updated_at ASC, t.id ASC
LIMIT ?`,
		string(domain.WorkspaceReady), string(domain.WorkspaceFailed), string(domain.WorkspaceRemoving),
		string(domain.TaskCancelled), string(domain.TaskFailed), string(domain.TaskComplete), string(domain.ResultVerified), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var taskIDs []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil, err
		}
		taskIDs = append(taskIDs, taskID)
	}
	return taskIDs, rows.Err()
}

func (s *SQLite) MarkWorkspaceReady(ctx context.Context, workspaceID, taskID, headRevision string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `
UPDATE workspaces SET state = ?, head_revision = ?, failure = '', updated_at = ?
WHERE id = ? AND task_id = ? AND state = ?`,
		string(domain.WorkspaceReady), headRevision, stamp, workspaceID, taskID, string(domain.WorkspacePreparing),
	)
	if err != nil {
		return fmt.Errorf("mark workspace ready: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	res, err = tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		string(domain.TaskWorkspaceReady), stamp, taskID, string(domain.TaskWaitingResource))
	if err != nil {
		return fmt.Errorf("mark task workspace ready: %w", err)
	}
	rows, _ = res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	return tx.Commit()
}

func (s *SQLite) RecordWorkspaceHeadForAttempt(ctx context.Context, taskID, attemptID string, epoch int64, expectedHead, candidateHead string, now time.Time) error {
	if expectedHead == "" || candidateHead == "" {
		return errors.New("expected and candidate workspace head are required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := validateAttemptAuthorityTx(ctx, tx, taskID, attemptID, epoch); err != nil {
		return err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `
UPDATE workspaces SET head_revision = ?, updated_at = ?
WHERE task_id = ? AND state = ? AND head_revision = ?`,
		candidateHead, stamp, taskID, string(domain.WorkspaceReady), expectedHead,
	)
	if err != nil {
		return fmt.Errorf("record workspace candidate head: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		var current string
		if err := tx.QueryRowContext(ctx, `SELECT head_revision FROM workspaces WHERE task_id = ? AND state = ?`, taskID, string(domain.WorkspaceReady)).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if current != candidateHead {
			return ErrStateConflict
		}
	}
	return tx.Commit()
}

func (s *SQLite) MarkWorkspaceFailed(ctx context.Context, workspaceID, taskID, failure string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE workspaces SET state = ?, failure = ?, updated_at = ? WHERE id = ? AND task_id = ? AND state = ?`,
		string(domain.WorkspaceFailed), failure, stamp, workspaceID, taskID, string(domain.WorkspacePreparing))
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	res, err = tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		string(domain.TaskBlocked), stamp, taskID, string(domain.TaskWaitingResource))
	if err != nil {
		return err
	}
	rows, _ = res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	return tx.Commit()
}

func (s *SQLite) BeginWorkspaceRemoval(ctx context.Context, taskID string, now time.Time) (domain.Workspace, error) {
	workspaceSnapshot, err := s.GetWorkspaceByTask(ctx, taskID)
	if err != nil {
		return domain.Workspace{}, err
	}
	if workspaceSnapshot.State == domain.WorkspaceRemoved {
		return workspaceSnapshot, nil
	}
	taskSnapshot, err := s.GetTask(ctx, taskID)
	if err != nil {
		return domain.Workspace{}, err
	}
	var completeResult domain.TaskResult
	if taskSnapshot.State == domain.TaskComplete {
		latest, ok, resultErr := s.LatestTaskResult(ctx, taskID)
		if resultErr != nil {
			return domain.Workspace{}, resultErr
		}
		if !ok || latest.Verdict != domain.ResultVerified || latest.IntegrationStatus != "INTEGRATED" || latest.WorkspaceDisposition != "RETAINED" || latest.FinalRevision != workspaceSnapshot.HeadRevision {
			return domain.Workspace{}, ErrWorkspaceRemovalUnsafe
		}
		completeResult = latest
	} else if taskSnapshot.State != domain.TaskCancelled && taskSnapshot.State != domain.TaskFailed {
		return domain.Workspace{}, ErrWorkspaceRemovalUnsafe
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.Workspace{}, err
	}
	defer tx.Rollback()

	workspace, err := getWorkspaceWithQueryer(ctx, tx, `SELECT id, task_id, project_id, path, base_revision, head_revision, state, failure, created_at, updated_at, removed_at FROM workspaces WHERE task_id = ?`, taskID)
	if err != nil {
		return domain.Workspace{}, err
	}
	if workspace.State == domain.WorkspaceRemoved {
		if err := tx.Commit(); err != nil {
			return domain.Workspace{}, err
		}
		return workspace, nil
	}
	var taskState string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, taskID).Scan(&taskState); err != nil {
		return domain.Workspace{}, err
	}
	state := domain.TaskState(taskState)
	switch state {
	case domain.TaskCancelled, domain.TaskFailed:
		if state != taskSnapshot.State {
			return domain.Workspace{}, ErrStateConflict
		}
	case domain.TaskComplete:
		if taskSnapshot.State != domain.TaskComplete {
			return domain.Workspace{}, ErrStateConflict
		}
		var currentResultID string
		var currentResultVersion int64
		if err := tx.QueryRowContext(ctx, `SELECT result_id, version FROM task_results WHERE task_id = ? ORDER BY version DESC LIMIT 1`, taskID).Scan(&currentResultID, &currentResultVersion); err != nil {
			return domain.Workspace{}, err
		}
		if currentResultID != completeResult.ID || currentResultVersion != completeResult.Version || completeResult.FinalRevision != workspace.HeadRevision {
			return domain.Workspace{}, ErrStateConflict
		}
	default:
		return domain.Workspace{}, ErrWorkspaceRemovalUnsafe
	}
	var unsafeAttempts int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_attempts WHERE task_id = ? AND authority_state != ?`,
		taskID, string(domain.AttemptPhysicallyTerminated)).Scan(&unsafeAttempts); err != nil {
		return domain.Workspace{}, err
	}
	if unsafeAttempts != 0 {
		return domain.Workspace{}, ErrPhysicalFenceRequired
	}
	if workspace.State == domain.WorkspaceRemoving {
		if err := tx.Commit(); err != nil {
			return domain.Workspace{}, err
		}
		return workspace, nil
	}
	if workspace.State != domain.WorkspaceReady && workspace.State != domain.WorkspaceFailed {
		return domain.Workspace{}, ErrWorkspaceRemovalUnsafe
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE workspaces SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		string(domain.WorkspaceRemoving), stamp, workspace.ID, string(workspace.State))
	if err != nil {
		return domain.Workspace{}, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.Workspace{}, ErrStateConflict
	}
	workspace.State = domain.WorkspaceRemoving
	workspace.UpdatedAt = now.UTC()
	if err := tx.Commit(); err != nil {
		return domain.Workspace{}, err
	}
	return workspace, nil
}

func (s *SQLite) FinishWorkspaceRemoval(ctx context.Context, workspaceID, newResultID string, now time.Time) error {
	workspaceSnapshot, err := getWorkspaceWithQueryer(ctx, s.db, `SELECT id, task_id, project_id, path, base_revision, head_revision, state, failure, created_at, updated_at, removed_at FROM workspaces WHERE id = ?`, workspaceID)
	if err != nil {
		return err
	}
	if workspaceSnapshot.State == domain.WorkspaceRemoved {
		return nil
	}
	taskSnapshot, err := s.GetTask(ctx, workspaceSnapshot.TaskID)
	if err != nil {
		return err
	}
	var completeResult domain.TaskResult
	if taskSnapshot.State == domain.TaskComplete {
		latest, ok, resultErr := s.LatestTaskResult(ctx, workspaceSnapshot.TaskID)
		if resultErr != nil {
			return resultErr
		}
		if !ok || latest.Verdict != domain.ResultVerified || latest.IntegrationStatus != "INTEGRATED" || latest.WorkspaceDisposition != "RETAINED" || latest.FinalRevision != workspaceSnapshot.HeadRevision || newResultID == "" {
			return ErrWorkspaceRemovalUnsafe
		}
		completeResult = latest
	} else if taskSnapshot.State != domain.TaskCancelled && taskSnapshot.State != domain.TaskFailed {
		return ErrWorkspaceRemovalUnsafe
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	workspace, err := getWorkspaceWithQueryer(ctx, tx, `SELECT id, task_id, project_id, path, base_revision, head_revision, state, failure, created_at, updated_at, removed_at FROM workspaces WHERE id = ?`, workspaceID)
	if err != nil {
		return err
	}
	if workspace.State == domain.WorkspaceRemoved {
		return tx.Commit()
	}
	if workspace.State != domain.WorkspaceRemoving {
		return ErrWorkspaceRemovalUnsafe
	}
	var taskState string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, workspace.TaskID).Scan(&taskState); err != nil {
		return err
	}
	state := domain.TaskState(taskState)
	if state == domain.TaskComplete {
		if taskSnapshot.State != domain.TaskComplete {
			return ErrStateConflict
		}
		var currentResultID string
		var currentResultVersion int64
		if err := tx.QueryRowContext(ctx, `SELECT result_id, version FROM task_results WHERE task_id = ? ORDER BY version DESC LIMIT 1`, workspace.TaskID).Scan(&currentResultID, &currentResultVersion); err != nil {
			return err
		}
		if currentResultID != completeResult.ID || currentResultVersion != completeResult.Version || completeResult.FinalRevision != workspace.HeadRevision {
			return ErrStateConflict
		}
		removed := completeResult
		removed.ID = newResultID
		removed.Version = completeResult.Version + 1
		removed.WorkspaceDisposition = "REMOVED"
		removed.PassFailEvidence = append(append([]string{}, completeResult.PassFailEvidence...), "workspace:"+workspace.ID+":REMOVED")
		removed.ChangedAreas = append([]string{}, completeResult.ChangedAreas...)
		removed.VerificationExecuted = append([]string{}, completeResult.VerificationExecuted...)
		removed.UnresolvedRisks = append([]string{}, completeResult.UnresolvedRisks...)
		removed.CreatedAt = now.UTC()
		removed.IntegrityHash, err = removed.IntegrityDigest()
		if err != nil {
			return err
		}
		if err := insertIntegrationResultTx(ctx, tx, removed); err != nil {
			return err
		}
	} else if state != domain.TaskCancelled && state != domain.TaskFailed {
		return ErrWorkspaceRemovalUnsafe
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE workspaces SET state = ?, updated_at = ?, removed_at = ? WHERE id = ? AND state = ?`,
		string(domain.WorkspaceRemoved), stamp, stamp, workspaceID, string(domain.WorkspaceRemoving))
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	return tx.Commit()
}

func getWorkspaceWithQueryer(ctx context.Context, q queryer, query string, arg string) (domain.Workspace, error) {
	var w domain.Workspace
	var state, created, updated string
	var removed sql.NullString
	err := q.QueryRowContext(ctx, query, arg).Scan(
		&w.ID, &w.TaskID, &w.ProjectID, &w.Path, &w.BaseRevision, &w.HeadRevision,
		&state, &w.Failure, &created, &updated, &removed,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Workspace{}, ErrNotFound
	}
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("get workspace: %w", err)
	}
	w.State = domain.WorkspaceState(state)
	var parseErr error
	if w.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created); parseErr != nil {
		return domain.Workspace{}, parseErr
	}
	if w.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated); parseErr != nil {
		return domain.Workspace{}, parseErr
	}
	if removed.Valid {
		tm, err := time.Parse(time.RFC3339Nano, removed.String)
		if err != nil {
			return domain.Workspace{}, err
		}
		w.RemovedAt = &tm
	}
	return w, nil
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
