package types

import (
	"testing"
	"time"

	pb "golang-project.local/proto"
)

// ---------------------------------------------------------------------------
// Tests: MetricType constants
// ---------------------------------------------------------------------------

func TestMetricTypeConstants(t *testing.T) {
	tests := []struct {
		metricType MetricType
		want       string
	}{
		{CPUMetricType, "cpu"},
		{DiskMetricType, "disk"},
		{NetworkMetricType, "network"},
		{FilesystemMetricType, "filesystem"},
		{LoadMetricType, "load"},
		{TopTalkersMetricType, "toptalkers"},
	}

	for _, tt := range tests {
		t.Run(string(tt.metricType), func(t *testing.T) {
			if string(tt.metricType) != tt.want {
				t.Errorf("MetricType = %q, want %q", tt.metricType, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: LoadMetric
// ---------------------------------------------------------------------------

func TestNewLoadMetric(t *testing.T) {
	ts := time.Now()
	m := NewLoadMetric(ts, &pb.LoadMetrics{Load1: 0.5})

	if m.Type() != LoadMetricType {
		t.Errorf("Type() = %v, want %v", m.Type(), LoadMetricType)
	}
	if m.Timestamp() != ts {
		t.Errorf("Timestamp() = %v, want %v", m.Timestamp(), ts)
	}
	val := m.Value().(*pb.LoadMetrics)
	if val.GetLoad1() != 0.5 {
		t.Errorf("Load1 = %v, want 0.5", val.GetLoad1())
	}
}

func TestLoadMetric_Merge_SameType(t *testing.T) {
	ts := time.Now()
	m1 := NewLoadMetric(ts, &pb.LoadMetrics{Load1: 0.5})
	m2 := NewLoadMetric(ts.Add(time.Second), &pb.LoadMetrics{Load1: 0.7})

	merged := m1.Merge(m2)
	lm, ok := merged.(*LoadMetric)
	if !ok {
		t.Fatalf("Merge() returned %T, want *LoadMetric", merged)
	}

	// Average: (0.5 + 0.7) / 2 = 0.6
	expected := 0.6
	if lm.Metrics.GetLoad1() != expected {
		t.Errorf("Load1 = %v, want %v", lm.Metrics.GetLoad1(), expected)
	}
	// Timestamp should be from m1 (the receiver)
	if lm.Timestamp() != ts {
		t.Errorf("Timestamp = %v, want %v", lm.Timestamp(), ts)
	}
}

func TestLoadMetric_Merge_DifferentType(t *testing.T) {
	m1 := NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5})
	m2 := NewCpuMetric(time.Now(), &pb.CpuMetrics{})

	merged := m1.Merge(m2)
	if merged != m1 {
		t.Error("Merge() with different type should return receiver")
	}
}

func TestLoadMetric_Merge_Nil(t *testing.T) {
	m1 := NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5})

	merged := m1.Merge(nil)
	if merged != m1 {
		t.Error("Merge() with nil should return receiver")
	}
}

// ---------------------------------------------------------------------------
// Tests: CpuMetric
// ---------------------------------------------------------------------------

func TestNewCpuMetric(t *testing.T) {
	ts := time.Now()
	m := NewCpuMetric(ts, &pb.CpuMetrics{
		PercentUser:   30.0,
		PercentSystem: 15.0,
		PercentIdle:   55.0,
	})

	if m.Type() != CPUMetricType {
		t.Errorf("Type() = %v, want %v", m.Type(), CPUMetricType)
	}
	if m.Timestamp() != ts {
		t.Errorf("Timestamp() = %v, want %v", m.Timestamp(), ts)
	}
	val := m.Value().(*pb.CpuMetrics)
	if val.GetPercentUser() != 30.0 {
		t.Errorf("PercentUser = %v, want 30.0", val.GetPercentUser())
	}
}

func TestCpuMetric_Merge(t *testing.T) {
	m1 := NewCpuMetric(time.Now(), &pb.CpuMetrics{
		PercentUser:   30.0,
		PercentSystem: 10.0,
		PercentIdle:   60.0,
	})
	m2 := NewCpuMetric(time.Now(), &pb.CpuMetrics{
		PercentUser:   50.0,
		PercentSystem: 20.0,
		PercentIdle:   30.0,
	})

	merged := m1.Merge(m2)
	cm, ok := merged.(*CpuMetric)
	if !ok {
		t.Fatalf("Merge() returned %T, want *CpuMetric", merged)
	}

	if cm.Metrics.GetPercentUser() != 40.0 {
		t.Errorf("PercentUser = %v, want 40.0", cm.Metrics.GetPercentUser())
	}
	if cm.Metrics.GetPercentSystem() != 15.0 {
		t.Errorf("PercentSystem = %v, want 15.0", cm.Metrics.GetPercentSystem())
	}
	if cm.Metrics.GetPercentIdle() != 45.0 {
		t.Errorf("PercentIdle = %v, want 45.0", cm.Metrics.GetPercentIdle())
	}
}

func TestCpuMetric_Merge_DifferentType(t *testing.T) {
	m1 := NewCpuMetric(time.Now(), &pb.CpuMetrics{})
	m2 := NewLoadMetric(time.Now(), &pb.LoadMetrics{})

	merged := m1.Merge(m2)
	if merged != m1 {
		t.Error("Merge() with different type should return receiver")
	}
}

// ---------------------------------------------------------------------------
// Tests: DiskIOMetric
// ---------------------------------------------------------------------------

func TestNewDiskIOMetric(t *testing.T) {
	ts := time.Now()
	m := NewDiskIOMetric(ts, &pb.DiskIOMetrics{
		Tps:           100.0,
		KbTotalPerSec: 500.0,
	})

	if m.Type() != DiskMetricType {
		t.Errorf("Type() = %v, want %v", m.Type(), DiskMetricType)
	}
	val := m.Value().(*pb.DiskIOMetrics)
	if val.GetTps() != 100.0 {
		t.Errorf("Tps = %v, want 100.0", val.GetTps())
	}
}

func TestDiskIOMetric_Merge(t *testing.T) {
	m1 := NewDiskIOMetric(time.Now(), &pb.DiskIOMetrics{Tps: 100, KbTotalPerSec: 500})
	m2 := NewDiskIOMetric(time.Now(), &pb.DiskIOMetrics{Tps: 200, KbTotalPerSec: 700})

	merged := m1.Merge(m2)
	dm, ok := merged.(*DiskIOMetric)
	if !ok {
		t.Fatalf("Merge() returned %T, want *DiskIOMetric", merged)
	}

	if dm.Metrics.GetTps() != 150.0 {
		t.Errorf("Tps = %v, want 150.0", dm.Metrics.GetTps())
	}
	if dm.Metrics.GetKbTotalPerSec() != 600.0 {
		t.Errorf("KbTotalPerSec = %v, want 600.0", dm.Metrics.GetKbTotalPerSec())
	}
}

// ---------------------------------------------------------------------------
// Tests: FilesystemUsageMetric
// ---------------------------------------------------------------------------

func TestNewFilesystemUsageMetric(t *testing.T) {
	ts := time.Now()
	m := NewFilesystemUsageMetric(ts, &pb.FilesystemUsage{
		MountPoint:   "/",
		Filesystem:   "/dev/sda1",
		PercentUsed:  45.0,
		InodePercent: 30.0,
	})

	if m.Type() != FilesystemMetricType {
		t.Errorf("Type() = %v, want %v", m.Type(), FilesystemMetricType)
	}
	val := m.Value().(*pb.FilesystemUsage)
	if val.GetMountPoint() != "/" {
		t.Errorf("MountPoint = %q, want /", val.GetMountPoint())
	}
}

func TestFilesystemUsageMetric_Merge_Latest(t *testing.T) {
	ts1 := time.Now()
	ts2 := ts1.Add(time.Second)

	m1 := NewFilesystemUsageMetric(ts1, &pb.FilesystemUsage{
		MountPoint: "/", PercentUsed: 45.0,
	})
	m2 := NewFilesystemUsageMetric(ts2, &pb.FilesystemUsage{
		MountPoint: "/", PercentUsed: 50.0,
	})

	// m2 has later timestamp, so it should be returned
	merged := m1.Merge(m2)
	fm, ok := merged.(*FilesystemUsageMetric)
	if !ok {
		t.Fatalf("Merge() returned %T, want *FilesystemUsageMetric", merged)
	}
	if fm.Metrics.GetPercentUsed() != 50.0 {
		t.Errorf("PercentUsed = %v, want 50.0 (latest)", fm.Metrics.GetPercentUsed())
	}
}

func TestFilesystemUsageMetric_Merge_Earlier(t *testing.T) {
	ts1 := time.Now()
	ts2 := ts1.Add(-time.Second)

	m1 := NewFilesystemUsageMetric(ts1, &pb.FilesystemUsage{
		MountPoint: "/", PercentUsed: 45.0,
	})
	m2 := NewFilesystemUsageMetric(ts2, &pb.FilesystemUsage{
		MountPoint: "/", PercentUsed: 50.0,
	})

	// m1 has later timestamp, so it should be returned
	merged := m1.Merge(m2)
	fm, ok := merged.(*FilesystemUsageMetric)
	if !ok {
		t.Fatalf("Merge() returned %T, want *FilesystemUsageMetric", merged)
	}
	if fm.Metrics.GetPercentUsed() != 45.0 {
		t.Errorf("PercentUsed = %v, want 45.0 (latest)", fm.Metrics.GetPercentUsed())
	}
}

func TestFilesystemUsageMetric_Merge_SameTimestamp(t *testing.T) {
	ts := time.Now()

	m1 := NewFilesystemUsageMetric(ts, &pb.FilesystemUsage{
		MountPoint: "/", PercentUsed: 45.0,
	})
	m2 := NewFilesystemUsageMetric(ts, &pb.FilesystemUsage{
		MountPoint: "/", PercentUsed: 50.0,
	})

	// Same timestamp: Merge returns the "other" value when timestamps are equal
	// (m.ts.After(o.ts) returns false, so it falls through to return o)
	merged := m1.Merge(m2)
	fm, ok := merged.(*FilesystemUsageMetric)
	if !ok {
		t.Fatalf("Merge() returned %T, want *FilesystemUsageMetric", merged)
	}
	if fm.Metrics.GetPercentUsed() != 50.0 {
		t.Errorf("PercentUsed = %v, want 50.0 (other, latest wins)", fm.Metrics.GetPercentUsed())
	}
}

// ---------------------------------------------------------------------------
// Tests: NetworkMetric
// ---------------------------------------------------------------------------

func TestNewNetworkMetric(t *testing.T) {
	ts := time.Now()
	m := NewNetworkMetric(ts, &pb.NetworkMetrics{
		ListeningSockets:       10,
		EstablishedConnections: 50,
	})

	if m.Type() != NetworkMetricType {
		t.Errorf("Type() = %v, want %v", m.Type(), NetworkMetricType)
	}
	val := m.Value().(*pb.NetworkMetrics)
	if val.GetEstablishedConnections() != 50 {
		t.Errorf("EstablishedConnections = %d, want 50", val.GetEstablishedConnections())
	}
}

func TestNetworkMetric_Merge_Latest(t *testing.T) {
	ts1 := time.Now()
	ts2 := ts1.Add(time.Second)

	m1 := NewNetworkMetric(ts1, &pb.NetworkMetrics{EstablishedConnections: 50})
	m2 := NewNetworkMetric(ts2, &pb.NetworkMetrics{EstablishedConnections: 100})

	merged := m1.Merge(m2)
	nm, ok := merged.(*NetworkMetric)
	if !ok {
		t.Fatalf("Merge() returned %T, want *NetworkMetric", merged)
	}
	if nm.Metrics.GetEstablishedConnections() != 100 {
		t.Errorf("EstablishedConnections = %d, want 100 (latest)", nm.Metrics.GetEstablishedConnections())
	}
}

// ---------------------------------------------------------------------------
// Tests: TopTalkersMetric
// ---------------------------------------------------------------------------

func TestNewTopTalkersMetric(t *testing.T) {
	ts := time.Now()
	m := NewTopTalkersMetric(ts, &pb.TopTalkers{
		ByProtocol: []*pb.ProtocolTraffic{
			{Protocol: "TCP", BytesSent: 1000},
		},
	})

	if m.Type() != TopTalkersMetricType {
		t.Errorf("Type() = %v, want %v", m.Type(), TopTalkersMetricType)
	}
	val := m.Value().(*pb.TopTalkers)
	if len(val.GetByProtocol()) != 1 {
		t.Errorf("ByProtocol length = %d, want 1", len(val.GetByProtocol()))
	}
}

func TestTopTalkersMetric_Merge_Latest(t *testing.T) {
	ts1 := time.Now()
	ts2 := ts1.Add(time.Second)

	m1 := NewTopTalkersMetric(ts1, &pb.TopTalkers{
		ByProtocol: []*pb.ProtocolTraffic{{Protocol: "TCP", BytesSent: 1000}},
	})
	m2 := NewTopTalkersMetric(ts2, &pb.TopTalkers{
		ByProtocol: []*pb.ProtocolTraffic{{Protocol: "UDP", BytesSent: 2000}},
	})

	merged := m1.Merge(m2)
	tm, ok := merged.(*TopTalkersMetric)
	if !ok {
		t.Fatalf("Merge() returned %T, want *TopTalkersMetric", merged)
	}
	if len(tm.Metrics.GetByProtocol()) != 1 {
		t.Errorf("ByProtocol length = %d, want 1", len(tm.Metrics.GetByProtocol()))
	}
	if tm.Metrics.GetByProtocol()[0].GetProtocol() != "UDP" {
		t.Errorf("Protocol = %s, want UDP (latest)", tm.Metrics.GetByProtocol()[0].GetProtocol())
	}
}

// ---------------------------------------------------------------------------
// Tests: Edge cases
// ---------------------------------------------------------------------------

func TestLoadMetric_Merge_ZeroValues(t *testing.T) {
	m1 := NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0})
	m2 := NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0})

	merged := m1.Merge(m2)
	lm := merged.(*LoadMetric)
	if lm.Metrics.GetLoad1() != 0 {
		t.Errorf("Load1 = %v, want 0", lm.Metrics.GetLoad1())
	}
}

func TestCpuMetric_Merge_ZeroValues(t *testing.T) {
	m1 := NewCpuMetric(time.Now(), &pb.CpuMetrics{})
	m2 := NewCpuMetric(time.Now(), &pb.CpuMetrics{})

	merged := m1.Merge(m2)
	cm := merged.(*CpuMetric)
	if cm.Metrics.GetPercentUser() != 0 {
		t.Errorf("PercentUser = %v, want 0", cm.Metrics.GetPercentUser())
	}
}

func TestMetricInterface(t *testing.T) {
	// Verify all metric types implement the Metric interface
	var metrics []Metric

	metrics = append(metrics, NewLoadMetric(time.Now(), &pb.LoadMetrics{}))
	metrics = append(metrics, NewCpuMetric(time.Now(), &pb.CpuMetrics{}))
	metrics = append(metrics, NewDiskIOMetric(time.Now(), &pb.DiskIOMetrics{}))
	metrics = append(metrics, NewFilesystemUsageMetric(time.Now(), &pb.FilesystemUsage{}))
	metrics = append(metrics, NewNetworkMetric(time.Now(), &pb.NetworkMetrics{}))
	metrics = append(metrics, NewTopTalkersMetric(time.Now(), &pb.TopTalkers{}))

	if len(metrics) != 6 {
		t.Errorf("metrics count = %d, want 6", len(metrics))
	}

	// Verify each has a unique type
	types := make(map[MetricType]bool)
	for _, m := range metrics {
		if types[m.Type()] {
			t.Errorf("duplicate metric type: %v", m.Type())
		}
		types[m.Type()] = true
	}
}

func TestMerge_NilOther(t *testing.T) {
	// All Merge methods should handle nil gracefully
	metrics := []Metric{
		NewLoadMetric(time.Now(), &pb.LoadMetrics{}),
		NewCpuMetric(time.Now(), &pb.CpuMetrics{}),
		NewDiskIOMetric(time.Now(), &pb.DiskIOMetrics{}),
		NewFilesystemUsageMetric(time.Now(), &pb.FilesystemUsage{}),
		NewNetworkMetric(time.Now(), &pb.NetworkMetrics{}),
		NewTopTalkersMetric(time.Now(), &pb.TopTalkers{}),
	}

	for _, m := range metrics {
		merged := m.Merge(nil)
		if merged != m {
			t.Errorf("%v.Merge(nil) should return receiver", m.Type())
		}
	}
}

func TestValue_TypeAssertion(t *testing.T) {
	// Verify Value() returns the correct underlying proto type
	tests := []struct {
		metric    Metric
		protoType interface{}
	}{
		{NewLoadMetric(time.Now(), &pb.LoadMetrics{}), &pb.LoadMetrics{}},
		{NewCpuMetric(time.Now(), &pb.CpuMetrics{}), &pb.CpuMetrics{}},
		{NewDiskIOMetric(time.Now(), &pb.DiskIOMetrics{}), &pb.DiskIOMetrics{}},
		{NewFilesystemUsageMetric(time.Now(), &pb.FilesystemUsage{}), &pb.FilesystemUsage{}},
		{NewNetworkMetric(time.Now(), &pb.NetworkMetrics{}), &pb.NetworkMetrics{}},
		{NewTopTalkersMetric(time.Now(), &pb.TopTalkers{}), &pb.TopTalkers{}},
	}

	for _, tt := range tests {
		t.Run(string(tt.metric.Type()), func(t *testing.T) {
			val := tt.metric.Value()
			switch val.(type) {
			case *pb.LoadMetrics, *pb.CpuMetrics, *pb.DiskIOMetrics, *pb.FilesystemUsage, *pb.NetworkMetrics, *pb.TopTalkers:
				// OK
			default:
				t.Errorf("Value() returned unexpected type %T", val)
			}
		})
	}
}
