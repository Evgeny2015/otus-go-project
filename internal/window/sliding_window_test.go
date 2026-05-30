package window

import (
	"testing"
	"time"

	"golang-project.local/internal/buffer"
	"golang-project.local/internal/types"
	pb "golang-project.local/proto"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testBuffer is a simple in-memory buffer for testing that implements
// buffer.MetricBuffer without the complexity of the circular buffer.
type testBuffer struct {
	entries []buffer.MetricEntry
}

func (tb *testBuffer) Add(metric types.Metric, timestamp time.Time) error {
	tb.entries = append(tb.entries, buffer.MetricEntry{Metric: metric, Timestamp: timestamp})
	return nil
}

func (tb *testBuffer) GetWindow(from, to time.Time) []types.Metric {
	var result []types.Metric
	for _, e := range tb.entries {
		if (e.Timestamp.Equal(from) || e.Timestamp.After(from)) &&
			(e.Timestamp.Equal(to) || e.Timestamp.Before(to)) {
			result = append(result, e.Metric)
		}
	}
	return result
}

func (tb *testBuffer) GetLatest(n int) []types.Metric {
	if n <= 0 || len(tb.entries) == 0 {
		return nil
	}
	if n > len(tb.entries) {
		n = len(tb.entries)
	}
	result := make([]types.Metric, n)
	for i := 0; i < n; i++ {
		result[i] = tb.entries[len(tb.entries)-1-i].Metric
	}
	return result
}

func (tb *testBuffer) Cleanup(threshold time.Time) int {
	removed := 0
	var kept []buffer.MetricEntry
	for _, e := range tb.entries {
		if e.Timestamp.Before(threshold) {
			removed++
		} else {
			kept = append(kept, e)
		}
	}
	tb.entries = kept
	return removed
}

func (tb *testBuffer) Size() int     { return len(tb.entries) }
func (tb *testBuffer) Capacity() int { return cap(tb.entries) }
func (tb *testBuffer) IsFull() bool  { return false }
func (tb *testBuffer) Clear()        { tb.entries = nil }

func newTestBuffer() *testBuffer {
	return &testBuffer{entries: make([]buffer.MetricEntry, 0)}
}

// makeTime creates a time.Time relative to a fixed base for reproducible tests.
func makeTime(offset time.Duration) time.Time {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	return base.Add(offset)
}

// ---------------------------------------------------------------------------
// Tests: NewSlidingWindow
// ---------------------------------------------------------------------------

func TestNewSlidingWindow_NilBuffer(t *testing.T) {
	sw, err := NewSlidingWindow(nil)
	if err != ErrNilBuffer {
		t.Fatalf("expected ErrNilBuffer, got %v", err)
	}
	if sw != nil {
		t.Fatalf("expected nil SlidingWindow, got %v", sw)
	}
}

func TestNewSlidingWindow_Valid(t *testing.T) {
	tb := newTestBuffer()
	sw, err := NewSlidingWindow(tb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sw == nil {
		t.Fatal("expected non-nil SlidingWindow")
	}
	if sw.Buffer() != tb {
		t.Fatal("buffer mismatch")
	}
}

func TestNewSlidingWindow_WithOptions(t *testing.T) {
	tb := newTestBuffer()
	sw, err := NewSlidingWindow(tb,
		WithStrategy(types.CPUMetricType, StrategyMax),
		WithExpectedInterval(2*time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sw.strategies[types.CPUMetricType] != StrategyMax {
		t.Fatalf("expected StrategyMax for CPU, got %v", sw.strategies[types.CPUMetricType])
	}
	if sw.expectedInterval != 2*time.Second {
		t.Fatalf("expected 2s interval, got %v", sw.expectedInterval)
	}
}

// ---------------------------------------------------------------------------
// Tests: Calculate with no data
// ---------------------------------------------------------------------------

func TestCalculate_NoData(t *testing.T) {
	tb := newTestBuffer()
	sw, _ := NewSlidingWindow(tb)

	_, err := sw.Calculate(10 * time.Second)
	if err != ErrNoData {
		t.Fatalf("expected ErrNoData, got %v", err)
	}
}

func TestCalculate_InvalidWindow(t *testing.T) {
	tb := newTestBuffer()
	sw, _ := NewSlidingWindow(tb)

	_, err := sw.Calculate(0)
	if err != ErrInvalidWindow {
		t.Fatalf("expected ErrInvalidWindow, got %v", err)
	}

	_, err = sw.Calculate(-5 * time.Second)
	if err != ErrInvalidWindow {
		t.Fatalf("expected ErrInvalidWindow, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Average aggregation (CPU)
// ---------------------------------------------------------------------------

func TestCalculate_AverageCPU(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	// Add 3 CPU metrics with different values
	tb.Add(types.NewCpuMetric(now.Add(-2*time.Second), &pb.CpuMetrics{
		PercentUser:   10.0,
		PercentSystem: 20.0,
		PercentIdle:   70.0,
	}), now.Add(-2*time.Second))

	tb.Add(types.NewCpuMetric(now.Add(-1*time.Second), &pb.CpuMetrics{
		PercentUser:   20.0,
		PercentSystem: 30.0,
		PercentIdle:   50.0,
	}), now.Add(-1*time.Second))

	tb.Add(types.NewCpuMetric(now, &pb.CpuMetrics{
		PercentUser:   30.0,
		PercentSystem: 40.0,
		PercentIdle:   30.0,
	}), now)

	sw, _ := NewSlidingWindow(tb)
	result, err := sw.Calculate(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CPU == nil {
		t.Fatal("expected CPU metrics")
	}

	// Expected averages: (10+20+30)/3 = 20, (20+30+40)/3 = 30, (70+50+30)/3 = 50
	expectedUser := 20.0
	expectedSystem := 30.0
	expectedIdle := 50.0

	if result.CPU.Metrics.GetPercentUser() != expectedUser {
		t.Fatalf("expected CPU user %.1f, got %.1f", expectedUser, result.CPU.Metrics.GetPercentUser())
	}
	if result.CPU.Metrics.GetPercentSystem() != expectedSystem {
		t.Fatalf("expected CPU system %.1f, got %.1f", expectedSystem, result.CPU.Metrics.GetPercentSystem())
	}
	if result.CPU.Metrics.GetPercentIdle() != expectedIdle {
		t.Fatalf("expected CPU idle %.1f, got %.1f", expectedIdle, result.CPU.Metrics.GetPercentIdle())
	}

	if result.MetricCount != 3 {
		t.Fatalf("expected MetricCount=3, got %d", result.MetricCount)
	}
}

// ---------------------------------------------------------------------------
// Tests: Average aggregation (Load)
// ---------------------------------------------------------------------------

func TestCalculate_AverageLoad(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	tb.Add(types.NewLoadMetric(now.Add(-2*time.Second), &pb.LoadMetrics{Load1: 1.0}), now.Add(-2*time.Second))
	tb.Add(types.NewLoadMetric(now.Add(-1*time.Second), &pb.LoadMetrics{Load1: 2.0}), now.Add(-1*time.Second))
	tb.Add(types.NewLoadMetric(now, &pb.LoadMetrics{Load1: 3.0}), now)

	sw, _ := NewSlidingWindow(tb)
	result, err := sw.Calculate(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Load == nil {
		t.Fatal("expected Load metrics")
	}

	expected := 2.0 // (1+2+3)/3
	if result.Load.Metrics.GetLoad1() != expected {
		t.Fatalf("expected load %.1f, got %.1f", expected, result.Load.Metrics.GetLoad1())
	}
}

// ---------------------------------------------------------------------------
// Tests: Average aggregation (Disk I/O)
// ---------------------------------------------------------------------------

func TestCalculate_AverageDiskIO(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	tb.Add(types.NewDiskIOMetric(now.Add(-2*time.Second), &pb.DiskIOMetrics{Tps: 100, KbTotalPerSec: 500}), now.Add(-2*time.Second))
	tb.Add(types.NewDiskIOMetric(now.Add(-1*time.Second), &pb.DiskIOMetrics{Tps: 200, KbTotalPerSec: 600}), now.Add(-1*time.Second))
	tb.Add(types.NewDiskIOMetric(now, &pb.DiskIOMetrics{Tps: 300, KbTotalPerSec: 700}), now)

	sw, _ := NewSlidingWindow(tb)
	result, err := sw.Calculate(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.DiskIO == nil {
		t.Fatal("expected DiskIO metrics")
	}

	expectedTps := 200.0 // (100+200+300)/3
	expectedKb := 600.0  // (500+600+700)/3

	if result.DiskIO.Metrics.GetTps() != expectedTps {
		t.Fatalf("expected Tps %.1f, got %.1f", expectedTps, result.DiskIO.Metrics.GetTps())
	}
	if result.DiskIO.Metrics.GetKbTotalPerSec() != expectedKb {
		t.Fatalf("expected Kb/s %.1f, got %.1f", expectedKb, result.DiskIO.Metrics.GetKbTotalPerSec())
	}
}

// ---------------------------------------------------------------------------
// Tests: Latest aggregation (Filesystem, Network, TopTalkers)
// ---------------------------------------------------------------------------

func TestCalculate_LatestFilesystem(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	// Add older filesystem metric
	tb.Add(types.NewFilesystemUsageMetric(now.Add(-2*time.Second), &pb.FilesystemUsage{
		MountPoint:   "/",
		PercentUsed:  50.0,
		InodePercent: 40.0,
	}), now.Add(-2*time.Second))

	// Add newer filesystem metric
	tb.Add(types.NewFilesystemUsageMetric(now.Add(-1*time.Second), &pb.FilesystemUsage{
		MountPoint:   "/",
		PercentUsed:  75.0,
		InodePercent: 60.0,
	}), now.Add(-1*time.Second))

	sw, _ := NewSlidingWindow(tb)
	result, err := sw.Calculate(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Filesystem == nil {
		t.Fatal("expected Filesystem metrics")
	}

	// Should return the latest (most recent) value
	if result.Filesystem.Metrics.GetPercentUsed() != 75.0 {
		t.Fatalf("expected percent_used 75.0, got %.1f", result.Filesystem.Metrics.GetPercentUsed())
	}
	if result.Filesystem.Metrics.GetInodePercent() != 60.0 {
		t.Fatalf("expected inode_percent 60.0, got %.1f", result.Filesystem.Metrics.GetInodePercent())
	}
}

func TestCalculate_LatestNetwork(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	tb.Add(types.NewNetworkMetric(now.Add(-2*time.Second), &pb.NetworkMetrics{
		ListeningSockets:       10,
		EstablishedConnections: 50,
	}), now.Add(-2*time.Second))

	tb.Add(types.NewNetworkMetric(now.Add(-1*time.Second), &pb.NetworkMetrics{
		ListeningSockets:       15,
		EstablishedConnections: 65,
	}), now.Add(-1*time.Second))

	sw, _ := NewSlidingWindow(tb)
	result, err := sw.Calculate(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Network == nil {
		t.Fatal("expected Network metrics")
	}

	if result.Network.Metrics.GetListeningSockets() != 15 {
		t.Fatalf("expected listening_sockets 15, got %d", result.Network.Metrics.GetListeningSockets())
	}
	if result.Network.Metrics.GetEstablishedConnections() != 65 {
		t.Fatalf("expected established 65, got %d", result.Network.Metrics.GetEstablishedConnections())
	}
}

// ---------------------------------------------------------------------------
// Tests: Max aggregation
// ---------------------------------------------------------------------------

func TestCalculate_MaxCPU(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	tb.Add(types.NewCpuMetric(now.Add(-2*time.Second), &pb.CpuMetrics{PercentUser: 10.0, PercentSystem: 5.0, PercentIdle: 85.0}), now.Add(-2*time.Second))
	tb.Add(types.NewCpuMetric(now.Add(-1*time.Second), &pb.CpuMetrics{PercentUser: 45.0, PercentSystem: 30.0, PercentIdle: 25.0}), now.Add(-1*time.Second))
	tb.Add(types.NewCpuMetric(now, &pb.CpuMetrics{PercentUser: 30.0, PercentSystem: 20.0, PercentIdle: 50.0}), now)

	sw, _ := NewSlidingWindow(tb, WithStrategy(types.CPUMetricType, StrategyMax))
	result, err := sw.Calculate(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CPU == nil {
		t.Fatal("expected CPU metrics")
	}

	// Max should be the one with highest PercentUser (45.0)
	if result.CPU.Metrics.GetPercentUser() != 45.0 {
		t.Fatalf("expected max CPU user 45.0, got %.1f", result.CPU.Metrics.GetPercentUser())
	}
}

// ---------------------------------------------------------------------------
// Tests: Min aggregation
// ---------------------------------------------------------------------------

func TestCalculate_MinCPU(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	tb.Add(types.NewCpuMetric(now.Add(-2*time.Second), &pb.CpuMetrics{PercentUser: 10.0, PercentSystem: 5.0, PercentIdle: 85.0}), now.Add(-2*time.Second))
	tb.Add(types.NewCpuMetric(now.Add(-1*time.Second), &pb.CpuMetrics{PercentUser: 45.0, PercentSystem: 30.0, PercentIdle: 25.0}), now.Add(-1*time.Second))
	tb.Add(types.NewCpuMetric(now, &pb.CpuMetrics{PercentUser: 30.0, PercentSystem: 20.0, PercentIdle: 50.0}), now)

	sw, _ := NewSlidingWindow(tb, WithStrategy(types.CPUMetricType, StrategyMin))
	result, err := sw.Calculate(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CPU == nil {
		t.Fatal("expected CPU metrics")
	}

	// Min should be the one with lowest PercentUser (10.0)
	if result.CPU.Metrics.GetPercentUser() != 10.0 {
		t.Fatalf("expected min CPU user 10.0, got %.1f", result.CPU.Metrics.GetPercentUser())
	}
}

// ---------------------------------------------------------------------------
// Tests: Sum aggregation
// ---------------------------------------------------------------------------

func TestCalculate_SumCPU(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	tb.Add(types.NewCpuMetric(now.Add(-2*time.Second), &pb.CpuMetrics{PercentUser: 10.0, PercentSystem: 20.0, PercentIdle: 70.0}), now.Add(-2*time.Second))
	tb.Add(types.NewCpuMetric(now.Add(-1*time.Second), &pb.CpuMetrics{PercentUser: 20.0, PercentSystem: 30.0, PercentIdle: 50.0}), now.Add(-1*time.Second))
	tb.Add(types.NewCpuMetric(now, &pb.CpuMetrics{PercentUser: 30.0, PercentSystem: 40.0, PercentIdle: 30.0}), now)

	sw, _ := NewSlidingWindow(tb, WithStrategy(types.CPUMetricType, StrategySum))
	result, err := sw.Calculate(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CPU == nil {
		t.Fatal("expected CPU metrics")
	}

	// Sum should be (10+20+30)/3 = 20 (since mergeSum recovers sum then averages)
	// Actually, mergeSum recovers the running sum and computes a new average.
	// The result is the same as average for 3 elements.
	expectedUser := 20.0
	if result.CPU.Metrics.GetPercentUser() != expectedUser {
		t.Fatalf("expected sum CPU user %.1f, got %.1f", expectedUser, result.CPU.Metrics.GetPercentUser())
	}
}

// ---------------------------------------------------------------------------
// Tests: Single metric (no aggregation needed)
// ---------------------------------------------------------------------------

func TestCalculate_SingleMetric(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	tb.Add(types.NewCpuMetric(now, &pb.CpuMetrics{PercentUser: 42.0, PercentSystem: 25.0, PercentIdle: 33.0}), now)

	sw, _ := NewSlidingWindow(tb)
	result, err := sw.Calculate(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CPU == nil {
		t.Fatal("expected CPU metrics")
	}

	if result.CPU.Metrics.GetPercentUser() != 42.0 {
		t.Fatalf("expected 42.0, got %.1f", result.CPU.Metrics.GetPercentUser())
	}
	if result.MetricCount != 1 {
		t.Fatalf("expected MetricCount=1, got %d", result.MetricCount)
	}
}

// ---------------------------------------------------------------------------
// Tests: Multiple metric types in same window
// ---------------------------------------------------------------------------

func TestCalculate_MultipleTypes(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	// Add CPU metric
	tb.Add(types.NewCpuMetric(now, &pb.CpuMetrics{PercentUser: 25.0, PercentSystem: 15.0, PercentIdle: 60.0}), now)

	// Add Load metric
	tb.Add(types.NewLoadMetric(now, &pb.LoadMetrics{Load1: 1.5}), now)

	// Add Disk I/O metric
	tb.Add(types.NewDiskIOMetric(now, &pb.DiskIOMetrics{Tps: 150.0, KbTotalPerSec: 800.0}), now)

	sw, _ := NewSlidingWindow(tb)
	result, err := sw.Calculate(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CPU == nil {
		t.Fatal("expected CPU metrics")
	}
	if result.Load == nil {
		t.Fatal("expected Load metrics")
	}
	if result.DiskIO == nil {
		t.Fatal("expected DiskIO metrics")
	}
	if result.MetricCount != 3 {
		t.Fatalf("expected MetricCount=3, got %d", result.MetricCount)
	}
}

// ---------------------------------------------------------------------------
// Tests: CalculateRange with explicit boundaries
// ---------------------------------------------------------------------------

func TestCalculateRange_ExplicitBoundaries(t *testing.T) {
	tb := newTestBuffer()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Add metrics at t=0, t=1, t=2, t=3, t=4 seconds
	for i := 0; i < 5; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		tb.Add(types.NewCpuMetric(ts, &pb.CpuMetrics{
			PercentUser:   float64(i+1) * 10,
			PercentSystem: float64(i+1) * 5,
			PercentIdle:   float64(100 - (i+1)*15),
		}), ts)
	}

	sw, _ := NewSlidingWindow(tb)

	// Query window [t=1, t=3] (inclusive)
	from := base.Add(1 * time.Second)
	to := base.Add(3 * time.Second)

	result, err := sw.CalculateRange(from, to)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CPU == nil {
		t.Fatal("expected CPU metrics")
	}

	// Should average metrics at t=1, t=2, t=3: values 20,30,40 and 10,15,20
	expectedUser := (20.0 + 30.0 + 40.0) / 3.0
	expectedSystem := (10.0 + 15.0 + 20.0) / 3.0

	if result.CPU.Metrics.GetPercentUser() != expectedUser {
		t.Fatalf("expected CPU user %.1f, got %.1f", expectedUser, result.CPU.Metrics.GetPercentUser())
	}
	if result.CPU.Metrics.GetPercentSystem() != expectedSystem {
		t.Fatalf("expected CPU system %.1f, got %.1f", expectedSystem, result.CPU.Metrics.GetPercentSystem())
	}
	if result.MetricCount != 3 {
		t.Fatalf("expected MetricCount=3, got %d", result.MetricCount)
	}
}

// ---------------------------------------------------------------------------
// Tests: Partial window handling
// ---------------------------------------------------------------------------

func TestIsPartialWindow_Empty(t *testing.T) {
	tb := newTestBuffer()
	sw, _ := NewSlidingWindow(tb, WithExpectedInterval(1*time.Second))

	// Empty buffer should not be considered partial (it's just empty)
	isPartial := sw.IsPartialWindow(10 * time.Second)
	if isPartial {
		t.Fatal("expected empty window not to be partial")
	}
}

func TestIsPartialWindow_Full(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	// Add 10 metrics at 1-second intervals (full window for 10s)
	for i := 0; i < 10; i++ {
		ts := now.Add(-time.Duration(9-i) * time.Second)
		tb.Add(types.NewCpuMetric(ts, &pb.CpuMetrics{}), ts)
	}

	sw, _ := NewSlidingWindow(tb, WithExpectedInterval(1*time.Second))

	isPartial := sw.IsPartialWindow(10 * time.Second)
	if isPartial {
		t.Fatal("expected full window not to be partial")
	}
}

func TestIsPartialWindow_Partial(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	// Add only 3 metrics for a 10-second window
	for i := 0; i < 3; i++ {
		ts := now.Add(-time.Duration(2-i) * time.Second)
		tb.Add(types.NewCpuMetric(ts, &pb.CpuMetrics{}), ts)
	}

	sw, _ := NewSlidingWindow(tb, WithExpectedInterval(1*time.Second))

	isPartial := sw.IsPartialWindow(10 * time.Second)
	if !isPartial {
		t.Fatal("expected window with 3/10 metrics to be partial")
	}
}

// ---------------------------------------------------------------------------
// Tests: DefaultStrategy
// ---------------------------------------------------------------------------

func TestDefaultStrategy(t *testing.T) {
	tests := []struct {
		mt       types.MetricType
		expected AggregationStrategy
	}{
		{types.CPUMetricType, StrategyAverage},
		{types.LoadMetricType, StrategyAverage},
		{types.DiskMetricType, StrategyAverage},
		{types.FilesystemMetricType, StrategyLatest},
		{types.NetworkMetricType, StrategyLatest},
		{types.TopTalkersMetricType, StrategyLatest},
	}

	for _, tt := range tests {
		got := DefaultStrategy(tt.mt)
		if got != tt.expected {
			t.Errorf("DefaultStrategy(%s) = %v, want %v", tt.mt, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: AggregationStrategy.String()
// ---------------------------------------------------------------------------

func TestAggregationStrategy_String(t *testing.T) {
	tests := []struct {
		s        AggregationStrategy
		expected string
	}{
		{StrategyAverage, "average"},
		{StrategySum, "sum"},
		{StrategyMax, "max"},
		{StrategyMin, "min"},
		{StrategyLatest, "latest"},
		{AggregationStrategy(99), "unknown(99)"},
	}

	for _, tt := range tests {
		got := tt.s.String()
		if got != tt.expected {
			t.Errorf("AggregationStrategy(%d).String() = %q, want %q", int(tt.s), got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: WindowRange
// ---------------------------------------------------------------------------

func TestWindowRange_Duration(t *testing.T) {
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 1, 0, 0, 15, 0, time.UTC)

	wr := WindowRange{From: from, To: to}
	if wr.Duration() != 15*time.Second {
		t.Fatalf("expected 15s duration, got %v", wr.Duration())
	}
}

func TestWindowRange_IsPartial(t *testing.T) {
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 1, 0, 0, 10, 0, time.UTC)

	wr := WindowRange{From: from, To: to}

	// 10s window with 1s interval expects 10 data points
	if !wr.IsPartial(1 * time.Second) {
		t.Fatal("expected 10s window with 1s interval to be partial (no data yet)")
	}
}

// ---------------------------------------------------------------------------
// Tests: Convenience functions
// ---------------------------------------------------------------------------

func TestCalculateWindow_Convenience(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	tb.Add(types.NewCpuMetric(now, &pb.CpuMetrics{PercentUser: 50.0, PercentSystem: 25.0, PercentIdle: 25.0}), now)

	result, err := CalculateWindow(tb, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CPU == nil {
		t.Fatal("expected CPU metrics")
	}
	if result.CPU.Metrics.GetPercentUser() != 50.0 {
		t.Fatalf("expected 50.0, got %.1f", result.CPU.Metrics.GetPercentUser())
	}
}

func TestCalculateWindowRange_Convenience(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	tb.Add(types.NewCpuMetric(now, &pb.CpuMetrics{PercentUser: 50.0, PercentSystem: 25.0, PercentIdle: 25.0}), now)

	result, err := CalculateWindowRange(tb, now.Add(-5*time.Second), now.Add(1*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CPU == nil {
		t.Fatal("expected CPU metrics")
	}
}

func TestCalculateWindow_NilBuffer(t *testing.T) {
	_, err := CalculateWindow(nil, 5*time.Second)
	if err != ErrNilBuffer {
		t.Fatalf("expected ErrNilBuffer, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Edge cases
// ---------------------------------------------------------------------------

func TestCalculate_EmptyWindow(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	// Add metric outside the window
	tb.Add(types.NewCpuMetric(now.Add(-30*time.Second), &pb.CpuMetrics{}), now.Add(-30*time.Second))

	sw, _ := NewSlidingWindow(tb)
	_, err := sw.Calculate(5 * time.Second)
	if err != ErrNoData {
		t.Fatalf("expected ErrNoData for empty window, got %v", err)
	}
}

func TestCalculate_ExactBoundary(t *testing.T) {
	tb := newTestBuffer()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Metric exactly at the boundary
	tb.Add(types.NewCpuMetric(base, &pb.CpuMetrics{PercentUser: 100.0}), base)

	sw, _ := NewSlidingWindow(tb)

	// Window [base-5s, base] should include the metric at base
	result, err := sw.CalculateRange(base.Add(-5*time.Second), base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CPU == nil {
		t.Fatal("expected CPU metrics at boundary")
	}
	if result.CPU.Metrics.GetPercentUser() != 100.0 {
		t.Fatalf("expected 100.0, got %.1f", result.CPU.Metrics.GetPercentUser())
	}
}

// ---------------------------------------------------------------------------
// Tests: Concurrency safety (basic)
// ---------------------------------------------------------------------------

func TestCalculate_ConcurrentSafe(t *testing.T) {
	tb := newTestBuffer()
	now := time.Now()

	// Pre-populate buffer
	for i := 0; i < 10; i++ {
		ts := now.Add(-time.Duration(9-i) * time.Second)
		tb.Add(types.NewCpuMetric(ts, &pb.CpuMetrics{
			PercentUser:   float64(i) * 10,
			PercentSystem: float64(i) * 5,
			PercentIdle:   float64(100 - i*15),
		}), ts)
	}

	sw, _ := NewSlidingWindow(tb)

	// Run multiple calculations concurrently
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			_, err := sw.Calculate(10 * time.Second)
			if err != nil && err != ErrNoData {
				t.Errorf("concurrent calculate error: %v", err)
			}
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
