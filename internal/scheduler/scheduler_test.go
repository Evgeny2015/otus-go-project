package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang-project.local/internal/buffer"
	"golang-project.local/internal/types"
	pb "golang-project.local/proto"
)

// ---------------------------------------------------------------------------
// Mock Collector
// ---------------------------------------------------------------------------

// mockCollector implements the Collector interface for testing.
type mockCollector struct {
	name    string
	enabled bool
	// Collect behaviour
	collectFn func(ctx context.Context) (types.Metric, error)
	// Call tracking
	collectCalls atomic.Int64
}

func newMockCollector(name string, enabled bool) *mockCollector {
	return &mockCollector{
		name:    name,
		enabled: enabled,
		collectFn: func(ctx context.Context) (types.Metric, error) {
			return types.NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5}), nil
		},
	}
}

func (m *mockCollector) Name() string { return m.name }

func (m *mockCollector) Enabled() bool { return m.enabled }

func (m *mockCollector) Collect(ctx context.Context) (types.Metric, error) {
	m.collectCalls.Add(1)
	return m.collectFn(ctx)
}

// ---------------------------------------------------------------------------
// Tests: NewCollectorScheduler
// ---------------------------------------------------------------------------

func TestNewCollectorScheduler_Defaults(t *testing.T) {
	cb := buffer.NewCircularBuffer(10)
	cs := NewCollectorScheduler(nil, cb, time.Second)

	if cs == nil {
		t.Fatal("NewCollectorScheduler returned nil")
	}
	if cs.backoffBase != DefaultBackoffBase {
		t.Errorf("backoffBase = %v, want %v", cs.backoffBase, DefaultBackoffBase)
	}
	if cs.backoffMax != DefaultBackoffMax {
		t.Errorf("backoffMax = %v, want %v", cs.backoffMax, DefaultBackoffMax)
	}
	if cs.maxRetries != DefaultMaxRetries {
		t.Errorf("maxRetries = %d, want %d", cs.maxRetries, DefaultMaxRetries)
	}
	if cs.interval != time.Second {
		t.Errorf("interval = %v, want %v", cs.interval, time.Second)
	}
	if cs.buffer != cb {
		t.Error("buffer not set correctly")
	}
}

func TestNewCollectorScheduler_WithOptions(t *testing.T) {
	cb := buffer.NewCircularBuffer(10)
	cs := NewCollectorScheduler(
		nil, cb, time.Second,
		WithBackoffBase(200*time.Millisecond),
		WithBackoffMax(5*time.Second),
		WithMaxRetries(5),
	)

	if cs.backoffBase != 200*time.Millisecond {
		t.Errorf("backoffBase = %v, want 200ms", cs.backoffBase)
	}
	if cs.backoffMax != 5*time.Second {
		t.Errorf("backoffMax = %v, want 5s", cs.backoffMax)
	}
	if cs.maxRetries != 5 {
		t.Errorf("maxRetries = %d, want 5", cs.maxRetries)
	}
}

func TestNewCollectorScheduler_NilCollectors(t *testing.T) {
	cb := buffer.NewCircularBuffer(10)
	cs := NewCollectorScheduler(nil, cb, time.Second)
	if cs.collectors == nil {
		// nil slice is fine, collectTick will just iterate zero times
	}
}

// ---------------------------------------------------------------------------
// Tests: GetBuffer
// ---------------------------------------------------------------------------

func TestGetBuffer(t *testing.T) {
	cb := buffer.NewCircularBuffer(10)
	cs := NewCollectorScheduler(nil, cb, time.Second)
	if got := cs.GetBuffer(); got != cb {
		t.Error("GetBuffer returned wrong buffer")
	}
}

// ---------------------------------------------------------------------------
// Tests: collectTick
// ---------------------------------------------------------------------------

func TestCollectTick_AllEnabled(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	mc1 := newMockCollector("load", true)
	mc2 := newMockCollector("cpu", true)

	cs := NewCollectorScheduler(
		[]Collector{mc1, mc2},
		cb,
		time.Second,
	)

	cs.collectTick(context.Background())

	if mc1.collectCalls.Load() != 1 {
		t.Errorf("collector 1 called %d times, want 1", mc1.collectCalls.Load())
	}
	if mc2.collectCalls.Load() != 1 {
		t.Errorf("collector 2 called %d times, want 1", mc2.collectCalls.Load())
	}
	if cb.Size() != 2 {
		t.Errorf("buffer size = %d, want 2", cb.Size())
	}
}

func TestCollectTick_DisabledCollector(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	mc1 := newMockCollector("load", true)
	mc2 := newMockCollector("cpu", false) // disabled

	cs := NewCollectorScheduler(
		[]Collector{mc1, mc2},
		cb,
		time.Second,
	)

	cs.collectTick(context.Background())

	if mc1.collectCalls.Load() != 1 {
		t.Errorf("enabled collector called %d times, want 1", mc1.collectCalls.Load())
	}
	if mc2.collectCalls.Load() != 0 {
		t.Errorf("disabled collector called %d times, want 0", mc2.collectCalls.Load())
	}
	if cb.Size() != 1 {
		t.Errorf("buffer size = %d, want 1", cb.Size())
	}
}

func TestCollectTick_AllDisabled(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	mc1 := newMockCollector("load", false)
	mc2 := newMockCollector("cpu", false)

	cs := NewCollectorScheduler(
		[]Collector{mc1, mc2},
		cb,
		time.Second,
	)

	cs.collectTick(context.Background())

	if mc1.collectCalls.Load() != 0 {
		t.Error("disabled collector should not be called")
	}
	if mc2.collectCalls.Load() != 0 {
		t.Error("disabled collector should not be called")
	}
	if cb.Size() != 0 {
		t.Errorf("buffer size = %d, want 0", cb.Size())
	}
}

func TestCollectTick_EmptyCollectors(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	cs := NewCollectorScheduler([]Collector{}, cb, time.Second)

	// Should not panic
	cs.collectTick(context.Background())

	if cb.Size() != 0 {
		t.Errorf("buffer size = %d, want 0", cb.Size())
	}
}

func TestCollectTick_NilCollectors(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	cs := NewCollectorScheduler(nil, cb, time.Second)

	// Should not panic
	cs.collectTick(context.Background())
}

// ---------------------------------------------------------------------------
// Tests: collectWithRetry
// ---------------------------------------------------------------------------

func TestCollectWithRetry_Success(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	mc := newMockCollector("load", true)
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		return types.NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5}), nil
	}

	cs := NewCollectorScheduler(
		[]Collector{mc},
		cb,
		time.Second,
	)

	cs.collectWithRetry(context.Background(), mc)

	if cb.Size() != 1 {
		t.Errorf("buffer size = %d, want 1", cb.Size())
	}
	if cs.successfulCollections.Load() != 1 {
		t.Errorf("successfulCollections = %d, want 1", cs.successfulCollections.Load())
	}
	if cs.failedCollections.Load() != 0 {
		t.Errorf("failedCollections = %d, want 0", cs.failedCollections.Load())
	}
}

func TestCollectWithRetry_RetryThenSuccess(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	var attempt atomic.Int64
	mc := newMockCollector("load", true)
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		attempt.Add(1)
		if attempt.Load() <= 2 {
			return nil, errors.New("transient error")
		}
		return types.NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5}), nil
	}

	cs := NewCollectorScheduler(
		[]Collector{mc},
		cb,
		time.Second,
		WithBackoffBase(time.Millisecond),
		WithBackoffMax(10*time.Millisecond),
		WithMaxRetries(3),
	)

	cs.collectWithRetry(context.Background(), mc)

	if cb.Size() != 1 {
		t.Errorf("buffer size = %d, want 1", cb.Size())
	}
	if cs.successfulCollections.Load() != 1 {
		t.Errorf("successfulCollections = %d, want 1", cs.successfulCollections.Load())
	}
	if cs.failedCollections.Load() != 0 {
		t.Errorf("failedCollections = %d, want 0", cs.failedCollections.Load())
	}
}

func TestCollectWithRetry_AllRetriesExhausted(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	mc := newMockCollector("load", true)
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		return nil, errors.New("permanent error")
	}

	cs := NewCollectorScheduler(
		[]Collector{mc},
		cb,
		time.Second,
		WithBackoffBase(time.Millisecond),
		WithBackoffMax(10*time.Millisecond),
		WithMaxRetries(2),
	)

	cs.collectWithRetry(context.Background(), mc)

	if cb.Size() != 0 {
		t.Errorf("buffer size = %d, want 0", cb.Size())
	}
	if cs.successfulCollections.Load() != 0 {
		t.Errorf("successfulCollections = %d, want 0", cs.successfulCollections.Load())
	}
	if cs.failedCollections.Load() != 1 {
		t.Errorf("failedCollections = %d, want 1", cs.failedCollections.Load())
	}
}

func TestCollectWithRetry_ContextCancelled(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	mc := newMockCollector("load", true)
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		return nil, errors.New("error")
	}

	cs := NewCollectorScheduler(
		[]Collector{mc},
		cb,
		time.Second,
		WithBackoffBase(time.Hour), // long backoff to ensure cancellation wins
		WithMaxRetries(3),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cs.collectWithRetry(ctx, mc)

	// Should return early without adding to buffer or incrementing failed
	if cb.Size() != 0 {
		t.Errorf("buffer size = %d, want 0", cb.Size())
	}
}

// ---------------------------------------------------------------------------
// Tests: GetStats
// ---------------------------------------------------------------------------

func TestGetStats_Initial(t *testing.T) {
	cb := buffer.NewCircularBuffer(10)
	cs := NewCollectorScheduler(nil, cb, time.Second)

	stats := cs.GetStats()

	if stats.TotalCollections != 0 {
		t.Errorf("TotalCollections = %d, want 0", stats.TotalCollections)
	}
	if stats.SuccessfulCollections != 0 {
		t.Errorf("SuccessfulCollections = %d, want 0", stats.SuccessfulCollections)
	}
	if stats.FailedCollections != 0 {
		t.Errorf("FailedCollections = %d, want 0", stats.FailedCollections)
	}
	if !stats.LastCollectionTime.IsZero() {
		t.Error("LastCollectionTime should be zero")
	}
	if stats.AverageCollectionDuration != 0 {
		t.Errorf("AverageCollectionDuration = %v, want 0", stats.AverageCollectionDuration)
	}
}

func TestGetStats_AfterCollection(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	mc := newMockCollector("load", true)
	// Add a small delay so AverageCollectionDuration > 0
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		time.Sleep(time.Microsecond)
		return types.NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5}), nil
	}
	cs := NewCollectorScheduler(
		[]Collector{mc},
		cb,
		time.Second,
	)

	cs.collectTick(context.Background())
	stats := cs.GetStats()

	if stats.TotalCollections != 1 {
		t.Errorf("TotalCollections = %d, want 1", stats.TotalCollections)
	}
	if stats.SuccessfulCollections != 1 {
		t.Errorf("SuccessfulCollections = %d, want 1", stats.SuccessfulCollections)
	}
	if stats.FailedCollections != 0 {
		t.Errorf("FailedCollections = %d, want 0", stats.FailedCollections)
	}
	if stats.LastCollectionTime.IsZero() {
		t.Error("LastCollectionTime should not be zero")
	}
	if stats.AverageCollectionDuration <= 0 {
		t.Errorf("AverageCollectionDuration = %v, want > 0", stats.AverageCollectionDuration)
	}
}

func TestGetStats_AfterFailedCollection(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	mc := newMockCollector("load", true)
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		return nil, errors.New("error")
	}

	cs := NewCollectorScheduler(
		[]Collector{mc},
		cb,
		time.Second,
		WithBackoffBase(time.Millisecond),
		WithBackoffMax(5*time.Millisecond),
		WithMaxRetries(1),
	)

	cs.collectWithRetry(context.Background(), mc)
	stats := cs.GetStats()

	if stats.TotalCollections != 0 {
		// collectWithRetry does not increment totalCollections; collectTick does
	}
	if stats.FailedCollections != 1 {
		t.Errorf("FailedCollections = %d, want 1", stats.FailedCollections)
	}
}

// ---------------------------------------------------------------------------
// Tests: Start / Stop
// ---------------------------------------------------------------------------

func TestStart_ContextCancelled(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	mc := newMockCollector("load", true)
	cs := NewCollectorScheduler(
		[]Collector{mc},
		cb,
		10*time.Millisecond,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Start

	err := cs.Start(ctx, 0)
	if err == nil {
		t.Error("Start should return error when context is cancelled")
	}
}

func TestStart_WithIntervalOverride(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	mc := newMockCollector("load", true)
	cs := NewCollectorScheduler(
		[]Collector{mc},
		cb,
		time.Second,
	)

	ctx, cancel := context.WithCancel(context.Background())

	// Start with a different interval
	errCh := make(chan error, 1)
	go func() {
		errCh <- cs.Start(ctx, 10*time.Millisecond)
	}()

	// Let it tick a few times
	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-errCh
	if err != context.Canceled {
		t.Errorf("Start returned %v, want context.Canceled", err)
	}

	if mc.collectCalls.Load() == 0 {
		t.Error("collector should have been called at least once")
	}
}

func TestStop_Idempotent(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	cs := NewCollectorScheduler(nil, cb, time.Second)

	// Stop on a scheduler that was never started should not panic
	err := cs.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v, want nil", err)
	}

	// Second call should also succeed (idempotent via sync.Once)
	err = cs.Stop()
	if err != nil {
		t.Errorf("second Stop() error = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Backoff calculation
// ---------------------------------------------------------------------------

func TestBackoff_CappedAtMax(t *testing.T) {
	cb := buffer.NewCircularBuffer(100)
	mc := newMockCollector("load", true)
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		return nil, errors.New("error")
	}

	cs := NewCollectorScheduler(
		[]Collector{mc},
		cb,
		time.Second,
		WithBackoffBase(100*time.Millisecond),
		WithBackoffMax(150*time.Millisecond), // cap at 150ms
		WithMaxRetries(5),
	)

	// The backoff for attempt 3 would be 100ms * 2^3 = 800ms, but capped at 150ms
	// We just verify it doesn't hang (fast backoff due to cap)
	done := make(chan struct{})
	go func() {
		cs.collectWithRetry(context.Background(), mc)
		close(done)
	}()

	select {
	case <-done:
		// Success - completed quickly
	case <-time.After(2 * time.Second):
		t.Fatal("collectWithRetry timed out - backoff may not be capped")
	}
}

// ---------------------------------------------------------------------------
// Tests: Concurrent safety
// ---------------------------------------------------------------------------

func TestConcurrentCollectTick(t *testing.T) {
	cb := buffer.NewCircularBuffer(1000)
	mc := newMockCollector("load", true)

	cs := NewCollectorScheduler(
		[]Collector{mc},
		cb,
		time.Second,
	)

	// Run collectTick concurrently
	done := make(chan struct{})
	go func() {
		cs.collectTick(context.Background())
		close(done)
	}()

	// Read stats concurrently
	stats := cs.GetStats()
	_ = stats

	<-done

	if cb.Size() != 1 {
		t.Errorf("buffer size = %d, want 1", cb.Size())
	}
}
