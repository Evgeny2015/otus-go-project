package collector

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"

	pb "golang-project.local/proto"
)

// ---------------------------------------------------------------------------
// NetworkMetrics collectors — connection state counts
// ---------------------------------------------------------------------------

// collectNetworkMetricsLinux collects network connection statistics on Linux
// using `ss -tuna` (all TCP and UDP sockets, numeric).
//
// Command: ss -tuna
// Relevant output lines:
//
//	State          Recv-Q   Send-Q   Local Address:Port    Peer Address:Port
//	LISTEN         0        128      0.0.0.0:22            0.0.0.0:*
//	ESTAB          0        0        10.0.0.1:54321        10.0.0.2:80
//	TIME-WAIT      0        0        10.0.0.1:54322        10.0.0.3:443
//	UNCONN         0        0        0.0.0.0:68            0.0.0.0:*        (UDP)
//
// We parse the State column and count occurrences.
func collectNetworkMetricsLinux(ctx context.Context) (*pb.NetworkMetrics, error) {
	cmd := execCommand(ctx, "ss", "-tuna")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec ss -tuna: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	metrics := &pb.NetworkMetrics{}

	// Skip header line
	if scanner.Scan() {
		// header consumed
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		state := fields[0]

		switch state {
		case "LISTEN":
			metrics.ListeningSockets++
		case "ESTAB":
			metrics.EstablishedConnections++
		case "TIME-WAIT":
			metrics.TimeWaitSockets++
		case "CLOSE-WAIT":
			metrics.CloseWaitSockets++
		case "FIN-WAIT-1", "FIN-WAIT-2":
			metrics.FinWaitSockets++
		case "SYN-SENT":
			metrics.SynSentSockets++
		case "UNCONN":
			// UDP sockets are shown as UNCONN
			metrics.UdpSockets++
		}
	}

	return metrics, scanner.Err()
}

// collectNetworkMetricsDarwin collects network connection statistics on macOS
// using `netstat -an -p tcp` and `netstat -an -p udp`.
//
// Commands:
//
//	netstat -an -p tcp
//	netstat -an -p udp
//
// TCP output lines:
//
//	tcp4       0      0  *.22                  *.*                    LISTEN
//	tcp4       0      0  10.0.0.1.54321        10.0.0.2.80           ESTABLISHED
//
// UDP output lines:
//
//	udp4       0      0  *.68                  *.*
func collectNetworkMetricsDarwin(ctx context.Context) (*pb.NetworkMetrics, error) {
	metrics := &pb.NetworkMetrics{}

	// Collect TCP connections
	cmd := execCommand(ctx, "netstat", "-an", "-p", "tcp")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec netstat -an -p tcp: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Active") || strings.HasPrefix(line, "Proto") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		state := fields[len(fields)-1]

		switch state {
		case "LISTEN":
			metrics.ListeningSockets++
		case "ESTABLISHED":
			metrics.EstablishedConnections++
		case "TIME_WAIT":
			metrics.TimeWaitSockets++
		case "CLOSE_WAIT":
			metrics.CloseWaitSockets++
		case "FIN_WAIT_1", "FIN_WAIT_2":
			metrics.FinWaitSockets++
		case "SYN_SENT":
			metrics.SynSentSockets++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan tcp netstat output: %w", err)
	}

	// Collect UDP sockets
	cmd = execCommand(ctx, "netstat", "-an", "-p", "udp")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec netstat -an -p udp: %w", err)
	}

	scanner = bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Active") || strings.HasPrefix(line, "Proto") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		// Any UDP line counts as a UDP socket
		metrics.UdpSockets++
	}

	return metrics, scanner.Err()
}

// collectNetworkMetricsWindows collects network connection statistics on Windows
// using `netstat -an`.
//
// Command: netstat -an
// Relevant output lines:
//
//	TCP    0.0.0.0:22             0.0.0.0:0              LISTENING
//	TCP    10.0.0.1:54321         10.0.0.2:80            ESTABLISHED
//	TCP    10.0.0.1:54322         10.0.0.3:443           TIME_WAIT
//	UDP    0.0.0.0:68             *:*                                    (no state column)
func collectNetworkMetricsWindows(ctx context.Context) (*pb.NetworkMetrics, error) {
	cmd := execCommand(ctx, "netstat", "-an")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec netstat -an: %w", err)
	}

	metrics := &pb.NetworkMetrics{}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		proto := strings.ToUpper(fields[0])
		switch proto {
		case "TCP":
			// Windows netstat output: Proto  Local  Foreign  State
			// State is the last field
			state := fields[len(fields)-1]
			switch state {
			case "LISTENING":
				metrics.ListeningSockets++
			case "ESTABLISHED":
				metrics.EstablishedConnections++
			case "TIME_WAIT":
				metrics.TimeWaitSockets++
			case "CLOSE_WAIT":
				metrics.CloseWaitSockets++
			case "FIN_WAIT_1", "FIN_WAIT_2":
				metrics.FinWaitSockets++
			case "SYN_SENT":
				metrics.SynSentSockets++
			}
		case "UDP":
			metrics.UdpSockets++
		}
	}

	return metrics, scanner.Err()
}

// ---------------------------------------------------------------------------
// TopTalkers collectors — traffic by protocol and connection
// ---------------------------------------------------------------------------

// collectTopTalkersLinux collects top talkers data on Linux.
//
// For protocol-level traffic, we parse /proc/net/dev which provides
// bytes and packets for each network interface. We aggregate by protocol
// (tcp/udp) using /proc/net/snmp which has per-protocol byte/packet counts.
//
// For connection-level traffic, we use `ss -tunaep` to list active connections
// with process info (but without byte counts, since ss doesn't provide them).
//
// Commands:
//   - cat /proc/net/snmp  — protocol-level bytes/packets
//   - ss -tunaep          — connection list with process info
func collectTopTalkersLinux(ctx context.Context) (*pb.TopTalkers, error) {
	tt := &pb.TopTalkers{}

	// --- Protocol-level traffic from /proc/net/snmp ---
	cmd := execCommand(ctx, "cat", "/proc/net/snmp")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec cat /proc/net/snmp: %w", err)
	}

	tt.ByProtocol = parseProcNetSNMP(string(output))

	// --- Connection-level list from ss -tunaep ---
	cmd = execCommand(ctx, "ss", "-tunaep")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec ss -tunaep: %w", err)
	}

	tt.ByConnection = parseSSConnections(string(output))

	return tt, nil
}

// parseProcNetSNMP parses /proc/net/snmp output to extract per-protocol
// traffic statistics (bytes and packets).
//
// Output format:
//
//	Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts
//	Tcp: 1 200 120000 -1 12345 6789 0 0 12 1000000 950000 100 0 50
//	Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors InCsumErrors
//	Udp: 500000 100 0 480000 0 0 0
func parseProcNetSNMP(output string) []*pb.ProtocolTraffic {
	var result []*pb.ProtocolTraffic

	lines := strings.Split(output, "\n")
	// We need to find Tcp and Udp sections (header + data pairs)
	for i := 0; i < len(lines)-1; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		// Check for Tcp header
		if strings.HasPrefix(line, "Tcp:") {
			tcpHeader := strings.Fields(line)
			// Next line should be data
			if i+1 < len(lines) {
				dataLine := strings.TrimSpace(lines[i+1])
				if strings.HasPrefix(dataLine, "Tcp:") {
					tcpData := strings.Fields(dataLine)
					// Find InSegs and OutSegs column indices
					inSegsIdx := -1
					outSegsIdx := -1
					for j, h := range tcpHeader {
						switch h {
						case "InSegs":
							inSegsIdx = j
						case "OutSegs":
							outSegsIdx = j
						}
					}
					if inSegsIdx > 0 && outSegsIdx > 0 && len(tcpData) > inSegsIdx && len(tcpData) > outSegsIdx {
						inSegs, _ := strconv.ParseInt(tcpData[inSegsIdx], 10, 64)
						outSegs, _ := strconv.ParseInt(tcpData[outSegsIdx], 10, 64)
						// Note: /proc/net/snmp doesn't have byte counts directly.
						// We use segments as a proxy. For byte counts, we'd need
						// /proc/net/netstat which has Extended TcpStats with BytesReceived/Acked.
						result = append(result, &pb.ProtocolTraffic{
							Protocol:        "tcp",
							PacketsReceived: inSegs,
							PacketsSent:     outSegs,
						})
					}
				}
			}
			i++ // skip the data line we just processed
			continue
		}

		// Check for Udp header
		if strings.HasPrefix(line, "Udp:") {
			udpHeader := strings.Fields(line)
			if i+1 < len(lines) {
				dataLine := strings.TrimSpace(lines[i+1])
				if strings.HasPrefix(dataLine, "Udp:") {
					udpData := strings.Fields(dataLine)
					inDatagramsIdx := -1
					outDatagramsIdx := -1
					for j, h := range udpHeader {
						switch h {
						case "InDatagrams":
							inDatagramsIdx = j
						case "OutDatagrams":
							outDatagramsIdx = j
						}
					}
					if inDatagramsIdx > 0 && outDatagramsIdx > 0 && len(udpData) > inDatagramsIdx && len(udpData) > outDatagramsIdx {
						inDg, _ := strconv.ParseInt(udpData[inDatagramsIdx], 10, 64)
						outDg, _ := strconv.ParseInt(udpData[outDatagramsIdx], 10, 64)
						result = append(result, &pb.ProtocolTraffic{
							Protocol:        "udp",
							PacketsReceived: inDg,
							PacketsSent:     outDg,
						})
					}
				}
			}
			i++ // skip the data line
			continue
		}
	}

	return result
}

// parseSSConnections parses `ss -tunaep` output to extract connection-level
// information (protocol, addresses, state, PID, process name).
//
// Output format:
//
//	State          Recv-Q   Send-Q   Local Address:Port    Peer Address:Port    Process
//	LISTEN         0        128      0.0.0.0:22            0.0.0.0:*            users:(("sshd",pid=1234,fd=3))
//	ESTAB          0        0        10.0.0.1:54321        10.0.0.2:80          users:(("nginx",pid=5678,fd=5))
//	UNCONN         0        0        0.0.0.0:68            0.0.0.0:*            users:(("dhclient",pid=9012,fd=7))
func parseSSConnections(output string) []*pb.ConnectionTraffic {
	var result []*pb.ConnectionTraffic

	scanner := bufio.NewScanner(strings.NewReader(output))
	// Skip header
	if scanner.Scan() {
		// header consumed
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		state := fields[0]
		localAddr := fields[3]
		remoteAddr := fields[4]

		// Determine protocol based on state
		proto := "tcp"
		if state == "UNCONN" {
			proto = "udp"
		}

		conn := &pb.ConnectionTraffic{
			Protocol:      proto,
			LocalAddress:  localAddr,
			RemoteAddress: remoteAddr,
		}

		result = append(result, conn)
	}

	return result
}

// collectTopTalkersDarwin collects top talkers data on macOS.
//
// For protocol-level traffic, we use `netstat -ib` which provides
// bytes and packets per network interface.
//
// For connection-level traffic, we use `lsof -i` or `netstat -an`.
//
// Commands:
//   - netstat -ib        — interface bytes/packets
//   - netstat -an -p tcp — active TCP connections
func collectTopTalkersDarwin(ctx context.Context) (*pb.TopTalkers, error) {
	tt := &pb.TopTalkers{}

	// --- Protocol-level traffic from netstat -ib ---
	cmd := execCommand(ctx, "netstat", "-ib")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec netstat -ib: %w", err)
	}

	tt.ByProtocol = parseNetstatIB(string(output))

	// --- Connection-level list from netstat -an -p tcp ---
	cmd = execCommand(ctx, "netstat", "-an", "-p", "tcp")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec netstat -an -p tcp: %w", err)
	}

	tt.ByConnection = parseDarwinNetstatConnections(string(output))

	return tt, nil
}

// parseNetstatIB parses `netstat -ib` output to extract per-interface
// bytes and packets, aggregated into protocol-level traffic.
//
// Output format:
//
//	Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
//	lo0   16384 <Link#1>                       12345     0  12345678    9876     0   9876543     0
//	en0   1500  <Link#2>                       99999     0 999999999   88888     0 888888888     0
func parseNetstatIB(output string) []*pb.ProtocolTraffic {
	var totalIpkts, totalOpkts int64
	var totalIbytes, totalObytes int64

	scanner := bufio.NewScanner(strings.NewReader(output))
	// Skip header line
	if scanner.Scan() {
		// header consumed
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}

		// Fields: Name Mtu Network Address Ipkts Ierrs Ibytes Opkts Oerrs Obytes Coll
		// Indices: 0    1   2       3       4     5     6      7     8     9     10
		ipkts, _ := strconv.ParseInt(fields[4], 10, 64)
		opkts, _ := strconv.ParseInt(fields[7], 10, 64)
		ibytes, _ := strconv.ParseInt(fields[6], 10, 64)
		obytes, _ := strconv.ParseInt(fields[9], 10, 64)

		totalIpkts += ipkts
		totalOpkts += opkts
		totalIbytes += ibytes
		totalObytes += obytes
	}

	// We return a single aggregated entry since netstat -ib doesn't
	// distinguish between TCP and UDP at the interface level.
	return []*pb.ProtocolTraffic{
		{
			Protocol:        "tcp", // aggregate all IP traffic
			BytesSent:       totalObytes,
			BytesReceived:   totalIbytes,
			PacketsSent:     totalOpkts,
			PacketsReceived: totalIpkts,
		},
	}
}

// parseDarwinNetstatConnections parses `netstat -an -p tcp` output on macOS
// to extract connection-level information.
func parseDarwinNetstatConnections(output string) []*pb.ConnectionTraffic {
	var result []*pb.ConnectionTraffic

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Active") || strings.HasPrefix(line, "Proto") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}

		// Format: Proto Recv-Q Send-Q Local Foreign State
		// Example: tcp4 0 0 10.0.0.1.54321 10.0.0.2.80 ESTABLISHED
		proto := fields[0]
		localAddr := fields[3]
		remoteAddr := fields[4]

		// Normalize protocol name
		protoName := "tcp"
		if strings.HasPrefix(strings.ToLower(proto), "tcp") {
			protoName = "tcp"
		} else if strings.HasPrefix(strings.ToLower(proto), "udp") {
			protoName = "udp"
		}

		result = append(result, &pb.ConnectionTraffic{
			Protocol:      protoName,
			LocalAddress:  localAddr,
			RemoteAddress: remoteAddr,
		})
	}

	return result
}

// collectTopTalkersWindows collects top talkers data on Windows.
//
// For protocol-level traffic, we use PowerShell to query network adapter
// statistics via Get-NetAdapterStatistics.
//
// For connection-level traffic, we use `netstat -an`.
//
// Commands:
//   - powershell Get-NetAdapterStatistics  — interface bytes/packets
//   - netstat -an                          — active connections
func collectTopTalkersWindows(ctx context.Context) (*pb.TopTalkers, error) {
	tt := &pb.TopTalkers{}

	// --- Protocol-level traffic from Get-NetAdapterStatistics ---
	cmd := execCommand(ctx, "powershell", "-Command",
		`Get-NetAdapterStatistics | Select-Object Name, ReceivedBytes, ReceivedPackets, SentBytes, SentPackets | Format-Table -HideTableHeaders | Out-String -Stream`)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec Get-NetAdapterStatistics: %w", err)
	}

	tt.ByProtocol = parseWindowsNetAdapter(string(output))

	// --- Connection-level list from netstat -an ---
	cmd = execCommand(ctx, "netstat", "-an")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec netstat -an: %w", err)
	}

	tt.ByConnection = parseWindowsNetstatConnections(string(output))

	return tt, nil
}

// parseWindowsNetAdapter parses Get-NetAdapterStatistics output.
// Expected format (space-separated values from Format-Table):
//
//	Ethernet0 123456789 1000 987654321 800
func parseWindowsNetAdapter(output string) []*pb.ProtocolTraffic {
	var totalReceivedBytes, totalSentBytes int64
	var totalReceivedPackets, totalSentPackets int64

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		rb, _ := strconv.ParseInt(fields[1], 10, 64)
		rp, _ := strconv.ParseInt(fields[2], 10, 64)
		sb, _ := strconv.ParseInt(fields[3], 10, 64)
		sp, _ := strconv.ParseInt(fields[4], 10, 64)

		totalReceivedBytes += rb
		totalSentBytes += sb
		totalReceivedPackets += rp
		totalSentPackets += sp
	}

	return []*pb.ProtocolTraffic{
		{
			Protocol:        "tcp", // aggregate all IP traffic
			BytesSent:       totalSentBytes,
			BytesReceived:   totalReceivedBytes,
			PacketsSent:     totalSentPackets,
			PacketsReceived: totalReceivedPackets,
		},
	}
}

// parseWindowsNetstatConnections parses `netstat -an` output on Windows
// to extract connection-level information.
func parseWindowsNetstatConnections(output string) []*pb.ConnectionTraffic {
	var result []*pb.ConnectionTraffic

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		proto := strings.ToLower(fields[0])
		localAddr := fields[1]
		remoteAddr := fields[2]

		protoName := "tcp"
		if proto == "udp" {
			protoName = "udp"
		}

		result = append(result, &pb.ConnectionTraffic{
			Protocol:      protoName,
			LocalAddress:  localAddr,
			RemoteAddress: remoteAddr,
		})
	}

	return result
}
