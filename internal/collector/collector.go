// Package collector provides platform-specific system metric collectors
// that execute shell commands and parse their output to fill metric structures.
package collector

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"golang-project.local/internal/types"
	pb "golang-project.local/proto"
)

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
func (c *LoadCollector) Collect(ctx context.Context) (types.Metric, error) {
	load, err := collectLoad(ctx)
	if err != nil {
		return nil, fmt.Errorf("load collection failed: %w", err)
	}
	return types.NewLoadMetric(time.Now(), load), nil
}

// Collect collects CPU metrics using the platform-specific command.
func (c *CpuCollector) Collect(ctx context.Context) (types.Metric, error) {
	cpu, err := collectCPU(ctx)
	if err != nil {
		return nil, fmt.Errorf("cpu collection failed: %w", err)
	}
	return types.NewCpuMetric(time.Now(), cpu), nil
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
func (c *DiskIOCollector) Collect(ctx context.Context) (types.Metric, error) {
	diskio, err := collectDiskIO(ctx)
	if err != nil {
		return nil, fmt.Errorf("disk I/O collection failed: %w", err)
	}
	return types.NewDiskIOMetric(time.Now(), diskio), nil
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
func (c *FilesystemUsageCollector) Collect(ctx context.Context) (types.Metric, error) {
	fs, err := collectFilesystemUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("filesystem usage collection failed: %w", err)
	}
	return types.NewFilesystemUsageMetric(time.Now(), fs), nil
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
func (c *NetworkCollector) Collect(ctx context.Context) (types.Metric, error) {
	net, err := collectNetworkMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("network metrics collection failed: %w", err)
	}
	return types.NewNetworkMetric(time.Now(), net), nil
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
func (c *TopTalkersCollector) Collect(ctx context.Context) (types.Metric, error) {
	tt, err := collectTopTalkers(ctx)
	if err != nil {
		return nil, fmt.Errorf("top talkers collection failed: %w", err)
	}
	return types.NewTopTalkersMetric(time.Now(), tt), nil
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
