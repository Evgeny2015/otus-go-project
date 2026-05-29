package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	pb "golang-project.local/proto"
)

// collectLoadLinux collects load average on Linux using `top -b -n1`.
//
// Command: top -b -n1
// Relevant output lines:
//
//	top - 16:20:30 up 10 days,  2:15,  1 user,  load average: 0.15, 0.10, 0.05
//
// The load average values are on the first line after "load average:".
func collectLoadLinux(ctx context.Context) (*pb.LoadMetrics, error) {
	cmd := execCommand(ctx, "top", "-b", "-n1")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec top -b -n1: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("top output is empty")
	}

	// First line contains load average: "top - ... load average: 0.15, 0.10, 0.05"
	firstLine := lines[0]
	loadIdx := strings.Index(firstLine, "load average:")
	if loadIdx == -1 {
		return nil, fmt.Errorf("load average not found in top output")
	}

	loadStr := firstLine[loadIdx+len("load average:"):]
	loadStr = strings.TrimSpace(loadStr)
	parts := strings.Split(loadStr, ",")
	if len(parts) < 1 {
		return nil, fmt.Errorf("unexpected load average format: %s", loadStr)
	}

	load1, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return nil, fmt.Errorf("parse load1: %w", err)
	}

	return &pb.LoadMetrics{Load1: load1}, nil
}

// collectLoadDarwin collects load average on macOS using `top -l 1`.
//
// Command: top -l 1
// Relevant output lines:
//
//	Processes: 456 total, 3 running, 453 sleeping, 0 zombie ...
//	Load Avg: 2.15, 2.10, 2.05
//	CPU usage: 10.0% user, 5.0% sys, 85.0% idle
func collectLoadDarwin(ctx context.Context) (*pb.LoadMetrics, error) {
	cmd := execCommand(ctx, "top", "-l", "1")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec top -l 1: %w", err)
	}

	lines := strings.Split(string(output), "\n")

	var loadLine string
	for _, line := range lines {
		if strings.HasPrefix(line, "Load Avg:") {
			loadLine = line
			break
		}
	}
	if loadLine == "" {
		return nil, fmt.Errorf("Load Avg line not found in top output")
	}

	// Format: "Load Avg: 2.15, 2.10, 2.05"
	colonIdx := strings.Index(loadLine, ":")
	if colonIdx == -1 {
		return nil, fmt.Errorf("unexpected Load Avg format: %s", loadLine)
	}
	loadStr := loadLine[colonIdx+1:]
	loadStr = strings.TrimSpace(loadStr)
	parts := strings.Split(loadStr, ",")
	if len(parts) < 1 {
		return nil, fmt.Errorf("unexpected load values format: %s", loadStr)
	}

	load1, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return nil, fmt.Errorf("parse load1: %w", err)
	}

	return &pb.LoadMetrics{Load1: load1}, nil
}

// collectLoadWindows collects load average on Windows using PowerShell/WMI.
//
// Command: powershell -Command "Get-CimInstance Win32_Processor | Select-Object -ExpandProperty LoadPercentage"
// Output:
//
//	15
//
// We use the CPU load percentage as a proxy for system load average (load1).
func collectLoadWindows(ctx context.Context) (*pb.LoadMetrics, error) {
	cmd := execCommand(ctx, "powershell", "-Command",
		"Get-CimInstance Win32_Processor | Select-Object -ExpandProperty LoadPercentage")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec Get-CimInstance Win32_Processor: %w", err)
	}

	loadStr := strings.TrimSpace(string(output))
	if loadStr == "" {
		return nil, fmt.Errorf("empty CPU load percentage output")
	}

	// The output may have multiple lines (one per core); take the last non-empty one
	lines := strings.Split(loadStr, "\n")
	var lastVal string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			lastVal = line
		}
	}

	loadPct, err := strconv.ParseFloat(lastVal, 64)
	if err != nil {
		return nil, fmt.Errorf("parse load percentage '%s': %w", lastVal, err)
	}

	return &pb.LoadMetrics{Load1: loadPct}, nil
}
