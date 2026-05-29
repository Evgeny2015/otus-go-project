package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	pb "golang-project.local/proto"
)

// collectDiskIOLinux collects disk I/O metrics on Linux using `iostat -d 1 1`.
//
// Command: iostat -d 1 1
// Relevant output lines (after header):
//
//	Device             tps    kB_read/s    kB_wrtn/s    kB_dscd/s    kB_read    kB_wrtn    kB_dscd
//	sda               5.23        10.50        20.75         0.00     1048576     2097152          0
//
// We parse tps and compute kb_total_per_sec = kB_read/s + kB_wrtn/s.
func collectDiskIOLinux(ctx context.Context) (*pb.DiskIOMetrics, error) {
	cmd := execCommand(ctx, "iostat", "-d", "1", "1")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec iostat -d 1 1: %w", err)
	}

	lines := strings.Split(string(output), "\n")

	// Find the header line starting with "Device"
	var headerIdx int = -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Device") {
			headerIdx = i
			break
		}
	}
	if headerIdx == -1 || headerIdx+1 >= len(lines) {
		return nil, fmt.Errorf("device header not found in iostat output")
	}

	// Determine column indices from header
	headerFields := strings.Fields(lines[headerIdx])
	tpsCol := -1
	kbReadSecCol := -1
	kbWrtnSecCol := -1
	for i, f := range headerFields {
		switch f {
		case "tps":
			tpsCol = i
		case "kB_read/s":
			kbReadSecCol = i
		case "kB_wrtn/s":
			kbWrtnSecCol = i
		}
	}

	if tpsCol == -1 {
		return nil, fmt.Errorf("tps column not found in iostat header")
	}

	// Aggregate across all devices
	var totalTPS, totalKBReadSec, totalKBWrtnSec float64
	deviceCount := 0

	for _, line := range lines[headerIdx+1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) <= tpsCol {
			continue
		}
		// Skip the "Device" header if it appears again (some iostat versions repeat)
		if fields[0] == "Device" {
			continue
		}

		tps, err := strconv.ParseFloat(fields[tpsCol], 64)
		if err != nil {
			continue
		}
		totalTPS += tps

		if kbReadSecCol != -1 && len(fields) > kbReadSecCol {
			if v, err := strconv.ParseFloat(fields[kbReadSecCol], 64); err == nil {
				totalKBReadSec += v
			}
		}
		if kbWrtnSecCol != -1 && len(fields) > kbWrtnSecCol {
			if v, err := strconv.ParseFloat(fields[kbWrtnSecCol], 64); err == nil {
				totalKBWrtnSec += v
			}
		}
		deviceCount++
	}

	if deviceCount == 0 {
		return nil, fmt.Errorf("no device data found in iostat output")
	}

	return &pb.DiskIOMetrics{
		Tps:           totalTPS,
		KbTotalPerSec: totalKBReadSec + totalKBWrtnSec,
	}, nil
}

// collectDiskIODarwin collects disk I/O metrics on macOS using `iostat -d -c 1`.
//
// Command: iostat -d -c 1
// Relevant output lines:
//
//	          disk0           disk1
//	KB/t tps  MB/s  KB/t tps  MB/s
//	20.50 5.23 10.00 30.00 3.10 8.00
//
// We parse tps and MB/s (converted to KB/s) for all disks and aggregate.
func collectDiskIODarwin(ctx context.Context) (*pb.DiskIOMetrics, error) {
	cmd := execCommand(ctx, "iostat", "-d", "-c", "1")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec iostat -d -c 1: %w", err)
	}

	lines := strings.Split(string(output), "\n")

	// macOS iostat output format:
	// Line 0: blank or header with disk names: "          disk0           disk1"
	// Line 1: sub-header: "KB/t tps  MB/s  KB/t tps  MB/s"
	// Line 2+: data lines: "20.50 5.23 10.00 30.00 3.10 8.00"
	//
	// We need to find the data line (the first line after the sub-header that has numeric values).

	var dataLine string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		// Data lines start with a float (KB/t value)
		if len(fields) >= 3 {
			if _, err := strconv.ParseFloat(fields[0], 64); err == nil {
				dataLine = trimmed
				break
			}
		}
	}

	if dataLine == "" {
		return nil, fmt.Errorf("no data line found in iostat output")
	}

	fields := strings.Fields(dataLine)
	// Each disk has 3 fields: KB/t, tps, MB/s
	// So fields are grouped in triples: [KB/t0, tps0, MB/s0, KB/t1, tps1, MB/s1, ...]
	const fieldsPerDisk = 3
	if len(fields) < fieldsPerDisk {
		return nil, fmt.Errorf("unexpected iostat data format: %s", dataLine)
	}

	var totalTPS, totalKBSec float64
	diskCount := 0

	for i := 0; i+fieldsPerDisk <= len(fields); i += fieldsPerDisk {
		tps, err := strconv.ParseFloat(fields[i+1], 64)
		if err != nil {
			continue
		}
		mbps, err := strconv.ParseFloat(fields[i+2], 64)
		if err != nil {
			continue
		}
		totalTPS += tps
		totalKBSec += mbps * 1024.0 // Convert MB/s to KB/s
		diskCount++
	}

	if diskCount == 0 {
		return nil, fmt.Errorf("no disk data parsed from iostat output")
	}

	return &pb.DiskIOMetrics{
		Tps:           totalTPS,
		KbTotalPerSec: totalKBSec,
	}, nil
}

// collectDiskIOWindows collects disk I/O metrics on Windows using PowerShell/WMI.
//
// Command: powershell -Command "Get-CimInstance Win32_PerfFormattedData_PerfDisk_LogicalDisk | ..."
// We query the _Total instance for Disk Transfers/sec and Disk Bytes/sec,
// then convert bytes/sec to KB/sec.
func collectDiskIOWindows(ctx context.Context) (*pb.DiskIOMetrics, error) {
	cmd := execCommand(ctx, "powershell", "-Command",
		`$disk = Get-CimInstance Win32_PerfFormattedData_PerfDisk_LogicalDisk | Where-Object Name -eq "_Total";
         $tps = [double]$disk.DiskTransfersPerSec;
         $bps = [double]$disk.DiskBytesPerSec;
         Write-Output "$tps $bps"`)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec Get-CimInstance Win32_PerfFormattedData_PerfDisk_LogicalDisk: %w", err)
	}

	line := strings.TrimSpace(string(output))
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil, fmt.Errorf("unexpected disk output format: %s", line)
	}

	tps, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return nil, fmt.Errorf("parse tps '%s': %w", parts[0], err)
	}

	bytesPerSec, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return nil, fmt.Errorf("parse bytes/sec '%s': %w", parts[1], err)
	}

	kbPerSec := bytesPerSec / 1024.0

	return &pb.DiskIOMetrics{
		Tps:           tps,
		KbTotalPerSec: kbPerSec,
	}, nil
}
