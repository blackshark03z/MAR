//go:build windows

package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"mar/internal/contextengine"
)

type fakePressureContextBuilder struct {
	builds int
}

func (b *fakePressureContextBuilder) Build(context.Context, contextengine.Request) (contextengine.Pack, error) {
	b.builds++
	return contextengine.Pack{Revision: "pressure"}, nil
}

func TestPressureAwareContextBuilderEvictsBeforeBuild(t *testing.T) {
	inner := &fakePressureContextBuilder{}
	evictions := 0
	builder := &pressureAwareContextBuilder{
		inner: inner,
		evict: func() int {
			evictions++
			return 1
		},
		memoryLoad: func() (float64, error) { return 95, nil },
		threshold:  80,
	}
	if _, err := builder.Build(context.Background(), contextengine.Request{}); err != nil {
		t.Fatal(err)
	}
	if evictions != 1 || inner.builds != 1 {
		t.Fatalf("pressure eviction/build ordering mismatch: evictions=%d builds=%d", evictions, inner.builds)
	}
}

func TestPressureAwareContextBuilderKeepsCacheBelowThreshold(t *testing.T) {
	inner := &fakePressureContextBuilder{}
	evictions := 0
	builder := &pressureAwareContextBuilder{
		inner:      inner,
		evict:      func() int { evictions++; return 1 },
		memoryLoad: func() (float64, error) { return 40, nil },
		threshold:  80,
	}
	if _, err := builder.Build(context.Background(), contextengine.Request{}); err != nil {
		t.Fatal(err)
	}
	if evictions != 0 || inner.builds != 1 {
		t.Fatalf("cache evicted without pressure: evictions=%d builds=%d", evictions, inner.builds)
	}
}

func TestMemoryPressureMonitorEvictsAfterInitialContextBuild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var evictions atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitorMemoryPressure(ctx, 5*time.Millisecond, 80, func() (float64, error) { return 95, nil }, func() int {
			evictions.Add(1)
			return 1
		})
	}()
	deadline := time.Now().Add(time.Second)
	for evictions.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if evictions.Load() == 0 {
		t.Fatal("live memory pressure did not evict populated optional cache")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("memory-pressure monitor did not stop with worker context")
	}
}
