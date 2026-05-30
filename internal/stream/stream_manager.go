// Package stream provides a StreamManager for managing active gRPC client
// streams and broadcasting metrics to all connected clients.
package stream

import (
	"context"
	"log"
	"sync"
	"sync/atomic"

	pb "golang-project.local/proto"
)

// ---------------------------------------------------------------------------
// ClientStream
// ---------------------------------------------------------------------------

// ClientStream represents a single connected client stream.
// It wraps the gRPC server stream with metadata for tracking.
type ClientStream struct {
	// ID is a unique identifier for this client connection.
	ID uint64

	// Stream is the gRPC server stream for sending metrics.
	Stream pb.StreamService_StreamMetricsServer

	// IntervalSeconds is the client's requested collection interval (N).
	IntervalSeconds int32

	// WindowSeconds is the client's requested aggregation window (M).
	WindowSeconds int32

	// ctx is the stream's context, used to detect disconnection.
	ctx context.Context

	// done is closed when the client disconnects.
	done chan struct{}
}

// Context returns the stream's context.
func (c *ClientStream) Context() context.Context {
	return c.ctx
}

// Done returns a channel that is closed when the client disconnects.
func (c *ClientStream) Done() <-chan struct{} {
	return c.done
}

// Close marks the client stream as disconnected.
func (c *ClientStream) Close() {
	select {
	case <-c.done:
		// Already closed
	default:
		close(c.done)
	}
}

// Send sends a SystemMetrics message to this client.
// Returns an error if the client has disconnected or the send fails.
func (c *ClientStream) Send(metrics *pb.SystemMetrics) error {
	select {
	case <-c.done:
		return context.Canceled
	default:
	}
	return c.Stream.Send(metrics)
}

// ---------------------------------------------------------------------------
// StreamManager
// ---------------------------------------------------------------------------

// StreamManager manages all active client streams. It provides:
//   - Thread-safe registration and unregistration of client streams
//   - Broadcasting metrics to all connected clients
//   - Tracking connected client count and statistics
//   - Graceful shutdown of all streams
type StreamManager struct {
	mu sync.RWMutex

	// clients maps client ID to active client stream.
	clients map[uint64]*ClientStream

	// nextID is the auto-incrementing client ID counter.
	nextID atomic.Uint64

	// totalClients tracks the total number of clients that have connected
	// (including disconnected ones) for statistics.
	totalClients atomic.Int64

	// maxConcurrentClients is the maximum number of concurrent clients allowed.
	// 0 means unlimited.
	maxConcurrentClients int
}

// StreamManagerOption configures a StreamManager.
type StreamManagerOption func(*StreamManager)

// WithMaxClients sets the maximum number of concurrent clients.
// If set to 0 (default), there is no limit.
func WithMaxClients(n int) StreamManagerOption {
	return func(sm *StreamManager) {
		sm.maxConcurrentClients = n
	}
}

// NewStreamManager creates a new StreamManager.
func NewStreamManager(opts ...StreamManagerOption) *StreamManager {
	sm := &StreamManager{
		clients: make(map[uint64]*ClientStream),
	}
	for _, opt := range opts {
		opt(sm)
	}
	return sm
}

// Register adds a new client stream to the manager and returns the
// assigned ClientStream. If maxConcurrentClients is exceeded, it returns
// an error.
//
// The returned ClientStream must be used for all subsequent Send calls.
// When the client disconnects, the caller MUST call Unregister to clean up.
func (sm *StreamManager) Register(stream pb.StreamService_StreamMetricsServer, intervalSec, windowSec int32) (*ClientStream, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check max clients limit
	if sm.maxConcurrentClients > 0 && len(sm.clients) >= sm.maxConcurrentClients {
		return nil, ErrMaxClientsReached
	}

	id := sm.nextID.Add(1)
	sm.totalClients.Add(1)

	cs := &ClientStream{
		ID:              id,
		Stream:          stream,
		IntervalSeconds: intervalSec,
		WindowSeconds:   windowSec,
		ctx:             stream.Context(),
		done:            make(chan struct{}),
	}

	sm.clients[id] = cs

	log.Printf("StreamManager: client %d registered (interval=%ds, window=%ds, total=%d)",
		id, intervalSec, windowSec, len(sm.clients))

	return cs, nil
}

// Unregister removes a client stream from the manager and cleans up resources.
// This should be called when a client disconnects or the stream ends.
func (sm *StreamManager) Unregister(cs *ClientStream) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.clients[cs.ID]; exists {
		delete(sm.clients, cs.ID)
		cs.Close()
		log.Printf("StreamManager: client %d unregistered (remaining=%d)", cs.ID, len(sm.clients))
	}
}

// Broadcast sends metrics to all connected clients. It skips clients
// that have disconnected (removing them from the active list).
//
// Returns the number of clients that successfully received the message.
func (sm *StreamManager) Broadcast(metrics *pb.SystemMetrics) int {
	sm.mu.RLock()
	// Copy the client map to avoid holding the lock during sends
	clients := make([]*ClientStream, 0, len(sm.clients))
	for _, cs := range sm.clients {
		clients = append(clients, cs)
	}
	sm.mu.RUnlock()

	if len(clients) == 0 {
		return 0
	}

	var wg sync.WaitGroup
	sentCount := int32(0)

	for _, cs := range clients {
		wg.Add(1)
		go func(client *ClientStream) {
			defer wg.Done()

			if err := client.Send(metrics); err != nil {
				log.Printf("StreamManager: failed to send to client %d: %v", client.ID, err)
				// Unregister disconnected clients
				sm.Unregister(client)
				return
			}
			atomic.AddInt32(&sentCount, 1)
		}(cs)
	}

	wg.Wait()
	return int(sentCount)
}

// BroadcastAsync sends metrics to all connected clients concurrently.
// Unlike Broadcast, it does not wait for all sends to complete.
// Returns immediately after initiating the sends.
func (sm *StreamManager) BroadcastAsync(metrics *pb.SystemMetrics) {
	sm.mu.RLock()
	clients := make([]*ClientStream, 0, len(sm.clients))
	for _, cs := range sm.clients {
		clients = append(clients, cs)
	}
	sm.mu.RUnlock()

	for _, cs := range clients {
		go func(client *ClientStream) {
			if err := client.Send(metrics); err != nil {
				log.Printf("StreamManager: failed to send to client %d: %v", client.ID, err)
				sm.Unregister(client)
			}
		}(cs)
	}
}

// ClientCount returns the number of currently connected clients.
func (sm *StreamManager) ClientCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.clients)
}

// TotalClients returns the total number of clients that have connected
// since the manager was created (including disconnected ones).
func (sm *StreamManager) TotalClients() int64 {
	return sm.totalClients.Load()
}

// GetClient returns a client stream by ID, or nil if not found.
func (sm *StreamManager) GetClient(id uint64) *ClientStream {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.clients[id]
}

// GetAllClients returns a snapshot of all currently connected clients.
func (sm *StreamManager) GetAllClients() []*ClientStream {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]*ClientStream, 0, len(sm.clients))
	for _, cs := range sm.clients {
		result = append(result, cs)
	}
	return result
}

// Shutdown gracefully disconnects all clients and cleans up resources.
// It waits for all in-flight sends to complete.
func (sm *StreamManager) Shutdown() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for id, cs := range sm.clients {
		cs.Close()
		delete(sm.clients, id)
	}

	log.Printf("StreamManager: shutdown complete, all clients disconnected")
}

// Stats returns current stream manager statistics.
func (sm *StreamManager) Stats() StreamStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return StreamStats{
		ActiveClients: len(sm.clients),
		TotalClients:  sm.totalClients.Load(),
		MaxConcurrent: sm.maxConcurrentClients,
	}
}

// ---------------------------------------------------------------------------
// StreamStats
// ---------------------------------------------------------------------------

// StreamStats contains stream manager statistics.
type StreamStats struct {
	ActiveClients int
	TotalClients  int64
	MaxConcurrent int
}
