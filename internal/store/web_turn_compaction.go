package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"mar/internal/domain"
)

type TerminalWebTurnCompaction struct {
	Rows         int64 `json:"rows"`
	RequestBytes int64 `json:"request_bytes"`
}

func (s *SQLite) TerminalWebTurnCompactionPlan(ctx context.Context, cutoff time.Time) (TerminalWebTurnCompaction, error) {
	if cutoff.IsZero() {
		return TerminalWebTurnCompaction{}, errors.New("terminal web turn compaction cutoff is required")
	}
	var result TerminalWebTurnCompaction
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(LENGTH(w.request_json)), 0)
FROM web_turns w
JOIN tasks t ON t.id = w.task_id
WHERE t.state IN (?, ?)
  AND t.updated_at <= ?
  AND w.responded_at IS NOT NULL
  AND w.request_compacted = 0`,
		string(domain.TaskComplete),
		string(domain.TaskCancelled),
		cutoff.UTC().Format(time.RFC3339Nano),
	).Scan(&result.Rows, &result.RequestBytes)
	if err != nil {
		return TerminalWebTurnCompaction{}, fmt.Errorf("plan terminal web turn compaction: %w", err)
	}
	return result, nil
}

func (s *SQLite) CompactTerminalWebTurnRequests(ctx context.Context, cutoff time.Time) (TerminalWebTurnCompaction, error) {
	plan, err := s.TerminalWebTurnCompactionPlan(ctx, cutoff)
	if err != nil || plan.Rows == 0 {
		return plan, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TerminalWebTurnCompaction{}, fmt.Errorf("begin terminal web turn compaction: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
UPDATE web_turns
SET request_json = x'', request_compacted = 1
WHERE turn_id IN (
	SELECT w.turn_id
	FROM web_turns w
	JOIN tasks t ON t.id = w.task_id
	WHERE t.state IN (?, ?)
	  AND t.updated_at <= ?
	  AND w.responded_at IS NOT NULL
	  AND w.request_compacted = 0
)`,
		string(domain.TaskComplete),
		string(domain.TaskCancelled),
		cutoff.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return TerminalWebTurnCompaction{}, fmt.Errorf("compact terminal web turn requests: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return TerminalWebTurnCompaction{}, fmt.Errorf("count compacted terminal web turns: %w", err)
	}
	if rows != plan.Rows {
		return TerminalWebTurnCompaction{}, fmt.Errorf("terminal web turn compaction changed %d rows, planned %d", rows, plan.Rows)
	}
	if err := tx.Commit(); err != nil {
		return TerminalWebTurnCompaction{}, fmt.Errorf("commit terminal web turn compaction: %w", err)
	}
	return plan, nil
}

func (s *SQLite) Vacuum(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("vacuum sqlite: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("truncate sqlite WAL after vacuum: %w", err)
	}
	return nil
}
