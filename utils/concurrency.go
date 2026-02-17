package utils

import (
	"sync"
)

// WorkerPool manages a fixed pool of goroutines that process jobs concurrently.
// It is safe to use from multiple goroutines.
type WorkerPool struct {
	jobs    chan func()
	wg      sync.WaitGroup
	once    sync.Once
	closeCh chan struct{}
}

// NewWorkerPool creates and starts a worker pool with the given number of workers.
func NewWorkerPool(workers int) *WorkerPool {
	pool := &WorkerPool{
		jobs:    make(chan func(), workers*10),
		closeCh: make(chan struct{}),
	}

	for i := 0; i < workers; i++ {
		pool.wg.Add(1)
		go pool.worker()
	}

	return pool
}

func (p *WorkerPool) worker() {
	defer p.wg.Done()
	for {
		select {
		case job, ok := <-p.jobs:
			if !ok {
				return
			}
			job()
		case <-p.closeCh:
			return
		}
	}
}

// Submit enqueues a job for execution.
// It blocks if the internal job buffer is full.
func (p *WorkerPool) Submit(job func()) {
	p.jobs <- job
}

// Wait blocks until all submitted jobs are finished, then shuts down the pool.
func (p *WorkerPool) Wait() {
	p.once.Do(func() {
		close(p.jobs)
	})
	p.wg.Wait()
}