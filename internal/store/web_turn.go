package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"mar/internal/domain"
)

var ErrWebTurnConflict = errors.New("web turn already has different content")

func (s *SQLite) PublishWebTurn(ctx context.Context, turn domain.WebTurn) (domain.WebTurn, bool, error) {
	if !turn.IntegrityValid() || len(turn.Response) != 0 {
		return domain.WebTurn{}, false, errors.New("pending web turn integrity is invalid")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	defer tx.Rollback()

	if existing, ok, err := webTurnByTaskRequest(ctx, tx, turn.TaskID, turn.RequestID); err != nil || ok {
		if err != nil {
			return domain.WebTurn{}, false, err
		}
		if existing.AttemptID != turn.AttemptID || existing.RunEpoch != turn.RunEpoch || !bytes.Equal(existing.Request, turn.Request) || !existing.IntegrityValid() {
			return domain.WebTurn{}, false, ErrWebTurnConflict
		}
		return existing, false, nil
	}

	var state string
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, turn.TaskID).Scan(&state); err != nil {
		return domain.WebTurn{}, false, err
	}
	if domain.TaskState(state) != domain.TaskRunning {
		return domain.WebTurn{}, false, ErrStateConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_attempts WHERE attempt_id = ? AND task_id = ? AND run_epoch = ? AND authority_state = ?`,
		turn.AttemptID, turn.TaskID, turn.RunEpoch, string(domain.AttemptActive)).Scan(&active); err != nil {
		return domain.WebTurn{}, false, err
	}
	if active != 1 {
		return domain.WebTurn{}, false, ErrStaleAttempt
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO web_turns(turn_id, task_id, attempt_id, run_epoch, request_id, request_json, response_json, request_hash, response_hash, integrity_hash, created_at, responded_at) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, '', ?, ?, NULL)`,
		turn.ID, turn.TaskID, turn.AttemptID, turn.RunEpoch, turn.RequestID, []byte(turn.Request), turn.RequestHash, turn.IntegrityHash, turn.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ? AND run_epoch = ?`,
		string(domain.TaskInputRequired), turn.CreatedAt.UTC().Format(time.RFC3339Nano), turn.TaskID, string(domain.TaskRunning), turn.RunEpoch)
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.WebTurn{}, false, ErrStateConflict
	}
	if err := tx.Commit(); err != nil {
		return domain.WebTurn{}, false, err
	}
	return turn, true, nil
}

func (s *SQLite) RespondWebTurn(ctx context.Context, completed domain.WebTurn) (domain.WebTurn, bool, error) {
	if !completed.IntegrityValid() || len(completed.Response) == 0 || completed.RespondedAt == nil {
		return domain.WebTurn{}, false, errors.New("completed web turn integrity is invalid")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	defer tx.Rollback()
	existing, err := webTurnByID(ctx, tx, completed.ID)
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	if existing.TaskID != completed.TaskID || existing.AttemptID != completed.AttemptID || existing.RunEpoch != completed.RunEpoch || existing.RequestID != completed.RequestID || !bytes.Equal(existing.Request, completed.Request) {
		return domain.WebTurn{}, false, ErrWebTurnConflict
	}
	if len(existing.Response) != 0 {
		if bytes.Equal(existing.Response, completed.Response) && existing.IntegrityValid() {
			return existing, false, nil
		}
		return domain.WebTurn{}, false, ErrWebTurnConflict
	}
	var state string
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, completed.TaskID).Scan(&state); err != nil {
		return domain.WebTurn{}, false, err
	}
	if domain.TaskState(state) != domain.TaskInputRequired {
		return domain.WebTurn{}, false, ErrStateConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_attempts WHERE attempt_id = ? AND task_id = ? AND run_epoch = ? AND authority_state = ?`,
		completed.AttemptID, completed.TaskID, completed.RunEpoch, string(domain.AttemptActive)).Scan(&active); err != nil {
		return domain.WebTurn{}, false, err
	}
	if active != 1 {
		return domain.WebTurn{}, false, ErrStaleAttempt
	}
	_, err = tx.ExecContext(ctx, `UPDATE web_turns SET response_json = ?, response_hash = ?, integrity_hash = ?, responded_at = ? WHERE turn_id = ? AND response_json IS NULL`,
		[]byte(completed.Response), completed.ResponseHash, completed.IntegrityHash, completed.RespondedAt.UTC().Format(time.RFC3339Nano), completed.ID)
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND state = ? AND run_epoch = ?`,
		string(domain.TaskRunning), completed.RespondedAt.UTC().Format(time.RFC3339Nano), completed.TaskID, string(domain.TaskInputRequired), completed.RunEpoch)
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return domain.WebTurn{}, false, ErrStateConflict
	}
	if err := tx.Commit(); err != nil {
		return domain.WebTurn{}, false, err
	}
	return completed, true, nil
}

func (s *SQLite) PendingWebTurn(ctx context.Context, taskID string) (domain.WebTurn, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT turn_id, task_id, attempt_id, run_epoch, request_id, request_json, response_json, request_hash, response_hash, integrity_hash, created_at, responded_at FROM web_turns WHERE task_id = ? AND responded_at IS NULL ORDER BY created_at DESC LIMIT 1`, taskID)
	turn, err := scanWebTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WebTurn{}, false, nil
	}
	if err != nil {
		return domain.WebTurn{}, false, err
	}
	if !turn.IntegrityValid() {
		return domain.WebTurn{}, false, errors.New("pending web turn integrity is invalid")
	}
	return turn, true, nil
}

func (s *SQLite) GetWebTurn(ctx context.Context, turnID string) (domain.WebTurn, error) {
	turn, err := webTurnByID(ctx, s.db, turnID)
	if err != nil {
		return domain.WebTurn{}, err
	}
	if !turn.IntegrityValid() {
		return domain.WebTurn{}, errors.New("web turn integrity is invalid")
	}
	return turn, nil
}

type webTurnScanner interface {
	Scan(...any) error
}

type webTurnQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func webTurnByTaskRequest(ctx context.Context, q webTurnQueryer, taskID, requestID string) (domain.WebTurn, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT turn_id, task_id, attempt_id, run_epoch, request_id, request_json, response_json, request_hash, response_hash, integrity_hash, created_at, responded_at FROM web_turns WHERE task_id = ? AND request_id = ?`, taskID, requestID)
	turn, err := scanWebTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WebTurn{}, false, nil
	}
	return turn, err == nil, err
}

func webTurnByID(ctx context.Context, q webTurnQueryer, turnID string) (domain.WebTurn, error) {
	row := q.QueryRowContext(ctx, `SELECT turn_id, task_id, attempt_id, run_epoch, request_id, request_json, response_json, request_hash, response_hash, integrity_hash, created_at, responded_at FROM web_turns WHERE turn_id = ?`, turnID)
	turn, err := scanWebTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WebTurn{}, ErrNotFound
	}
	return turn, err
}

func scanWebTurn(row webTurnScanner) (domain.WebTurn, error) {
	var turn domain.WebTurn
	var request, response []byte
	var created string
	var responded sql.NullString
	if err := row.Scan(&turn.ID, &turn.TaskID, &turn.AttemptID, &turn.RunEpoch, &turn.RequestID, &request, &response, &turn.RequestHash, &turn.ResponseHash, &turn.IntegrityHash, &created, &responded); err != nil {
		return domain.WebTurn{}, err
	}
	turn.Request = append(json.RawMessage(nil), request...)
	if len(response) != 0 {
		turn.Response = append(json.RawMessage(nil), response...)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.WebTurn{}, fmt.Errorf("parse web turn created_at: %w", err)
	}
	turn.CreatedAt = createdAt
	if responded.Valid {
		value, err := time.Parse(time.RFC3339Nano, responded.String)
		if err != nil {
			return domain.WebTurn{}, fmt.Errorf("parse web turn responded_at: %w", err)
		}
		turn.RespondedAt = &value
	}
	return turn, nil
}
