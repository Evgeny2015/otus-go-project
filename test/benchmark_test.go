// Package integration provides performance benchmarks for the system monitoring
// daemon. These benchmarks measure:
//   - Memory usage under load (circular buffer with many entries)
//   - Concurrent client handling (stream manager with many clients)
//   - Collection latency (scheduler tick duration)
//   - Aggregation performance (sliding window calculations)
//   - Serialization/deserialization overhead
//
// Run benchmarks with:
//
//	go test -bench=. -benchmem ./test/...
package integration

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"golang-project.local/internal/buffer"
	"golang-project.local/internal/config"
	"golang-project.local/internal/scheduler"
	"golang-project.local/internal/server"
	"golang-project.local/internal/stream"
	"golang-project.local/internal/types"
	pb "golang-project.local/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// ---------------------------------------------------------------------------
// Benchmark: CircularBuffer throughput
// ---------------------------------------------------------------------------

func BenchmarkCircularBuffer_Add(b *testing.B) {
	cb := buffer.NewCircularBuffer(10000)
	now := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := types.NewLoadMetric(now, &pb.LoadMetrics{Load1: 0.5})
		cb.Add(m, now)
	}
}

func BenchmarkCircularBuffer_GetWindow(b *testing.B) {
	cb := buffer.NewCircularBuffer(10000)
	now := time.Now()

	// Pre-fill buffer
	for i := 0; i < 1000; i++ {
		m := types.NewLoadMetric(now.Add(-time.Duration(i)*time.Second), &pb.LoadMetrics{Load1: 0.5})
		cb.Add(m, now.Add(-time.Duration(i)*time.Second))
	}

	from := now.Add(-100 * time.Second)
	to := now

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.GetWindow(from, to)
	}
}

func BenchmarkCircularBuffer_GetLatest(b *testing.B) {
	cb := buffer.NewCircularBuffer(10000)
	now := time.Now()

	for i := 0; i < 1000; i++ {
		m := types.NewLoadMetric(now.Add(-time.Duration(i)*time.Second), &pb.LoadMetrics{Load1: 0.5})
		cb.Add(m, now.Add(-time.Duration(i)*time.Second))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.GetLatest(10)
	}
}

// ---------------------------------------------------------------------------
// Benchmark: StreamManager broadcast
// ---------------------------------------------------------------------------

func BenchmarkStreamManager_Broadcast(b *testing.B) {
	sm := stream.NewStreamManager()

	// Register N clients
	const numClients = 10
	for i := 0; i < numClients; i++ {
		ms := newBenchMockStream()
		ms.sendFn = func(m *pb.SystemMetrics) error {
			return nil
		}
		sm.Register(ms, 5, 15)
	}

	metrics := &pb.SystemMetrics{
		Timestamp:       uint64(time.Now().UnixNano()),
		IntervalSeconds: 5,
		WindowSeconds:   15,
		Load:            &pb.LoadMetrics{Load1: 0.5},
		Cpu:             &pb.CpuMetrics{PercentUser: 30, PercentSystem: 10, PercentIdle: 60},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.Broadcast(metrics)
	}
}

func BenchmarkStreamManager_BroadcastAsync(b *testing.B) {
	sm := stream.NewStreamManager()

	const numClients = 10
	for i := 0; i < numClients; i++ {
		ms := newBenchMockStream()
		ms.sendFn = func(m *pb.SystemMetrics) error {
			return nil
		}
		sm.Register(ms, 5, 15)
	}

	metrics := &pb.SystemMetrics{
		Timestamp: uint64(time.Now().UnixNano()),
		Load:      &pb.LoadMetrics{Load1: 0.5},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.BroadcastAsync(metrics)
	}
}

// ---------------------------------------------------------------------------
// Benchmark: Scheduler collection
// ---------------------------------------------------------------------------

func BenchmarkScheduler_CollectTick(b *testing.B) {
	cb := buffer.NewCircularBuffer(10000)

	collectors := make([]scheduler.Collector, 6)
	for i, name := range []string{"load", "cpu", "disk", "filesystem", "network", "toptalkers"} {
		collectors[i] = newBenchCollector(name)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchCollectTick(collectors, cb, context.Background())
	}
}

// benchCollectTick simulates the collectTick logic for benchmarking.
// It calls Collect on each enabled collector and adds results to the buffer.
func benchCollectTick(collectors []scheduler.Collector, buf buffer.MetricBuffer, ctx context.Context) {
	var wg sync.WaitGroup
	for _, c := range collectors {
		if !c.Enabled() {
			continue
		}
		wg.Add(1)
		go func(col scheduler.Collector) {
			defer wg.Done()
			metric, err := col.Collect(ctx)
			if err == nil && metric != nil {
				buf.Add(metric, time.Now())
			}
		}(c)
	}
	wg.Wait()
}

// benchCollector is a fast mock collector for benchmarking.
type benchCollector struct {
	name string
}

func newBenchCollector(name string) *benchCollector {
	return &benchCollector{name: name}
}

func (c *benchCollector) Name() string  { return c.name }
func (c *benchCollector) Enabled() bool { return true }

func (c *benchCollector) Collect(ctx context.Context) (types.Metric, error) {
	now := time.Now()
	switch c.name {
	case "load":
		return types.NewLoadMetric(now, &pb.LoadMetrics{Load1: 0.5}), nil
	case "cpu":
		return types.NewCpuMetric(now, &pb.CpuMetrics{PercentUser: 30, PercentSystem: 10, PercentIdle: 60}), nil
	case "disk":
		return types.NewDiskIOMetric(now, &pb.DiskIOMetrics{Tps: 100, KbTotalPerSec: 500}), nil
	case "filesystem":
		return types.NewFilesystemUsageMetric(now, &pb.FilesystemUsage{MountPoint: "/", PercentUsed: 45}), nil
	case "network":
		return types.NewNetworkMetric(now, &pb.NetworkMetrics{EstablishedConnections: 50}), nil
	case "toptalkers":
		return types.NewTopTalkersMetric(now, &pb.TopTalkers{
			ByProtocol: []*pb.ProtocolTraffic{{Protocol: "TCP", BytesSent: 1000}},
		}), nil
	default:
		return types.NewLoadMetric(now, &pb.LoadMetrics{Load1: 0.5}), nil
	}
}

// ---------------------------------------------------------------------------
// Benchmark: Metric Merge operations
// ---------------------------------------------------------------------------

func BenchmarkMetricMerge_Load(b *testing.B) {
	m1 := types.NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5})
	m2 := types.NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.7})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m1.Merge(m2)
	}
}

func BenchmarkMetricMerge_CPU(b *testing.B) {
	m1 := types.NewCpuMetric(time.Now(), &pb.CpuMetrics{PercentUser: 30, PercentSystem: 10, PercentIdle: 60})
	m2 := types.NewCpuMetric(time.Now(), &pb.CpuMetrics{PercentUser: 50, PercentSystem: 20, PercentIdle: 30})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m1.Merge(m2)
	}
}

func BenchmarkMetricMerge_Filesystem(b *testing.B) {
	m1 := types.NewFilesystemUsageMetric(time.Now(), &pb.FilesystemUsage{MountPoint: "/", PercentUsed: 45})
	m2 := types.NewFilesystemUsageMetric(time.Now().Add(time.Second), &pb.FilesystemUsage{MountPoint: "/", PercentUsed: 50})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m1.Merge(m2)
	}
}

// ---------------------------------------------------------------------------
// Benchmark: StreamManager concurrent operations
// ---------------------------------------------------------------------------

func BenchmarkStreamManager_ConcurrentRegister(b *testing.B) {
	sm := stream.NewStreamManager()

	b.ResetTimer()
	b.RunParallel(func(pb2 *testing.PB) {
		for pb2.Next() {
			ms := newBenchMockStream()
			ms.sendFn = func(m *pb.SystemMetrics) error {
				return nil
			}
			cs, err := sm.Register(ms, 5, 15)
			if err == nil {
				sm.Unregister(cs)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Benchmark: gRPC streaming (requires running server)
// ---------------------------------------------------------------------------

func BenchmarkGRPCStreaming(b *testing.B) {
	cfg := config.DefaultConfig()
	cfg.Monitoring.EnabledCollectors = []string{"load", "cpu"}

	s := NewBenchServer(cfg)

	// Create gRPC server and register the service
	grpcServer := grpc.NewServer()
	s.RegisterService(grpcServer)

	// Listen on random port
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		b.Fatalf("failed to listen: %v", err)
	}

	// Start gRPC server in background
	go func() {
		_ = grpcServer.Serve(lis)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Start(ctx); err != nil {
		b.Fatalf("Start() error: %v", err)
	}

	addr := lis.Addr().String()

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		b.Fatalf("failed to create gRPC client: %v", err)
	}
	defer conn.Close()

	client := pb.NewStreamServiceClient(conn)

	streamCtx, streamCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer streamCancel()

	stream, err := client.StreamMetrics(streamCtx, &pb.MetricRequest{
		IntervalSeconds: 1,
		WindowSeconds:   5,
	})
	if err != nil {
		b.Fatalf("StreamMetrics() error: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := stream.Recv()
		if err != nil {
			b.Fatalf("Recv() error: %v", err)
		}
	}

	grpcServer.GracefulStop()
	s.Stop()
}

// NewBenchServer creates a server for benchmarking.
func NewBenchServer(cfg *config.Config) *server.Server {
	return server.New(cfg)
}

// ---------------------------------------------------------------------------
// Benchmark: Memory usage under load
// ---------------------------------------------------------------------------

func BenchmarkMemoryUsage(b *testing.B) {
	b.ReportAllocs()

	cb := buffer.NewCircularBuffer(10000)
	now := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := types.NewLoadMetric(now.Add(time.Duration(i)*time.Second), &pb.LoadMetrics{Load1: float64(i % 100)})
		cb.Add(m, now.Add(time.Duration(i)*time.Second))
	}
}

// ---------------------------------------------------------------------------
// Benchmark: Concurrent buffer access
// ---------------------------------------------------------------------------

func BenchmarkConcurrentBufferAccess(b *testing.B) {
	cb := buffer.NewCircularBuffer(10000)
	now := time.Now()

	// Pre-fill
	for i := 0; i < 1000; i++ {
		m := types.NewLoadMetric(now.Add(-time.Duration(i)*time.Second), &pb.LoadMetrics{Load1: 0.5})
		cb.Add(m, now.Add(-time.Duration(i)*time.Second))
	}

	b.ResetTimer()
	b.RunParallel(func(pb2 *testing.PB) {
		for pb2.Next() {
			cb.GetWindow(now.Add(-100*time.Second), now)
		}
	})
}

// ---------------------------------------------------------------------------
// Benchmark: StreamManager memory with many clients
// ---------------------------------------------------------------------------

func BenchmarkStreamManager_ManyClients(b *testing.B) {
	for _, numClients := range []int{10, 50, 100} {
		b.Run(fmt.Sprintf("clients=%d", numClients), func(b *testing.B) {
			sm := stream.NewStreamManager()

			// Register clients
			streams := make([]*benchMockStream, numClients)
			for i := 0; i < numClients; i++ {
				ms := newBenchMockStream()
				ms.sendFn = func(m *pb.SystemMetrics) error {
					return nil
				}
				streams[i] = ms
				sm.Register(ms, 5, 15)
			}

			metrics := &pb.SystemMetrics{
				Timestamp: uint64(time.Now().UnixNano()),
				Load:      &pb.LoadMetrics{Load1: 0.5},
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sm.Broadcast(metrics)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Benchmark: Worker pool throughput
// ---------------------------------------------------------------------------

func BenchmarkWorkerPool_Throughput(b *testing.B) {
	wp := scheduler.NewWorkerPool(4)
	defer wp.Stop()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb2 *testing.PB) {
		for pb2.Next() {
			mc := newBenchCollector("load")
			resultCh := wp.Submit(ctx, mc)
			<-resultCh
		}
	})
}

// ---------------------------------------------------------------------------
// Helper: benchMockStream for benchmarks
// ---------------------------------------------------------------------------

type benchMockStream struct {
	ctx    context.Context
	sendFn func(m *pb.SystemMetrics) error
}

func newBenchMockStream() *benchMockStream {
	return &benchMockStream{
		ctx: context.Background(),
	}
}

func (m *benchMockStream) Send(metrics *pb.SystemMetrics) error {
	if m.sendFn != nil {
		return m.sendFn(metrics)
	}
	return nil
}

func (m *benchMockStream) Context() context.Context {
	return m.ctx
}

func (m *benchMockStream) RecvMsg(msg interface{}) error {
	return nil
}

func (m *benchMockStream) SendMsg(msg interface{}) error {
	return nil
}

func (m *benchMockStream) SetHeader(md metadata.MD) error {
	return nil
}

func (m *benchMockStream) SendHeader(md metadata.MD) error {
	return nil
}

func (m *benchMockStream) SetTrailer(md metadata.MD) {
}

// ---------------------------------------------------------------------------
// Benchmark: Scheduler with worker pool
// ---------------------------------------------------------------------------

func BenchmarkSchedulerWithWorkerPool(b *testing.B) {
	wp := scheduler.NewWorkerPool(4)
	defer wp.Stop()

	collectors := make([]scheduler.Collector, 6)
	for i, name := range []string{"load", "cpu", "disk", "filesystem", "network", "toptalkers"} {
		collectors[i] = newBenchCollector(name)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for _, col := range collectors {
			if !col.Enabled() {
				continue
			}
			wg.Add(1)
			go func(c scheduler.Collector) {
				defer wg.Done()
				resultCh := wp.Submit(ctx, c)
				<-resultCh // Just wait for completion, don't inspect fields
			}(col)
		}
		wg.Wait()
	}
}
