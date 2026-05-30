package window

import (
	"errors"
	"sync"
	"time"

	"golang-project.local/internal/buffer"
	"golang-project.local/internal/types"
	pb "golang-project.local/proto"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	// ErrNoData is returned when there are no metrics in the requested window.
	ErrNoData = errors.New("no metric data available in the requested window")

	// ErrInvalidWindow is returned when the window size is invalid (<= 0).
	ErrInvalidWindow = errors.New("window size must be greater than zero")

	// ErrNilBuffer is returned when a nil buffer is provided.
	ErrNilBuffer = errors.New("metric buffer cannot be nil")
)

// ---------------------------------------------------------------------------
// SlidingWindow
// ---------------------------------------------------------------------------

// SlidingWindow computes aggregated metrics over configurable time windows.
// It reads raw metrics from a MetricBuffer and applies the appropriate
// aggregation strategy for each metric type.
//
// The calculator handles partial windows gracefully: at startup, when fewer
// collection ticks have occurred than the window size, it aggregates over
// whatever data is available rather than returning an error.
type SlidingWindow struct {
	mu sync.RWMutex

	// buffer is the source of raw metric data.
	buffer buffer.MetricBuffer

	// strategies maps metric types to their aggregation strategy.
	// If a type is not present, the default strategy is used.
	strategies map[types.MetricType]AggregationStrategy

	// expectedInterval is the expected time between collection ticks.
	// Used to determine if a window is partial.
	expectedInterval time.Duration
}

// SlidingWindowOption configures a SlidingWindow instance.
type SlidingWindowOption func(*SlidingWindow)

// WithStrategy overrides the default aggregation strategy for a specific
// metric type.
func WithStrategy(mt types.MetricType, s AggregationStrategy) SlidingWindowOption {
	return func(sw *SlidingWindow) {
		sw.strategies[mt] = s
	}
}

// WithExpectedInterval sets the expected collection interval. This is used
// to determine whether a window is partial (i.e., contains fewer data points
// than expected for a full window).
func WithExpectedInterval(d time.Duration) SlidingWindowOption {
	return func(sw *SlidingWindow) {
		sw.expectedInterval = d
	}
}

// NewSlidingWindow creates a new SlidingWindow calculator backed by the
// given MetricBuffer.
//
// The buffer provides thread-safe access to raw metrics. The calculator
// reads from the buffer on each Calculate() call, so the buffer should
// be populated by a running CollectorScheduler.
func NewSlidingWindow(buf buffer.MetricBuffer, opts ...SlidingWindowOption) (*SlidingWindow, error) {
	if buf == nil {
		return nil, ErrNilBuffer
	}

	sw := &SlidingWindow{
		buffer:           buf,
		strategies:       make(map[types.MetricType]AggregationStrategy),
		expectedInterval: time.Second, // default: 1-second collection interval
	}

	for _, opt := range opts {
		opt(sw)
	}

	return sw, nil
}

// Calculate aggregates all metrics in the buffer over the specified time
// window [now - windowSize, now]. It returns an AggregatedMetrics containing
// the aggregated values for each metric type that has data in the window.
//
// If no data is available in the window, it returns ErrNoData.
// If windowSize <= 0, it returns ErrInvalidWindow.
func (sw *SlidingWindow) Calculate(windowSize time.Duration) (*AggregatedMetrics, error) {
	if windowSize <= 0 {
		return nil, ErrInvalidWindow
	}

	now := time.Now()
	from := now.Add(-windowSize)

	return sw.CalculateRange(from, now)
}

// CalculateRange aggregates all metrics in the buffer within the specified
// time range [from, to]. This is useful when you need to align window
// boundaries precisely (e.g., on collection tick boundaries).
//
// If no data is available in the range, it returns ErrNoData.
func (sw *SlidingWindow) CalculateRange(from, to time.Time) (*AggregatedMetrics, error) {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	// Get raw metrics from the buffer
	metrics := sw.buffer.GetWindow(from, to)
	if len(metrics) == 0 {
		return nil, ErrNoData
	}

	// Group metrics by type
	grouped := sw.groupByType(metrics)

	// Aggregate each group
	result := &AggregatedMetrics{
		Window: WindowRange{
			From: from,
			To:   to,
		},
		MetricCount: len(metrics),
	}

	for mt, group := range grouped {
		strategy := sw.strategyFor(mt)
		aggregated := sw.aggregate(mt, group, strategy)
		sw.assignResult(result, mt, aggregated)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// groupByType partitions metrics by their type.
func (sw *SlidingWindow) groupByType(metrics []types.Metric) map[types.MetricType][]types.Metric {
	grouped := make(map[types.MetricType][]types.Metric)
	for _, m := range metrics {
		mt := m.Type()
		grouped[mt] = append(grouped[mt], m)
	}
	return grouped
}

// strategyFor returns the aggregation strategy for the given metric type,
// falling back to the default if no override was configured.
func (sw *SlidingWindow) strategyFor(mt types.MetricType) AggregationStrategy {
	if s, ok := sw.strategies[mt]; ok {
		return s
	}
	return DefaultStrategy(mt)
}

// aggregate applies the given strategy to a group of metrics of the same type.
func (sw *SlidingWindow) aggregate(mt types.MetricType, metrics []types.Metric, strategy AggregationStrategy) types.Metric {
	if len(metrics) == 0 {
		return nil
	}
	if len(metrics) == 1 {
		return metrics[0]
	}

	switch strategy {
	case StrategyAverage:
		return sw.aggregateAverage(metrics)
	case StrategySum:
		return sw.aggregateSum(metrics)
	case StrategyMax:
		return sw.aggregateMax(metrics)
	case StrategyMin:
		return sw.aggregateMin(metrics)
	case StrategyLatest:
		return sw.aggregateLatest(metrics)
	default:
		return sw.aggregateAverage(metrics)
	}
}

// ---------------------------------------------------------------------------
// Strategy implementations
// ---------------------------------------------------------------------------

// aggregateAverage computes the arithmetic mean of all metrics in the group.
// Since Merge() computes (a+b)/2, pairwise merging does NOT yield the correct
// overall average for more than 2 elements. Instead, we use the mergeSum
// approach which recovers the running sum, adds the new value, and recomputes
// the average.
func (sw *SlidingWindow) aggregateAverage(metrics []types.Metric) types.Metric {
	if len(metrics) == 0 {
		return nil
	}
	result := metrics[0]
	for i := 1; i < len(metrics); i++ {
		result = sw.mergeSum(result, metrics[i], i+1)
	}
	return result
}

// aggregateSum sums all metrics of the same type. For types where Merge()
// computes an average, we reconstruct the sum by multiplying the pairwise
// average by the number of elements merged so far.
func (sw *SlidingWindow) aggregateSum(metrics []types.Metric) types.Metric {
	if len(metrics) == 0 {
		return nil
	}

	result := metrics[0]
	for i := 1; i < len(metrics); i++ {
		// Merge computes (a+b)/2. To get the sum, we need to track
		// the running sum differently. We use the first metric as a
		// base and add subsequent values.
		result = sw.mergeSum(result, metrics[i], i+1)
	}
	return result
}

// mergeSum combines two metrics by summing their values. This is a
// type-specific operation since the generic Merge() averages.
func (sw *SlidingWindow) mergeSum(a, b types.Metric, count int) types.Metric {
	// For types where Merge() averages, we can recover the sum by
	// converting the running average back to a sum, adding the new
	// value, and converting back.
	//
	// runningSum = runningAvg * (count-1) + newValue
	// newAvg = runningSum / count
	//
	// But since Merge() does (a+b)/2, we need to handle this per type.

	switch a.Type() {
	case types.CPUMetricType:
		return sw.mergeCPUSum(a, b, count)
	case types.LoadMetricType:
		return sw.mergeLoadSum(a, b, count)
	case types.DiskMetricType:
		return sw.mergeDiskIOSum(a, b, count)
	default:
		// For snapshot types (filesystem, network, toptalkers),
		// sum doesn't make sense; fall back to latest.
		return b
	}
}

func (sw *SlidingWindow) mergeCPUSum(a, b types.Metric, count int) types.Metric {
	am := a.(*types.CpuMetric)
	bm := b.(*types.CpuMetric)

	// Recover sum from running average: sum = avg * (count-1)
	// Then add new value: newSum = sum + newValue
	// Then compute new average: newAvg = newSum / count
	prevCount := count - 1
	sumUser := am.Metrics.GetPercentUser() * float64(prevCount)
	sumSystem := am.Metrics.GetPercentSystem() * float64(prevCount)
	sumIdle := am.Metrics.GetPercentIdle() * float64(prevCount)

	newSumUser := sumUser + bm.Metrics.GetPercentUser()
	newSumSystem := sumSystem + bm.Metrics.GetPercentSystem()
	newSumIdle := sumIdle + bm.Metrics.GetPercentIdle()

	return &types.CpuMetric{
		Metrics: &pb.CpuMetrics{
			PercentUser:   newSumUser / float64(count),
			PercentSystem: newSumSystem / float64(count),
			PercentIdle:   newSumIdle / float64(count),
		},
	}
}

func (sw *SlidingWindow) mergeLoadSum(a, b types.Metric, count int) types.Metric {
	am := a.(*types.LoadMetric)
	bm := b.(*types.LoadMetric)

	prevCount := count - 1
	sumLoad1 := am.Metrics.GetLoad1() * float64(prevCount)
	newSumLoad1 := sumLoad1 + bm.Metrics.GetLoad1()

	return &types.LoadMetric{
		Metrics: &pb.LoadMetrics{
			Load1: newSumLoad1 / float64(count),
		},
	}
}

func (sw *SlidingWindow) mergeDiskIOSum(a, b types.Metric, count int) types.Metric {
	am := a.(*types.DiskIOMetric)
	bm := b.(*types.DiskIOMetric)

	prevCount := count - 1
	sumTps := am.Metrics.GetTps() * float64(prevCount)
	sumKb := am.Metrics.GetKbTotalPerSec() * float64(prevCount)

	newSumTps := sumTps + bm.Metrics.GetTps()
	newSumKb := sumKb + bm.Metrics.GetKbTotalPerSec()

	return &types.DiskIOMetric{
		Metrics: &pb.DiskIOMetrics{
			Tps:           newSumTps / float64(count),
			KbTotalPerSec: newSumKb / float64(count),
		},
	}
}

// aggregateMax selects the metric with the maximum values. For CPU and
// disk metrics, we compare the primary field (PercentUser, Tps).
func (sw *SlidingWindow) aggregateMax(metrics []types.Metric) types.Metric {
	result := metrics[0]
	for i := 1; i < len(metrics); i++ {
		if sw.greaterThan(metrics[i], result) {
			result = metrics[i]
		}
	}
	return result
}

// aggregateMin selects the metric with the minimum values.
func (sw *SlidingWindow) aggregateMin(metrics []types.Metric) types.Metric {
	result := metrics[0]
	for i := 1; i < len(metrics); i++ {
		if sw.lessThan(metrics[i], result) {
			result = metrics[i]
		}
	}
	return result
}

// aggregateLatest returns the metric with the most recent timestamp.
func (sw *SlidingWindow) aggregateLatest(metrics []types.Metric) types.Metric {
	result := metrics[0]
	for i := 1; i < len(metrics); i++ {
		if metrics[i].Timestamp().After(result.Timestamp()) {
			result = metrics[i]
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Comparison helpers for max/min strategies
// ---------------------------------------------------------------------------

// greaterThan returns true if a has a higher primary value than b.
func (sw *SlidingWindow) greaterThan(a, b types.Metric) bool {
	switch a.Type() {
	case types.CPUMetricType:
		am := a.(*types.CpuMetric)
		bm := b.(*types.CpuMetric)
		return am.Metrics.GetPercentUser() > bm.Metrics.GetPercentUser()
	case types.LoadMetricType:
		am := a.(*types.LoadMetric)
		bm := b.(*types.LoadMetric)
		return am.Metrics.GetLoad1() > bm.Metrics.GetLoad1()
	case types.DiskMetricType:
		am := a.(*types.DiskIOMetric)
		bm := b.(*types.DiskIOMetric)
		return am.Metrics.GetTps() > bm.Metrics.GetTps()
	default:
		// For snapshot types, compare by timestamp (newer = greater)
		return a.Timestamp().After(b.Timestamp())
	}
}

// lessThan returns true if a has a lower primary value than b.
func (sw *SlidingWindow) lessThan(a, b types.Metric) bool {
	switch a.Type() {
	case types.CPUMetricType:
		am := a.(*types.CpuMetric)
		bm := b.(*types.CpuMetric)
		return am.Metrics.GetPercentUser() < bm.Metrics.GetPercentUser()
	case types.LoadMetricType:
		am := a.(*types.LoadMetric)
		bm := b.(*types.LoadMetric)
		return am.Metrics.GetLoad1() < bm.Metrics.GetLoad1()
	case types.DiskMetricType:
		am := a.(*types.DiskIOMetric)
		bm := b.(*types.DiskIOMetric)
		return am.Metrics.GetTps() < bm.Metrics.GetTps()
	default:
		// For snapshot types, compare by timestamp (older = lesser)
		return a.Timestamp().Before(b.Timestamp())
	}
}

// ---------------------------------------------------------------------------
// Result assignment
// ---------------------------------------------------------------------------

// assignResult places the aggregated metric into the appropriate field of
// the AggregatedMetrics result.
func (sw *SlidingWindow) assignResult(result *AggregatedMetrics, mt types.MetricType, m types.Metric) {
	switch mt {
	case types.CPUMetricType:
		if cpu, ok := m.(*types.CpuMetric); ok {
			result.CPU = cpu
		}
	case types.LoadMetricType:
		if load, ok := m.(*types.LoadMetric); ok {
			result.Load = load
		}
	case types.DiskMetricType:
		if disk, ok := m.(*types.DiskIOMetric); ok {
			result.DiskIO = disk
		}
	case types.FilesystemMetricType:
		if fs, ok := m.(*types.FilesystemUsageMetric); ok {
			result.Filesystem = fs
		}
	case types.NetworkMetricType:
		if net, ok := m.(*types.NetworkMetric); ok {
			result.Network = net
		}
	case types.TopTalkersMetricType:
		if tt, ok := m.(*types.TopTalkersMetric); ok {
			result.TopTalkers = tt
		}
	}
}

// ---------------------------------------------------------------------------
// Convenience methods
// ---------------------------------------------------------------------------

// Buffer returns the underlying metric buffer.
func (sw *SlidingWindow) Buffer() buffer.MetricBuffer {
	return sw.buffer
}

// IsPartialWindow checks whether the given window duration would contain
// fewer data points than expected, indicating a partial window (e.g., at
// startup before enough collection ticks have occurred).
//
// A window is considered partial when:
//   - There is at least one data point in the window
//   - The number of data points is less than expected for a full window
//
// An empty buffer returns false (no data is not the same as partial data).
func (sw *SlidingWindow) IsPartialWindow(windowSize time.Duration) bool {
	now := time.Now()
	from := now.Add(-windowSize)
	metrics := sw.buffer.GetWindow(from, now)

	// No data at all means the window is empty, not partial
	if len(metrics) == 0 {
		return false
	}

	expectedCount := int(windowSize / sw.expectedInterval)
	if expectedCount <= 1 {
		return false
	}

	return len(metrics) < expectedCount
}
