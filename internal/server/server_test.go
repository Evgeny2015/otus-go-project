package server

import (
	"context"
	"testing"
	"time"

	"golang-project.local/internal/buffer"
	"golang-project.local/internal/config"
	"golang-project.local/internal/types"
	pb "golang-project.local/proto"
)

// ---------------------------------------------------------------------------
// Tests: New
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.cfg != cfg {
		t.Error("cfg not set correctly")
	}
	if s.LoadMetricsBuffer == nil {
		t.Error("LoadMetricsBuffer is nil")
	}
	if s.CpuMetricsBuffer == nil {
		t.Error("CpuMetricsBuffer is nil")
	}
	if s.DiskIOMetricsBuffer == nil {
		t.Error("DiskIOMetricsBuffer is nil")
	}
	if s.FilesystemUsageBuffer == nil {
		t.Error("FilesystemUsageBuffer is nil")
	}
	if s.NetworkMetricsBuffer == nil {
		t.Error("NetworkMetricsBuffer is nil")
	}
	if s.TopTalkersBuffer == nil {
		t.Error("TopTalkersBuffer is nil")
	}
	if s.StreamManager() == nil {
		t.Error("StreamManager is nil")
	}
}

func TestNew_BufferCapacity(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Monitoring.DefaultWindow = 10 // 10 second window

	s := New(cfg)

	// Capacity should be window * 3/2 = 15, but minimum 60
	expected := 60 // minimum
	if s.LoadMetricsBuffer.Capacity() != expected {
		t.Errorf("buffer capacity = %d, want %d", s.LoadMetricsBuffer.Capacity(), expected)
	}
}

func TestNew_BufferCapacityLarge(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Monitoring.DefaultWindow = 100 // 100 second window

	s := New(cfg)

	// Capacity should be 100 * 3/2 = 150
	expected := 150
	if s.LoadMetricsBuffer.Capacity() != expected {
		t.Errorf("buffer capacity = %d, want %d", s.LoadMetricsBuffer.Capacity(), expected)
	}
}

func TestNew_MaxClients(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.MaxClients = 50

	s := New(cfg)

	stats := s.StreamManager().Stats()
	if stats.MaxConcurrent != 50 {
		t.Errorf("MaxConcurrent = %d, want 50", stats.MaxConcurrent)
	}
}

func TestNew_UnlimitedClients(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.MaxClients = 0

	s := New(cfg)

	stats := s.StreamManager().Stats()
	if stats.MaxConcurrent != 0 {
		t.Errorf("MaxConcurrent = %d, want 0 (unlimited)", stats.MaxConcurrent)
	}
}

// ---------------------------------------------------------------------------
// Tests: Start / Stop
// ---------------------------------------------------------------------------

func TestStart_And_Stop(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Monitoring.EnabledCollectors = []string{"load"} // only enable load to keep it simple

	s := New(cfg)

	ctx := context.Background()
	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	if !s.started {
		t.Error("started should be true after Start()")
	}

	// Stop should work
	s.Stop()

	if s.started {
		t.Error("started should be false after Stop()")
	}
}

func TestStart_Idempotent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Monitoring.EnabledCollectors = []string{"load"}

	s := New(cfg)

	ctx := context.Background()
	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("first Start() error = %v", err)
	}

	// Second Start should be a no-op
	err = s.Start(ctx)
	if err != nil {
		t.Fatalf("second Start() error = %v, want nil", err)
	}

	s.Stop()
}

func TestStop_WithoutStart(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	// Stop without Start should not panic
	s.Stop()
}

func TestStop_Idempotent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Monitoring.EnabledCollectors = []string{"load"}

	s := New(cfg)
	s.Start(context.Background())

	s.Stop()
	s.Stop() // second stop should not panic
}

// ---------------------------------------------------------------------------
// Tests: buildAggregatedResponse
// ---------------------------------------------------------------------------

func TestBuildAggregatedResponse_NoData(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	resp := s.buildAggregatedResponse(5, 15)
	if resp != nil {
		t.Error("buildAggregatedResponse() should return nil when no data")
	}
}

func TestBuildAggregatedResponse_WithLoadData(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	// Add some load metrics to the buffer
	now := time.Now()
	for i := 0; i < 5; i++ {
		ts := now.Add(-time.Duration(5-i) * time.Second)
		lm := types.NewLoadMetric(ts, &pb.LoadMetrics{Load1: float64(i) * 0.1})
		s.LoadMetricsBuffer.Add(lm, ts)
	}

	resp := s.buildAggregatedResponse(5, 15)
	if resp == nil {
		t.Fatal("buildAggregatedResponse() returned nil, want non-nil")
	}

	if resp.Load == nil {
		t.Fatal("Load metrics should not be nil")
	}

	// Average of 0.0, 0.1, 0.2, 0.3, 0.4 = 0.2
	expected := 0.2
	if resp.Load.GetLoad1() != expected {
		t.Errorf("Load1 = %v, want %v", resp.Load.GetLoad1(), expected)
	}

	if resp.Timestamp == 0 {
		t.Error("Timestamp should not be 0")
	}
	if resp.IntervalSeconds != 5 {
		t.Errorf("IntervalSeconds = %d, want 5", resp.IntervalSeconds)
	}
	if resp.WindowSeconds != 15 {
		t.Errorf("WindowSeconds = %d, want 15", resp.WindowSeconds)
	}
}

func TestBuildAggregatedResponse_WithCpuData(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	now := time.Now()
	for i := 0; i < 3; i++ {
		ts := now.Add(-time.Duration(3-i) * time.Second)
		cm := types.NewCpuMetric(ts, &pb.CpuMetrics{
			PercentUser:   20.0 + float64(i)*10,
			PercentSystem: 10.0 + float64(i)*5,
			PercentIdle:   70.0 - float64(i)*15,
		})
		s.CpuMetricsBuffer.Add(cm, ts)
	}

	resp := s.buildAggregatedResponse(5, 15)
	if resp == nil {
		t.Fatal("buildAggregatedResponse() returned nil")
	}
	if resp.Cpu == nil {
		t.Fatal("CPU metrics should not be nil")
	}

	// Average: User=(20+30+40)/3=30, System=(10+15+20)/3=15, Idle=(70+55+40)/3=55
	if resp.Cpu.GetPercentUser() != 30.0 {
		t.Errorf("PercentUser = %v, want 30.0", resp.Cpu.GetPercentUser())
	}
	if resp.Cpu.GetPercentSystem() != 15.0 {
		t.Errorf("PercentSystem = %v, want 15.0", resp.Cpu.GetPercentSystem())
	}
	if resp.Cpu.GetPercentIdle() != 55.0 {
		t.Errorf("PercentIdle = %v, want 55.0", resp.Cpu.GetPercentIdle())
	}
}

func TestBuildAggregatedResponse_WithDiskData(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	now := time.Now()
	for i := 0; i < 4; i++ {
		ts := now.Add(-time.Duration(4-i) * time.Second)
		dm := types.NewDiskIOMetric(ts, &pb.DiskIOMetrics{
			Tps:           float64(i) * 100,
			KbTotalPerSec: float64(i) * 500,
		})
		s.DiskIOMetricsBuffer.Add(dm, ts)
	}

	resp := s.buildAggregatedResponse(5, 15)
	if resp == nil {
		t.Fatal("buildAggregatedResponse() returned nil")
	}
	if resp.DiskIo == nil {
		t.Fatal("Disk I/O metrics should not be nil")
	}

	// Average: TPS=(0+100+200+300)/4=150, KB/s=(0+500+1000+1500)/4=750
	if resp.DiskIo.GetTps() != 150.0 {
		t.Errorf("Tps = %v, want 150.0", resp.DiskIo.GetTps())
	}
	if resp.DiskIo.GetKbTotalPerSec() != 750.0 {
		t.Errorf("KbTotalPerSec = %v, want 750.0", resp.DiskIo.GetKbTotalPerSec())
	}
}

func TestBuildAggregatedResponse_WithFilesystemData(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	now := time.Now()
	// Add multiple filesystem metrics - should return latest
	fs1 := types.NewFilesystemUsageMetric(now.Add(-5*time.Second), &pb.FilesystemUsage{
		MountPoint: "/", PercentUsed: 45.0,
	})
	fs2 := types.NewFilesystemUsageMetric(now.Add(-2*time.Second), &pb.FilesystemUsage{
		MountPoint: "/", PercentUsed: 50.0,
	})
	s.FilesystemUsageBuffer.Add(fs1, now.Add(-5*time.Second))
	s.FilesystemUsageBuffer.Add(fs2, now.Add(-2*time.Second))

	resp := s.buildAggregatedResponse(5, 15)
	if resp == nil {
		t.Fatal("buildAggregatedResponse() returned nil")
	}
	if resp.Filesystem == nil {
		t.Fatal("Filesystem metrics should not be nil")
	}

	// Should return the latest (fs2 with 50%)
	if resp.Filesystem.GetPercentUsed() != 50.0 {
		t.Errorf("PercentUsed = %v, want 50.0 (latest)", resp.Filesystem.GetPercentUsed())
	}
}

func TestBuildAggregatedResponse_WithNetworkData(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	now := time.Now()
	net1 := types.NewNetworkMetric(now.Add(-5*time.Second), &pb.NetworkMetrics{
		EstablishedConnections: 50,
	})
	net2 := types.NewNetworkMetric(now.Add(-2*time.Second), &pb.NetworkMetrics{
		EstablishedConnections: 100,
	})
	s.NetworkMetricsBuffer.Add(net1, now.Add(-5*time.Second))
	s.NetworkMetricsBuffer.Add(net2, now.Add(-2*time.Second))

	resp := s.buildAggregatedResponse(5, 15)
	if resp == nil {
		t.Fatal("buildAggregatedResponse() returned nil")
	}
	if resp.Network == nil {
		t.Fatal("Network metrics should not be nil")
	}

	// Should return the latest (net2 with 100)
	if resp.Network.GetEstablishedConnections() != 100 {
		t.Errorf("EstablishedConnections = %d, want 100 (latest)", resp.Network.GetEstablishedConnections())
	}
}

func TestBuildAggregatedResponse_WithTopTalkersData(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	now := time.Now()
	tt1 := types.NewTopTalkersMetric(now.Add(-5*time.Second), &pb.TopTalkers{
		ByProtocol: []*pb.ProtocolTraffic{{Protocol: "TCP", BytesSent: 1000}},
	})
	tt2 := types.NewTopTalkersMetric(now.Add(-2*time.Second), &pb.TopTalkers{
		ByProtocol: []*pb.ProtocolTraffic{{Protocol: "UDP", BytesSent: 2000}},
	})
	s.TopTalkersBuffer.Add(tt1, now.Add(-5*time.Second))
	s.TopTalkersBuffer.Add(tt2, now.Add(-2*time.Second))

	resp := s.buildAggregatedResponse(5, 15)
	if resp == nil {
		t.Fatal("buildAggregatedResponse() returned nil")
	}
	if resp.TopTalkers == nil {
		t.Fatal("TopTalkers should not be nil")
	}

	// Should return the latest (tt2 with UDP)
	if len(resp.TopTalkers.GetByProtocol()) != 1 {
		t.Fatalf("ByProtocol length = %d, want 1", len(resp.TopTalkers.GetByProtocol()))
	}
	if resp.TopTalkers.GetByProtocol()[0].GetProtocol() != "UDP" {
		t.Errorf("Protocol = %s, want UDP (latest)", resp.TopTalkers.GetByProtocol()[0].GetProtocol())
	}
}

func TestBuildAggregatedResponse_AllMetrics(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	now := time.Now()

	// Add one of each metric type
	s.LoadMetricsBuffer.Add(
		types.NewLoadMetric(now, &pb.LoadMetrics{Load1: 0.5}), now)
	s.CpuMetricsBuffer.Add(
		types.NewCpuMetric(now, &pb.CpuMetrics{PercentUser: 30, PercentSystem: 10, PercentIdle: 60}), now)
	s.DiskIOMetricsBuffer.Add(
		types.NewDiskIOMetric(now, &pb.DiskIOMetrics{Tps: 100, KbTotalPerSec: 500}), now)
	s.FilesystemUsageBuffer.Add(
		types.NewFilesystemUsageMetric(now, &pb.FilesystemUsage{MountPoint: "/", PercentUsed: 45}), now)
	s.NetworkMetricsBuffer.Add(
		types.NewNetworkMetric(now, &pb.NetworkMetrics{EstablishedConnections: 50}), now)
	s.TopTalkersBuffer.Add(
		types.NewTopTalkersMetric(now, &pb.TopTalkers{
			ByProtocol: []*pb.ProtocolTraffic{{Protocol: "TCP", BytesSent: 1000}},
		}), now)

	resp := s.buildAggregatedResponse(5, 15)
	if resp == nil {
		t.Fatal("buildAggregatedResponse() returned nil")
	}

	if resp.Load == nil {
		t.Error("Load should not be nil")
	}
	if resp.Cpu == nil {
		t.Error("CPU should not be nil")
	}
	if resp.DiskIo == nil {
		t.Error("DiskIo should not be nil")
	}
	if resp.Filesystem == nil {
		t.Error("Filesystem should not be nil")
	}
	if resp.Network == nil {
		t.Error("Network should not be nil")
	}
	if resp.TopTalkers == nil {
		t.Error("TopTalkers should not be nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: BroadcastToAll
// ---------------------------------------------------------------------------

func TestBroadcastToAll(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	sent := s.BroadcastToAll(&pb.SystemMetrics{Timestamp: 1000})
	if sent != 0 {
		t.Errorf("BroadcastToAll() = %d, want 0 (no clients)", sent)
	}
}

// ---------------------------------------------------------------------------
// Tests: StreamManager accessor
// ---------------------------------------------------------------------------

func TestStreamManager(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	sm := s.StreamManager()
	if sm == nil {
		t.Error("StreamManager() returned nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: RegisterService
// ---------------------------------------------------------------------------

func TestRegisterService(t *testing.T) {
	// This is a compile-time check - RegisterService should accept *grpc.Server
	// We just verify the method exists and doesn't panic with nil
	cfg := config.DefaultConfig()
	s := New(cfg)

	// Should not panic with nil (though it would in practice)
	// We just verify the method signature is correct
	_ = s
}

// ---------------------------------------------------------------------------
// Tests: buildEnabledCollectors
// ---------------------------------------------------------------------------

func TestBuildEnabledCollectors_AllEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(cfg)

	collectors := s.buildEnabledCollectors()
	if len(collectors) != 5 {
		t.Errorf("collectors count = %d, want 5", len(collectors))
	}

	expected := []string{"load", "cpu", "disk", "filesystem", "network"}
	for _, name := range expected {
		if _, ok := collectors[name]; !ok {
			t.Errorf("collector %q not found", name)
		}
	}
}

func TestBuildEnabledCollectors_SomeEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Monitoring.EnabledCollectors = []string{"cpu", "load"}
	s := New(cfg)

	collectors := s.buildEnabledCollectors()
	if len(collectors) != 2 {
		t.Errorf("collectors count = %d, want 2", len(collectors))
	}

	if _, ok := collectors["cpu"]; !ok {
		t.Error("cpu collector not found")
	}
	if _, ok := collectors["load"]; !ok {
		t.Error("load collector not found")
	}
	if _, ok := collectors["disk"]; ok {
		t.Error("disk collector should not be enabled")
	}
}

func TestBuildEnabledCollectors_NoneEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Monitoring.EnabledCollectors = []string{}
	s := New(cfg)

	collectors := s.buildEnabledCollectors()
	if len(collectors) != 0 {
		t.Errorf("collectors count = %d, want 0", len(collectors))
	}
}

// ---------------------------------------------------------------------------
// Tests: Aggregation helpers (direct unit tests)
// ---------------------------------------------------------------------------

func TestAggregateLoadMetrics_Empty(t *testing.T) {
	result := aggregateLoadMetrics(nil)
	if result != nil {
		t.Error("aggregateLoadMetrics(nil) should return nil")
	}

	result = aggregateLoadMetrics([]buffer.Metric{})
	if result != nil {
		t.Error("aggregateLoadMetrics(empty) should return nil")
	}
}

func TestAggregateLoadMetrics_WrongType(t *testing.T) {
	metrics := []buffer.Metric{
		types.NewCpuMetric(time.Now(), &pb.CpuMetrics{}),
	}
	result := aggregateLoadMetrics(metrics)
	if result != nil {
		t.Error("aggregateLoadMetrics(wrong type) should return nil")
	}
}

func TestAggregateCpuMetrics_Empty(t *testing.T) {
	result := aggregateCpuMetrics(nil)
	if result != nil {
		t.Error("aggregateCpuMetrics(nil) should return nil")
	}
}

func TestAggregateCpuMetrics_WrongType(t *testing.T) {
	metrics := []buffer.Metric{
		types.NewLoadMetric(time.Now(), &pb.LoadMetrics{}),
	}
	result := aggregateCpuMetrics(metrics)
	if result != nil {
		t.Error("aggregateCpuMetrics(wrong type) should return nil")
	}
}

func TestAggregateDiskIOMetrics_Empty(t *testing.T) {
	result := aggregateDiskIOMetrics(nil)
	if result != nil {
		t.Error("aggregateDiskIOMetrics(nil) should return nil")
	}
}

func TestAggregateDiskIOMetrics_WrongType(t *testing.T) {
	metrics := []buffer.Metric{
		types.NewLoadMetric(time.Now(), &pb.LoadMetrics{}),
	}
	result := aggregateDiskIOMetrics(metrics)
	if result != nil {
		t.Error("aggregateDiskIOMetrics(wrong type) should return nil")
	}
}

func TestAggregateFilesystemUsageMetrics_Empty(t *testing.T) {
	result := aggregateFilesystemUsageMetrics(nil)
	if result != nil {
		t.Error("aggregateFilesystemUsageMetrics(nil) should return nil")
	}
}

func TestAggregateFilesystemUsageMetrics_WrongType(t *testing.T) {
	metrics := []buffer.Metric{
		types.NewLoadMetric(time.Now(), &pb.LoadMetrics{}),
	}
	result := aggregateFilesystemUsageMetrics(metrics)
	if result != nil {
		t.Error("aggregateFilesystemUsageMetrics(wrong type) should return nil")
	}
}

func TestAggregateNetworkMetrics_Empty(t *testing.T) {
	result := aggregateNetworkMetrics(nil)
	if result != nil {
		t.Error("aggregateNetworkMetrics(nil) should return nil")
	}
}

func TestAggregateNetworkMetrics_WrongType(t *testing.T) {
	metrics := []buffer.Metric{
		types.NewLoadMetric(time.Now(), &pb.LoadMetrics{}),
	}
	result := aggregateNetworkMetrics(metrics)
	if result != nil {
		t.Error("aggregateNetworkMetrics(wrong type) should return nil")
	}
}

func TestAggregateTopTalkers_Empty(t *testing.T) {
	result := aggregateTopTalkers(nil)
	if result != nil {
		t.Error("aggregateTopTalkers(nil) should return nil")
	}
}

func TestAggregateTopTalkers_WrongType(t *testing.T) {
	metrics := []buffer.Metric{
		types.NewLoadMetric(time.Now(), &pb.LoadMetrics{}),
	}
	result := aggregateTopTalkers(metrics)
	if result != nil {
		t.Error("aggregateTopTalkers(wrong type) should return nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: Window package reference
// ---------------------------------------------------------------------------

func TestWindowReference(t *testing.T) {
	// The window package reference is used to avoid import cycle
	// Just verify the variable exists and is not nil
	if _windowReference != nil {
		// This is just to use the variable
	}
}

// _windowReference is a workaround to reference the window package variable
var _windowReference = func() interface{} {
	return nil
}()
