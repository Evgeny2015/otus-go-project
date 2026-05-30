package stream

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "golang-project.local/proto"
	"google.golang.org/grpc/metadata"
)

// ---------------------------------------------------------------------------
// Mock gRPC server stream
// ---------------------------------------------------------------------------

// mockStream implements pb.StreamService_StreamMetricsServer for testing.
type mockStream struct {
	ctx    context.Context
	sendFn func(m *pb.SystemMetrics) error
	// Track sends
	sendCount atomic.Int32
	// Simulate send error after N calls
	failAfter int32
	// Record received metrics
	lastMetric atomic.Value // stores *pb.SystemMetrics
}

func newMockStream() *mockStream {
	return &mockStream{
		ctx:       context.Background(),
		failAfter: -1,
	}
}

func newMockStreamWithCancel() (*mockStream, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ms := &mockStream{
		ctx:       ctx,
		failAfter: -1,
	}
	return ms, cancel
}

func (m *mockStream) Send(metrics *pb.SystemMetrics) error {
	count := m.sendCount.Add(1)
	m.lastMetric.Store(metrics)

	if m.failAfter >= 0 && count > m.failAfter {
		return errors.New("send failed")
	}
	if m.sendFn != nil {
		return m.sendFn(metrics)
	}
	return nil
}

func (m *mockStream) Context() context.Context {
	return m.ctx
}

func (m *mockStream) RecvMsg(msg interface{}) error {
	return nil
}

func (m *mockStream) SendMsg(msg interface{}) error {
	return nil
}

func (m *mockStream) SetHeader(md metadata.MD) error {
	return nil
}

func (m *mockStream) SendHeader(md metadata.MD) error {
	return nil
}

func (m *mockStream) SetTrailer(md metadata.MD) {
}

// ---------------------------------------------------------------------------
// Tests: NewStreamManager
// ---------------------------------------------------------------------------

func TestNewStreamManager(t *testing.T) {
	sm := NewStreamManager()
	if sm == nil {
		t.Fatal("NewStreamManager returned nil")
	}
	if sm.ClientCount() != 0 {
		t.Errorf("ClientCount() = %d, want 0", sm.ClientCount())
	}
	if sm.TotalClients() != 0 {
		t.Errorf("TotalClients() = %d, want 0", sm.TotalClients())
	}
}

func TestNewStreamManager_WithMaxClients(t *testing.T) {
	sm := NewStreamManager(WithMaxClients(5))
	if sm.maxConcurrentClients != 5 {
		t.Errorf("maxConcurrentClients = %d, want 5", sm.maxConcurrentClients)
	}
}

// ---------------------------------------------------------------------------
// Tests: Register
// ---------------------------------------------------------------------------

func TestRegister_Success(t *testing.T) {
	sm := NewStreamManager()
	ms := newMockStream()

	cs, err := sm.Register(ms, 5, 15)
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
	if cs == nil {
		t.Fatal("Register() returned nil ClientStream")
	}
	if cs.ID != 1 {
		t.Errorf("cs.ID = %d, want 1", cs.ID)
	}
	if cs.IntervalSeconds != 5 {
		t.Errorf("cs.IntervalSeconds = %d, want 5", cs.IntervalSeconds)
	}
	if cs.WindowSeconds != 15 {
		t.Errorf("cs.WindowSeconds = %d, want 15", cs.WindowSeconds)
	}
	if sm.ClientCount() != 1 {
		t.Errorf("ClientCount() = %d, want 1", sm.ClientCount())
	}
	if sm.TotalClients() != 1 {
		t.Errorf("TotalClients() = %d, want 1", sm.TotalClients())
	}
}

func TestRegister_MultipleClients(t *testing.T) {
	sm := NewStreamManager()

	cs1, _ := sm.Register(newMockStream(), 5, 15)
	cs2, _ := sm.Register(newMockStream(), 10, 30)
	cs3, _ := sm.Register(newMockStream(), 1, 5)

	if cs1.ID != 1 || cs2.ID != 2 || cs3.ID != 3 {
		t.Errorf("IDs = %d, %d, %d, want 1, 2, 3", cs1.ID, cs2.ID, cs3.ID)
	}
	if sm.ClientCount() != 3 {
		t.Errorf("ClientCount() = %d, want 3", sm.ClientCount())
	}
	if sm.TotalClients() != 3 {
		t.Errorf("TotalClients() = %d, want 3", sm.TotalClients())
	}
}

func TestRegister_MaxClientsReached(t *testing.T) {
	sm := NewStreamManager(WithMaxClients(2))

	_, err1 := sm.Register(newMockStream(), 5, 15)
	if err1 != nil {
		t.Fatalf("first Register() error = %v, want nil", err1)
	}

	_, err2 := sm.Register(newMockStream(), 5, 15)
	if err2 != nil {
		t.Fatalf("second Register() error = %v, want nil", err2)
	}

	_, err3 := sm.Register(newMockStream(), 5, 15)
	if err3 != ErrMaxClientsReached {
		t.Errorf("third Register() error = %v, want ErrMaxClientsReached", err3)
	}

	if sm.ClientCount() != 2 {
		t.Errorf("ClientCount() = %d, want 2", sm.ClientCount())
	}
}

func TestRegister_UnlimitedClients(t *testing.T) {
	sm := NewStreamManager() // maxConcurrentClients = 0 (unlimited)

	for i := 0; i < 100; i++ {
		_, err := sm.Register(newMockStream(), 5, 15)
		if err != nil {
			t.Fatalf("Register() %d error = %v, want nil", i, err)
		}
	}

	if sm.ClientCount() != 100 {
		t.Errorf("ClientCount() = %d, want 100", sm.ClientCount())
	}
}

// ---------------------------------------------------------------------------
// Tests: Unregister
// ---------------------------------------------------------------------------

func TestUnregister_Success(t *testing.T) {
	sm := NewStreamManager()
	cs, _ := sm.Register(newMockStream(), 5, 15)

	sm.Unregister(cs)

	if sm.ClientCount() != 0 {
		t.Errorf("ClientCount() = %d, want 0", sm.ClientCount())
	}
	// TotalClients should still be 1 (historical count)
	if sm.TotalClients() != 1 {
		t.Errorf("TotalClients() = %d, want 1", sm.TotalClients())
	}
}

func TestUnregister_UnknownClient(t *testing.T) {
	sm := NewStreamManager()
	cs := &ClientStream{ID: 999, done: make(chan struct{})}

	// Should not panic
	sm.Unregister(cs)
}

func TestUnregister_ClientStreamClosed(t *testing.T) {
	sm := NewStreamManager()
	cs, _ := sm.Register(newMockStream(), 5, 15)

	sm.Unregister(cs)

	// Verify the done channel is closed
	select {
	case <-cs.Done():
		// OK - channel is closed
	default:
		t.Error("ClientStream.Done() channel should be closed after Unregister")
	}
}

// ---------------------------------------------------------------------------
// Tests: ClientStream
// ---------------------------------------------------------------------------

func TestClientStream_Send(t *testing.T) {
	ms := newMockStream()
	cs := &ClientStream{
		ID:     1,
		Stream: ms,
		ctx:    ms.Context(),
		done:   make(chan struct{}),
	}

	metrics := &pb.SystemMetrics{Timestamp: 1000}
	err := cs.Send(metrics)
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}
}

func TestClientStream_Send_AfterClose(t *testing.T) {
	ms := newMockStream()
	cs := &ClientStream{
		ID:     1,
		Stream: ms,
		ctx:    ms.Context(),
		done:   make(chan struct{}),
	}
	cs.Close()

	err := cs.Send(&pb.SystemMetrics{})
	if err != context.Canceled {
		t.Errorf("Send() after Close = %v, want context.Canceled", err)
	}
}

func TestClientStream_Close_Idempotent(t *testing.T) {
	cs := &ClientStream{
		ID:   1,
		done: make(chan struct{}),
	}

	cs.Close()
	cs.Close() // should not panic
}

func TestClientStream_Done(t *testing.T) {
	cs := &ClientStream{
		ID:   1,
		done: make(chan struct{}),
	}

	select {
	case <-cs.Done():
		t.Error("Done() channel should not be closed initially")
	default:
		// OK
	}

	cs.Close()

	select {
	case <-cs.Done():
		// OK - channel is now closed
	default:
		t.Error("Done() channel should be closed after Close()")
	}
}

// ---------------------------------------------------------------------------
// Tests: Broadcast
// ---------------------------------------------------------------------------

func TestBroadcast_NoClients(t *testing.T) {
	sm := NewStreamManager()

	sent := sm.Broadcast(&pb.SystemMetrics{Timestamp: 1000})
	if sent != 0 {
		t.Errorf("Broadcast() = %d, want 0", sent)
	}
}

func TestBroadcast_SingleClient(t *testing.T) {
	sm := NewStreamManager()
	ms := newMockStream()
	sm.Register(ms, 5, 15)

	sent := sm.Broadcast(&pb.SystemMetrics{Timestamp: 1000})
	if sent != 1 {
		t.Errorf("Broadcast() = %d, want 1", sent)
	}
	if ms.sendCount.Load() != 1 {
		t.Errorf("sendCount = %d, want 1", ms.sendCount.Load())
	}
}

func TestBroadcast_MultipleClients(t *testing.T) {
	sm := NewStreamManager()
	const numClients = 10

	streams := make([]*mockStream, numClients)
	for i := 0; i < numClients; i++ {
		ms := newMockStream()
		streams[i] = ms
		sm.Register(ms, 5, 15)
	}

	sent := sm.Broadcast(&pb.SystemMetrics{Timestamp: 1000})
	if sent != numClients {
		t.Errorf("Broadcast() = %d, want %d", sent, numClients)
	}

	for i, ms := range streams {
		if ms.sendCount.Load() != 1 {
			t.Errorf("stream %d sendCount = %d, want 1", i, ms.sendCount.Load())
		}
	}
}

func TestBroadcast_WithFailedClient(t *testing.T) {
	sm := NewStreamManager()

	ms1 := newMockStream()
	ms2 := newMockStream()
	ms2.failAfter = 0 // fail on first send

	sm.Register(ms1, 5, 15)
	sm.Register(ms2, 5, 15)

	sent := sm.Broadcast(&pb.SystemMetrics{Timestamp: 1000})
	if sent != 1 {
		t.Errorf("Broadcast() = %d, want 1 (only one client should succeed)", sent)
	}

	// Failed client should be unregistered
	if sm.ClientCount() != 1 {
		t.Errorf("ClientCount() = %d, want 1 (failed client removed)", sm.ClientCount())
	}
}

func TestBroadcast_AllClientsFail(t *testing.T) {
	sm := NewStreamManager()

	ms1 := newMockStream()
	ms1.failAfter = 0
	ms2 := newMockStream()
	ms2.failAfter = 0

	sm.Register(ms1, 5, 15)
	sm.Register(ms2, 5, 15)

	sent := sm.Broadcast(&pb.SystemMetrics{Timestamp: 1000})
	if sent != 0 {
		t.Errorf("Broadcast() = %d, want 0", sent)
	}

	if sm.ClientCount() != 0 {
		t.Errorf("ClientCount() = %d, want 0", sm.ClientCount())
	}
}

// ---------------------------------------------------------------------------
// Tests: BroadcastAsync
// ---------------------------------------------------------------------------

func TestBroadcastAsync_NoClients(t *testing.T) {
	sm := NewStreamManager()

	// Should not panic
	sm.BroadcastAsync(&pb.SystemMetrics{Timestamp: 1000})
}

func TestBroadcastAsync_SingleClient(t *testing.T) {
	sm := NewStreamManager()
	ms := newMockStream()
	sm.Register(ms, 5, 15)

	sm.BroadcastAsync(&pb.SystemMetrics{Timestamp: 1000})

	// Give async send time to complete
	time.Sleep(50 * time.Millisecond)

	if ms.sendCount.Load() != 1 {
		t.Errorf("sendCount = %d, want 1", ms.sendCount.Load())
	}
}

func TestBroadcastAsync_MultipleClients(t *testing.T) {
	sm := NewStreamManager()
	const numClients = 10

	streams := make([]*mockStream, numClients)
	for i := 0; i < numClients; i++ {
		ms := newMockStream()
		streams[i] = ms
		sm.Register(ms, 5, 15)
	}

	sm.BroadcastAsync(&pb.SystemMetrics{Timestamp: 1000})

	time.Sleep(100 * time.Millisecond)

	for i, ms := range streams {
		if ms.sendCount.Load() != 1 {
			t.Errorf("stream %d sendCount = %d, want 1", i, ms.sendCount.Load())
		}
	}
}

func TestBroadcastAsync_FailedClient(t *testing.T) {
	sm := NewStreamManager()

	ms1 := newMockStream()
	ms2 := newMockStream()
	ms2.failAfter = 0

	sm.Register(ms1, 5, 15)
	sm.Register(ms2, 5, 15)

	sm.BroadcastAsync(&pb.SystemMetrics{Timestamp: 1000})

	time.Sleep(100 * time.Millisecond)

	// Failed client should be unregistered
	if sm.ClientCount() != 1 {
		t.Errorf("ClientCount() = %d, want 1", sm.ClientCount())
	}
}

// ---------------------------------------------------------------------------
// Tests: Shutdown
// ---------------------------------------------------------------------------

func TestShutdown_NoClients(t *testing.T) {
	sm := NewStreamManager()

	// Should not panic
	sm.Shutdown()
}

func TestShutdown_WithClients(t *testing.T) {
	sm := NewStreamManager()

	ms1 := newMockStream()
	ms2 := newMockStream()
	sm.Register(ms1, 5, 15)
	sm.Register(ms2, 5, 15)

	sm.Shutdown()

	if sm.ClientCount() != 0 {
		t.Errorf("ClientCount() = %d, want 0 after Shutdown", sm.ClientCount())
	}
}

func TestShutdown_ClientsClosed(t *testing.T) {
	sm := NewStreamManager()

	cs1, _ := sm.Register(newMockStream(), 5, 15)
	cs2, _ := sm.Register(newMockStream(), 5, 15)

	sm.Shutdown()

	// Verify all client done channels are closed
	select {
	case <-cs1.Done():
	default:
		t.Error("client 1 should be closed after Shutdown")
	}
	select {
	case <-cs2.Done():
	default:
		t.Error("client 2 should be closed after Shutdown")
	}
}

// ---------------------------------------------------------------------------
// Tests: GetClient / GetAllClients
// ---------------------------------------------------------------------------

func TestGetClient_Found(t *testing.T) {
	sm := NewStreamManager()
	cs, _ := sm.Register(newMockStream(), 5, 15)

	got := sm.GetClient(cs.ID)
	if got == nil {
		t.Fatal("GetClient() returned nil for existing client")
	}
	if got.ID != cs.ID {
		t.Errorf("GetClient().ID = %d, want %d", got.ID, cs.ID)
	}
}

func TestGetClient_NotFound(t *testing.T) {
	sm := NewStreamManager()

	got := sm.GetClient(999)
	if got != nil {
		t.Errorf("GetClient() = %v, want nil for unknown ID", got)
	}
}

func TestGetAllClients(t *testing.T) {
	sm := NewStreamManager()
	cs1, _ := sm.Register(newMockStream(), 5, 15)
	cs2, _ := sm.Register(newMockStream(), 10, 30)

	clients := sm.GetAllClients()
	if len(clients) != 2 {
		t.Errorf("GetAllClients() = %d, want 2", len(clients))
	}

	// Verify both IDs are present
	ids := map[uint64]bool{cs1.ID: true, cs2.ID: true}
	for _, c := range clients {
		delete(ids, c.ID)
	}
	if len(ids) != 0 {
		t.Errorf("missing client IDs: %v", ids)
	}
}

func TestGetAllClients_Empty(t *testing.T) {
	sm := NewStreamManager()

	clients := sm.GetAllClients()
	if len(clients) != 0 {
		t.Errorf("GetAllClients() = %d, want 0", len(clients))
	}
}

// ---------------------------------------------------------------------------
// Tests: Stats
// ---------------------------------------------------------------------------

func TestStats_Initial(t *testing.T) {
	sm := NewStreamManager(WithMaxClients(10))

	stats := sm.Stats()
	if stats.ActiveClients != 0 {
		t.Errorf("ActiveClients = %d, want 0", stats.ActiveClients)
	}
	if stats.TotalClients != 0 {
		t.Errorf("TotalClients = %d, want 0", stats.TotalClients)
	}
	if stats.MaxConcurrent != 10 {
		t.Errorf("MaxConcurrent = %d, want 10", stats.MaxConcurrent)
	}
}

func TestStats_AfterRegisterAndUnregister(t *testing.T) {
	sm := NewStreamManager()

	cs, _ := sm.Register(newMockStream(), 5, 15)
	stats := sm.Stats()
	if stats.ActiveClients != 1 {
		t.Errorf("ActiveClients = %d, want 1", stats.ActiveClients)
	}
	if stats.TotalClients != 1 {
		t.Errorf("TotalClients = %d, want 1", stats.TotalClients)
	}

	sm.Unregister(cs)
	stats = sm.Stats()
	if stats.ActiveClients != 0 {
		t.Errorf("ActiveClients after unregister = %d, want 0", stats.ActiveClients)
	}
	if stats.TotalClients != 1 {
		t.Errorf("TotalClients after unregister = %d, want 1", stats.TotalClients)
	}
}

// ---------------------------------------------------------------------------
// Tests: Concurrent safety
// ---------------------------------------------------------------------------

func TestConcurrentRegisterAndBroadcast(t *testing.T) {
	sm := NewStreamManager()

	var wg sync.WaitGroup

	// Register clients concurrently
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ms := newMockStream()
			_, err := sm.Register(ms, 5, 15)
			if err != nil {
				t.Errorf("concurrent Register() error = %v", err)
			}
		}()
	}

	wg.Wait()

	if sm.ClientCount() != 20 {
		t.Errorf("ClientCount() = %d, want 20", sm.ClientCount())
	}

	// Broadcast concurrently with unregister
	var broadcastWg sync.WaitGroup
	for i := 0; i < 5; i++ {
		broadcastWg.Add(1)
		go func() {
			defer broadcastWg.Done()
			sm.Broadcast(&pb.SystemMetrics{Timestamp: uint64(time.Now().UnixNano())})
		}()
	}

	// Unregister some clients concurrently
	for _, cs := range sm.GetAllClients()[:5] {
		broadcastWg.Add(1)
		go func(c *ClientStream) {
			defer broadcastWg.Done()
			sm.Unregister(c)
		}(cs)
	}

	broadcastWg.Wait()
}

func TestConcurrentShutdown(t *testing.T) {
	sm := NewStreamManager()

	for i := 0; i < 10; i++ {
		sm.Register(newMockStream(), 5, 15)
	}

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.Shutdown()
		}()
	}
	wg.Wait()

	// After shutdown, client count should be 0
	if sm.ClientCount() != 0 {
		t.Errorf("ClientCount() after Shutdown = %d, want 0", sm.ClientCount())
	}
}

// ---------------------------------------------------------------------------
// Tests: ClientStream Context
// ---------------------------------------------------------------------------

func TestClientStream_Context(t *testing.T) {
	ms, cancel := newMockStreamWithCancel()
	cs := &ClientStream{
		ID:     1,
		Stream: ms,
		ctx:    ms.Context(),
		done:   make(chan struct{}),
	}

	if cs.Context() != ms.Context() {
		t.Error("Context() returned wrong context")
	}

	cancel()
	select {
	case <-cs.Context().Done():
		// OK
	default:
		t.Error("Context().Done() should be closed after cancellation")
	}
}

// ---------------------------------------------------------------------------
// Tests: Edge cases
// ---------------------------------------------------------------------------

func TestRegister_ZeroIntervalAndWindow(t *testing.T) {
	sm := NewStreamManager()
	cs, err := sm.Register(newMockStream(), 0, 0)
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
	if cs.IntervalSeconds != 0 {
		t.Errorf("IntervalSeconds = %d, want 0", cs.IntervalSeconds)
	}
	if cs.WindowSeconds != 0 {
		t.Errorf("WindowSeconds = %d, want 0", cs.WindowSeconds)
	}
}

func TestBroadcast_EmptyMetrics(t *testing.T) {
	sm := NewStreamManager()
	ms := newMockStream()
	sm.Register(ms, 5, 15)

	sent := sm.Broadcast(nil)
	if sent != 1 {
		t.Errorf("Broadcast(nil) = %d, want 1", sent)
	}
}

func TestClientStream_Close_AlreadyClosed(t *testing.T) {
	cs := &ClientStream{
		ID:   1,
		done: make(chan struct{}),
	}
	close(cs.done)

	// Should not panic
	cs.Close()
}
