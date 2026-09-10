package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"mar/internal/domain"
)

const maxObservationArtifactBytes = 8 << 20

// PersistObservation durably records a tool observation before model-context
// bounding. The trusted service, not the LPAC worker, owns the filesystem
// artifact and the SQLite index. Repeating the same attempt/tool identity is
// idempotent only when the evidence metadata/content are identical.
func (s *TaskService) PersistObservation(ctx context.Context, taskID, attemptID string, epoch int64, toolCallID, kind, raw string, sourceBytes int64, sourceComplete bool) (domain.ObservationArtifact, error) {
	if s == nil || s.store == nil || strings.TrimSpace(toolCallID) == "" || strings.TrimSpace(kind) == "" {
		return domain.ObservationArtifact{}, errors.New("observation artifact service identity is incomplete")
	}
	if err := s.store.ValidateAttemptAuthority(ctx, taskID, attemptID, epoch); err != nil {
		return domain.ObservationArtifact{}, err
	}
	rawBytes := []byte(raw)
	if sourceBytes <= 0 {
		sourceBytes = int64(len(rawBytes))
	}
	if sourceBytes < int64(len(rawBytes)) {
		sourceBytes = int64(len(rawBytes))
	}
	content := rawBytes
	complete := sourceComplete
	if len(content) > maxObservationArtifactBytes {
		content = content[:maxObservationArtifactBytes]
		complete = false
	}
	truncated := !complete
	contentSum := sha256.Sum256(content)
	identitySum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s", taskID, attemptID, epoch, toolCallID, kind)))
	a := domain.ObservationArtifact{
		Handle:        "obs-" + hex.EncodeToString(identitySum[:]),
		TaskID:        taskID,
		AttemptID:     attemptID,
		RunEpoch:      epoch,
		ToolCallID:    toolCallID,
		Kind:          kind,
		SHA256:        hex.EncodeToString(contentSum[:]),
		CapturedBytes: int64(len(content)),
		SourceBytes:   sourceBytes,
		Complete:      complete,
		Truncated:     truncated,
		CreatedAt:     s.now().UTC(),
	}
	return s.store.PutObservationArtifact(ctx, a, content)
}

func (s *TaskService) ReadObservationArtifact(ctx context.Context, taskID, attemptID string, epoch int64, handle string, offset int64, maxBytes int) (domain.ObservationArtifactChunk, error) {
	if s == nil || s.store == nil || strings.TrimSpace(taskID) == "" || strings.TrimSpace(attemptID) == "" || epoch <= 0 {
		return domain.ObservationArtifactChunk{}, errors.New("observation artifact read identity is incomplete")
	}
	// Historical evidence remains inspectable after an attempt terminates, but
	// the caller must present the exact durable identity indexed by the handle.
	return s.store.ReadObservationArtifact(ctx, taskID, attemptID, epoch, handle, offset, maxBytes)
}
