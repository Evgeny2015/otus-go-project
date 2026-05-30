// Package client provides output formatting utilities for the monitoring
// client. It supports table-formatted terminal output with optional
// color-coding for threshold-based highlighting.
package client

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	pb "golang-project.local/proto"
)

// ---------------------------------------------------------------------------
// ANSI color codes for terminal output
// ---------------------------------------------------------------------------

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

// useColor indicates whether ANSI color codes should be emitted.
// It is set to false when output is not a terminal.
var useColor = true

// DisableColors disables ANSI color output. Useful when redirecting
// output to a file or when the terminal does not support colors.
func DisableColors() { useColor = false }

// EnableColors enables ANSI color output (default).
func EnableColors() { useColor = false }

// colorize wraps text in ANSI color codes if colors are enabled.
func colorize(text string, color string) string {
	if !useColor {
		return text
	}
	return color + text + colorReset
}

// bold returns text wrapped in ANSI bold codes if colors are enabled.
func bold(text string) string {
	if !useColor {
		return text
	}
	return colorBold + text + colorReset
}

// ---------------------------------------------------------------------------
// MetricType filter
// ---------------------------------------------------------------------------

// MetricType represents a metric category that can be filtered.
type MetricType string

const (
	FilterCPU        MetricType = "cpu"
	FilterLoad       MetricType = "load"
	FilterDisk       MetricType = "disk"
	FilterFilesystem MetricType = "filesystem"
	FilterNetwork    MetricType = "network"
	FilterTopTalkers MetricType = "toptalkers"
)

// AllMetricTypes returns all available metric type filters.
func AllMetricTypes() []MetricType {
	return []MetricType{FilterCPU, FilterLoad, FilterDisk, FilterFilesystem, FilterNetwork, FilterTopTalkers}
}

// ParseMetricTypes parses a comma-separated list of metric type names.
// Returns all types if the input is empty or contains "all".
func ParseMetricTypes(s string) []MetricType {
	if s == "" || s == "all" {
		return AllMetricTypes()
	}
	var result []MetricType
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		switch part {
		case "cpu":
			result = append(result, FilterCPU)
		case "load":
			result = append(result, FilterLoad)
		case "disk":
			result = append(result, FilterDisk)
		case "filesystem", "fs":
			result = append(result, FilterFilesystem)
		case "network", "net":
			result = append(result, FilterNetwork)
		case "toptalkers", "talkers":
			result = append(result, FilterTopTalkers)
		}
	}
	if len(result) == 0 {
		return AllMetricTypes()
	}
	return result
}

// ---------------------------------------------------------------------------
// Thresholds for color-coded output
// ---------------------------------------------------------------------------

// Thresholds defines the warning and critical boundaries for each metric.
type Thresholds struct {
	CPUPercentUser   float64 // warning if > this value
	CPUPercentSystem float64
	FSUsagePercent   float64
	FSInodePercent   float64
	LoadThreshold    float64
}

// DefaultThresholds returns sensible default threshold values.
func DefaultThresholds() Thresholds {
	return Thresholds{
		CPUPercentUser:   70.0,
		CPUPercentSystem: 50.0,
		FSUsagePercent:   80.0,
		FSInodePercent:   80.0,
		LoadThreshold:    4.0,
	}
}

// thresholdColor returns a color based on whether the value exceeds
// the given threshold. Green for OK, yellow for warning, red for critical.
func thresholdColor(value, warnThreshold float64) string {
	if value >= warnThreshold*1.2 {
		return colorRed
	}
	if value >= warnThreshold {
		return colorYellow
	}
	return colorGreen
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

// fmtFloat formats a float64 with the given precision.
func fmtFloat(v float64, prec int) string {
	return fmt.Sprintf("%.*f", prec, v)
}

// fmtPercent formats a percentage value with one decimal place.
func fmtPercent(v float64) string {
	return fmt.Sprintf("%5.1f%%", v)
}

// fmtLoad formats a load average value.
func fmtLoad(v float64) string {
	return fmt.Sprintf("%5.2f", v)
}

// fmtTPS formats a transactions-per-second value.
func fmtTPS(v float64) string {
	return fmt.Sprintf("%8.1f", v)
}

// fmtKBs formats a KB/s value.
func fmtKBs(v float64) string {
	return fmt.Sprintf("%8.1f", v)
}

// fmtBytes formats a byte count in human-readable form.
func fmtBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	val := float64(b) / float64(div)
	switch exp {
	case 0:
		return fmt.Sprintf("%.1f KB", val)
	case 1:
		return fmt.Sprintf("%.1f MB", val)
	case 2:
		return fmt.Sprintf("%.1f GB", val)
	default:
		return fmt.Sprintf("%.1f TB", val)
	}
}

// fmtPackets formats a packet count.
func fmtPackets(p int64) string {
	if p >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(p)/1_000_000)
	}
	if p >= 1000 {
		return fmt.Sprintf("%.1fK", float64(p)/1000)
	}
	return fmt.Sprintf("%d", p)
}

// ---------------------------------------------------------------------------
// Table rendering
// ---------------------------------------------------------------------------

// table is a simple table builder for aligned column output.
type table struct {
	headers []string
	rows    [][]string
	widths  []int
}

func newTable(headers ...string) *table {
	t := &table{headers: headers, widths: make([]int, len(headers))}
	for i, h := range headers {
		t.widths[i] = len(h)
	}
	return t
}

func (t *table) addRow(cols ...string) {
	if len(cols) > len(t.widths) {
		// Extend widths if needed
		newWidths := make([]int, len(cols))
		copy(newWidths, t.widths)
		t.widths = newWidths
	}
	for i, c := range cols {
		if i < len(t.widths) && len(c) > t.widths[i] {
			t.widths[i] = len(c)
		}
	}
	t.rows = append(t.rows, cols)
}

func (t *table) render() string {
	var b strings.Builder

	// No data
	if len(t.rows) == 0 {
		return ""
	}

	// Ensure all rows have the same number of columns
	maxCols := len(t.widths)
	for _, row := range t.rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}
	// Extend widths if needed
	for len(t.widths) < maxCols {
		t.widths = append(t.widths, 0)
	}

	// Build separator line
	sep := "+"
	for _, w := range t.widths {
		sep += strings.Repeat("-", w+2) + "+"
	}
	sep += "\n"

	// Header
	b.WriteString(sep)
	b.WriteString("|")
	for i, h := range t.headers {
		b.WriteString(" ")
		b.WriteString(fmt.Sprintf("%-*s", t.widths[i], h))
		b.WriteString(" |")
	}
	b.WriteString("\n")
	b.WriteString(sep)

	// Rows
	for _, row := range t.rows {
		b.WriteString("|")
		for i, col := range row {
			if i < len(t.widths) {
				b.WriteString(" ")
				b.WriteString(fmt.Sprintf("%-*s", t.widths[i], col))
				b.WriteString(" |")
			}
		}
		// Pad missing columns
		for i := len(row); i < len(t.widths); i++ {
			b.WriteString(" ")
			b.WriteString(fmt.Sprintf("%-*s", t.widths[i], ""))
			b.WriteString(" |")
		}
		b.WriteString("\n")
	}
	b.WriteString(sep)

	return b.String()
}

// ---------------------------------------------------------------------------
// Metric display formatters
// ---------------------------------------------------------------------------

// FormatMetrics formats a SystemMetrics message into a human-readable string.
// The filters parameter controls which metric types are displayed; if nil or
// empty, all metrics are shown.
func FormatMetrics(m *pb.SystemMetrics, filters []MetricType, thresholds Thresholds) string {
	if m == nil {
		return ""
	}

	var b strings.Builder

	// Timestamp header
	ts := time.Unix(0, int64(m.GetTimestamp()))
	b.WriteString(bold(fmt.Sprintf("\n=== Metrics at %s ===\n", ts.Format("15:04:05.000"))))
	b.WriteString(fmt.Sprintf("   Interval: %ds  Window: %ds\n", m.GetIntervalSeconds(), m.GetWindowSeconds()))

	shouldShow := func(mt MetricType) bool {
		if len(filters) == 0 {
			return true
		}
		for _, f := range filters {
			if f == mt {
				return true
			}
		}
		return false
	}

	// CPU metrics
	if m.GetCpu() != nil && shouldShow(FilterCPU) {
		b.WriteString(formatCPU(m.GetCpu(), thresholds))
	}

	// Load metrics
	if m.GetLoad() != nil && shouldShow(FilterLoad) {
		b.WriteString(formatLoad(m.GetLoad(), thresholds))
	}

	// Disk I/O metrics
	if m.GetDiskIo() != nil && shouldShow(FilterDisk) {
		b.WriteString(formatDiskIO(m.GetDiskIo()))
	}

	// Filesystem metrics
	if m.GetFilesystem() != nil && shouldShow(FilterFilesystem) {
		b.WriteString(formatFilesystem(m.GetFilesystem(), thresholds))
	}

	// Network metrics
	if m.GetNetwork() != nil && shouldShow(FilterNetwork) {
		b.WriteString(formatNetwork(m.GetNetwork()))
	}

	// Top talkers
	if m.GetTopTalkers() != nil && shouldShow(FilterTopTalkers) {
		b.WriteString(formatTopTalkers(m.GetTopTalkers()))
	}

	return b.String()
}

// formatCPU formats CPU metrics with color-coded thresholds.
func formatCPU(cpu *pb.CpuMetrics, t Thresholds) string {
	var b strings.Builder
	b.WriteString(bold("\n  CPU Usage:\n"))

	userColor := thresholdColor(cpu.GetPercentUser(), t.CPUPercentUser)
	sysColor := thresholdColor(cpu.GetPercentSystem(), t.CPUPercentSystem)

	b.WriteString(fmt.Sprintf("    %s: %s  %s: %s  %s: %s\n",
		colorize("User", userColor), fmtPercent(cpu.GetPercentUser()),
		colorize("System", sysColor), fmtPercent(cpu.GetPercentSystem()),
		colorize("Idle", colorGreen), fmtPercent(cpu.GetPercentIdle()),
	))
	return b.String()
}

// formatLoad formats load average metrics.
func formatLoad(load *pb.LoadMetrics, t Thresholds) string {
	var b strings.Builder
	b.WriteString(bold("\n  System Load:\n"))

	loadColor := thresholdColor(load.GetLoad1(), t.LoadThreshold)
	b.WriteString(fmt.Sprintf("    %s: %s\n",
		colorize("Load 1min", loadColor), fmtLoad(load.GetLoad1())))
	return b.String()
}

// formatDiskIO formats disk I/O metrics.
func formatDiskIO(disk *pb.DiskIOMetrics) string {
	var b strings.Builder
	b.WriteString(bold("\n  Disk I/O:\n"))
	b.WriteString(fmt.Sprintf("    TPS: %s  KB/s: %s\n",
		fmtTPS(disk.GetTps()), fmtKBs(disk.GetKbTotalPerSec())))
	return b.String()
}

// formatFilesystem formats filesystem usage metrics with color-coded thresholds.
func formatFilesystem(fs *pb.FilesystemUsage, t Thresholds) string {
	var b strings.Builder
	b.WriteString(bold("\n  Filesystem:\n"))

	usedColor := thresholdColor(fs.GetPercentUsed(), t.FSUsagePercent)
	inodeColor := thresholdColor(fs.GetInodePercent(), t.FSInodePercent)

	b.WriteString(fmt.Sprintf("    Mount: %s\n", fs.GetMountPoint()))
	b.WriteString(fmt.Sprintf("    Device: %s\n", fs.GetFilesystem()))
	b.WriteString(fmt.Sprintf("    Used: %s  Inode: %s\n",
		colorize(fmtPercent(fs.GetPercentUsed()), usedColor),
		colorize(fmtPercent(fs.GetInodePercent()), inodeColor)))
	return b.String()
}

// formatNetwork formats network connection statistics.
func formatNetwork(net *pb.NetworkMetrics) string {
	var b strings.Builder
	b.WriteString(bold("\n  Network Connections:\n"))

	t := newTable("State", "Count")
	t.addRow("LISTEN", fmt.Sprintf("%d", net.GetListeningSockets()))
	t.addRow("ESTABLISHED", fmt.Sprintf("%d", net.GetEstablishedConnections()))
	t.addRow("TIME_WAIT", fmt.Sprintf("%d", net.GetTimeWaitSockets()))
	t.addRow("CLOSE_WAIT", fmt.Sprintf("%d", net.GetCloseWaitSockets()))
	t.addRow("FIN_WAIT", fmt.Sprintf("%d", net.GetFinWaitSockets()))
	t.addRow("SYN_SENT", fmt.Sprintf("%d", net.GetSynSentSockets()))
	t.addRow("UDP", fmt.Sprintf("%d", net.GetUdpSockets()))

	// Indent the table
	tableStr := t.render()
	if tableStr != "" {
		for _, line := range strings.Split(strings.TrimRight(tableStr, "\n"), "\n") {
			b.WriteString("    ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// formatTopTalkers formats network top talkers data.
func formatTopTalkers(tt *pb.TopTalkers) string {
	var b strings.Builder

	// By protocol
	if len(tt.GetByProtocol()) > 0 {
		b.WriteString(bold("\n  Top Talkers by Protocol:\n"))
		t := newTable("Protocol", "Sent", "Received", "Pkts Sent", "Pkts Recv")
		for _, p := range tt.GetByProtocol() {
			t.addRow(
				p.GetProtocol(),
				fmtBytes(p.GetBytesSent()),
				fmtBytes(p.GetBytesReceived()),
				fmtPackets(p.GetPacketsSent()),
				fmtPackets(p.GetPacketsReceived()),
			)
		}
		tableStr := t.render()
		if tableStr != "" {
			for _, line := range strings.Split(strings.TrimRight(tableStr, "\n"), "\n") {
				b.WriteString("    ")
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
	}

	// By connection
	if len(tt.GetByConnection()) > 0 {
		b.WriteString(bold("\n  Top Talkers by Connection:\n"))
		t := newTable("Proto", "Local", "Remote", "Sent", "Recv")
		for _, c := range tt.GetByConnection() {
			t.addRow(
				c.GetProtocol(),
				c.GetLocalAddress(),
				c.GetRemoteAddress(),
				fmtBytes(c.GetBytesSent()),
				fmtBytes(c.GetBytesReceived()),
			)
		}
		tableStr := t.render()
		if tableStr != "" {
			for _, line := range strings.Split(strings.TrimRight(tableStr, "\n"), "\n") {
				b.WriteString("    ")
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Summary line (compact, single-line format)
// ---------------------------------------------------------------------------

// FormatSummaryLine formats a one-line summary of the most important metrics.
// Useful for continuous scrolling display.
func FormatSummaryLine(m *pb.SystemMetrics, thresholds Thresholds) string {
	if m == nil {
		return ""
	}

	ts := time.Unix(0, int64(m.GetTimestamp()))
	var parts []string

	parts = append(parts, ts.Format("15:04:05"))

	if cpu := m.GetCpu(); cpu != nil {
		userColor := thresholdColor(cpu.GetPercentUser(), thresholds.CPUPercentUser)
		parts = append(parts, colorize(fmt.Sprintf("CPU:%.1f%%", cpu.GetPercentUser()), userColor))
	}

	if load := m.GetLoad(); load != nil {
		loadColor := thresholdColor(load.GetLoad1(), thresholds.LoadThreshold)
		parts = append(parts, colorize(fmt.Sprintf("Load:%.2f", load.GetLoad1()), loadColor))
	}

	if disk := m.GetDiskIo(); disk != nil {
		parts = append(parts, fmt.Sprintf("Disk:%.1ftps", disk.GetTps()))
	}

	if fs := m.GetFilesystem(); fs != nil {
		usedColor := thresholdColor(fs.GetPercentUsed(), thresholds.FSUsagePercent)
		parts = append(parts, colorize(fmt.Sprintf("FS:%.1f%%", fs.GetPercentUsed()), usedColor))
	}

	if net := m.GetNetwork(); net != nil {
		parts = append(parts, fmt.Sprintf("Net:%dEst", net.GetEstablishedConnections()))
	}

	return strings.Join(parts, " | ")
}

// ---------------------------------------------------------------------------
// Help text
// ---------------------------------------------------------------------------

// HelpText returns the help/usage text for the client.
func HelpText() string {
	return `Usage: client [flags]

A monitoring client that connects to the system monitoring daemon and
displays real-time system metrics.

Flags:
  --server=<host:port>    gRPC server address (default: localhost:50051)
  --interval=<seconds>    Collection interval N (default: 5)
  --window=<seconds>      Aggregation window M (default: 15)
  --filter=<types>        Comma-separated metric types to display
                          (default: all)
                          Valid types: cpu, load, disk, filesystem, network,
                          toptalkers
  --no-color              Disable ANSI color output
  --compact               Use compact single-line output format
  --help, -h              Show this help message

Examples:
  client
  client --server=192.168.1.100:50051
  client --interval=10 --window=30
  client --filter=cpu,load,network
  client --compact --no-color
`
}

// ---------------------------------------------------------------------------
// Signal handling messages
// ---------------------------------------------------------------------------

// PausedMessage returns a message indicating streaming is paused.
func PausedMessage() string {
	return bold(colorize(" [PAUSED] Press 'p' to resume or 'q' to quit ", colorYellow))
}

// ResumedMessage returns a message indicating streaming is resumed.
func ResumedMessage() string {
	return bold(colorize(" [RESUMED] ", colorGreen))
}

// QuitMessage returns a message indicating the client is quitting.
func QuitMessage() string {
	return bold(colorize(" [QUITTING] ", colorRed))
}

// ---------------------------------------------------------------------------
// Terminal helper
// ---------------------------------------------------------------------------

// IsTerminal returns true if stdout is a terminal.
func IsTerminal() bool {
	// On Windows, we could use golang.org/x/term, but since we're
	// restricted to the standard library, we check via a simple stat.
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// ClearScreen returns an ANSI escape sequence to clear the terminal.
func ClearScreen() string {
	if !useColor {
		return ""
	}
	return "\033[2J\033[H"
}

// SortTopTalkers sorts top talkers data for consistent display.
func SortTopTalkers(tt *pb.TopTalkers) {
	sort.Slice(tt.ByProtocol, func(i, j int) bool {
		return tt.ByProtocol[i].GetBytesSent()+tt.ByProtocol[i].GetBytesReceived() >
			tt.ByProtocol[j].GetBytesSent()+tt.ByProtocol[j].GetBytesReceived()
	})
	sort.Slice(tt.ByConnection, func(i, j int) bool {
		return tt.ByConnection[i].GetBytesSent()+tt.ByConnection[i].GetBytesReceived() >
			tt.ByConnection[j].GetBytesSent()+tt.ByConnection[j].GetBytesReceived()
	})
}
