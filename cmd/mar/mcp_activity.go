package main

import (
	"sync"
	"time"

	"mar/internal/mcpedge"
)

const mcpRecentOperationLimit = 256

type mcpRecentOperation struct {
	CallID     uint64    `json:"call_id"`
	At         time.Time `json:"at"`
	Phase      string    `json:"phase"`
	Tool       string    `json:"tool"`
	Operation  string    `json:"operation,omitempty"`
	ProjectID  string    `json:"project_id,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Outcome    string    `json:"outcome,omitempty"`
}

type mcpActivityBuffer struct {
	mu      sync.Mutex
	limit   int
	events  []mcpRecentOperation
	dropped int64
}

func newMCPActivityBuffer(limit int) *mcpActivityBuffer {
	if limit <= 0 {
		limit = mcpRecentOperationLimit
	}
	return &mcpActivityBuffer{limit: limit, events: make([]mcpRecentOperation, 0, limit)}
}

func (b *mcpActivityBuffer) Observe(event mcpedge.ToolCallEvent) {
	if b == nil {
		return
	}
	item := mcpRecentOperation{
		CallID: event.CallID, At: event.At.UTC(), Phase: event.Phase, Tool: event.Tool,
		Operation: event.Operation, ProjectID: event.ProjectID, DurationMS: event.DurationMS, Outcome: event.Outcome,
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) >= b.limit {
		copy(b.events, b.events[1:])
		b.events[len(b.events)-1] = item
		b.dropped++
		return
	}
	b.events = append(b.events, item)
}

func (b *mcpActivityBuffer) Snapshot() ([]mcpRecentOperation, int64) {
	if b == nil {
		return nil, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := append([]mcpRecentOperation(nil), b.events...)
	return out, b.dropped
}
