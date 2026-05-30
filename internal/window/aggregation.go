// Package window provides a sliding window calculator for aggregating
// metrics over configurable time windows. It supports multiple aggregation
// strategies (average, sum, max, min) and handles partial windows at startup.
package window

import (
	"fmt"
	"time"

	"golang-project.local/internal/types"
)

// ---------------------------------------------------------------------------
// Aggregation Strategy
// ---------------------------------------------------------------------------

// AggregationStrategy defines how metrics are combined within a window.
type AggregationStrategy int

const (
	// StrategyAverage computes the arithmetic mean of metric values.
	// Suitable for CPU %, load averages, and similar rate-based metrics.
	StrategyAverage AggregationStrategy = iota

	// StrategySum computes the total sum of metric values.
	// Suitable for counters like bytes transferred, packets sent.
	StrategySum

	// StrategyMax selects the maximum value in the window.
	// Useful for peak detection (e.g., peak CPU, max disk I/O).
	StrategyMax

	// StrategyMin selects the minimum value in the window.
	// Useful for trough detection (e.g., minimum free space).
	StrategyMin

	// StrategyLatest selects the most recent value in the window.
	// Suitable for snapshot-based metrics (e.g., filesystem usage,
	// connection counts) where averaging is not meaningful.
	StrategyLatest
)

// String returns a human-readable name for the strategy.
func (s AggregationStrategy) String() string {
	switch s {
	case StrategyAverage:
		return "average"
	case StrategySum:
		return "sum"
	case StrategyMax:
		return "max"
	case StrategyMin:
		return "min"
	case StrategyLatest:
		return "latest"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ---------------------------------------------------------------------------
// MetricTypeStrategy maps each metric type to its default aggregation strategy.
// ---------------------------------------------------------------------------

// DefaultStrategy returns the recommended aggregation strategy for a given
// metric type. This ensures sensible defaults while allowing overrides.
func DefaultStrategy(mt types.MetricType) AggregationStrategy {
	switch mt {
	case types.CPUMetricType:
		return StrategyAverage
	case types.LoadMetricType:
		return StrategyAverage
	case types.DiskMetricType:
		return StrategyAverage
	case types.FilesystemMetricType:
		return StrategyLatest
	case types.NetworkMetricType:
		return StrategyLatest
	case types.TopTalkersMetricType:
		return StrategyLatest
	default:
		return StrategyAverage
	}
}

// ---------------------------------------------------------------------------
// AggregatedMetrics holds the result of a sliding window calculation.
// ---------------------------------------------------------------------------

// AggregatedMetrics contains the aggregated values for all metric types
// over a specific time window.
type AggregatedMetrics struct {
	// Window describes the time range of this aggregation.
	Window WindowRange

	// Per-type aggregated metrics. Each field is nil if no data was
	// available for that metric type during the window.
	CPU        *types.CpuMetric
	Load       *types.LoadMetric
	DiskIO     *types.DiskIOMetric
	Filesystem *types.FilesystemUsageMetric
	Network    *types.NetworkMetric
	TopTalkers *types.TopTalkersMetric

	// MetricCount is the number of raw metric points that contributed
	// to this aggregation. Useful for understanding data density.
	MetricCount int
}

// WindowRange describes the time boundaries of an aggregation window.
type WindowRange struct {
	From time.Time
	To   time.Time
}

// Duration returns the length of the window.
func (w WindowRange) Duration() time.Duration {
	return w.To.Sub(w.From)
}

// IsPartial returns true if the window contains less data than expected
// for a full window of its duration (e.g., at startup before enough
// collection ticks have occurred).
func (w WindowRange) IsPartial(expectedInterval time.Duration) bool {
	expectedCount := int(w.Duration() / expectedInterval)
	// Allow for one missing tick (jitter, scheduling delays)
	return expectedCount > 1
}
