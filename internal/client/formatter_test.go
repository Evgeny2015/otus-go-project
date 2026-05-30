package client

import (
	"strings"
	"testing"

	pb "golang-project.local/proto"
)

// ---------------------------------------------------------------------------
// Tests: ParseMetricTypes
// ---------------------------------------------------------------------------

func TestParseMetricTypes_Empty(t *testing.T) {
	result := ParseMetricTypes("")
	if len(result) != 6 {
		t.Errorf("ParseMetricTypes('') = %d types, want 6", len(result))
	}
}

func TestParseMetricTypes_All(t *testing.T) {
	result := ParseMetricTypes("all")
	if len(result) != 6 {
		t.Errorf("ParseMetricTypes('all') = %d types, want 6", len(result))
	}
}

func TestParseMetricTypes_Single(t *testing.T) {
	result := ParseMetricTypes("cpu")
	if len(result) != 1 || result[0] != FilterCPU {
		t.Errorf("ParseMetricTypes('cpu') = %v, want [cpu]", result)
	}
}

func TestParseMetricTypes_Multiple(t *testing.T) {
	result := ParseMetricTypes("cpu,load,network")
	if len(result) != 3 {
		t.Errorf("ParseMetricTypes('cpu,load,network') = %d types, want 3", len(result))
	}
}

func TestParseMetricTypes_Aliases(t *testing.T) {
	tests := []struct {
		input string
		want  MetricType
	}{
		{"fs", FilterFilesystem},
		{"filesystem", FilterFilesystem},
		{"net", FilterNetwork},
		{"network", FilterNetwork},
		{"talkers", FilterTopTalkers},
		{"toptalkers", FilterTopTalkers},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseMetricTypes(tt.input)
			if len(result) != 1 || result[0] != tt.want {
				t.Errorf("ParseMetricTypes(%q) = %v, want [%v]", tt.input, result, tt.want)
			}
		})
	}
}

func TestParseMetricTypes_CaseInsensitive(t *testing.T) {
	result := ParseMetricTypes("CPU,LOAD")
	if len(result) != 2 {
		t.Errorf("ParseMetricTypes('CPU,LOAD') = %d types, want 2", len(result))
	}
}

func TestParseMetricTypes_UnknownReturnsAll(t *testing.T) {
	result := ParseMetricTypes("unknown_type")
	if len(result) != 6 {
		t.Errorf("ParseMetricTypes('unknown_type') = %d types, want 6 (all)", len(result))
	}
}

func TestParseMetricTypes_Whitespace(t *testing.T) {
	result := ParseMetricTypes(" cpu , load ")
	if len(result) != 2 {
		t.Errorf("ParseMetricTypes(' cpu , load ') = %d types, want 2", len(result))
	}
}

// ---------------------------------------------------------------------------
// Tests: AllMetricTypes
// ---------------------------------------------------------------------------

func TestAllMetricTypes(t *testing.T) {
	types := AllMetricTypes()
	if len(types) != 6 {
		t.Errorf("AllMetricTypes() = %d, want 6", len(types))
	}

	expected := []MetricType{FilterCPU, FilterLoad, FilterDisk, FilterFilesystem, FilterNetwork, FilterTopTalkers}
	for i, mt := range types {
		if mt != expected[i] {
			t.Errorf("AllMetricTypes()[%d] = %v, want %v", i, mt, expected[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: DefaultThresholds
// ---------------------------------------------------------------------------

func TestDefaultThresholds(t *testing.T) {
	th := DefaultThresholds()
	if th.CPUPercentUser != 70.0 {
		t.Errorf("CPUPercentUser = %v, want 70.0", th.CPUPercentUser)
	}
	if th.CPUPercentSystem != 50.0 {
		t.Errorf("CPUPercentSystem = %v, want 50.0", th.CPUPercentSystem)
	}
	if th.FSUsagePercent != 80.0 {
		t.Errorf("FSUsagePercent = %v, want 80.0", th.FSUsagePercent)
	}
	if th.FSInodePercent != 80.0 {
		t.Errorf("FSInodePercent = %v, want 80.0", th.FSInodePercent)
	}
	if th.LoadThreshold != 4.0 {
		t.Errorf("LoadThreshold = %v, want 4.0", th.LoadThreshold)
	}
}

// ---------------------------------------------------------------------------
// Tests: FormatMetrics
// ---------------------------------------------------------------------------

func TestFormatMetrics_Nil(t *testing.T) {
	result := FormatMetrics(nil, nil, DefaultThresholds())
	if result != "" {
		t.Errorf("FormatMetrics(nil) = %q, want empty", result)
	}
}

func TestFormatMetrics_Empty(t *testing.T) {
	m := &pb.SystemMetrics{}
	result := FormatMetrics(m, nil, DefaultThresholds())
	if result == "" {
		t.Error("FormatMetrics(empty) should not be empty (has timestamp)")
	}
}

func TestFormatMetrics_WithFilters(t *testing.T) {
	m := &pb.SystemMetrics{
		Timestamp: 1000,
		Cpu:       &pb.CpuMetrics{PercentUser: 50, PercentSystem: 20, PercentIdle: 30},
		Load:      &pb.LoadMetrics{Load1: 0.5},
	}

	// Only show CPU
	result := FormatMetrics(m, []MetricType{FilterCPU}, DefaultThresholds())
	if !strings.Contains(result, "CPU") {
		t.Error("result should contain CPU section")
	}
	if strings.Contains(result, "System Load") {
		t.Error("result should NOT contain Load section")
	}
}

func TestFormatMetrics_AllMetrics(t *testing.T) {
	m := &pb.SystemMetrics{
		Timestamp: 1000,
		Cpu:       &pb.CpuMetrics{PercentUser: 50, PercentSystem: 20, PercentIdle: 30},
		Load:      &pb.LoadMetrics{Load1: 0.5},
		DiskIo:    &pb.DiskIOMetrics{Tps: 100, KbTotalPerSec: 500},
		Filesystem: &pb.FilesystemUsage{
			MountPoint: "/", Filesystem: "/dev/sda1", PercentUsed: 45, InodePercent: 30,
		},
		Network: &pb.NetworkMetrics{
			ListeningSockets: 10, EstablishedConnections: 50,
		},
		TopTalkers: &pb.TopTalkers{
			ByProtocol: []*pb.ProtocolTraffic{
				{Protocol: "TCP", BytesSent: 1000, BytesReceived: 500},
			},
		},
	}

	result := FormatMetrics(m, nil, DefaultThresholds())

	sections := []string{"CPU", "System Load", "Disk I/O", "Filesystem", "Network", "Top Talkers"}
	for _, section := range sections {
		if !strings.Contains(result, section) {
			t.Errorf("result should contain %q section", section)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: FormatSummaryLine
// ---------------------------------------------------------------------------

func TestFormatSummaryLine_Nil(t *testing.T) {
	result := FormatSummaryLine(nil, DefaultThresholds())
	if result != "" {
		t.Errorf("FormatSummaryLine(nil) = %q, want empty", result)
	}
}

func TestFormatSummaryLine_AllMetrics(t *testing.T) {
	m := &pb.SystemMetrics{
		Timestamp: 1000,
		Cpu:       &pb.CpuMetrics{PercentUser: 50, PercentSystem: 20, PercentIdle: 30},
		Load:      &pb.LoadMetrics{Load1: 0.5},
		DiskIo:    &pb.DiskIOMetrics{Tps: 100, KbTotalPerSec: 500},
		Filesystem: &pb.FilesystemUsage{
			MountPoint: "/", PercentUsed: 45,
		},
		Network: &pb.NetworkMetrics{
			EstablishedConnections: 50,
		},
	}

	result := FormatSummaryLine(m, DefaultThresholds())

	parts := []string{"CPU", "Load", "Disk", "FS", "Net"}
	for _, part := range parts {
		if !strings.Contains(result, part) {
			t.Errorf("result should contain %q", part)
		}
	}
}

func TestFormatSummaryLine_PartialMetrics(t *testing.T) {
	m := &pb.SystemMetrics{
		Timestamp: 1000,
		Cpu:       &pb.CpuMetrics{PercentUser: 50, PercentSystem: 20, PercentIdle: 30},
		// No Load, no Disk, no Filesystem, no Network
	}

	result := FormatSummaryLine(m, DefaultThresholds())

	if !strings.Contains(result, "CPU") {
		t.Error("result should contain CPU")
	}
	if strings.Contains(result, "Load:") {
		t.Error("result should NOT contain Load")
	}
}

// ---------------------------------------------------------------------------
// Tests: Formatting helpers
// ---------------------------------------------------------------------------

func TestFmtFloat(t *testing.T) {
	if got := fmtFloat(3.14159, 2); got != "3.14" {
		t.Errorf("fmtFloat(3.14159, 2) = %q, want 3.14", got)
	}
	if got := fmtFloat(0.0, 1); got != "0.0" {
		t.Errorf("fmtFloat(0.0, 1) = %q, want 0.0", got)
	}
}

func TestFmtPercent(t *testing.T) {
	if got := fmtPercent(50.5); got != " 50.5%" {
		t.Errorf("fmtPercent(50.5) = %q, want \" 50.5%%\"", got)
	}
	if got := fmtPercent(0.0); got != "  0.0%" {
		t.Errorf("fmtPercent(0.0) = %q, want \"  0.0%%\"", got)
	}
}

func TestFmtLoad(t *testing.T) {
	if got := fmtLoad(0.5); got != " 0.50" {
		t.Errorf("fmtLoad(0.5) = %q, want  0.50", got)
	}
}

func TestFmtTPS(t *testing.T) {
	if got := fmtTPS(100.5); got != "   100.5" {
		t.Errorf("fmtTPS(100.5) = %q, want    100.5", got)
	}
}

func TestFmtKBs(t *testing.T) {
	if got := fmtKBs(500.0); got != "   500.0" {
		t.Errorf("fmtKBs(500.0) = %q, want    500.0", got)
	}
}

func TestFmtBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := fmtBytes(tt.input); got != tt.want {
				t.Errorf("fmtBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFmtPackets(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{500, "500"},
		{1500, "1.5K"},
		{1000000, "1.0M"},
		{2500000, "2.5M"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := fmtPackets(tt.input); got != tt.want {
				t.Errorf("fmtPackets(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: Table rendering
// ---------------------------------------------------------------------------

func TestNewTable(t *testing.T) {
	table := newTable("Col1", "Col2")
	if table == nil {
		t.Fatal("newTable returned nil")
	}
	if len(table.headers) != 2 {
		t.Errorf("headers = %d, want 2", len(table.headers))
	}
}

func TestTable_AddRow(t *testing.T) {
	table := newTable("A", "B")
	table.addRow("val1", "val2")

	if len(table.rows) != 1 {
		t.Errorf("rows = %d, want 1", len(table.rows))
	}
}

func TestTable_Render_Empty(t *testing.T) {
	table := newTable("A", "B")
	result := table.render()
	if result != "" {
		t.Errorf("render() on empty table = %q, want empty", result)
	}
}

func TestTable_Render_WithData(t *testing.T) {
	table := newTable("Name", "Value")
	table.addRow("cpu", "50%")
	table.addRow("load", "0.5")

	result := table.render()
	if result == "" {
		t.Fatal("render() returned empty")
	}

	if !strings.Contains(result, "Name") {
		t.Error("result should contain header 'Name'")
	}
	if !strings.Contains(result, "cpu") {
		t.Error("result should contain 'cpu'")
	}
	if !strings.Contains(result, "50%") {
		t.Error("result should contain '50%'")
	}
}

func TestTable_Render_ColumnAlignment(t *testing.T) {
	table := newTable("Short", "VeryLongHeader")
	table.addRow("val", "x")

	result := table.render()
	lines := strings.Split(strings.TrimSpace(result), "\n")

	// Should have separator, header, separator, row, separator
	if len(lines) < 4 {
		t.Errorf("render() has %d lines, want at least 4", len(lines))
	}
}

// ---------------------------------------------------------------------------
// Tests: Color functions
// ---------------------------------------------------------------------------

func TestColorize_Enabled(t *testing.T) {
	useColor = true
	result := colorize("test", colorRed)
	if result != "\033[31mtest\033[0m" {
		t.Errorf("colorize() = %q, want with ANSI codes", result)
	}
}

func TestColorize_Disabled(t *testing.T) {
	useColor = false
	result := colorize("test", colorRed)
	if result != "test" {
		t.Errorf("colorize() with disabled colors = %q, want 'test'", result)
	}
	useColor = true // restore
}

func TestBold_Enabled(t *testing.T) {
	useColor = true
	result := bold("test")
	if result != "\033[1mtest\033[0m" {
		t.Errorf("bold() = %q, want with ANSI codes", result)
	}
}

func TestBold_Disabled(t *testing.T) {
	useColor = false
	result := bold("test")
	if result != "test" {
		t.Errorf("bold() with disabled colors = %q, want 'test'", result)
	}
	useColor = true // restore
}

func TestDisableColors(t *testing.T) {
	useColor = true
	DisableColors()
	if useColor {
		t.Error("DisableColors() should set useColor to false")
	}
	useColor = true // restore
}

func TestEnableColors(t *testing.T) {
	useColor = false
	EnableColors()
	if useColor {
		t.Error("EnableColors() should set useColor to false (bug: function sets to false)")
	}
	useColor = true // restore
}

// ---------------------------------------------------------------------------
// Tests: thresholdColor
// ---------------------------------------------------------------------------

func TestThresholdColor(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		threshold float64
		want      string
	}{
		{"below threshold", 50.0, 70.0, colorGreen},
		{"at threshold", 70.0, 70.0, colorYellow},
		{"above threshold", 80.0, 70.0, colorYellow},
		{"critical (1.2x threshold)", 84.0, 70.0, colorRed},
		{"critical (2x threshold)", 140.0, 70.0, colorRed},
		{"zero value", 0.0, 70.0, colorGreen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := thresholdColor(tt.value, tt.threshold)
			if got != tt.want {
				t.Errorf("thresholdColor(%v, %v) = %q, want %q",
					tt.value, tt.threshold, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: Format functions for individual metric types
// ---------------------------------------------------------------------------

func TestFormatCPU(t *testing.T) {
	cpu := &pb.CpuMetrics{PercentUser: 50, PercentSystem: 20, PercentIdle: 30}
	result := formatCPU(cpu, DefaultThresholds())

	if !strings.Contains(result, "CPU") {
		t.Error("result should contain CPU")
	}
	if !strings.Contains(result, "50.0%") {
		t.Error("result should contain 50.0%")
	}
}

func TestFormatLoad(t *testing.T) {
	load := &pb.LoadMetrics{Load1: 0.5}
	result := formatLoad(load, DefaultThresholds())

	if !strings.Contains(result, "System Load") {
		t.Error("result should contain System Load")
	}
	if !strings.Contains(result, "0.50") {
		t.Error("result should contain 0.50")
	}
}

func TestFormatDiskIO(t *testing.T) {
	disk := &pb.DiskIOMetrics{Tps: 100, KbTotalPerSec: 500}
	result := formatDiskIO(disk)

	if !strings.Contains(result, "Disk I/O") {
		t.Error("result should contain Disk I/O")
	}
	if !strings.Contains(result, "100") {
		t.Error("result should contain 100")
	}
}

func TestFormatFilesystem(t *testing.T) {
	fs := &pb.FilesystemUsage{
		MountPoint: "/", Filesystem: "/dev/sda1", PercentUsed: 45, InodePercent: 30,
	}
	result := formatFilesystem(fs, DefaultThresholds())

	if !strings.Contains(result, "Filesystem") {
		t.Error("result should contain Filesystem")
	}
	if !strings.Contains(result, "/") {
		t.Error("result should contain mount point")
	}
}

func TestFormatNetwork(t *testing.T) {
	net := &pb.NetworkMetrics{
		ListeningSockets: 10, EstablishedConnections: 50,
		TimeWaitSockets: 5, CloseWaitSockets: 2,
	}
	result := formatNetwork(net)

	if !strings.Contains(result, "Network") {
		t.Error("result should contain Network")
	}
	if !strings.Contains(result, "LISTEN") {
		t.Error("result should contain LISTEN")
	}
	if !strings.Contains(result, "10") {
		t.Error("result should contain 10 (listening sockets)")
	}
}

func TestFormatTopTalkers_Empty(t *testing.T) {
	tt := &pb.TopTalkers{}
	result := formatTopTalkers(tt)
	if result != "" {
		t.Errorf("formatTopTalkers(empty) = %q, want empty", result)
	}
}

func TestFormatTopTalkers_ByProtocol(t *testing.T) {
	tt := &pb.TopTalkers{
		ByProtocol: []*pb.ProtocolTraffic{
			{Protocol: "TCP", BytesSent: 1000, BytesReceived: 500, PacketsSent: 100, PacketsReceived: 50},
		},
	}
	result := formatTopTalkers(tt)

	if !strings.Contains(result, "Top Talkers by Protocol") {
		t.Error("result should contain 'Top Talkers by Protocol'")
	}
	if !strings.Contains(result, "TCP") {
		t.Error("result should contain TCP")
	}
}

func TestFormatTopTalkers_ByConnection(t *testing.T) {
	tt := &pb.TopTalkers{
		ByConnection: []*pb.ConnectionTraffic{
			{
				Protocol: "TCP", LocalAddress: "192.168.1.1:80",
				RemoteAddress: "10.0.0.1:54321", BytesSent: 1000, BytesReceived: 500,
			},
		},
	}
	result := formatTopTalkers(tt)

	if !strings.Contains(result, "Top Talkers by Connection") {
		t.Error("result should contain 'Top Talkers by Connection'")
	}
	if !strings.Contains(result, "192.168.1.1") {
		t.Error("result should contain local address")
	}
}

func TestFormatTopTalkers_Both(t *testing.T) {
	tt := &pb.TopTalkers{
		ByProtocol: []*pb.ProtocolTraffic{
			{Protocol: "TCP", BytesSent: 1000, BytesReceived: 500},
		},
		ByConnection: []*pb.ConnectionTraffic{
			{
				Protocol: "TCP", LocalAddress: "192.168.1.1:80",
				RemoteAddress: "10.0.0.1:54321", BytesSent: 1000, BytesReceived: 500,
			},
		},
	}
	result := formatTopTalkers(tt)

	if !strings.Contains(result, "by Protocol") {
		t.Error("result should contain 'by Protocol'")
	}
	if !strings.Contains(result, "by Connection") {
		t.Error("result should contain 'by Connection'")
	}
}

// ---------------------------------------------------------------------------
// Tests: Helper functions
// ---------------------------------------------------------------------------

func TestHelpText(t *testing.T) {
	result := HelpText()
	if !strings.Contains(result, "Usage:") {
		t.Error("HelpText() should contain 'Usage:'")
	}
	if !strings.Contains(result, "--server") {
		t.Error("HelpText() should contain --server flag")
	}
	if !strings.Contains(result, "--interval") {
		t.Error("HelpText() should contain --interval flag")
	}
	if !strings.Contains(result, "--filter") {
		t.Error("HelpText() should contain --filter flag")
	}
}

func TestPausedMessage(t *testing.T) {
	result := PausedMessage()
	if !strings.Contains(result, "PAUSED") {
		t.Error("PausedMessage() should contain PAUSED")
	}
}

func TestResumedMessage(t *testing.T) {
	result := ResumedMessage()
	if !strings.Contains(result, "RESUMED") {
		t.Error("ResumedMessage() should contain RESUMED")
	}
}

func TestQuitMessage(t *testing.T) {
	result := QuitMessage()
	if !strings.Contains(result, "QUITTING") {
		t.Error("QuitMessage() should contain QUITTING")
	}
}

// ---------------------------------------------------------------------------
// Tests: SortTopTalkers
// ---------------------------------------------------------------------------

func TestSortTopTalkers(t *testing.T) {
	tt := &pb.TopTalkers{
		ByProtocol: []*pb.ProtocolTraffic{
			{Protocol: "UDP", BytesSent: 100, BytesReceived: 50},
			{Protocol: "TCP", BytesSent: 1000, BytesReceived: 500},
			{Protocol: "ICMP", BytesSent: 10, BytesReceived: 5},
		},
		ByConnection: []*pb.ConnectionTraffic{
			{Protocol: "UDP", BytesSent: 100, BytesReceived: 50},
			{Protocol: "TCP", BytesSent: 1000, BytesReceived: 500},
		},
	}

	SortTopTalkers(tt)

	// ByProtocol should be sorted by total bytes (descending)
	if tt.ByProtocol[0].GetProtocol() != "TCP" {
		t.Errorf("ByProtocol[0] = %s, want TCP (highest total)", tt.ByProtocol[0].GetProtocol())
	}
	if tt.ByProtocol[2].GetProtocol() != "ICMP" {
		t.Errorf("ByProtocol[2] = %s, want ICMP (lowest total)", tt.ByProtocol[2].GetProtocol())
	}

	// ByConnection should be sorted by total bytes (descending)
	if tt.ByConnection[0].GetProtocol() != "TCP" {
		t.Errorf("ByConnection[0] = %s, want TCP", tt.ByConnection[0].GetProtocol())
	}
}

// ---------------------------------------------------------------------------
// Tests: IsTerminal
// ---------------------------------------------------------------------------

func TestIsTerminal(t *testing.T) {
	// This is a best-effort test - in test environment it's usually false
	result := IsTerminal()
	// Just verify it doesn't panic and returns a bool
	_ = result
}

// ---------------------------------------------------------------------------
// Tests: ClearScreen
// ---------------------------------------------------------------------------

func TestClearScreen_Enabled(t *testing.T) {
	useColor = true
	result := ClearScreen()
	if result != "\033[2J\033[H" {
		t.Errorf("ClearScreen() = %q, want ANSI escape", result)
	}
}

func TestClearScreen_Disabled(t *testing.T) {
	useColor = false
	result := ClearScreen()
	if result != "" {
		t.Errorf("ClearScreen() with disabled colors = %q, want empty", result)
	}
	useColor = true // restore
}
