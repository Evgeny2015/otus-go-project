package server

import (
	"context"
	"log"
	"time"

	"golang-project.local/internal/buffer"
	"golang-project.local/internal/collector"
	"golang-project.local/internal/scheduler"
	"golang-project.local/internal/types"
	pb "golang-project.local/proto"
	"google.golang.org/grpc"
)

// Server implements the gRPC server logic
type Server struct {
	pb.UnimplementedStreamServiceServer

	LoadMetricsBuffer     *buffer.CircularBuffer
	CpuMetricsBuffer      *buffer.CircularBuffer
	DiskIOMetricsBuffer   *buffer.CircularBuffer
	FilesystemUsageBuffer *buffer.CircularBuffer
	NetworkMetricsBuffer  *buffer.CircularBuffer
	TopTalkersBuffer      *buffer.CircularBuffer

	// Schedulers manage periodic collection for each metric type.
	// Each scheduler runs its own collector(s) on a 1-second ticker
	// with retry logic and statistics tracking.
	loadScheduler       *scheduler.CollectorScheduler
	cpuScheduler        *scheduler.CollectorScheduler
	diskIOScheduler     *scheduler.CollectorScheduler
	filesystemScheduler *scheduler.CollectorScheduler
	networkScheduler    *scheduler.CollectorScheduler
	topTalkersScheduler *scheduler.CollectorScheduler
}

// New creates a new Server instance
func New() *Server {
	return &Server{
		LoadMetricsBuffer:     buffer.NewCircularBuffer(300),
		CpuMetricsBuffer:      buffer.NewCircularBuffer(300),
		DiskIOMetricsBuffer:   buffer.NewCircularBuffer(300),
		FilesystemUsageBuffer: buffer.NewCircularBuffer(300),
		NetworkMetricsBuffer:  buffer.NewCircularBuffer(300),
		TopTalkersBuffer:      buffer.NewCircularBuffer(300),
	}
}

// StreamMetrics implements the streaming RPC for system metrics.
// Each metric type has its own CollectorScheduler that runs on a
// 1-second ticker with retry logic and statistics tracking.
// The main loop reads from buffers on the configured interval and
// sends aggregated responses to the client.
func (s *Server) StreamMetrics(req *pb.MetricRequest, stream pb.StreamService_StreamMetricsServer) error {
	log.Printf("Starting metric stream: interval=%ds, window=%ds", req.GetIntervalSeconds(), req.GetWindowSeconds())

	ctx := stream.Context()

	// Create collectors
	loadCollector := collector.NewLoadCollector()
	cpuCollector := collector.NewCpuCollector()
	diskIOCollector := collector.NewDiskIOCollector()
	filesystemCollector := collector.NewFilesystemUsageCollector()
	networkCollector := collector.NewNetworkCollector()
	topTalkersCollector := collector.NewTopTalkersCollector()

	// Create CollectorSchedulers with retry logic for each metric type.
	// Each scheduler manages one collector writing to its dedicated buffer.
	s.loadScheduler = scheduler.NewCollectorScheduler(
		[]scheduler.Collector{loadCollector},
		s.LoadMetricsBuffer,
		1*time.Second,
	)
	s.cpuScheduler = scheduler.NewCollectorScheduler(
		[]scheduler.Collector{cpuCollector},
		s.CpuMetricsBuffer,
		1*time.Second,
	)
	s.diskIOScheduler = scheduler.NewCollectorScheduler(
		[]scheduler.Collector{diskIOCollector},
		s.DiskIOMetricsBuffer,
		1*time.Second,
	)
	s.filesystemScheduler = scheduler.NewCollectorScheduler(
		[]scheduler.Collector{filesystemCollector},
		s.FilesystemUsageBuffer,
		1*time.Second,
	)
	s.networkScheduler = scheduler.NewCollectorScheduler(
		[]scheduler.Collector{networkCollector},
		s.NetworkMetricsBuffer,
		1*time.Second,
	)
	s.topTalkersScheduler = scheduler.NewCollectorScheduler(
		[]scheduler.Collector{topTalkersCollector},
		s.TopTalkersBuffer,
		1*time.Second,
	)

	// Start all schedulers concurrently in background goroutines.
	// Each runs until the context is cancelled.
	startScheduler := func(sched *scheduler.CollectorScheduler, name string) {
		go func() {
			if err := sched.Start(ctx, 0); err != nil && err != context.Canceled {
				log.Printf("Scheduler %s exited: %v", name, err)
			}
		}()
	}

	startScheduler(s.loadScheduler, "load")
	startScheduler(s.cpuScheduler, "cpu")
	startScheduler(s.diskIOScheduler, "diskio")
	startScheduler(s.filesystemScheduler, "filesystem")
	startScheduler(s.networkScheduler, "network")
	startScheduler(s.topTalkersScheduler, "toptalkers")

	// Main loop: send aggregated metrics on the configured interval
	sendTicker := time.NewTicker(time.Duration(req.GetIntervalSeconds()) * time.Second)
	defer sendTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Stream context cancelled: %v", ctx.Err())
			return ctx.Err()

		case <-sendTicker.C:
			now := time.Now()
			from := now.Add(-time.Duration(req.GetWindowSeconds()) * time.Second)

			loadMetrics := s.LoadMetricsBuffer.GetWindow(from, now)
			cpuMetrics := s.CpuMetricsBuffer.GetWindow(from, now)
			diskIOMetrics := s.DiskIOMetricsBuffer.GetWindow(from, now)
			filesystemMetrics := s.FilesystemUsageBuffer.GetWindow(from, now)
			networkMetrics := s.NetworkMetricsBuffer.GetWindow(from, now)
			topTalkersMetrics := s.TopTalkersBuffer.GetWindow(from, now)

			response := &pb.SystemMetrics{
				Timestamp:       uint64(now.UnixNano()),
				IntervalSeconds: req.GetIntervalSeconds(),
				WindowSeconds:   req.GetWindowSeconds(),
				Load:            aggregateLoadMetrics(loadMetrics),
				Cpu:             aggregateCpuMetrics(cpuMetrics),
				DiskIo:          aggregateDiskIOMetrics(diskIOMetrics),
			}

			// Attach the latest filesystem usage if available
			if fs := aggregateFilesystemUsageMetrics(filesystemMetrics); fs != nil {
				response.Filesystem = fs
			}

			// Attach the latest network metrics if available
			if net := aggregateNetworkMetrics(networkMetrics); net != nil {
				response.Network = net
			}

			// Attach the latest top talkers if available
			if tt := aggregateTopTalkers(topTalkersMetrics); tt != nil {
				response.TopTalkers = tt
			}

			if err := stream.Send(response); err != nil {
				log.Printf("Failed to send metrics: %v", err)
				return err
			}
		}
	}
}

// aggregateLoadMetrics averages load metrics from the window
func aggregateLoadMetrics(metrics []buffer.Metric) *pb.LoadMetrics {
	if len(metrics) == 0 {
		return nil
	}

	var sumLoad1 float64
	count := 0

	for _, m := range metrics {
		lm, ok := m.(*types.LoadMetric)
		if !ok {
			continue
		}
		sumLoad1 += lm.Metrics.GetLoad1()
		count++
	}

	if count == 0 {
		return nil
	}

	return &pb.LoadMetrics{
		Load1: sumLoad1 / float64(count),
	}
}

// aggregateCpuMetrics averages CPU metrics from the window
func aggregateCpuMetrics(metrics []buffer.Metric) *pb.CpuMetrics {
	if len(metrics) == 0 {
		return nil
	}

	var sumUser, sumSystem, sumIdle float64
	count := 0

	for _, m := range metrics {
		cm, ok := m.(*types.CpuMetric)
		if !ok {
			continue
		}
		sumUser += cm.Metrics.GetPercentUser()
		sumSystem += cm.Metrics.GetPercentSystem()
		sumIdle += cm.Metrics.GetPercentIdle()
		count++
	}

	if count == 0 {
		return nil
	}

	return &pb.CpuMetrics{
		PercentUser:   sumUser / float64(count),
		PercentSystem: sumSystem / float64(count),
		PercentIdle:   sumIdle / float64(count),
	}
}

// aggregateDiskIOMetrics averages disk I/O metrics from the window
func aggregateDiskIOMetrics(metrics []buffer.Metric) *pb.DiskIOMetrics {
	if len(metrics) == 0 {
		return nil
	}

	var sumTPS, sumKBTotalPerSec float64
	count := 0

	for _, m := range metrics {
		dm, ok := m.(*types.DiskIOMetric)
		if !ok {
			continue
		}
		sumTPS += dm.Metrics.GetTps()
		sumKBTotalPerSec += dm.Metrics.GetKbTotalPerSec()
		count++
	}

	if count == 0 {
		return nil
	}

	return &pb.DiskIOMetrics{
		Tps:           sumTPS / float64(count),
		KbTotalPerSec: sumKBTotalPerSec / float64(count),
	}
}

// aggregateFilesystemUsageMetrics returns the latest filesystem usage metric from the window.
// Since filesystem usage is relatively stable, we return the most recent value.
func aggregateFilesystemUsageMetrics(metrics []buffer.Metric) *pb.FilesystemUsage {
	if len(metrics) == 0 {
		return nil
	}

	// Return the most recent metric
	latest := metrics[len(metrics)-1]
	fm, ok := latest.(*types.FilesystemUsageMetric)
	if !ok {
		return nil
	}

	return &pb.FilesystemUsage{
		MountPoint:   fm.Metrics.GetMountPoint(),
		Filesystem:   fm.Metrics.GetFilesystem(),
		PercentUsed:  fm.Metrics.GetPercentUsed(),
		InodePercent: fm.Metrics.GetInodePercent(),
	}
}

// aggregateNetworkMetrics returns the latest network metrics from the window.
// Since network connection counts change frequently, we return the most recent
// snapshot to avoid averaging state counts.
func aggregateNetworkMetrics(metrics []buffer.Metric) *pb.NetworkMetrics {
	if len(metrics) == 0 {
		return nil
	}

	// Return the most recent metric
	latest := metrics[len(metrics)-1]
	nm, ok := latest.(*types.NetworkMetric)
	if !ok {
		return nil
	}

	return &pb.NetworkMetrics{
		ListeningSockets:       nm.Metrics.GetListeningSockets(),
		EstablishedConnections: nm.Metrics.GetEstablishedConnections(),
		TimeWaitSockets:        nm.Metrics.GetTimeWaitSockets(),
		CloseWaitSockets:       nm.Metrics.GetCloseWaitSockets(),
		FinWaitSockets:         nm.Metrics.GetFinWaitSockets(),
		SynSentSockets:         nm.Metrics.GetSynSentSockets(),
		UdpSockets:             nm.Metrics.GetUdpSockets(),
	}
}

// aggregateTopTalkers returns the latest top talkers data from the window.
// Since top talkers data is a snapshot, we return the most recent value.
func aggregateTopTalkers(metrics []buffer.Metric) *pb.TopTalkers {
	if len(metrics) == 0 {
		return nil
	}

	// Return the most recent metric
	latest := metrics[len(metrics)-1]
	tm, ok := latest.(*types.TopTalkersMetric)
	if !ok {
		return nil
	}

	return &pb.TopTalkers{
		ByProtocol:   tm.Metrics.GetByProtocol(),
		ByConnection: tm.Metrics.GetByConnection(),
	}
}

// RegisterService registers the server with a gRPC server
func (s *Server) RegisterService(grpcServer *grpc.Server) {
	pb.RegisterStreamServiceServer(grpcServer, s)
}
