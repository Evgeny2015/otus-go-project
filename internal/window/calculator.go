package window

import (
	"time"

	"golang-project.local/internal/buffer"
)

// CalculateWindow is a convenience function that creates a temporary
// SlidingWindow calculator and computes aggregated metrics over the
// specified window size.
//
// This is useful for one-off calculations. For repeated calculations,
// create a SlidingWindow instance with NewSlidingWindow() and reuse it.
//
// Parameters:
//   - buf: the metric buffer to read from (must not be nil)
//   - windowSize: the duration of the sliding window (e.g., 15 * time.Second)
//
// Returns:
//   - AggregatedMetrics: the aggregated values for all metric types with data
//   - error: ErrNoData if no data in window, ErrInvalidWindow if windowSize <= 0,
//     ErrNilBuffer if buf is nil
func CalculateWindow(buf buffer.MetricBuffer, windowSize time.Duration) (*AggregatedMetrics, error) {
	sw, err := NewSlidingWindow(buf)
	if err != nil {
		return nil, err
	}
	return sw.Calculate(windowSize)
}

// CalculateWindowRange is a convenience function that creates a temporary
// SlidingWindow calculator and computes aggregated metrics over the
// specified time range [from, to].
//
// This is useful for one-off calculations with explicit time boundaries.
// For repeated calculations, create a SlidingWindow instance with
// NewSlidingWindow() and reuse it.
func CalculateWindowRange(buf buffer.MetricBuffer, from, to time.Time) (*AggregatedMetrics, error) {
	sw, err := NewSlidingWindow(buf)
	if err != nil {
		return nil, err
	}
	return sw.CalculateRange(from, to)
}
