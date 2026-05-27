// Package collector provides platform-specific system metric collectors
// that execute shell commands and parse their output to fill LoadMetric
// and CpuMetric structures.
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
