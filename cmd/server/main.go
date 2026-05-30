// Package main is the entry point for the system monitoring daemon.
//
// It starts a gRPC server that:
//   - Begins background metric collection from all enabled subsystems
//   - Accepts client connections for streaming aggregated metrics
//   - Supports configurable collection intervals and aggregation windows
//
// Configuration is loaded in this order (later overrides earlier):
//  1. Built-in defaults
//  2. YAML config file (--config flag)
//  3. Command-line flags
//  4. Environment variables (MON_GRPC_PORT, MON_INTERVAL, etc.)
//
// Usage:
//
//	go run cmd/server/main.go --port=50051
//	go run cmd/server/main.go --config=configs/config.yaml
//	go run cmd/server/main.go --port=50051 --interval=10 --window=30
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/pflag"
	"golang-project.local/internal/config"
	"golang-project.local/internal/server"
	"google.golang.org/grpc"
)

func main() {
	// ---------------------------------------------------------------
	// 1. Load defaults
	// ---------------------------------------------------------------
	cfg := config.DefaultConfig()

	// ---------------------------------------------------------------
	// 2. Register CLI flags (using defaults as initial values)
	// ---------------------------------------------------------------
	fs := pflag.CommandLine
	config.RegisterFlags(cfg, fs)

	// Add a help flag
	var showHelp bool
	fs.BoolVarP(&showHelp, "help", "h", false, "show this help message")

	fs.Parse(os.Args[1:])

	if showHelp {
		fmt.Fprintf(os.Stderr, "System Monitoring Daemon\n\n")
		fmt.Fprintf(os.Stderr, "A Go-based daemon that collects system metrics and streams\n")
		fmt.Fprintf(os.Stderr, "them to clients via gRPC with configurable aggregation windows.\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s [flags]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment variables:\n")
		fmt.Fprintf(os.Stderr, "  MON_GRPC_PORT    gRPC server port (overrides --port)\n")
		fmt.Fprintf(os.Stderr, "  MON_MAX_CLIENTS  max concurrent clients (overrides --max-clients)\n")
		fmt.Fprintf(os.Stderr, "  MON_INTERVAL     default collection interval in seconds\n")
		fmt.Fprintf(os.Stderr, "  MON_WINDOW       default aggregation window in seconds\n")
		fmt.Fprintf(os.Stderr, "  MON_COLLECTORS   comma-separated list of enabled collectors\n")
		os.Exit(0)
	}

	// ---------------------------------------------------------------
	// 3. Load YAML config file (if specified)
	// ---------------------------------------------------------------
	configPath := config.GetConfigFilePath(fs)
	if configPath != "" {
		fileCfg, err := config.Load(configPath)
		if err != nil {
			log.Fatalf("Failed to load config file %q: %v", configPath, err)
		}
		// Merge: file config becomes the new base, then flags override on top.
		// Since RegisterFlags already bound cfg fields to flags, we need to
		// apply file values first, then re-parse flags to override.
		cfg = fileCfg
		config.RegisterFlags(cfg, fs)
		if err := fs.Parse(os.Args[1:]); err != nil {
			log.Fatalf("Failed to re-parse flags after config load: %v", err)
		}
	}

	// ---------------------------------------------------------------
	// 4. Apply environment variable overrides
	// ---------------------------------------------------------------
	config.OverrideFromEnv(cfg)

	// ---------------------------------------------------------------
	// 5. Validate configuration
	// ---------------------------------------------------------------
	warnings, errs := config.Validate(cfg)
	for _, w := range warnings {
		log.Printf("Config warning: %s", w)
	}
	if len(errs) > 0 {
		for _, e := range errs {
			log.Printf("Config error: %s", e)
		}
		log.Fatalf("Configuration validation failed with %d error(s)", len(errs))
	}

	// ---------------------------------------------------------------
	// 6. Start server
	// ---------------------------------------------------------------
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("Starting system monitoring daemon")
	log.Printf("%s", cfg)

	// Create the gRPC server
	grpcServer := grpc.NewServer()

	// Create and start the metric server with configuration
	metricServer := server.New(cfg)
	metricServer.RegisterService(grpcServer)

	// Start background metric collection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := metricServer.Start(ctx); err != nil {
		log.Fatalf("Failed to start metric collection: %v", err)
	}

	// Start listening
	addr := fmt.Sprintf(":%d", cfg.Server.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", addr, err)
	}

	// Channel to capture server errors
	errCh := make(chan error, 1)

	// Start gRPC server in background
	go func() {
		log.Printf("gRPC server listening on %s", lis.Addr().String())
		if err := grpcServer.Serve(lis); err != nil {
			errCh <- fmt.Errorf("gRPC server error: %w", err)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("Received signal %v, shutting down...", sig)
	case err := <-errCh:
		log.Printf("Server error: %v", err)
	}

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	done := make(chan struct{}, 1)
	go func() {
		// Stop metric collection
		metricServer.Stop()

		// Gracefully stop gRPC server
		grpcServer.GracefulStop()

		close(done)
	}()

	select {
	case <-done:
		log.Printf("Server shutdown complete")
	case <-shutdownCtx.Done():
		log.Printf("Server shutdown timed out, forcing stop")
		grpcServer.Stop()
	}
}
