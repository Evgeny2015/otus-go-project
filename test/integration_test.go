// Package integration provides end-to-end tests for the system monitoring daemon.
// These tests verify the full pipeline: gRPC server -> metric collection ->
// aggregation -> streaming to client.
//
// To run these tests:
//
//	go test -v ./test/...
//
// Note: These tests start a real gRPC server and may require certain system
// commands to be available (top, iostat, ss, df, etc. depending on platform).
package integration

import (
	"context"
	"net"
	"testing"
	"time"

	"golang-project.local/internal/config"
	"golang-project.local/internal/server"
	pb "golang-project.local/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// startTestServer creates and starts a server with a minimal configuration
// for integration testing. Returns the server, gRPC server, listener address,
// and a shutdown function.
func startTestServer(t *testing.T, collectors []string) (*server.Server, *grpc.Server, string, func()) {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Server.GRPCPort = 0 // use random port
	if len(collectors) > 0 {
		cfg.Monitoring.EnabledCollectors = collectors
	} else {
		cfg.Monitoring.EnabledCollectors = []string{"load", "cpu"}
	}

	s := server.New(cfg)

	// Create gRPC server and register the service
	grpcServer := grpc.NewServer()
	s.RegisterService(grpcServer)

	// Listen on random port
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	// Start gRPC server in background
	go func() {
		_ = grpcServer.Serve(lis)
	}()

	// Start metric collection
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		cancel()
		grpcServer.Stop()
		t.Fatalf("failed to start server: %v", err)
	}

	addr := lis.Addr().String()

	shutdown := func() {
		s.Stop()
		grpcServer.GracefulStop()
		cancel()
	}

	return s, grpcServer, addr, shutdown
}

// ---------------------------------------------------------------------------
// Test: Server starts and stops without error
// ---------------------------------------------------------------------------

func TestServerStartStop(t *testing.T) {
	_, _, _, shutdown := startTestServer(t, []string{"load", "cpu"})
	shutdown()
}

// ---------------------------------------------------------------------------
// Test: Server is idempotent on start
// ---------------------------------------------------------------------------

func TestServerStartIdempotent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Monitoring.EnabledCollectors = []string{"load"}

	s := server.New(cfg)
	ctx := context.Background()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("first Start() error: %v", err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatalf("second Start() error: %v", err)
	}

	s.Stop()
}

// ---------------------------------------------------------------------------
// Test: gRPC connection and streaming
// ---------------------------------------------------------------------------

func TestGRPCStreaming(t *testing.T) {
	_, _, addr, shutdown := startTestServer(t, []string{"load", "cpu"})
	defer shutdown()

	// Connect to the gRPC server
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create gRPC client: %v", err)
	}
	defer conn.Close()

	client := pb.NewStreamServiceClient(conn)

	// Create a streaming request
	req := &pb.MetricRequest{
		IntervalSeconds: 1,
		WindowSeconds:   5,
	}

	streamCtx, streamCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer streamCancel()

	stream, err := client.StreamMetrics(streamCtx, req)
	if err != nil {
		t.Fatalf("StreamMetrics() error: %v", err)
	}

	// Read at least one metric from the stream
	select {
	case <-streamCtx.Done():
		t.Fatal("timeout waiting for metrics")
	default:
		metrics, err := stream.Recv()
		if err != nil {
			t.Fatalf("Recv() error: %v", err)
		}
		if metrics == nil {
			t.Fatal("received nil metrics")
		}
		if metrics.Timestamp == 0 {
			t.Error("Timestamp should not be 0")
		}
		t.Logf("Received metrics: timestamp=%d, interval=%d, window=%d",
			metrics.Timestamp, metrics.IntervalSeconds, metrics.WindowSeconds)
	}
}

// ---------------------------------------------------------------------------
// Test: Multiple clients can connect simultaneously
// ---------------------------------------------------------------------------

func TestMultipleClients(t *testing.T) {
	_, _, addr, shutdown := startTestServer(t, []string{"load"})
	defer shutdown()

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create gRPC client: %v", err)
	}
	defer conn.Close()

	client := pb.NewStreamServiceClient(conn)

	const numClients = 5
	streams := make([]pb.StreamService_StreamMetricsClient, numClients)

	for i := 0; i < numClients; i++ {
		streamCtx, streamCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer streamCancel()

		stream, err := client.StreamMetrics(streamCtx, &pb.MetricRequest{
			IntervalSeconds: 1,
			WindowSeconds:   5,
		})
		if err != nil {
			t.Fatalf("client %d StreamMetrics() error: %v", i, err)
		}
		streams[i] = stream
	}

	// Verify all clients can receive metrics
	for i, stream := range streams {
		select {
		case <-time.After(3 * time.Second):
			t.Errorf("client %d timed out waiting for metrics", i)
		default:
			metrics, err := stream.Recv()
			if err != nil {
				t.Errorf("client %d Recv() error: %v", i, err)
				continue
			}
			if metrics == nil {
				t.Errorf("client %d received nil metrics", i)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Client disconnection is handled gracefully
// ---------------------------------------------------------------------------

func TestClientDisconnect(t *testing.T) {
	s, _, addr, shutdown := startTestServer(t, []string{"load"})
	defer shutdown()

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create gRPC client: %v", err)
	}
	defer conn.Close()

	client := pb.NewStreamServiceClient(conn)

	// Connect a client
	streamCtx, streamCancel := context.WithTimeout(context.Background(), 5*time.Second)
	stream, err := client.StreamMetrics(streamCtx, &pb.MetricRequest{
		IntervalSeconds: 1,
		WindowSeconds:   5,
	})
	if err != nil {
		t.Fatalf("StreamMetrics() error: %v", err)
	}

	// Read one metric
	_, err = stream.Recv()
	if err != nil {
		t.Fatalf("first Recv() error: %v", err)
	}

	// Disconnect the client
	streamCancel()

	// Give server time to process disconnection
	time.Sleep(100 * time.Millisecond)

	// Server should still be running
	if s.StreamManager().ClientCount() != 0 {
		t.Errorf("ClientCount() = %d, want 0 after disconnect", s.StreamManager().ClientCount())
	}
}

// ---------------------------------------------------------------------------
// Test: Server handles rapid connect/disconnect cycles
// ---------------------------------------------------------------------------

func TestRapidConnectDisconnect(t *testing.T) {
	_, _, addr, shutdown := startTestServer(t, []string{"load"})
	defer shutdown()

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create gRPC client: %v", err)
	}
	defer conn.Close()

	client := pb.NewStreamServiceClient(conn)

	const numCycles = 10
	for i := 0; i < numCycles; i++ {
		streamCtx, streamCancel := context.WithTimeout(context.Background(), time.Second)
		stream, err := client.StreamMetrics(streamCtx, &pb.MetricRequest{
			IntervalSeconds: 1,
			WindowSeconds:   5,
		})
		if err != nil {
			t.Fatalf("cycle %d StreamMetrics() error: %v", i, err)
		}

		// Try to read (may or may not get data before cancellation)
		_, _ = stream.Recv()

		streamCancel()
		time.Sleep(50 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// Test: BroadcastToAll sends to all connected clients
// ---------------------------------------------------------------------------

func TestBroadcastToAll(t *testing.T) {
	s, _, addr, shutdown := startTestServer(t, []string{"load"})
	defer shutdown()

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create gRPC client: %v", err)
	}
	defer conn.Close()

	client := pb.NewStreamServiceClient(conn)

	// Connect two clients
	streamCtx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()
	stream1, err := client.StreamMetrics(streamCtx1, &pb.MetricRequest{
		IntervalSeconds: 10,
		WindowSeconds:   30,
	})
	if err != nil {
		t.Fatalf("client 1 StreamMetrics() error: %v", err)
	}

	streamCtx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	stream2, err := client.StreamMetrics(streamCtx2, &pb.MetricRequest{
		IntervalSeconds: 10,
		WindowSeconds:   30,
	})
	if err != nil {
		t.Fatalf("client 2 StreamMetrics() error: %v", err)
	}

	// Wait for both clients to be registered
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.StreamManager().ClientCount() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Broadcast to all
	sent := s.BroadcastToAll(&pb.SystemMetrics{
		Timestamp:       uint64(time.Now().UnixNano()),
		IntervalSeconds: 5,
		WindowSeconds:   15,
	})

	if sent != 2 {
		t.Errorf("BroadcastToAll() = %d, want 2 (clients=%d)", sent, s.StreamManager().ClientCount())
	}

	// Both clients should receive the broadcast
	_, err = stream1.Recv()
	if err != nil {
		t.Errorf("client 1 Recv() error: %v", err)
	}
	_, err = stream2.Recv()
	if err != nil {
		t.Errorf("client 2 Recv() error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: Server handles invalid client requests
// ---------------------------------------------------------------------------

func TestInvalidClientRequest(t *testing.T) {
	_, _, addr, shutdown := startTestServer(t, []string{"load"})
	defer shutdown()

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create gRPC client: %v", err)
	}
	defer conn.Close()

	client := pb.NewStreamServiceClient(conn)

	// Request with zero interval and window (should use defaults: 5s interval, 15s window)
	streamCtx, streamCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer streamCancel()

	stream, err := client.StreamMetrics(streamCtx, &pb.MetricRequest{
		IntervalSeconds: 0,
		WindowSeconds:   0,
	})
	if err != nil {
		t.Fatalf("StreamMetrics() with zero params error: %v", err)
	}

	// Should still receive metrics with default values
	metrics, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error: %v", err)
	}
	if metrics == nil {
		t.Fatal("received nil metrics")
	}
	t.Logf("Received metrics with defaults: interval=%d, window=%d",
		metrics.IntervalSeconds, metrics.WindowSeconds)
}
