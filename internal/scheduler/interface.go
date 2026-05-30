package scheduler

import (
	"context"
	"time"

	"golang-project.local/internal/buffer"
)

// Collector defines the interface for metric collectors
type Collector interface {
	Name() string
	Collect(ctx context.Context) (buffer.Metric, error)
	Enabled() bool
}

// Scheduler manages periodic collection of metrics
type Scheduler interface {
	// Start begins the periodic collection with given interval
	Start(ctx context.Context, interval time.Duration) error

	// Stop gracefully stops the scheduler
	Stop() error

	// GetBuffer returns the underlying metric buffer
	GetBuffer() buffer.MetricBuffer

	// GetStats returns scheduler statistics
	GetStats() Stats
}

// Stats contains scheduler performance statistics
type Stats struct {
	TotalCollections          int64
	SuccessfulCollections     int64
	FailedCollections         int64
	LastCollectionTime        time.Time
	AverageCollectionDuration time.Duration
}
