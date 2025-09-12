package pool

import (
	"errors"
	"sync"
)

type WorkerPool struct {
	q    chan func()
	done chan struct{}
	wg   sync.WaitGroup
}

func NewWorkerPool(n, queue int) *WorkerPool {
	if n <= 0 {
		n = 1
	}
	if queue <= 0 {
		queue = 1024
	}
	wp := &WorkerPool{
		q:    make(chan func(), queue),
		done: make(chan struct{}),
	}
	wp.wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wp.wg.Done()
			for {
				select {
				case f := <-wp.q:
					if f != nil {
						f()
					}
				case <-wp.done:
					return
				}
			}
		}()
	}
	return wp
}

// Submit blocks if the queue is full, ensuring backpressure.
func (w *WorkerPool) Submit(f func()) {
	w.q <- f
}

// TrySubmit does not block; returns error if queue is full.
func (w *WorkerPool) TrySubmit(f func()) error {
	select {
	case w.q <- f:
		return nil
	default:
		return errors.New("worker pool queue is full")
	}
}

func (w *WorkerPool) Stop() {
	close(w.done)
	w.wg.Wait()
}
