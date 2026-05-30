// Package collector provides platform-specific system metric collectors
// that execute shell commands and parse their output to fill LoadMetric,
// CpuMetric, and DiskIOMetric structures.
package collector

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"golang-project.local/internal/buffer"
	pb "golang-project.local/proto"
)

// LoadMetric wraps proto.LoadMetrics to implement buffer.Metric
type LoadMetric struct {
	ts      time.Time
	Metrics *pb.LoadMetrics
}

func (m *LoadMetric) Timestamp() time.Time {
	return m.ts
}

func (m *LoadMetric) Value() interface{} {
	return m.Metrics
}

// CpuMetric wraps proto.CpuMetrics to implement buffer.Metric
type CpuMetric struct {
	ts      time.Time
	Metrics *pb.CpuMetrics
}

func (m *CpuMetric) Timestamp() time.Time {
	return m.ts
}

func (m *CpuMetric) Value() interface{} {
	return m.Metrics
}

// LoadCollector collects system load average metrics.
type LoadCollector struct {
	enabled bool
}

// CpuCollector collects CPU usage metrics.
type CpuCollector struct {
	enabled bool
}

// NewLoadCollector creates a new LoadCollector.
func NewLoadCollector() *LoadCollector {
	return &LoadCollector{enabled: true}
}

// NewCpuCollector creates a new CpuCollector.
func NewCpuCollector() *CpuCollector {
	return &CpuCollector{enabled: true}
}

// Name returns the collector name.
func (c *LoadCollector) Name() string {
	return "load"
}

// Name returns the collector name.
func (c *CpuCollector) Name() string {
	return "cpu"
}

// Enabled returns whether the collector is enabled.
func (c *LoadCollector) Enabled() bool {
	return c.enabled
}

// Enabled returns whether the collector is enabled.
func (c *CpuCollector) Enabled() bool {
	return c.enabled
}

// Collect collects load metrics using the platform-specific command.
func (c *LoadCollector) Collect(ctx context.Context) (buffer.Metric, error) {
	load, err := collectLoad(ctx)
	if err != nil {
		return nil, fmt.Errorf("load collection failed: %w", err)
	}
	return &LoadMetric{
		Metrics: load,
	}, nil
}

// Collect collects CPU metrics using the platform-specific command.
func (c *CpuCollector) Collect(ctx context.Context) (buffer.Metric, error) {
	cpu, err := collectCPU(ctx)
	if err != nil {
		return nil, fmt.Errorf("cpu collection failed: %w", err)
	}
	return &CpuMetric{
		Metrics: cpu,
	}, nil
}

// collectLoad dispatches to the platform-specific implementation.
func collectLoad(ctx context.Context) (*pb.LoadMetrics, error) {
	switch runtime.GOOS {
	case "linux":
		return collectLoadLinux(ctx)
	case "darwin":
		return collectLoadDarwin(ctx)
	case "windows":
		return collectLoadWindows(ctx)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// collectCPU dispatches to the platform-specific implementation.
func collectCPU(ctx context.Context) (*pb.CpuMetrics, error) {
	switch runtime.GOOS {
	case "linux":
		return collectCPULinux(ctx)
	case "darwin":
		return collectCPUDarwin(ctx)
	case "windows":
		return collectCPUWindows(ctx)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// DiskIOMetric wraps proto.DiskIOMetrics to implement buffer.Metric
type DiskIOMetric struct {
	ts      time.Time
	Metrics *pb.DiskIOMetrics
}

func (m *DiskIOMetric) Timestamp() time.Time {
	return m.ts
}

func (m *DiskIOMetric) Value() interface{} {
	return m.Metrics
}

// DiskIOCollector collects disk I/O metrics.
type DiskIOCollector struct {
	enabled bool
}

// NewDiskIOCollector creates a new DiskIOCollector.
func NewDiskIOCollector() *DiskIOCollector {
	return &DiskIOCollector{enabled: true}
}

// Name returns the collector name.
func (c *DiskIOCollector) Name() string {
	return "diskio"
}

// Enabled returns whether the collector is enabled.
func (c *DiskIOCollector) Enabled() bool {
	return c.enabled
}

// Collect collects disk I/O metrics using the platform-specific command.
func (c *DiskIOCollector) Collect(ctx context.Context) (buffer.Metric, error) {
	diskio, err := collectDiskIO(ctx)
	if err != nil {
		return nil, fmt.Errorf("disk I/O collection failed: %w", err)
	}
	return &DiskIOMetric{
		Metrics: diskio,
	}, nil
}

// collectDiskIO dispatches to the platform-specific implementation.
func collectDiskIO(ctx context.Context) (*pb.DiskIOMetrics, error) {
	switch runtime.GOOS {
	case "linux":
		return collectDiskIOLinux(ctx)
	case "darwin":
		return collectDiskIODarwin(ctx)
	case "windows":
		return collectDiskIOWindows(ctx)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// FilesystemUsageMetric wraps proto.FilesystemUsage to implement buffer.Metric
type FilesystemUsageMetric struct {
	ts      time.Time
	Metrics *pb.FilesystemUsage
}

func (m *FilesystemUsageMetric) Timestamp() time.Time {
	return m.ts
}

func (m *FilesystemUsageMetric) Value() interface{} {
	return m.Metrics
}

// FilesystemUsageCollector collects filesystem usage metrics.
type FilesystemUsageCollector struct {
	enabled bool
}

// NewFilesystemUsageCollector creates a new FilesystemUsageCollector.
func NewFilesystemUsageCollector() *FilesystemUsageCollector {
	return &FilesystemUsageCollector{enabled: true}
}

// Name returns the collector name.
func (c *FilesystemUsageCollector) Name() string {
	return "filesystem"
}

// Enabled returns whether the collector is enabled.
func (c *FilesystemUsageCollector) Enabled() bool {
	return c.enabled
}

// Collect collects filesystem usage metrics using the platform-specific command.
func (c *FilesystemUsageCollector) Collect(ctx context.Context) (buffer.Metric, error) {
	fs, err := collectFilesystemUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("filesystem usage collection failed: %w", err)
	}
	return &FilesystemUsageMetric{
		Metrics: fs,
	}, nil
}

// collectFilesystemUsage dispatches to the platform-specific implementation.
func collectFilesystemUsage(ctx context.Context) (*pb.FilesystemUsage, error) {
	switch runtime.GOOS {
	case "linux":
		return collectFilesystemUsageLinux(ctx)
	case "darwin":
		return collectFilesystemUsageDarwin(ctx)
	case "windows":
		return collectFilesystemUsageWindows(ctx)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// ---------------------------------------------------------------------------
// NetworkMetrics
// ---------------------------------------------------------------------------

// NetworkMetric wraps proto.NetworkMetrics to implement buffer.Metric
type NetworkMetric struct {
	ts      time.Time
	Metrics *pb.NetworkMetrics
}

func (m *NetworkMetric) Timestamp() time.Time {
	return m.ts
}

func (m *NetworkMetric) Value() interface{} {
	return m.Metrics
}

// NetworkCollector collects network connection statistics.
type NetworkCollector struct {
	enabled bool
}

// NewNetworkCollector creates a new NetworkCollector.
func NewNetworkCollector() *NetworkCollector {
	return &NetworkCollector{enabled: true}
}

// Name returns the collector name.
func (c *NetworkCollector) Name() string {
	return "network"
}

// Enabled returns whether the collector is enabled.
func (c *NetworkCollector) Enabled() bool {
	return c.enabled
}

// Collect collects network metrics using the platform-specific command.
func (c *NetworkCollector) Collect(ctx context.Context) (buffer.Metric, error) {
	net, err := collectNetworkMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("network metrics collection failed: %w", err)
	}
	return &NetworkMetric{
		Metrics: net,
	}, nil
}

// collectNetworkMetrics dispatches to the platform-specific implementation.
func collectNetworkMetrics(ctx context.Context) (*pb.NetworkMetrics, error) {
	switch runtime.GOOS {
	case "linux":
		return collectNetworkMetricsLinux(ctx)
	case "darwin":
		return collectNetworkMetricsDarwin(ctx)
	case "windows":
		return collectNetworkMetricsWindows(ctx)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// ---------------------------------------------------------------------------
// TopTalkers
// ---------------------------------------------------------------------------

// TopTalkersMetric wraps proto.TopTalkers to implement buffer.Metric
type TopTalkersMetric struct {
	ts      time.Time
	Metrics *pb.TopTalkers
}

func (m *TopTalkersMetric) Timestamp() time.Time {
	return m.ts
}

func (m *TopTalkersMetric) Value() interface{} {
	return m.Metrics
}

// TopTalkersCollector collects network top talkers data.
type TopTalkersCollector struct {
	enabled bool
}

// NewTopTalkersCollector creates a new TopTalkersCollector.
func NewTopTalkersCollector() *TopTalkersCollector {
	return &TopTalkersCollector{enabled: true}
}

// Name returns the collector name.
func (c *TopTalkersCollector) Name() string {
	return "toptalkers"
}

// Enabled returns whether the collector is enabled.
func (c *TopTalkersCollector) Enabled() bool {
	return c.enabled
}

// Collect collects top talkers data using the platform-specific command.
func (c *TopTalkersCollector) Collect(ctx context.Context) (buffer.Metric, error) {
	tt, err := collectTopTalkers(ctx)
	if err != nil {
		return nil, fmt.Errorf("top talkers collection failed: %w", err)
	}
	return &TopTalkersMetric{
		Metrics: tt,
	}, nil
}

// collectTopTalkers dispatches to the platform-specific implementation.
func collectTopTalkers(ctx context.Context) (*pb.TopTalkers, error) {
	switch runtime.GOOS {
	case "linux":
		return collectTopTalkersLinux(ctx)
	case "darwin":
		return collectTopTalkersDarwin(ctx)
	case "windows":
		return collectTopTalkersWindows(ctx)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
