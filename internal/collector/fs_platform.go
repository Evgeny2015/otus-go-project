package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	pb "golang-project.local/proto"
)

// collectFilesystemUsageLinux collects filesystem usage on Linux using `df -B1 --output=source,target,pcent,ipcent`.
//
// Command: df -B1 --output=source,target,pcent,ipcent
// Relevant output lines (header + data):
//
//	Filesystem           Mount                 Use%  IUse%
//	/dev/sda1            /                     42.3  1.5
//	tmpfs                /run                  0.1   0.0
//
// We parse each line and return the root mount ("/") entry.
func collectFilesystemUsageLinux(ctx context.Context) (*pb.FilesystemUsage, error) {
	cmd := execCommand(ctx, "df", "-B1", "--output=source,target,pcent,ipcent")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec df: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("df output has no data lines")
	}

	// First line is header, skip it
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		filesystem := fields[0]
		mountPoint := fields[1]
		pcentStr := strings.TrimSuffix(fields[2], "%")
		ipcentStr := strings.TrimSuffix(fields[3], "%")

		// We are interested in the root mount point "/"
		if mountPoint != "/" {
			continue
		}

		pcent, err := strconv.ParseFloat(pcentStr, 64)
		if err != nil {
			return nil, fmt.Errorf("parse percent used '%s': %w", pcentStr, err)
		}

		ipcent, err := strconv.ParseFloat(ipcentStr, 64)
		if err != nil {
			return nil, fmt.Errorf("parse inode percent '%s': %w", ipcentStr, err)
		}

		return &pb.FilesystemUsage{
			MountPoint:   mountPoint,
			Filesystem:   filesystem,
			PercentUsed:  pcent,
			InodePercent: ipcent,
		}, nil
	}

	return nil, fmt.Errorf("root mount point '/' not found in df output")
}

// collectFilesystemUsageDarwin collects filesystem usage on macOS using `df -lk /`.
//
// Command: df -lk /
// Relevant output lines:
//
//	Filesystem    1024-blocks      Used Available Capacity iused ifree %iused  Mounted on
//	/dev/disk1s1   488245328 228830080 258809248      47% 5598956 0    100%   /
//
// We parse the root mount line for capacity and inode percentages.
func collectFilesystemUsageDarwin(ctx context.Context) (*pb.FilesystemUsage, error) {
	cmd := execCommand(ctx, "df", "-lk", "/")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec df -lk /: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("df output has no data lines")
	}

	// Find the data line (second non-empty line after header)
	var dataLine string
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip the header line if it appears again
		if strings.HasPrefix(line, "Filesystem") {
			continue
		}
		dataLine = line
		break
	}

	if dataLine == "" {
		return nil, fmt.Errorf("no data line found in df output")
	}

	fields := strings.Fields(dataLine)
	if len(fields) < 9 {
		return nil, fmt.Errorf("unexpected df output format: %s", dataLine)
	}

	// macOS df -lk output columns:
	// 0: Filesystem    1: 1024-blocks  2: Used  3: Available  4: Capacity%  5: iused  6: ifree  7: %iused  8: Mounted on
	// Capacity is like "47%" and %iused is like "100%"
	capacityStr := strings.TrimSuffix(fields[4], "%")
	iusedStr := strings.TrimSuffix(fields[7], "%")

	capacity, err := strconv.ParseFloat(capacityStr, 64)
	if err != nil {
		return nil, fmt.Errorf("parse capacity '%s': %w", capacityStr, err)
	}

	iused, err := strconv.ParseFloat(iusedStr, 64)
	if err != nil {
		return nil, fmt.Errorf("parse iused '%s': %w", iusedStr, err)
	}

	return &pb.FilesystemUsage{
		MountPoint:   "/",
		Filesystem:   fields[0],
		PercentUsed:  capacity,
		InodePercent: iused,
	}, nil
}

// collectFilesystemUsageWindows collects filesystem usage on Windows using PowerShell/WMI.
//
// Command: powershell -Command "Get-CimInstance Win32_LogicalDisk | Where-Object DriveType -eq 3 | Select-Object DeviceID, Size, FreeSpace"
// We compute percent used from Size and FreeSpace.
// Inode percentage is not applicable on Windows (returns 0).
func collectFilesystemUsageWindows(ctx context.Context) (*pb.FilesystemUsage, error) {
	cmd := execCommand(ctx, "powershell", "-Command",
		`Get-CimInstance Win32_LogicalDisk | Where-Object DriveType -eq 3 | ForEach-Object {
         $total = [double]$_.Size;
         $free = [double]$_.FreeSpace;
         $used = $total - $free;
         $pct = 0.0;
         if ($total -gt 0) { $pct = ($used / $total) * 100.0 };
         Write-Output "$($_.DeviceID) $total $free $pct"
        }`)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec Get-CimInstance Win32_LogicalDisk: %w", err)
	}

	lines := strings.Split(string(output), "\n")

	// Find the system drive (C:) entry
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		deviceID := fields[0]
		// We are interested in the system drive "C:"
		if !strings.EqualFold(deviceID, "C:") {
			continue
		}

		pcent, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			return nil, fmt.Errorf("parse percent used '%s': %w", fields[3], err)
		}

		return &pb.FilesystemUsage{
			MountPoint:   deviceID + "\\",
			Filesystem:   deviceID,
			PercentUsed:  pcent,
			InodePercent: 0, // Not applicable on Windows
		}, nil
	}

	return nil, fmt.Errorf("system drive C: not found in logical disk output")
}
