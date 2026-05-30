package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang-project.local/internal/types"
	pb "golang-project.local/proto"
)

// ---------------------------------------------------------------------------
// Tests: NewWorkerPool
// ---------------------------------------------------------------------------

func TestNewWorkerPool_DefaultWorkers(t *testing.T) {
	wp := NewWorkerPool(0)
	defer wp.Stop()

	if wp.WorkerCount() != 4 {
		t.Errorf("WorkerCount() = %d, want 4", wp.WorkerCount())
	}
}

func TestNewWorkerPool_NegativeWorkers(t *testing.T) {
	wp := NewWorkerPool(-5)
	defer wp.Stop()

	if wp.WorkerCount() != 4 {
		t.Errorf("WorkerCount() = %d, want 4", wp.WorkerCount())
	}
}

func TestNewWorkerPool_SpecificWorkers(t *testing.T) {
	wp := NewWorkerPool(8)
	defer wp.Stop()

	if wp.WorkerCount() != 8 {
		t.Errorf("WorkerCount() = %d, want 8", wp.WorkerCount())
	}
}

// ---------------------------------------------------------------------------
// Tests: Submit
// ---------------------------------------------------------------------------

func TestSubmit_Success(t *testing.T) {
	wp := NewWorkerPool(2)
	defer wp.Stop()

	mc := newMockCollector("load", true)
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		return types.NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5}), nil
	}

	resultCh := wp.Submit(context.Background(), mc)
	result := <-resultCh

	if result.err != nil {
		t.Errorf("Submit() error = %v, want nil", result.err)
	}
	if result.metric == nil {
		t.Fatal("Submit() metric = nil, want non-nil")
	}

	lm, ok := result.metric.(*types.LoadMetric)
	if !ok {
		t.Fatalf("expected *types.LoadMetric, got %T", result.metric)
	}
	if lm.Metrics.GetLoad1() != 0.5 {
		t.Errorf("Load1 = %v, want 0.5", lm.Metrics.GetLoad1())
	}
}

func TestSubmit_Error(t *testing.T) {
	wp := NewWorkerPool(2)
	defer wp.Stop()

	mc := newMockCollector("load", true)
	expectedErr := errors.New("collection failed")
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		return nil, expectedErr
	}

	resultCh := wp.Submit(context.Background(), mc)
	result := <-resultCh

	if result.err != expectedErr {
		t.Errorf("Submit() error = %v, want %v", result.err, expectedErr)
	}
	if result.metric != nil {
		t.Error("Submit() metric = non-nil, want nil on error")
	}
}

func TestSubmit_ContextCancelled(t *testing.T) {
	wp := NewWorkerPool(2)
	defer wp.Stop()

	mc := newMockCollector("load", true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	resultCh := wp.Submit(ctx, mc)
	result := <-resultCh

	if result.err == nil {
		t.Error("Submit() error = nil, want context error")
	}
}

func TestSubmit_MultipleTasks(t *testing.T) {
	wp := NewWorkerPool(4)
	defer wp.Stop()

	const numTasks = 20
	resultChs := make([]<-chan collectionResult, numTasks)

	for i := 0; i < numTasks; i++ {
		mc := newMockCollector("load", true)
		mc.collectFn = func(ctx context.Context) (types.Metric, error) {
			return types.NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5}), nil
		}
		resultChs[i] = wp.Submit(context.Background(), mc)
	}

	successCount := 0
	for _, ch := range resultChs {
		result := <-ch
		if result.err == nil {
			successCount++
		}
	}

	if successCount != numTasks {
		t.Errorf("successful tasks = %d, want %d", successCount, numTasks)
	}
}

func TestSubmit_ConcurrentExecutions(t *testing.T) {
	wp := NewWorkerPool(4)
	defer wp.Stop()

	var concurrent atomic.Int64
	var maxConcurrent atomic.Int64

	mc := newMockCollector("load", true)
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		cur := concurrent.Add(1)
		defer concurrent.Add(-1)

		// Track max concurrency
		for {
			prev := maxConcurrent.Load()
			if cur <= prev || maxConcurrent.CompareAndSwap(prev, cur) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond) // simulate work
		return types.NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5}), nil
	}

	const numTasks = 8
	resultChs := make([]<-chan collectionResult, numTasks)
	for i := 0; i < numTasks; i++ {
		resultChs[i] = wp.Submit(context.Background(), mc)
	}

	for _, ch := range resultChs {
		<-ch
	}

	if maxConcurrent.Load() < 2 {
		t.Errorf("max concurrent = %d, want at least 2 (parallel execution)", maxConcurrent.Load())
	}
}

// ---------------------------------------------------------------------------
// Tests: Stop
// ---------------------------------------------------------------------------

func TestWorkerPool_Stop_Idempotent(t *testing.T) {
	wp := NewWorkerPool(2)

	// First stop
	wp.Stop()

	// Second stop should not panic
	wp.Stop()
}

func TestStop_WaitsForTasks(t *testing.T) {
	wp := NewWorkerPool(2)

	var completed atomic.Bool
	mc := newMockCollector("load", true)
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		time.Sleep(50 * time.Millisecond)
		completed.Store(true)
		return types.NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5}), nil
	}

	resultCh := wp.Submit(context.Background(), mc)

	// Give the worker a moment to pick up the task
	time.Sleep(5 * time.Millisecond)

	// Stop should wait for the task to complete
	wp.Stop()

	if !completed.Load() {
		t.Error("task should have completed before Stop returned")
	}

	// Result should still be readable (use timeout to prevent hanging)
	select {
	case <-resultCh:
	case <-time.After(time.Second):
		t.Log("result not received within timeout")
	}
}

func TestStop_NoNewTasksAfterStop(t *testing.T) {
	wp := NewWorkerPool(2)
	wp.Stop()

	mc := newMockCollector("load", true)
	resultCh := wp.Submit(context.Background(), mc)

	// The task should still be accepted (buffered channel) but may not execute
	select {
	case result := <-resultCh:
		if result.err == nil {
			// Task may or may not execute after stop; either is acceptable
		}
	case <-time.After(100 * time.Millisecond):
		// No result - also acceptable
	}
}

// ---------------------------------------------------------------------------
// Tests: WorkerCount
// ---------------------------------------------------------------------------

func TestWorkerCount(t *testing.T) {
	wp := NewWorkerPool(6)
	defer wp.Stop()

	if wp.WorkerCount() != 6 {
		t.Errorf("WorkerCount() = %d, want 6", wp.WorkerCount())
	}
}

// ---------------------------------------------------------------------------
// Tests: Context cancellation during task execution
// ---------------------------------------------------------------------------

func TestSubmit_ContextCancelledDuringExecution(t *testing.T) {
	wp := NewWorkerPool(2)
	defer wp.Stop()

	mc := newMockCollector("load", true)
	mc.collectFn = func(ctx context.Context) (types.Metric, error) {
		// Simulate a long-running collection that checks context
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
			return types.NewLoadMetric(time.Now(), &pb.LoadMetrics{Load1: 0.5}), nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	resultCh := wp.Submit(ctx, mc)

	// Cancel context before collection completes
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Use timeout to prevent test hanging
	select {
	case result := <-resultCh:
		if result.err == nil {
			t.Log("task completed before cancellation (race) - acceptable")
		}
	case <-time.After(time.Second):
		t.Log("no result received within timeout (context cancelled during execution)")
	}
}
