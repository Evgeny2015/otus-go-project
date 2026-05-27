package buffer

import (
	"sync"
	"time"
)

// MetricEntry wraps a metric with its timestamp
type MetricEntry struct {
	Metric    Metric
	Timestamp time.Time
}

// CircularBuffer implements a thread-safe circular buffer for metrics
type CircularBuffer struct {
	mu       sync.RWMutex
	metrics  []MetricEntry
	capacity int
	head     int // index of newest element
	tail     int // index of oldest element
	size     int
}

// NewCircularBuffer creates a new circular buffer with given capacity
func NewCircularBuffer(capacity int) *CircularBuffer {
	if capacity <= 0 {
		capacity = 300 // default: 5 minutes at 1-second intervals
	}
	return &CircularBuffer{
		metrics:  make([]MetricEntry, capacity),
		capacity: capacity,
		head:     -1,
		tail:     0,
		size:     0,
	}
}

// Add adds a metric to the buffer
func (cb *CircularBuffer) Add(metric Metric, timestamp time.Time) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Move head pointer
	cb.head = (cb.head + 1) % cb.capacity

	// Store metric
	cb.metrics[cb.head] = MetricEntry{
		Metric:    metric,
		Timestamp: timestamp,
	}

	// Update size and tail pointer
	if cb.size < cb.capacity {
		cb.size++
	} else {
		// Buffer is full, move tail pointer
		cb.tail = (cb.tail + 1) % cb.capacity
	}

	return nil
}

// GetWindow returns metrics within the specified time range
func (cb *CircularBuffer) GetWindow(from, to time.Time) []Metric {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.size == 0 {
		return []Metric{}
	}

	var result []Metric

	// Iterate through buffer in chronological order
	for i := 0; i < cb.size; i++ {
		idx := (cb.tail + i) % cb.capacity
		entry := cb.metrics[idx]

		// Check if timestamp is within range
		if (entry.Timestamp.Equal(from) || entry.Timestamp.After(from)) &&
			(entry.Timestamp.Equal(to) || entry.Timestamp.Before(to)) {
			result = append(result, entry.Metric)
		}
	}

	return result
}

// GetLatest returns the most recent n metrics
func (cb *CircularBuffer) GetLatest(n int) []Metric {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if n <= 0 || cb.size == 0 {
		return []Metric{}
	}

	if n > cb.size {
		n = cb.size
	}

	result := make([]Metric, n)

	// Start from head and go backwards
	for i := 0; i < n; i++ {
		idx := (cb.head - i + cb.capacity) % cb.capacity
		result[i] = cb.metrics[idx].Metric
	}

	return result
}

// Cleanup removes metrics older than threshold
func (cb *CircularBuffer) Cleanup(threshold time.Time) int {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.size == 0 {
		return 0
	}

	removed := 0

	// Find first metric not older than threshold
	for cb.size > 0 {
		entry := cb.metrics[cb.tail]
		if entry.Timestamp.Before(threshold) {
			// Remove this entry by moving tail pointer
			cb.tail = (cb.tail + 1) % cb.capacity
			cb.size--
			removed++
		} else {
			break
		}
	}

	// If buffer becomes empty, reset head
	if cb.size == 0 {
		cb.head = -1
		cb.tail = 0
	}

	return removed
}

// Size returns current number of metrics
func (cb *CircularBuffer) Size() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.size
}

// Capacity returns buffer capacity
func (cb *CircularBuffer) Capacity() int {
	return cb.capacity
}

// Type returns the type of the buffer
func (cb *CircularBuffer) Type() string {
	return "circular_buffer"
}

// IsFull returns true if buffer is at capacity
func (cb *CircularBuffer) IsFull() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.size == cb.capacity
}

// Clear removes all metrics
func (cb *CircularBuffer) Clear() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.head = -1
	cb.tail = 0
	cb.size = 0
	// Clear references to help GC
	for i := range cb.metrics {
		cb.metrics[i] = MetricEntry{}
	}
}
