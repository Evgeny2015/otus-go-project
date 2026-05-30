// Package server provides the gRPC server implementation for streaming
// system metrics to connected clients. It manages metric collection,
// aggregation, and broadcasting to all active client streams.
package server

import (
	"context"
	"log"
	"sync"
	"time"

	"golang-project.local/internal/buffer"
	"golang-project.local/internal/collector"
	"golang-project.local/internal/config"
	"golang-project.local/internal/scheduler"
	"golang-project.local/internal/stream"
	"golang-project.local/internal/types"
	"golang-project.local/internal/window"
	pb "golang-project.local/proto"
	"google.golang.org/grpc"
)

// Server implements the gRPC server logic for streaming system metrics.
//
// It manages:
//   - A shared set of CollectorSchedulers that run continuously on 1-second ticks
//   - A StreamManager that tracks all connected clients
//   - A main collection loop that aggregates metrics and broadcasts to clients
//
// The server starts schedulers once at startup (not per-connection), ensuring
// efficient resource usage regardless of the number of connected clients.
type Server struct {
	pb.UnimplementedStreamServiceServer

	// cfg holds the resolved configuration (defaults + file + flags + env).
	cfg *config.Config

	// Metric buffers store raw metrics from each subsystem.
	// These are populated by the schedulers and read by the aggregation loop.
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

	// streamManager manages all active client streams.
	streamManager *stream.StreamManager

	// ctx is the server-level context for controlling background goroutines.
	ctx    context.Context
	cancel context.CancelFunc

	// wg tracks background goroutines for graceful shutdown.
	wg sync.WaitGroup

	// started indicates whether the server's background collection has started.
	started bool
	mu      sync.Mutex
}

// New creates a new Server instance with initialized metric buffers
// and a StreamManager, using the provided configuration.
//
// The cfg parameter controls:
//   - Buffer capacity (derived from the maximum expected window)
//   - Stream manager max clients limit
//   - Which collectors are enabled
func New(cfg *config.Config) *Server {
	// Buffer capacity: we need to hold at least cfg.Monitoring.DefaultWindow
	// data points at 1-second resolution. Add a safety margin of 50%.
	bufferCapacity := cfg.Monitoring.DefaultWindow * 3 / 2
	if bufferCapacity < 60 {
		bufferCapacity = 60 // minimum 60 entries (1 minute)
	}

	// Configure stream manager with max clients from config
	var streamOpts []stream.StreamManagerOption
	if cfg.Server.MaxClients > 0 {
		streamOpts = append(streamOpts, stream.WithMaxClients(cfg.Server.MaxClients))
	}

	return &Server{
		cfg:                   cfg,
		LoadMetricsBuffer:     buffer.NewCircularBuffer(bufferCapacity),
		CpuMetricsBuffer:      buffer.NewCircularBuffer(bufferCapacity),
		DiskIOMetricsBuffer:   buffer.NewCircularBuffer(bufferCapacity),
		FilesystemUsageBuffer: buffer.NewCircularBuffer(bufferCapacity),
		NetworkMetricsBuffer:  buffer.NewCircularBuffer(bufferCapacity),
		TopTalkersBuffer:      buffer.NewCircularBuffer(bufferCapacity),
		streamManager:         stream.NewStreamManager(streamOpts...),
	}
}

// Start begins the background metric collection. It creates all collectors
// and schedulers, then starts them in background goroutines.
//
// The collection runs continuously until Stop() is called or the context
// is cancelled. Each scheduler collects metrics on a 1-second ticker and
// stores them in the corresponding buffer.
//
// Start is idempotent: calling it multiple times has no effect after the
// first successful start.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		log.Printf("Server: already started, ignoring duplicate Start() call")
		return nil
	}

	s.ctx, s.cancel = context.WithCancel(ctx)

	// Determine which collectors to enable based on configuration.
	// We always create all collectors, but only start schedulers for
	// enabled ones. This allows runtime toggling in the future.
	collectors := s.buildEnabledCollectors()

	// Create CollectorSchedulers with retry logic for each metric type.
	// Each scheduler manages one collector writing to its dedicated buffer.
	schedulers := make(map[string]*scheduler.CollectorScheduler)

	if col, ok := collectors["load"]; ok && col != nil {
		s.loadScheduler = scheduler.NewCollectorScheduler(
			[]scheduler.Collector{col}, s.LoadMetricsBuffer, 1*time.Second,
		)
		schedulers["load"] = s.loadScheduler
	}
	if col, ok := collectors["cpu"]; ok && col != nil {
		s.cpuScheduler = scheduler.NewCollectorScheduler(
			[]scheduler.Collector{col}, s.CpuMetricsBuffer, 1*time.Second,
		)
		schedulers["cpu"] = s.cpuScheduler
	}
	if col, ok := collectors["disk"]; ok && col != nil {
		s.diskIOScheduler = scheduler.NewCollectorScheduler(
			[]scheduler.Collector{col}, s.DiskIOMetricsBuffer, 1*time.Second,
		)
		schedulers["diskio"] = s.diskIOScheduler
	}
	if col, ok := collectors["filesystem"]; ok && col != nil {
		s.filesystemScheduler = scheduler.NewCollectorScheduler(
			[]scheduler.Collector{col}, s.FilesystemUsageBuffer, 1*time.Second,
		)
		schedulers["filesystem"] = s.filesystemScheduler
	}
	if col, ok := collectors["network"]; ok && col != nil {
		s.networkScheduler = scheduler.NewCollectorScheduler(
			[]scheduler.Collector{col}, s.NetworkMetricsBuffer, 1*time.Second,
		)
		schedulers["network"] = s.networkScheduler
	}
	if col, ok := collectors["toptalkers"]; ok && col != nil {
		s.topTalkersScheduler = scheduler.NewCollectorScheduler(
			[]scheduler.Collector{col}, s.TopTalkersBuffer, 1*time.Second,
		)
		schedulers["toptalkers"] = s.topTalkersScheduler
	}

	// Start all enabled schedulers concurrently in background goroutines.
	startScheduler := func(sched *scheduler.CollectorScheduler, name string) {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := sched.Start(s.ctx, 0); err != nil && err != context.Canceled {
				log.Printf("Server: scheduler %s exited: %v", name, err)
			}
		}()
	}

	for name, sched := range schedulers {
		startScheduler(sched, name)
	}

	s.started = true
	log.Printf("Server: background metric collection started with %d scheduler(s)", len(schedulers))
	return nil
}

// buildEnabledCollectors creates collector instances for each enabled
// metric type based on the server configuration. Only collectors whose
// names appear in cfg.Monitoring.EnabledCollectors are created.
func (s *Server) buildEnabledCollectors() map[string]scheduler.Collector {
	enabled := make(map[string]scheduler.Collector)

	if s.cfg.Monitoring.CollectorEnabled("load") {
		enabled["load"] = collector.NewLoadCollector()
	}
	if s.cfg.Monitoring.CollectorEnabled("cpu") {
		enabled["cpu"] = collector.NewCpuCollector()
	}
	if s.cfg.Monitoring.CollectorEnabled("disk") {
		enabled["disk"] = collector.NewDiskIOCollector()
	}
	if s.cfg.Monitoring.CollectorEnabled("filesystem") {
		enabled["filesystem"] = collector.NewFilesystemUsageCollector()
	}
	if s.cfg.Monitoring.CollectorEnabled("network") {
		enabled["network"] = collector.NewNetworkCollector()
	}
	if s.cfg.Monitoring.CollectorEnabled("toptalkers") {
		enabled["toptalkers"] = collector.NewTopTalkersCollector()
	}

	return enabled
}

// Stop gracefully stops the server, shutting down all schedulers and
// disconnecting all clients. It waits for all background goroutines to
// complete before returning.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return
	}

	log.Printf("Server: stopping...")

	// Cancel the server context to stop all schedulers
	if s.cancel != nil {
		s.cancel()
	}

	// Wait for all scheduler goroutines to finish
	s.wg.Wait()

	// Disconnect all clients
	s.streamManager.Shutdown()

	s.started = false
	log.Printf("Server: stopped")
}

// StreamMetrics implements the streaming RPC for system metrics.
// Each client connection gets its own goroutine that reads from the
// shared metric buffers and sends aggregated responses on the
// configured interval.
//
// The method:
//  1. Registers the client with the StreamManager
//  2. Creates a per-client ticker for the requested interval
//  3. On each tick, reads from shared buffers, aggregates, and sends
//  4. Handles client disconnection gracefully
func (s *Server) StreamMetrics(req *pb.MetricRequest, stream pb.StreamService_StreamMetricsServer) error {
	intervalSec := req.GetIntervalSeconds()
	windowSec := req.GetWindowSeconds()

	// Validate parameters
	if intervalSec <= 0 {
		intervalSec = 5 // default: 5 seconds
	}
	if windowSec <= 0 {
		windowSec = 15 // default: 15 seconds
	}

	log.Printf("Server: new client stream request (interval=%ds, window=%ds)", intervalSec, windowSec)

	// Register with stream manager
	cs, err := s.streamManager.Register(stream, intervalSec, windowSec)
	if err != nil {
		log.Printf("Server: failed to register client: %v", err)
		return err
	}

	// Ensure cleanup on exit
	defer s.streamManager.Unregister(cs)

	// Main loop: send aggregated metrics on the configured interval
	sendTicker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer sendTicker.Stop()

	for {
		select {
		case <-cs.Context().Done():
			log.Printf("Server: client %d disconnected: %v", cs.ID, cs.Context().Err())
			return cs.Context().Err()

		case <-sendTicker.C:
			response := s.buildAggregatedResponse(intervalSec, windowSec)
			if response == nil {
				continue
			}

			if err := cs.Send(response); err != nil {
				log.Printf("Server: failed to send metrics to client %d: %v", cs.ID, err)
				return err
			}
		}
	}
}

// buildAggregatedResponse reads from all metric buffers and builds a
// SystemMetrics response with aggregated values over the configured window.
// Returns nil if no data is available.
func (s *Server) buildAggregatedResponse(intervalSec, windowSec int32) *pb.SystemMetrics {
	now := time.Now()
	from := now.Add(-time.Duration(windowSec) * time.Second)

	loadMetrics := s.LoadMetricsBuffer.GetWindow(from, now)
	cpuMetrics := s.CpuMetricsBuffer.GetWindow(from, now)
	diskIOMetrics := s.DiskIOMetricsBuffer.GetWindow(from, now)
	filesystemMetrics := s.FilesystemUsageBuffer.GetWindow(from, now)
	networkMetrics := s.NetworkMetricsBuffer.GetWindow(from, now)
	topTalkersMetrics := s.TopTalkersBuffer.GetWindow(from, now)

	// If no data at all, return nil
	if len(loadMetrics) == 0 && len(cpuMetrics) == 0 && len(diskIOMetrics) == 0 &&
		len(filesystemMetrics) == 0 && len(networkMetrics) == 0 && len(topTalkersMetrics) == 0 {
		return nil
	}

	response := &pb.SystemMetrics{
		Timestamp:       uint64(now.UnixNano()),
		IntervalSeconds: intervalSec,
		WindowSeconds:   windowSec,
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

	return response
}

// BroadcastToAll sends the given metrics to all connected clients via the
// StreamManager. This is useful for server-initiated broadcasts outside
// of the per-client StreamMetrics loop.
func (s *Server) BroadcastToAll(metrics *pb.SystemMetrics) int {
	return s.streamManager.Broadcast(metrics)
}

// StreamManager returns the server's StreamManager instance.
func (s *Server) StreamManager() *stream.StreamManager {
	return s.streamManager
}

// RegisterService registers the server with a gRPC server.
func (s *Server) RegisterService(grpcServer *grpc.Server) {
	pb.RegisterStreamServiceServer(grpcServer, s)
}

// ---------------------------------------------------------------------------
// Aggregation helpers
// ---------------------------------------------------------------------------

// aggregateLoadMetrics averages load metrics from the window.
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

// aggregateCpuMetrics averages CPU metrics from the window.
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

// aggregateDiskIOMetrics averages disk I/O metrics from the window.
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

// aggregateFilesystemUsageMetrics returns the latest filesystem usage metric
// from the window. Since filesystem usage is relatively stable, we return
// the most recent value.
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

// Ensure window package is used (referenced in plan but aggregation
// helpers here are self-contained for simplicity).
var _ = window.CalculateWindow
