package effects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"mar/internal/domain"
	"mar/internal/store"
)

type Decision string

const (
	DecisionDispatch           Decision = "DISPATCH"
	DecisionReconcile          Decision = "RECONCILE"
	DecisionAlreadyApplied     Decision = "ALREADY_APPLIED"
	DecisionObservedNotApplied Decision = "OBSERVED_NOT_APPLIED"
)

type Manager struct {
	store *store.SQLite
	now   func() time.Time
}

func New(s *store.SQLite) *Manager { return &Manager{store: s, now: time.Now} }

// Plan is idempotent on operation_id + immutable intent hash.
func (m *Manager) Plan(ctx context.Context, intent domain.EffectIntent) (domain.EffectRecord, Decision, error) {
	if err := intent.Validate(); err != nil {
		return domain.EffectRecord{}, "", err
	}
	hash, err := intent.Hash()
	if err != nil {
		return domain.EffectRecord{}, "", err
	}
	now := m.now().UTC()
	record := domain.EffectRecord{Intent: intent, IntentHash: hash, State: domain.EffectPrepared, CreatedAt: now, UpdatedAt: now}
	existing, _, err := m.store.PrepareEffect(ctx, record)
	if err != nil {
		return domain.EffectRecord{}, "", err
	}
	return classify(existing)
}

// AuthorizeDispatch durably marks DISPATCHED before caller starts the physical
// side effect. If this succeeds and the process crashes, retry MUST reconcile.
func (m *Manager) AuthorizeDispatch(ctx context.Context, operationID, taskID, attemptID string, epoch int64) (domain.EffectRecord, error) {
	return m.store.MarkEffectDispatched(ctx, operationID, taskID, attemptID, epoch, m.now().UTC())
}

func (m *Manager) ObserveApplied(ctx context.Context, operationID string, result json.RawMessage) (domain.EffectRecord, error) {
	return m.store.ObserveEffect(ctx, operationID, domain.OutcomeApplied, result, m.now().UTC())
}

func (m *Manager) ObserveNotApplied(ctx context.Context, operationID string, result json.RawMessage) (domain.EffectRecord, error) {
	return m.store.ObserveEffect(ctx, operationID, domain.OutcomeNotApplied, result, m.now().UTC())
}

// RearmAfterNotApplied is explicit reconciliation, not an automatic retry.
func (m *Manager) RearmAfterNotApplied(ctx context.Context, operationID string) (domain.EffectRecord, error) {
	return m.store.RearmNotApplied(ctx, operationID, m.now().UTC())
}

func classify(record domain.EffectRecord) (domain.EffectRecord, Decision, error) {
	switch record.State {
	case domain.EffectPrepared:
		return record, DecisionDispatch, nil
	case domain.EffectDispatched:
		return record, DecisionReconcile, store.ErrEffectReconcile
	case domain.EffectObserved:
		switch record.ObservationOutcome {
		case domain.OutcomeApplied:
			return record, DecisionAlreadyApplied, nil
		case domain.OutcomeNotApplied:
			return record, DecisionObservedNotApplied, nil
		default:
			return domain.EffectRecord{}, "", errors.New("observed effect has invalid outcome")
		}
	default:
		return domain.EffectRecord{}, "", fmt.Errorf("unknown effect state %q", record.State)
	}
}
