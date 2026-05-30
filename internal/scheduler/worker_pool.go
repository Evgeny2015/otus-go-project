package scheduler

import (
	"context"
	"log"
	"sync"
)

// WorkerPool manages a fixed pool of goroutines for concurrent metric collection.
// It distributes collection tasks across workers, limiting the number of
// concurrent system calls (e.g., exec.Command, /proc reads) to avoid
// overwhelming the system.
type WorkerPool struct {
	workers  int
	tasks    chan collectionTask
	wg       sync.WaitGroup
	quit     chan struct{}
	stopOnce sync.Once
}

// collectionTask represents a single metric collection job.
type collectionTask struct {
	collector Collector
	ctx       context.Context
	result    chan collectionResult
}

// collectionResult holds the result of a single collection job.
type collectionResult struct {
	metric interface{}
	err    error
}

// NewWorkerPool creates a worker pool with the given number of workers.
// If workers <= 0, it defaults to runtime.NumCPU().
func NewWorkerPool(workers int) *WorkerPool {
	if workers <= 0 {
		workers = 4 // default
	}

	wp := &WorkerPool{
		workers: workers,
		tasks:   make(chan collectionTask, workers*2),
		quit:    make(chan struct{}),
	}

	wp.start()
	return wp
}

// start launches the worker goroutines.
func (wp *WorkerPool) start() {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
	log.Printf("WorkerPool started with %d workers", wp.workers)
}

// worker is a single goroutine that processes collection tasks.
func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.quit:
			return
		case task := <-wp.tasks:
			wp.executeTask(id, task)
		}
	}
}

// executeTask runs a single collection task and sends the result.
func (wp *WorkerPool) executeTask(id int, task collectionTask) {
	// Check context before starting
	select {
	case <-task.ctx.Done():
		task.result <- collectionResult{err: task.ctx.Err()}
		return
	default:
	}

	metric, err := task.collector.Collect(task.ctx)

	// Send result (non-blocking send with select to handle context cancellation)
	select {
	case task.result <- collectionResult{metric: metric, err: err}:
	case <-task.ctx.Done():
		// Context cancelled while waiting to send result
	}
}

// Submit sends a collection task to the worker pool and returns a channel
// that will receive the result. The caller must read from the result channel
// to avoid goroutine leaks.
func (wp *WorkerPool) Submit(ctx context.Context, col Collector) <-chan collectionResult {
	resultCh := make(chan collectionResult, 1)

	task := collectionTask{
		collector: col,
		ctx:       ctx,
		result:    resultCh,
	}

	select {
	case wp.tasks <- task:
	case <-ctx.Done():
		// Context cancelled, send empty result
		go func() {
			resultCh <- collectionResult{err: ctx.Err()}
		}()
	}

	return resultCh
}

// Stop gracefully shuts down the worker pool, waiting for all in-flight
// tasks to complete.
func (wp *WorkerPool) Stop() {
	wp.stopOnce.Do(func() {
		close(wp.quit)
		wp.wg.Wait()
		log.Printf("WorkerPool stopped")
	})
}

// WorkerCount returns the number of workers in the pool.
func (wp *WorkerPool) WorkerCount() int {
	return wp.workers
}
