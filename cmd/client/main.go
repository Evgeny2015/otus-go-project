// Package main is the entry point for the system monitoring client.
//
// It connects to the system monitoring daemon via gRPC and displays
// real-time system metrics in a human-readable format. The client
// supports:
//   - Configurable collection interval (N) and aggregation window (M)
//   - Filtering by metric type (cpu, load, disk, filesystem, network, toptalkers)
//   - Color-coded output for threshold-based highlighting
//   - Compact single-line mode for continuous scrolling
//   - Interactive pause/resume and quit via keyboard input
//
// Usage:
//
//	go run cmd/client/main.go
//	go run cmd/client/main.go --server=192.168.1.100:50051
//	go run cmd/client/main.go --interval=10 --window=30
//	go run cmd/client/main.go --filter=cpu,load,network
//	go run cmd/client/main.go --compact --no-color
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/pflag"
	"golang-project.local/internal/client"
	pb "golang-project.local/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// config holds the client's command-line configuration.
type config struct {
	serverAddr string
	interval   int
	window     int
	filter     string
	noColor    bool
	compact    bool
	showHelp   bool
}

// parseFlags parses command-line flags into a config.
func parseFlags() config {
	var cfg config

	pflag.StringVar(&cfg.serverAddr, "server", "localhost:50051",
		"gRPC server address (host:port)")
	pflag.IntVar(&cfg.interval, "interval", 5,
		"collection interval in seconds (N)")
	pflag.IntVar(&cfg.window, "window", 15,
		"aggregation window in seconds (M)")
	pflag.StringVar(&cfg.filter, "filter", "all",
		"comma-separated metric types to display "+
			"(cpu,load,disk,filesystem,network,toptalkers)")
	pflag.BoolVar(&cfg.noColor, "no-color", false,
		"disable ANSI color output")
	pflag.BoolVar(&cfg.compact, "compact", false,
		"use compact single-line output format")
	pflag.BoolVarP(&cfg.showHelp, "help", "h", false,
		"show this help message")

	pflag.Parse()

	return cfg
}

// ---------------------------------------------------------------------------
// Interactive keyboard input
// ---------------------------------------------------------------------------

// controlState tracks the interactive state of the client.
type controlState struct {
	paused atomic.Bool
	quit   atomic.Bool
}

// listenKeyboard reads keyboard input in a background goroutine.
// It supports:
//   - 'p' or 'P': toggle pause/resume
//   - 'q' or 'Q': quit
//   - 'c' or 'C': clear screen (in non-compact mode)
func listenKeyboard(state *controlState, compact bool) {
	reader := bufio.NewReader(os.Stdin)
	for {
		char, err := reader.ReadByte()
		if err != nil {
			return
		}

		switch char {
		case 'p', 'P':
			if state.paused.Load() {
				state.paused.Store(false)
				fmt.Fprint(os.Stderr, client.ResumedMessage())
			} else {
				state.paused.Store(true)
				fmt.Fprint(os.Stderr, client.PausedMessage())
			}

		case 'q', 'Q':
			state.quit.Store(true)
			fmt.Fprint(os.Stderr, client.QuitMessage())
			return

		case 'c', 'C':
			if !compact {
				fmt.Fprint(os.Stderr, client.ClearScreen())
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	// ---------------------------------------------------------------
	// 1. Parse configuration
	// ---------------------------------------------------------------
	cfg := parseFlags()

	if cfg.showHelp {
		fmt.Fprint(os.Stderr, client.HelpText())
		os.Exit(0)
	}

	// Configure color support
	if cfg.noColor || !client.IsTerminal() {
		client.DisableColors()
	}

	// Parse metric type filters
	filters := client.ParseMetricTypes(cfg.filter)

	// ---------------------------------------------------------------
	// 2. Connect to gRPC server
	// ---------------------------------------------------------------
	conn, err := grpc.Dial(cfg.serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		log.Fatalf("Failed to connect to %s: %v", cfg.serverAddr, err)
	}
	defer conn.Close()

	log.Printf("Connected to server at %s", cfg.serverAddr)
	log.Printf("Requesting metrics: interval=%ds, window=%ds, filter=%s",
		cfg.interval, cfg.window, cfg.filter)

	// ---------------------------------------------------------------
	// 3. Create gRPC client and start streaming
	// ---------------------------------------------------------------
	c := pb.NewStreamServiceClient(conn)

	// Use a long-lived context; the stream will stay open until we cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := c.StreamMetrics(ctx, &pb.MetricRequest{
		IntervalSeconds: int32(cfg.interval),
		WindowSeconds:   int32(cfg.window),
	})
	if err != nil {
		log.Fatalf("Failed to start metric stream: %v", err)
	}

	// ---------------------------------------------------------------
	// 4. Set up interactive keyboard control
	// ---------------------------------------------------------------
	state := &controlState{}
	go listenKeyboard(state, cfg.compact)

	// Also listen for OS signals (Ctrl+C)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// ---------------------------------------------------------------
	// 5. Receive and display metrics
	// ---------------------------------------------------------------
	thresholds := client.DefaultThresholds()
	firstDisplay := true

	for {
		// Check for quit signal
		if state.quit.Load() {
			log.Printf("Quit requested")
			break
		}

		// Check for OS signal
		select {
		case <-sigCh:
			log.Printf("Interrupt received, shutting down...")
			state.quit.Store(true)
		default:
		}

		// If paused, wait before reading
		if state.paused.Load() {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		// Receive the next metric message
		resp, err := stream.Recv()
		if err == io.EOF {
			log.Printf("Stream ended by server")
			break
		}
		if err != nil {
			log.Printf("Stream error: %v", err)
			break
		}

		// Display the metrics
		if cfg.compact {
			// Compact single-line mode
			line := client.FormatSummaryLine(resp, thresholds)
			if line != "" {
				fmt.Println(line)
			}
		} else {
			// Full table mode
			if firstDisplay {
				fmt.Fprint(os.Stderr, client.ClearScreen())
				firstDisplay = false
			}

			output := client.FormatMetrics(resp, filters, thresholds)
			if output != "" {
				fmt.Print(output)
			}
		}
	}

	log.Printf("Client disconnected")
}
