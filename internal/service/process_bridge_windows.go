//go:build windows

package service

import (
	"context"
	"errors"

	"mar/internal/processctl"
)

// ConfirmAttemptProcessTermination is the production path from physical OS
// containment to durable attempt state. A zero/forged proof is rejected.
func (s *TaskService) ConfirmAttemptProcessTermination(ctx context.Context, proof processctl.TerminationProof, terminalStatus string) error {
	if !proof.Valid() {
		return errors.New("invalid physical termination proof")
	}
	ref := proof.Attempt()
	return s.store.ConfirmAttemptTerminated(ctx, ref.TaskID, ref.AttemptID, ref.RunEpoch, terminalStatus, proof.ConfirmedAt())
}
