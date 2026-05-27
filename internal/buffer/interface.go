package buffer

import (
	"time"
)

// Metric represents a generic metric with timestamp and value
type Metric interface {
	Timestamp() time.Time
	Value() interface{}
}

// MetricBuffer defines the interface for thread-safe circular buffer operations
type MetricBuffer interface {
	// Add metric with timestamp to the buffer
	Add(metric Metric, timestamp time.Time) error

	// GetWindow returns all metrics within the specified time range [from, to]
	GetWindow(from, to time.Time) []Metric

	// GetLatest returns the most recent n metrics
	GetLatest(n int) []Metric

	// Cleanup removes metrics older than the threshold time
	Cleanup(threshold time.Time) int

	// Size returns current number of metrics in buffer
	Size() int

	// Capacity returns maximum capacity of buffer
	Capacity() int

	// IsFull returns true if buffer is at capacity
	IsFull() bool

	// Clear removes all metrics from buffer
	Clear()
}
