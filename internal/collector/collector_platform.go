package collector

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	pb "golang-project.local/proto"
)

// execCommand is a variable so tests can override it with a fake command runner.
var execCommand = exec.CommandContext

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

// collectCPULinux collects CPU usage on Linux using `top -b -n1`.
//
// Command: top -b -n1
// Relevant output lines (after header):
//
//	%Cpu(s):  5.0 us,  2.0 sy,  0.0 ni, 92.0 id,  1.0 wa,  0.0 hi,  0.0 si,  0.0 st
//
// We parse us (user), sy (system), id (idle) percentages.
func collectCPULinux(ctx context.Context) (*pb.CpuMetrics, error) {
	cmd := execCommand(ctx, "top", "-b", "-n1")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec top -b -n1: %w", err)
	}

	lines := strings.Split(string(output), "\n")

	var cpuLine string
	for _, line := range lines {
		if strings.HasPrefix(line, "%Cpu") || strings.HasPrefix(line, "%CPU") {
			cpuLine = line
			break
		}
	}
	if cpuLine == "" {
		return nil, fmt.Errorf("CPU line not found in top output")
	}

	// Parse fields like: %Cpu(s):  5.0 us,  2.0 sy,  0.0 ni, 92.0 id,  1.0 wa, ...
	// Remove the leading "%Cpu(s):" or similar prefix
	colonIdx := strings.Index(cpuLine, ":")
	if colonIdx == -1 {
		return nil, fmt.Errorf("unexpected CPU line format: %s", cpuLine)
	}
	fieldsStr := cpuLine[colonIdx+1:]

	// Split by comma
	fields := strings.Split(fieldsStr, ",")

	var user, system, idle float64
	for _, field := range fields {
		field = strings.TrimSpace(field)
		parts := strings.Fields(field)
		if len(parts) < 2 {
			continue
		}
		val, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			continue
		}
		suffix := parts[1]
		switch suffix {
		case "us":
			user = val
		case "sy":
			system = val
		case "id":
			idle = val
		}
	}

	return &pb.CpuMetrics{
		PercentUser:   user,
		PercentSystem: system,
		PercentIdle:   idle,
	}, nil
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

// collectCPUDarwin collects CPU usage on macOS using `top -l 1`.
//
// Command: top -l 1
// Relevant output line:
//
//	CPU usage: 10.0% user, 5.0% sys, 85.0% idle
func collectCPUDarwin(ctx context.Context) (*pb.CpuMetrics, error) {
	cmd := execCommand(ctx, "top", "-l", "1")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec top -l 1: %w", err)
	}

	lines := strings.Split(string(output), "\n")

	var cpuLine string
	for _, line := range lines {
		if strings.HasPrefix(line, "CPU usage:") {
			cpuLine = line
			break
		}
	}
	if cpuLine == "" {
		return nil, fmt.Errorf("CPU usage line not found in top output")
	}

	// Format: "CPU usage: 10.0% user, 5.0% sys, 85.0% idle"
	colonIdx := strings.Index(cpuLine, ":")
	if colonIdx == -1 {
		return nil, fmt.Errorf("unexpected CPU usage format: %s", cpuLine)
	}
	fieldsStr := cpuLine[colonIdx+1:]

	fields := strings.Split(fieldsStr, ",")

	var user, system, idle float64
	for _, field := range fields {
		field = strings.TrimSpace(field)
		// Each field: "10.0% user"
		parts := strings.Fields(field)
		if len(parts) < 2 {
			continue
		}
		valStr := strings.TrimSuffix(parts[0], "%")
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		suffix := parts[1]
		switch suffix {
		case "user":
			user = val
		case "sys":
			system = val
		case "idle":
			idle = val
		}
	}

	return &pb.CpuMetrics{
		PercentUser:   user,
		PercentSystem: system,
		PercentIdle:   idle,
	}, nil
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

// collectCPUWindows collects CPU usage on Windows using PowerShell/WMI.
//
// Command: powershell -Command "Get-CimInstance Win32_Processor | Select-Object -ExpandProperty LoadPercentage"
// Output:
//
//	15
//
// We interpret the single LoadPercentage value as total CPU usage.
// Since WMI only gives total usage, we derive user/system/idle using
// a secondary WMI query for kernel time breakdown.
func collectCPUWindows(ctx context.Context) (*pb.CpuMetrics, error) {
	// Get total CPU load percentage
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

	lines := strings.Split(loadStr, "\n")
	var lastVal string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			lastVal = line
		}
	}

	totalUsage, err := strconv.ParseFloat(lastVal, 64)
	if err != nil {
		return nil, fmt.Errorf("parse CPU load percentage '%s': %w", lastVal, err)
	}

	// Get user/system breakdown from WMI
	userPct, sysPct := getWindowsCPUBreakdown(ctx, totalUsage)

	idle := 100.0 - totalUsage
	if idle < 0 {
		idle = 0
	}

	return &pb.CpuMetrics{
		PercentUser:   userPct,
		PercentSystem: sysPct,
		PercentIdle:   idle,
	}, nil
}

// getWindowsCPUBreakdown queries WMI for user and privileged time percentages.
// Uses Win32_PerfFormattedData_PerfOS_Processor for detailed CPU breakdown.
func getWindowsCPUBreakdown(ctx context.Context, totalUsage float64) (userPct, sysPct float64) {
	// Query the _Total processor instance for user and privileged time
	cmd := execCommand(ctx, "powershell", "-Command",
		`$cpu = Get-CimInstance Win32_PerfFormattedData_PerfOS_Processor | Where-Object Name -eq "_Total";
         $user = [double]$cpu.PercentUserTime;
         $priv = [double]$cpu.PercentPrivilegedTime;
         Write-Output "$user $priv"`)
	output, err := cmd.Output()
	if err != nil {
		// Fallback: approximate 70/30 user/system split
		return totalUsage * 0.7, totalUsage * 0.3
	}

	line := strings.TrimSpace(string(output))
	parts := strings.Fields(line)
	if len(parts) >= 2 {
		u, err1 := strconv.ParseFloat(parts[0], 64)
		s, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 == nil && err2 == nil {
			return u, s
		}
	}

	return totalUsage * 0.7, totalUsage * 0.3
}
