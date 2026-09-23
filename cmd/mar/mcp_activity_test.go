package main

import (
	"testing"
	"time"

	"mar/internal/mcpedge"
)

func TestMCPActivityBufferIsBoundedAndCountsDroppedEvents(t *testing.T) {
	buffer := newMCPActivityBuffer(2)
	for i := 1; i <= 3; i++ {
		buffer.Observe(mcpedge.ToolCallEvent{
			CallID: uint64(i), At: time.Unix(int64(i), 0).UTC(), Phase: "complete",
			Tool: "project", Operation: "context", ProjectID: "mar", Outcome: "ok",
		})
	}
	events, dropped := buffer.Snapshot()
	if len(events) != 2 || dropped != 1 {
		t.Fatalf("bounded activity buffer mismatch: len=%d dropped=%d events=%+v", len(events), dropped, events)
	}
	if events[0].CallID != 2 || events[1].CallID != 3 {
		t.Fatalf("activity buffer did not retain newest events: %+v", events)
	}
}
