package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mar/internal/domain"
)

const maxObservationArtifactReadBytes = 16 << 10

func (s *SQLite) PutObservationArtifact(ctx context.Context, artifact domain.ObservationArtifact, content []byte) (domain.ObservationArtifact, error) {
	if s == nil || s.db == nil || !validObservationArtifactHandle(artifact.Handle) {
		return domain.ObservationArtifact{}, errors.New("invalid observation artifact store or handle")
	}
	if artifact.TaskID == "" || artifact.AttemptID == "" || artifact.RunEpoch <= 0 || artifact.ToolCallID == "" || artifact.Kind == "" {
		return domain.ObservationArtifact{}, errors.New("observation artifact identity is incomplete")
	}
	if artifact.CapturedBytes != int64(len(content)) || artifact.SourceBytes < artifact.CapturedBytes || artifact.Truncated == artifact.Complete {
		return domain.ObservationArtifact{}, errors.New("observation artifact size/completeness metadata is inconsistent")
	}
	sum := sha256.Sum256(content)
	if !strings.EqualFold(artifact.SHA256, hex.EncodeToString(sum[:])) {
		return domain.ObservationArtifact{}, errors.New("observation artifact hash does not match content")
	}
	if err := s.ValidateAttemptAuthority(ctx, artifact.TaskID, artifact.AttemptID, artifact.RunEpoch); err != nil {
		return domain.ObservationArtifact{}, err
	}
	if existing, err := s.GetObservationArtifact(ctx, artifact.Handle); err == nil {
		if observationArtifactEqual(existing, artifact) {
			return existing, nil
		}
		return domain.ObservationArtifact{}, errors.New("observation artifact handle already indexes different evidence")
	} else if !errors.Is(err, ErrNotFound) {
		return domain.ObservationArtifact{}, err
	}
	if err := os.MkdirAll(s.observationArtifactRoot, 0o700); err != nil {
		return domain.ObservationArtifact{}, fmt.Errorf("create observation artifact root: %w", err)
	}
	finalPath := filepath.Join(s.observationArtifactRoot, artifact.Handle+".bin")
	tmp, err := os.CreateTemp(s.observationArtifactRoot, ".observation-*")
	if err != nil {
		return domain.ObservationArtifact{}, err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return domain.ObservationArtifact{}, err
	}
	if _, err := tmp.Write(content); err != nil {
		return domain.ObservationArtifact{}, err
	}
	if err := tmp.Sync(); err != nil {
		return domain.ObservationArtifact{}, err
	}
	if err := tmp.Close(); err != nil {
		return domain.ObservationArtifact{}, err
	}
	// Recheck authority after filesystem I/O and immediately before publishing
	// the durable index. A stale worker may leave at most an unindexed temp file.
	if err := s.ValidateAttemptAuthority(ctx, artifact.TaskID, artifact.AttemptID, artifact.RunEpoch); err != nil {
		return domain.ObservationArtifact{}, err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return domain.ObservationArtifact{}, fmt.Errorf("publish observation artifact content: %w", err)
	}
	committed = true
	_, err = s.db.ExecContext(ctx, `
INSERT INTO observation_artifacts(handle, task_id, attempt_id, run_epoch, tool_call_id, kind, content_sha256, captured_bytes, source_bytes, complete, truncated, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, artifact.Handle, artifact.TaskID, artifact.AttemptID, artifact.RunEpoch, artifact.ToolCallID, artifact.Kind, artifact.SHA256, artifact.CapturedBytes, artifact.SourceBytes, boolInt(artifact.Complete), boolInt(artifact.Truncated), artifact.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		_ = os.Remove(finalPath)
		return domain.ObservationArtifact{}, err
	}
	return artifact, nil
}

func (s *SQLite) GetObservationArtifact(ctx context.Context, handle string) (domain.ObservationArtifact, error) {
	if !validObservationArtifactHandle(handle) {
		return domain.ObservationArtifact{}, ErrNotFound
	}
	var a domain.ObservationArtifact
	var complete, truncated int
	var created string
	err := s.db.QueryRowContext(ctx, `
SELECT handle, task_id, attempt_id, run_epoch, tool_call_id, kind, content_sha256, captured_bytes, source_bytes, complete, truncated, created_at
FROM observation_artifacts WHERE handle = ?`, handle).Scan(&a.Handle, &a.TaskID, &a.AttemptID, &a.RunEpoch, &a.ToolCallID, &a.Kind, &a.SHA256, &a.CapturedBytes, &a.SourceBytes, &complete, &truncated, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ObservationArtifact{}, ErrNotFound
	}
	if err != nil {
		return domain.ObservationArtifact{}, err
	}
	a.Complete, a.Truncated = complete != 0, truncated != 0
	a.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.ObservationArtifact{}, err
	}
	return a, nil
}

func (s *SQLite) ListObservationArtifacts(ctx context.Context, taskID, attemptID string, epoch int64, limit int) ([]domain.ObservationArtifact, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(attemptID) == "" || epoch <= 0 || limit <= 0 || limit > 32 {
		return nil, errors.New("observation artifact list identity or bound is invalid")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT handle, task_id, attempt_id, run_epoch, tool_call_id, kind, content_sha256, captured_bytes, source_bytes, complete, truncated, created_at
FROM observation_artifacts
WHERE task_id = ? AND attempt_id = ? AND run_epoch = ?
ORDER BY created_at DESC, handle DESC
LIMIT ?`, taskID, attemptID, epoch, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ObservationArtifact, 0, limit)
	for rows.Next() {
		var a domain.ObservationArtifact
		var complete, truncated int
		var created string
		if err := rows.Scan(&a.Handle, &a.TaskID, &a.AttemptID, &a.RunEpoch, &a.ToolCallID, &a.Kind, &a.SHA256, &a.CapturedBytes, &a.SourceBytes, &complete, &truncated, &created); err != nil {
			return nil, err
		}
		a.Complete, a.Truncated = complete != 0, truncated != 0
		a.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLite) ReadObservationArtifact(ctx context.Context, taskID, attemptID string, epoch int64, handle string, offset int64, maxBytes int) (domain.ObservationArtifactChunk, error) {
	if offset < 0 || maxBytes <= 0 || maxBytes > maxObservationArtifactReadBytes {
		return domain.ObservationArtifactChunk{}, errors.New("observation artifact read is outside bounded range")
	}
	a, err := s.GetObservationArtifact(ctx, handle)
	if err != nil {
		return domain.ObservationArtifactChunk{}, err
	}
	if a.TaskID != taskID || a.AttemptID != attemptID || a.RunEpoch != epoch {
		return domain.ObservationArtifactChunk{}, ErrStaleAttempt
	}
	path := filepath.Join(s.observationArtifactRoot, a.Handle+".bin")
	f, err := os.Open(path)
	if err != nil {
		return domain.ObservationArtifactChunk{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return domain.ObservationArtifactChunk{}, err
	}
	if n != a.CapturedBytes || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.SHA256) {
		return domain.ObservationArtifactChunk{}, errors.New("observation artifact content failed integrity verification")
	}
	if offset > a.CapturedBytes {
		return domain.ObservationArtifactChunk{}, errors.New("observation artifact offset exceeds content size")
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return domain.ObservationArtifactChunk{}, err
	}
	buf := make([]byte, min(maxBytes, int(a.CapturedBytes-offset)))
	nRead, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return domain.ObservationArtifactChunk{}, err
	}
	buf = buf[:nRead]
	next := offset + int64(nRead)
	return domain.ObservationArtifactChunk{Artifact: a, Offset: offset, Data: string(buf), NextOffset: next, EOF: next >= a.CapturedBytes}, nil
}

func validObservationArtifactHandle(handle string) bool {
	if !strings.HasPrefix(handle, "obs-") || len(handle) != 68 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(handle, "obs-"))
	return err == nil
}

func observationArtifactEqual(a, b domain.ObservationArtifact) bool {
	return a.Handle == b.Handle && a.TaskID == b.TaskID && a.AttemptID == b.AttemptID && a.RunEpoch == b.RunEpoch && a.ToolCallID == b.ToolCallID && a.Kind == b.Kind && strings.EqualFold(a.SHA256, b.SHA256) && a.CapturedBytes == b.CapturedBytes && a.SourceBytes == b.SourceBytes && a.Complete == b.Complete && a.Truncated == b.Truncated
}
