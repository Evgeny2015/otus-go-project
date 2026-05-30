package scheduler

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"golang-project.local/internal/buffer"
)

// DefaultBackoffBase is the base duration for exponential backoff on retry.
const DefaultBackoffBase = 100 * time.Millisecond

// DefaultBackoffMax is the maximum backoff duration.
const DefaultBackoffMax = 10 * time.Second

// DefaultMaxRetries is the maximum number of consecutive retries before
// a collector is temporarily skipped.
const DefaultMaxRetries = 3

// CollectorScheduler manages periodic collection of metrics from multiple
// collectors into a single thread-safe buffer. It provides:
//   - 1-second ticker-based periodic collection
//   - Concurrent collection from all enabled collectors
//   - Exponential backoff retry logic for failed collections
//   - Statistics tracking (success/failure counts, durations)
//   - Graceful shutdown via context cancellation
type CollectorScheduler struct {
	collectors []Collector
	buffer     buffer.MetricBuffer
	interval   time.Duration

	// Retry configuration
	backoffBase time.Duration
	backoffMax  time.Duration
	maxRetries  int

	// Statistics
	totalCollections        atomic.Int64
	successfulCollections   atomic.Int64
	failedCollections       atomic.Int64
	lastCollectionTime      atomic.Value // stores time.Time
	totalCollectionDuration atomic.Int64 // nanoseconds
	collectionCountForAvg   atomic.Int64

	// Concurrency control
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// CollectorSchedulerOption configures a CollectorScheduler.
type CollectorSchedulerOption func(*CollectorScheduler)

// WithBackoffBase sets the base backoff duration for retry logic.
func WithBackoffBase(d time.Duration) CollectorSchedulerOption {
	return func(cs *CollectorScheduler) {
		cs.backoffBase = d
	}
}

// WithBackoffMax sets the maximum backoff duration.
func WithBackoffMax(d time.Duration) CollectorSchedulerOption {
	return func(cs *CollectorScheduler) {
		cs.backoffMax = d
	}
}

// WithMaxRetries sets the maximum number of consecutive retries.
func WithMaxRetries(n int) CollectorSchedulerOption {
	return func(cs *CollectorScheduler) {
		cs.maxRetries = n
	}
}

// NewCollectorScheduler creates a new CollectorScheduler that collects
// metrics from the given collectors and stores them in the provided buffer.
//
// The interval parameter controls how often collection runs. All enabled
// collectors are invoked concurrently on each tick.
func NewCollectorScheduler(
	collectors []Collector,
	buf buffer.MetricBuffer,
	interval time.Duration,
	opts ...CollectorSchedulerOption,
) *CollectorScheduler {
	cs := &CollectorScheduler{
		collectors:  collectors,
		buffer:      buf,
		interval:    interval,
		backoffBase: DefaultBackoffBase,
		backoffMax:  DefaultBackoffMax,
		maxRetries:  DefaultMaxRetries,
		done:        make(chan struct{}),
	}
	cs.lastCollectionTime.Store(time.Time{})
	for _, opt := range opts {
		opt(cs)
	}
	return cs
}

// Start begins periodic collection. It blocks until the context is cancelled.
// Each tick triggers concurrent collection from all enabled collectors.
func (cs *CollectorScheduler) Start(ctx context.Context, interval time.Duration) error {
	if interval > 0 {
		cs.interval = interval
	}

	ticker := time.NewTicker(cs.interval)
	defer ticker.Stop()

	log.Printf("CollectorScheduler started: interval=%s, collectors=%d",
		cs.interval, len(cs.collectors))

	for {
		select {
		case <-ctx.Done():
			log.Printf("CollectorScheduler stopped: %v", ctx.Err())
			return ctx.Err()

		case <-ticker.C:
			cs.collectTick(ctx)
		}
	}
}

// collectTick runs one round of collection across all enabled collectors concurrently.
func (cs *CollectorScheduler) collectTick(ctx context.Context) {
	cs.totalCollections.Add(1)
	cs.lastCollectionTime.Store(time.Now())

	var wg sync.WaitGroup

	for _, c := range cs.collectors {
		if !c.Enabled() {
			continue
		}
		wg.Add(1)
		go func(col Collector) {
			defer wg.Done()
			cs.collectWithRetry(ctx, col)
		}(c)
	}

	wg.Wait()
}

// collectWithRetry attempts to collect a metric from the given collector,
// retrying with exponential backoff on failure up to maxRetries times.
func (cs *CollectorScheduler) collectWithRetry(ctx context.Context, col Collector) {
	var lastErr error

	for attempt := 0; attempt <= cs.maxRetries; attempt++ {
		// Check context before each attempt
		select {
		case <-ctx.Done():
			return
		default:
		}

		start := time.Now()
		metric, err := col.Collect(ctx)
		duration := time.Since(start)

		// Track duration for average calculation
		cs.totalCollectionDuration.Add(duration.Nanoseconds())
		cs.collectionCountForAvg.Add(1)

		if err == nil {
			cs.successfulCollections.Add(1)
			if err := cs.buffer.Add(metric, time.Now()); err != nil {
				log.Printf("CollectorScheduler: failed to add %s metric to buffer: %v",
					col.Name(), err)
			}
			return
		}

		lastErr = err

		if attempt < cs.maxRetries {
			// Exponential backoff: base * 2^attempt, capped at maxBackoff
			backoff := cs.backoffBase * (1 << attempt)
			if backoff > cs.backoffMax {
				backoff = cs.backoffMax
			}

			log.Printf("CollectorScheduler: %s collection failed (attempt %d/%d): %v; retrying in %v",
				col.Name(), attempt+1, cs.maxRetries, err, backoff)

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}
	}

	// All retries exhausted
	cs.failedCollections.Add(1)
	log.Printf("CollectorScheduler: %s collection failed after %d retries: %v",
		col.Name(), cs.maxRetries, lastErr)
}

// Stop gracefully stops the scheduler. It waits for any in-flight collection
// to complete before returning.
func (cs *CollectorScheduler) Stop() error {
	var err error
	cs.stopOnce.Do(func() {
		close(cs.done)
		cs.wg.Wait()
		log.Printf("CollectorScheduler stopped: total=%d, success=%d, failed=%d",
			cs.totalCollections.Load(), cs.successfulCollections.Load(), cs.failedCollections.Load())
	})
	return err
}

// GetBuffer returns the underlying metric buffer.
func (cs *CollectorScheduler) GetBuffer() buffer.MetricBuffer {
	return cs.buffer
}

// GetStats returns scheduler performance statistics.
func (cs *CollectorScheduler) GetStats() Stats {
	total := cs.totalCollections.Load()
	success := cs.successfulCollections.Load()
	failed := cs.failedCollections.Load()
	lastTime, _ := cs.lastCollectionTime.Load().(time.Time)

	var avgDuration time.Duration
	if count := cs.collectionCountForAvg.Load(); count > 0 {
		totalNS := cs.totalCollectionDuration.Load()
		avgDuration = time.Duration(totalNS / count)
	}

	return Stats{
		TotalCollections:          total,
		SuccessfulCollections:     success,
		FailedCollections:         failed,
		LastCollectionTime:        lastTime,
		AverageCollectionDuration: avgDuration,
	}
}
