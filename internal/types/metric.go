// Package types provides the core metric type definitions used throughout
// the monitoring system. It defines the Metric interface that all metric
// types must implement, along with MetricType constants for identification
// and concrete metric wrapper types for each subsystem.
package types

import (
	"time"

	pb "golang-project.local/proto"
)

// MetricType identifies the category of a metric.
type MetricType string

const (
	// CPUMetricType identifies CPU usage metrics.
	CPUMetricType MetricType = "cpu"
	// DiskMetricType identifies disk I/O metrics.
	DiskMetricType MetricType = "disk"
	// NetworkMetricType identifies network connection metrics.
	NetworkMetricType MetricType = "network"
	// FilesystemMetricType identifies filesystem usage metrics.
	FilesystemMetricType MetricType = "filesystem"
	// LoadMetricType identifies system load average metrics.
	LoadMetricType MetricType = "load"
	// TopTalkersMetricType identifies network top talkers metrics.
	TopTalkersMetricType MetricType = "toptalkers"
)

// Metric defines the interface that all metric types must implement.
// This enables uniform handling in buffers, schedulers, and aggregators.
type Metric interface {
	// Type returns the metric type identifier.
	Type() MetricType

	// Timestamp returns when the metric was collected.
	Timestamp() time.Time

	// Value returns the underlying metric data.
	Value() interface{}

	// Merge combines this metric with another of the same type and returns
	// the result. Used for sliding window aggregation.
	Merge(other Metric) Metric
}

// ---------------------------------------------------------------------------
// Load Metric
// ---------------------------------------------------------------------------

// LoadMetric wraps proto.LoadMetrics to implement the Metric interface.
type LoadMetric struct {
	ts      time.Time
	Metrics *pb.LoadMetrics
}

// NewLoadMetric creates a new LoadMetric with the given timestamp and data.
func NewLoadMetric(ts time.Time, m *pb.LoadMetrics) *LoadMetric {
	return &LoadMetric{ts: ts, Metrics: m}
}

func (m *LoadMetric) Type() MetricType     { return LoadMetricType }
func (m *LoadMetric) Timestamp() time.Time { return m.ts }
func (m *LoadMetric) Value() interface{}   { return m.Metrics }

// Merge averages two load metrics together.
func (m *LoadMetric) Merge(other Metric) Metric {
	o, ok := other.(*LoadMetric)
	if !ok {
		return m
	}
	return &LoadMetric{
		ts: m.ts,
		Metrics: &pb.LoadMetrics{
			Load1: (m.Metrics.GetLoad1() + o.Metrics.GetLoad1()) / 2,
		},
	}
}

// ---------------------------------------------------------------------------
// CPU Metric
// ---------------------------------------------------------------------------

// CpuMetric wraps proto.CpuMetrics to implement the Metric interface.
type CpuMetric struct {
	ts      time.Time
	Metrics *pb.CpuMetrics
}

// NewCpuMetric creates a new CpuMetric with the given timestamp and data.
func NewCpuMetric(ts time.Time, m *pb.CpuMetrics) *CpuMetric {
	return &CpuMetric{ts: ts, Metrics: m}
}

func (m *CpuMetric) Type() MetricType     { return CPUMetricType }
func (m *CpuMetric) Timestamp() time.Time { return m.ts }
func (m *CpuMetric) Value() interface{}   { return m.Metrics }

// Merge averages two CPU metrics together.
func (m *CpuMetric) Merge(other Metric) Metric {
	o, ok := other.(*CpuMetric)
	if !ok {
		return m
	}
	return &CpuMetric{
		ts: m.ts,
		Metrics: &pb.CpuMetrics{
			PercentUser:   (m.Metrics.GetPercentUser() + o.Metrics.GetPercentUser()) / 2,
			PercentSystem: (m.Metrics.GetPercentSystem() + o.Metrics.GetPercentSystem()) / 2,
			PercentIdle:   (m.Metrics.GetPercentIdle() + o.Metrics.GetPercentIdle()) / 2,
		},
	}
}

// ---------------------------------------------------------------------------
// Disk I/O Metric
// ---------------------------------------------------------------------------

// DiskIOMetric wraps proto.DiskIOMetrics to implement the Metric interface.
type DiskIOMetric struct {
	ts      time.Time
	Metrics *pb.DiskIOMetrics
}

// NewDiskIOMetric creates a new DiskIOMetric with the given timestamp and data.
func NewDiskIOMetric(ts time.Time, m *pb.DiskIOMetrics) *DiskIOMetric {
	return &DiskIOMetric{ts: ts, Metrics: m}
}

func (m *DiskIOMetric) Type() MetricType     { return DiskMetricType }
func (m *DiskIOMetric) Timestamp() time.Time { return m.ts }
func (m *DiskIOMetric) Value() interface{}   { return m.Metrics }

// Merge averages two disk I/O metrics together.
func (m *DiskIOMetric) Merge(other Metric) Metric {
	o, ok := other.(*DiskIOMetric)
	if !ok {
		return m
	}
	return &DiskIOMetric{
		ts: m.ts,
		Metrics: &pb.DiskIOMetrics{
			Tps:           (m.Metrics.GetTps() + o.Metrics.GetTps()) / 2,
			KbTotalPerSec: (m.Metrics.GetKbTotalPerSec() + o.Metrics.GetKbTotalPerSec()) / 2,
		},
	}
}

// ---------------------------------------------------------------------------
// Filesystem Usage Metric
// ---------------------------------------------------------------------------

// FilesystemUsageMetric wraps proto.FilesystemUsage to implement the Metric interface.
type FilesystemUsageMetric struct {
	ts      time.Time
	Metrics *pb.FilesystemUsage
}

// NewFilesystemUsageMetric creates a new FilesystemUsageMetric with the given timestamp and data.
func NewFilesystemUsageMetric(ts time.Time, m *pb.FilesystemUsage) *FilesystemUsageMetric {
	return &FilesystemUsageMetric{ts: ts, Metrics: m}
}

func (m *FilesystemUsageMetric) Type() MetricType     { return FilesystemMetricType }
func (m *FilesystemUsageMetric) Timestamp() time.Time { return m.ts }
func (m *FilesystemUsageMetric) Value() interface{}   { return m.Metrics }

// Merge returns the most recent filesystem usage metric (filesystem is stable,
// so we keep the latest value rather than averaging).
func (m *FilesystemUsageMetric) Merge(other Metric) Metric {
	o, ok := other.(*FilesystemUsageMetric)
	if !ok {
		return m
	}
	// Return the one with the later timestamp
	if m.ts.After(o.ts) {
		return m
	}
	return o
}

// ---------------------------------------------------------------------------
// Network Metric
// ---------------------------------------------------------------------------

// NetworkMetric wraps proto.NetworkMetrics to implement the Metric interface.
type NetworkMetric struct {
	ts      time.Time
	Metrics *pb.NetworkMetrics
}

// NewNetworkMetric creates a new NetworkMetric with the given timestamp and data.
func NewNetworkMetric(ts time.Time, m *pb.NetworkMetrics) *NetworkMetric {
	return &NetworkMetric{ts: ts, Metrics: m}
}

func (m *NetworkMetric) Type() MetricType     { return NetworkMetricType }
func (m *NetworkMetric) Timestamp() time.Time { return m.ts }
func (m *NetworkMetric) Value() interface{}   { return m.Metrics }

// Merge returns the most recent network metric (connection counts are
// snapshot-based, so we keep the latest value rather than averaging).
func (m *NetworkMetric) Merge(other Metric) Metric {
	o, ok := other.(*NetworkMetric)
	if !ok {
		return m
	}
	if m.ts.After(o.ts) {
		return m
	}
	return o
}

// ---------------------------------------------------------------------------
// Top Talkers Metric
// ---------------------------------------------------------------------------

// TopTalkersMetric wraps proto.TopTalkers to implement the Metric interface.
type TopTalkersMetric struct {
	ts      time.Time
	Metrics *pb.TopTalkers
}

// NewTopTalkersMetric creates a new TopTalkersMetric with the given timestamp and data.
func NewTopTalkersMetric(ts time.Time, m *pb.TopTalkers) *TopTalkersMetric {
	return &TopTalkersMetric{ts: ts, Metrics: m}
}

func (m *TopTalkersMetric) Type() MetricType     { return TopTalkersMetricType }
func (m *TopTalkersMetric) Timestamp() time.Time { return m.ts }
func (m *TopTalkersMetric) Value() interface{}   { return m.Metrics }

// Merge returns the most recent top talkers data (snapshot-based).
func (m *TopTalkersMetric) Merge(other Metric) Metric {
	o, ok := other.(*TopTalkersMetric)
	if !ok {
		return m
	}
	if m.ts.After(o.ts) {
		return m
	}
	return o
}
