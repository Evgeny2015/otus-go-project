package buffer

import (
	"testing"
	"time"
)

// mockMetric implements the Metric interface for testing
type mockMetric struct {
	metricType string
	timestamp  time.Time
	value      interface{}
}

func (m mockMetric) Timestamp() time.Time {
	return m.timestamp
}

func (m mockMetric) Value() interface{} {
	return m.value
}

func TestNewCircularBuffer(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		wantCap  int
	}{
		{"positive capacity", 10, 10},
		{"zero capacity", 0, 300},      // default
		{"negative capacity", -5, 300}, // default
		{"single element", 1, 1},
		{"large capacity", 1000, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircularBuffer(tt.capacity)
			if cb == nil {
				t.Fatal("NewCircularBuffer returned nil")
			}
			if cb.Capacity() != tt.wantCap {
				t.Errorf("Capacity() = %v, want %v", cb.Capacity(), tt.wantCap)
			}
			if cb.Size() != 0 {
				t.Errorf("Size() = %v, want 0", cb.Size())
			}
			if cb.IsFull() {
				t.Error("IsFull() = true, want false for empty buffer")
			}
		})
	}
}

func TestCircularBuffer_Add(t *testing.T) {
	cb := NewCircularBuffer(3)
	now := time.Now()

	// Add first metric
	m1 := mockMetric{metricType: "cpu", timestamp: now, value: 0.5}
	err := cb.Add(m1, now)
	if err != nil {
		t.Errorf("Add() error = %v, want nil", err)
	}
	if cb.Size() != 1 {
		t.Errorf("Size() after first add = %v, want 1", cb.Size())
	}

	// Add second metric
	m2 := mockMetric{metricType: "memory", timestamp: now.Add(time.Second), value: 1024}
	err = cb.Add(m2, now.Add(time.Second))
	if err != nil {
		t.Errorf("Add() error = %v, want nil", err)
	}
	if cb.Size() != 2 {
		t.Errorf("Size() after second add = %v, want 2", cb.Size())
	}

	// Add third metric (fill buffer)
	m3 := mockMetric{metricType: "disk", timestamp: now.Add(2 * time.Second), value: 500}
	err = cb.Add(m3, now.Add(2*time.Second))
	if err != nil {
		t.Errorf("Add() error = %v, want nil", err)
	}
	if cb.Size() != 3 {
		t.Errorf("Size() after third add = %v, want 3", cb.Size())
	}
	if !cb.IsFull() {
		t.Error("IsFull() = false, want true for full buffer")
	}

	// Add fourth metric (overwrite oldest)
	m4 := mockMetric{metricType: "network", timestamp: now.Add(3 * time.Second), value: 100}
	err = cb.Add(m4, now.Add(3*time.Second))
	if err != nil {
		t.Errorf("Add() error = %v, want nil", err)
	}
	if cb.Size() != 3 {
		t.Errorf("Size() after overflow = %v, want 3", cb.Size())
	}
	if !cb.IsFull() {
		t.Error("IsFull() = false, want true after overflow")
	}
}

func TestCircularBuffer_GetWindow(t *testing.T) {
	cb := NewCircularBuffer(5)
	baseTime := time.Now()

	// Add metrics at 0, 1, 2, 3, 4 seconds
	for i := 0; i < 5; i++ {
		m := mockMetric{
			metricType: "test",
			timestamp:  baseTime.Add(time.Duration(i) * time.Second),
			value:      i,
		}
		cb.Add(m, baseTime.Add(time.Duration(i)*time.Second))
	}

	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want int
	}{
		{"entire range", baseTime, baseTime.Add(5 * time.Second), 5},
		{"middle range", baseTime.Add(time.Second), baseTime.Add(4 * time.Second), 4},     // includes timestamp at 4 (equal to to)
		{"single point", baseTime.Add(2 * time.Second), baseTime.Add(3 * time.Second), 2}, // includes timestamp at 3 (equal to to)
		{"no matches before", baseTime.Add(-5 * time.Second), baseTime.Add(-1 * time.Second), 0},
		{"no matches after", baseTime.Add(10 * time.Second), baseTime.Add(15 * time.Second), 0},
		{"exact boundaries", baseTime.Add(2 * time.Second), baseTime.Add(4 * time.Second), 3}, // includes 2,3,4 (4 equal to to)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := cb.GetWindow(tt.from, tt.to)
			if len(metrics) != tt.want {
				t.Errorf("GetWindow() len = %v, want %v", len(metrics), tt.want)
			}
			// Verify timestamps are within range (inclusive on both ends)
			for _, m := range metrics {
				ts := m.Timestamp()
				if ts.Before(tt.from) || ts.After(tt.to) {
					t.Errorf("metric timestamp %v outside range [%v, %v]", ts, tt.from, tt.to)
				}
			}
		})
	}

	// Test with empty buffer
	cb2 := NewCircularBuffer(3)
	metrics := cb2.GetWindow(baseTime, baseTime.Add(time.Hour))
	if len(metrics) != 0 {
		t.Errorf("GetWindow() on empty buffer = %v, want 0", len(metrics))
	}
}

func TestCircularBuffer_GetLatest(t *testing.T) {
	cb := NewCircularBuffer(5)
	now := time.Now()

	// Add 3 metrics
	for i := 0; i < 3; i++ {
		m := mockMetric{
			metricType: "test",
			timestamp:  now.Add(time.Duration(i) * time.Second),
			value:      i,
		}
		cb.Add(m, now.Add(time.Duration(i)*time.Second))
	}

	tests := []struct {
		name string
		n    int
		want int
	}{
		{"n less than size", 2, 2},
		{"n equal to size", 3, 3},
		{"n greater than size", 5, 3},
		{"n zero", 0, 0},
		{"n negative", -1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := cb.GetLatest(tt.n)
			if len(metrics) != tt.want {
				t.Errorf("GetLatest(%v) len = %v, want %v", tt.n, len(metrics), tt.want)
			}
			// Verify order is from newest to oldest
			for i := 0; i < len(metrics)-1; i++ {
				curr := metrics[i].Timestamp()
				next := metrics[i+1].Timestamp()
				if !curr.After(next) && !curr.Equal(next) {
					t.Errorf("metrics not in descending timestamp order: %v <= %v", curr, next)
				}
			}
		})
	}

	// Test with full buffer (overflow scenario)
	cb2 := NewCircularBuffer(3)
	for i := 0; i < 5; i++ { // Add 5 metrics to 3-capacity buffer
		m := mockMetric{
			metricType: "overflow",
			timestamp:  now.Add(time.Duration(i) * time.Second),
			value:      i,
		}
		cb2.Add(m, now.Add(time.Duration(i)*time.Second))
	}
	// Buffer should contain metrics at seconds 2,3,4 (oldest 0,1 overwritten)
	latest := cb2.GetLatest(3)
	if len(latest) != 3 {
		t.Fatalf("GetLatest(3) len = %v, want 3", len(latest))
	}
	// Check values (should be 4,3,2)
	for i, m := range latest {
		val := m.Value().(int)
		expected := 4 - i
		if val != expected {
			t.Errorf("latest[%v].Value() = %v, want %v", i, val, expected)
		}
	}
}

func TestCircularBuffer_Cleanup(t *testing.T) {
	cb := NewCircularBuffer(5)
	baseTime := time.Now()

	// Add metrics at -2, -1, 0, 1, 2 seconds relative to baseTime
	for i := -2; i <= 2; i++ {
		m := mockMetric{
			metricType: "test",
			timestamp:  baseTime.Add(time.Duration(i) * time.Second),
			value:      i,
		}
		cb.Add(m, baseTime.Add(time.Duration(i)*time.Second))
	}

	// Cleanup metrics older than baseTime (should remove -2, -1)
	removed := cb.Cleanup(baseTime)
	if removed != 2 {
		t.Errorf("Cleanup() removed = %v, want 2", removed)
	}
	if cb.Size() != 3 {
		t.Errorf("Size() after cleanup = %v, want 3", cb.Size())
	}

	// Verify remaining metrics are 0,1,2
	window := cb.GetWindow(baseTime.Add(-time.Hour), baseTime.Add(time.Hour))
	if len(window) != 3 {
		t.Fatalf("GetWindow() len = %v, want 3", len(window))
	}
	for i, m := range window {
		val := m.Value().(int)
		if val != i {
			t.Errorf("window[%v].Value() = %v, want %v", i, val, i)
		}
	}

	// Cleanup with threshold far in future (should remove all)
	removed = cb.Cleanup(baseTime.Add(time.Hour))
	if removed != 3 {
		t.Errorf("Cleanup() second call removed = %v, want 3", removed)
	}
	if cb.Size() != 0 {
		t.Errorf("Size() after second cleanup = %v, want 0", cb.Size())
	}
	if cb.IsFull() {
		t.Error("IsFull() should be false after buffer emptied")
	}

	// Cleanup on empty buffer
	cb3 := NewCircularBuffer(3)
	removed = cb3.Cleanup(baseTime)
	if removed != 0 {
		t.Errorf("Cleanup() on empty buffer removed = %v, want 0", removed)
	}
}

func TestCircularBuffer_SizeCapacityIsFull(t *testing.T) {
	cb := NewCircularBuffer(3)

	if cb.Capacity() != 3 {
		t.Errorf("Capacity() = %v, want 3", cb.Capacity())
	}
	if cb.Size() != 0 {
		t.Errorf("initial Size() = %v, want 0", cb.Size())
	}
	if cb.IsFull() {
		t.Error("initial IsFull() = true, want false")
	}

	// Add one metric
	m := mockMetric{metricType: "test", timestamp: time.Now(), value: 1}
	cb.Add(m, time.Now())
	if cb.Size() != 1 {
		t.Errorf("Size() after one add = %v, want 1", cb.Size())
	}
	if cb.IsFull() {
		t.Error("IsFull() with one item = true, want false")
	}

	// Fill buffer
	cb.Add(m, time.Now())
	cb.Add(m, time.Now())
	if cb.Size() != 3 {
		t.Errorf("Size() after filling = %v, want 3", cb.Size())
	}
	if !cb.IsFull() {
		t.Error("IsFull() when full = false, want true")
	}

	// Add one more (overflow)
	cb.Add(m, time.Now())
	if cb.Size() != 3 {
		t.Errorf("Size() after overflow = %v, want 3", cb.Size())
	}
	if !cb.IsFull() {
		t.Error("IsFull() after overflow = false, want true")
	}
}

func TestCircularBuffer_Clear(t *testing.T) {
	cb := NewCircularBuffer(3)
	now := time.Now()

	// Add some metrics
	for i := 0; i < 3; i++ {
		m := mockMetric{
			metricType: "test",
			timestamp:  now.Add(time.Duration(i) * time.Second),
			value:      i,
		}
		cb.Add(m, now.Add(time.Duration(i)*time.Second))
	}

	if cb.Size() != 3 {
		t.Errorf("Size() before clear = %v, want 3", cb.Size())
	}

	cb.Clear()

	if cb.Size() != 0 {
		t.Errorf("Size() after clear = %v, want 0", cb.Size())
	}
	if cb.IsFull() {
		t.Error("IsFull() after clear = true, want false")
	}

	// Verify GetWindow returns empty
	metrics := cb.GetWindow(now.Add(-time.Hour), now.Add(time.Hour))
	if len(metrics) != 0 {
		t.Errorf("GetWindow() after clear len = %v, want 0", len(metrics))
	}

	// Verify we can add again after clear
	m := mockMetric{metricType: "new", timestamp: now, value: 100}
	err := cb.Add(m, now)
	if err != nil {
		t.Errorf("Add() after clear error = %v, want nil", err)
	}
	if cb.Size() != 1 {
		t.Errorf("Size() after add after clear = %v, want 1", cb.Size())
	}
}

func TestCircularBuffer_ConcurrentAccess(t *testing.T) {
	cb := NewCircularBuffer(100)
	done := make(chan bool)
	now := time.Now()

	// Start multiple goroutines adding metrics
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 20; j++ {
				m := mockMetric{
					metricType: "concurrent",
					timestamp:  now.Add(time.Duration(id*100+j) * time.Millisecond),
					value:      id*100 + j,
				}
				cb.Add(m, now.Add(time.Duration(id*100+j)*time.Millisecond))
			}
			done <- true
		}(i)
	}

	// Start goroutines reading
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				_ = cb.Size()
				_ = cb.IsFull()
				_ = cb.GetLatest(5)
				_ = cb.GetWindow(now.Add(-time.Hour), now.Add(time.Hour))
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}

	// Verify buffer state is consistent
	size := cb.Size()
	if size < 0 || size > 100 {
		t.Errorf("Size() = %v, should be between 0 and 100", size)
	}

	// Cleanup should work concurrently
	removed := cb.Cleanup(now.Add(time.Hour))
	if removed < 0 || removed > size {
		t.Errorf("Cleanup() removed = %v, should be between 0 and %v", removed, size)
	}
}
