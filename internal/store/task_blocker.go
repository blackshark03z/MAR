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

func validateTaskBlocker(blocker domain.TaskBlocker) error {
	if strings.TrimSpace(blocker.TaskID) == "" || strings.TrimSpace(string(blocker.Phase)) == "" || strings.TrimSpace(blocker.Code) == "" || strings.TrimSpace(blocker.Detail) == "" {
		return errors.New("task blocker requires task_id, phase, code and detail")
	}
	return nil
}

func upsertTaskBlockerTx(ctx context.Context, tx *sql.Tx, blocker domain.TaskBlocker, now time.Time) error {
	if err := validateTaskBlocker(blocker); err != nil {
		return err
	}
	created := blocker.CreatedAt
	if created.IsZero() {
		created = now.UTC()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO task_blockers(task_id, phase, code, detail, recovery, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(task_id) DO UPDATE SET phase=excluded.phase, code=excluded.code, detail=excluded.detail, recovery=excluded.recovery, updated_at=excluded.updated_at`, blocker.TaskID, string(blocker.Phase), strings.TrimSpace(blocker.Code), strings.TrimSpace(blocker.Detail), strings.TrimSpace(blocker.Recovery), created.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert task blocker: %w", err)
	}
	return nil
}

func (s *SQLite) BlockTask(ctx context.Context, taskID string, from domain.TaskState, blocker domain.TaskBlocker, now time.Time) error {
	blocker.TaskID = taskID
	if err := validateTaskBlocker(blocker); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ? AND NOT EXISTS (SELECT 1 FROM execution_attempts a WHERE a.task_id = tasks.id AND a.authority_state != ?)`, string(domain.TaskBlocked), now.UTC().Format(time.RFC3339Nano), taskID, string(from), string(domain.AttemptPhysicallyTerminated))
	if err != nil {
		return fmt.Errorf("block task: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	if err := upsertTaskBlockerTx(ctx, tx, blocker, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) SetTaskBlocker(ctx context.Context, blocker domain.TaskBlocker, now time.Time) error {
	if err := validateTaskBlocker(blocker); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, blocker.TaskID).Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if domain.TaskState(state) != domain.TaskBlocked {
		return ErrStateConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET updated_at = ? WHERE id = ? AND state = ?`, now.UTC().Format(time.RFC3339Nano), blocker.TaskID, string(domain.TaskBlocked)); err != nil {
		return err
	}
	if err := upsertTaskBlockerTx(ctx, tx, blocker, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) CurrentTaskBlocker(ctx context.Context, taskID string) (domain.TaskBlocker, bool, error) {
	var b domain.TaskBlocker
	var phase, created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT task_id, phase, code, detail, recovery, created_at, updated_at FROM task_blockers WHERE task_id = ?`, taskID).Scan(&b.TaskID, &phase, &b.Code, &b.Detail, &b.Recovery, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskBlocker{}, false, nil
	}
	if err != nil {
		return domain.TaskBlocker{}, false, err
	}
	b.Phase = domain.BlockerPhase(phase)
	if b.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return domain.TaskBlocker{}, false, err
	}
	if b.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return domain.TaskBlocker{}, false, err
	}
	return b, true, nil
}

func clearTaskBlockerTx(ctx context.Context, tx *sql.Tx, taskID string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM task_blockers WHERE task_id = ?`, taskID)
	return err
}
func (s *SQLite) ClearTaskBlocker(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM task_blockers WHERE task_id = ?`, taskID)
	return err
}
func (s *SQLite) RecoverBlockedWithoutWorkspaceToPreflight(ctx context.Context, taskID string, now time.Time) error {
	return s.recoverNoWorkspaceToPreflight(ctx, taskID, domain.TaskBlocked, now)
}
func (s *SQLite) RecoverWorkspaceReadyWithoutWorkspaceToPreflight(ctx context.Context, taskID string, now time.Time) error {
	return s.recoverNoWorkspaceToPreflight(ctx, taskID, domain.TaskWorkspaceReady, now)
}
func (s *SQLite) recoverNoWorkspaceToPreflight(ctx context.Context, taskID string, from domain.TaskState, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ? AND run_epoch = 0 AND NOT EXISTS (SELECT 1 FROM execution_attempts a WHERE a.task_id = tasks.id) AND NOT EXISTS (SELECT 1 FROM workspaces w WHERE w.task_id = tasks.id)`, string(domain.TaskPreflight), now.UTC().Format(time.RFC3339Nano), taskID, string(from))
	if err != nil {
		return fmt.Errorf("recover no-workspace task to preflight: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return ErrStateConflict
	}
	if err := clearTaskBlockerTx(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}
