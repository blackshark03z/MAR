package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mar/internal/domain"
	"mar/internal/model"
)

type WebEpisodeUsage struct {
	Decisions        int64 `json:"decisions"`
	ControlCalls     int64 `json:"control_calls"`
	PayloadBytes     int64 `json:"payload_bytes"`
	WorkerToolCalls  int64 `json:"worker_tool_calls"`
	ModelTotalTokens int64 `json:"model_total_tokens"`
}

type TaskConvergenceUsage struct {
	ModelDecisions   int64         `json:"model_decisions"`
	WorkerToolCalls  int64         `json:"worker_tool_calls"`
	ModelTotalTokens int64         `json:"model_total_tokens"`
	ActiveExecution  time.Duration `json:"active_execution"`
	Attempts         int64         `json:"attempts"`
	NoProgressStreak int64         `json:"no_progress_streak"`
}

func (s *SQLite) WebEpisodeUsage(ctx context.Context, taskID, attemptID string, epoch int64, requireCurrent bool) (WebEpisodeUsage, error) {
	taskID = strings.TrimSpace(taskID)
	attemptID = strings.TrimSpace(attemptID)
	if taskID == "" || attemptID == "" || epoch <= 0 {
		return WebEpisodeUsage{}, errors.New("web episode usage requires task, attempt and positive epoch")
	}
	if requireCurrent {
		var currentEpoch int64
		var authority string
		err := s.db.QueryRowContext(ctx, `
SELECT t.run_epoch, a.authority_state
FROM tasks t
JOIN execution_attempts a ON a.task_id=t.id AND a.attempt_id=? AND a.run_epoch=?
WHERE t.id=?`, attemptID, epoch, taskID).Scan(&currentEpoch, &authority)
		if err != nil {
			return WebEpisodeUsage{}, err
		}
		if currentEpoch != epoch || domain.AttemptAuthorityState(authority) != domain.AttemptActive {
			return WebEpisodeUsage{}, ErrStaleAttempt
		}
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT turn_id, task_id, attempt_id, run_epoch, request_id, request_json, response_json,
       request_hash, response_hash, integrity_hash, created_at, responded_at
FROM web_turns WHERE task_id=? AND attempt_id=? AND run_epoch=? ORDER BY created_at`, taskID, attemptID, epoch)
	if err != nil {
		return WebEpisodeUsage{}, err
	}
	defer rows.Close()
	var usage WebEpisodeUsage
	for rows.Next() {
		turn, err := scanWebTurn(rows)
		if err != nil {
			return WebEpisodeUsage{}, err
		}
		if !turn.IntegrityValid() {
			return WebEpisodeUsage{}, errors.New("web episode contains invalid turn integrity")
		}
		usage.ControlCalls++
		usage.PayloadBytes += int64(len(turn.Request))
		if len(turn.Response) == 0 {
			continue
		}
		usage.ControlCalls++
		usage.Decisions++
		usage.PayloadBytes += int64(len(turn.Response))
		var response model.TurnResponse
		if err := json.Unmarshal(turn.Response, &response); err != nil {
			return WebEpisodeUsage{}, fmt.Errorf("decode durable web turn response: %w", err)
		}
		usage.WorkerToolCalls += int64(len(response.Message.ToolCalls))
		usage.ModelTotalTokens += response.Usage.TotalTokens
	}
	if err := rows.Err(); err != nil {
		return WebEpisodeUsage{}, err
	}
	return usage, nil
}

type convergenceAttempt struct {
	id         string
	epoch      int64
	started    time.Time
	terminated *time.Time
}

func (s *SQLite) TaskConvergenceUsage(ctx context.Context, taskID string, now time.Time) (TaskConvergenceUsage, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return TaskConvergenceUsage{}, errors.New("task convergence usage requires task id")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT attempt_id, run_epoch, started_at, terminated_at
FROM execution_attempts WHERE task_id=? ORDER BY run_epoch`, taskID)
	if err != nil {
		return TaskConvergenceUsage{}, err
	}
	var attempts []convergenceAttempt
	for rows.Next() {
		var a convergenceAttempt
		var startedRaw string
		var terminatedRaw sql.NullString
		if err := rows.Scan(&a.id, &a.epoch, &startedRaw, &terminatedRaw); err != nil {
			rows.Close()
			return TaskConvergenceUsage{}, err
		}
		a.started, err = time.Parse(time.RFC3339Nano, startedRaw)
		if err != nil {
			rows.Close()
			return TaskConvergenceUsage{}, fmt.Errorf("parse attempt start: %w", err)
		}
		if terminatedRaw.Valid {
			tm, parseErr := time.Parse(time.RFC3339Nano, terminatedRaw.String)
			if parseErr != nil {
				rows.Close()
				return TaskConvergenceUsage{}, fmt.Errorf("parse attempt termination: %w", parseErr)
			}
			a.terminated = &tm
		}
		attempts = append(attempts, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return TaskConvergenceUsage{}, err
	}
	if err := rows.Close(); err != nil {
		return TaskConvergenceUsage{}, err
	}

	var out TaskConvergenceUsage
	var signatures []string
	for _, a := range attempts {
		out.Attempts++
		end := now.UTC()
		if a.terminated != nil {
			end = a.terminated.UTC()
		}
		if end.After(a.started) {
			out.ActiveExecution += end.Sub(a.started)
		}
		episode, err := s.WebEpisodeUsage(ctx, taskID, a.id, a.epoch, false)
		if err != nil {
			return TaskConvergenceUsage{}, err
		}
		out.ModelDecisions += episode.Decisions
		out.WorkerToolCalls += episode.WorkerToolCalls
		out.ModelTotalTokens += episode.ModelTotalTokens
		if a.terminated != nil {
			sig, err := s.attemptProgressSignature(ctx, taskID, a.epoch)
			if err != nil {
				return TaskConvergenceUsage{}, err
			}
			signatures = append(signatures, sig)
		}
	}
	if len(signatures) > 1 {
		last := signatures[len(signatures)-1]
		for i := len(signatures) - 2; i >= 0 && signatures[i] == last; i-- {
			out.NoProgressStreak++
		}
	}
	return out, nil
}

func (s *SQLite) attemptProgressSignature(ctx context.Context, taskID string, epoch int64) (string, error) {
	var revision string
	err := s.db.QueryRowContext(ctx, `
SELECT current_revision FROM semantic_checkpoints
WHERE task_id=? AND run_epoch=? ORDER BY version DESC LIMIT 1`, taskID, epoch).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "none", nil
	}
	if err != nil {
		return "", err
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return "none", nil
	}
	return revision, nil
}
