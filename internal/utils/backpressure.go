package utils

import (
	"context"
	"errors"
	"sync"
	"time"
)

// BoundedWorkerPool provides a worker pool with backpressure
type BoundedWorkerPool struct {
	jobs      chan func()
	workers   int
	semaphore chan struct{}
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	timeout   time.Duration
	closed    bool
	stats     struct {
		submitted int64
		completed int64
		rejected  int64
	}
	mu sync.RWMutex
}

// BoundedWorkerPoolConfig configures the bounded worker pool
type BoundedWorkerPoolConfig struct {
	Workers int
	Queue   int
	Timeout time.Duration
}

// NewBoundedWorkerPool creates a new bounded worker pool
func NewBoundedWorkerPool(cfg BoundedWorkerPoolConfig) *BoundedWorkerPool {
	// Apply defaults for invalid parameters
	if cfg.Workers <= 0 {
		cfg.Workers = 10 // Default workers
	}
	if cfg.Queue <= 0 {
		cfg.Queue = 100 // Default queue size
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second // Default timeout
	}

	ctx, cancel := context.WithCancel(context.Background())

	p := &BoundedWorkerPool{
		jobs:      make(chan func(), cfg.Queue),
		workers:   cfg.Workers,
		semaphore: make(chan struct{}, cfg.Workers),
		ctx:       ctx,
		cancel:    cancel,
		timeout:   cfg.Timeout,
	}

	// Start workers
	for i := 0; i < cfg.Workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}

	return p
}

// Submit submits a job to the pool with backpressure
func (p *BoundedWorkerPool) Submit(job func()) error {
	if job == nil {
		return errors.New("job cannot be nil")
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("pool is closed")
	}
	p.stats.submitted++
	p.mu.Unlock()

	select {
	case p.jobs <- job:
		return nil
	case <-p.ctx.Done():
		p.mu.Lock()
		p.stats.rejected++
		p.mu.Unlock()
		return p.ctx.Err()
	default:
		p.mu.Lock()
		p.stats.rejected++
		p.mu.Unlock()
		return errors.New("pool at capacity")
	}
}

// SubmitWithTimeout submits a job with a timeout
func (p *BoundedWorkerPool) SubmitWithTimeout(job func(), timeout time.Duration) error {
	if job == nil {
		return errors.New("job cannot be nil")
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("pool is closed")
	}
	p.stats.submitted++
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	select {
	case p.jobs <- job:
		return nil
	case <-ctx.Done():
		p.mu.Lock()
		p.stats.rejected++
		p.mu.Unlock()
		return ctx.Err()
	}
}

// worker is the main worker goroutine
func (p *BoundedWorkerPool) worker() {
	defer p.wg.Done()

	for {
		select {
		case job, ok := <-p.jobs:
			if !ok {
				// Channel closed, exit worker
				return
			}
			// Acquire semaphore
			select {
			case p.semaphore <- struct{}{}:
				// Execute job
				func() {
					defer func() {
						<-p.semaphore // Release semaphore
						p.mu.Lock()
						p.stats.completed++
						p.mu.Unlock()
					}()

					// Recover from panics
					defer func() {
						if r := recover(); r != nil {
							// Log panic but continue
							// In a real implementation, you'd want to log this
						}
					}()

					job()
				}()
			case <-p.ctx.Done():
				return
			}
		case <-p.ctx.Done():
			return
		}
	}
}

// Close shuts down the pool gracefully, waiting for all queued jobs to complete
func (p *BoundedWorkerPool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()

	// Close the jobs channel to prevent new submissions
	close(p.jobs)
	// Wait for all workers to finish processing queued jobs
	p.wg.Wait()
	// Now cancel the context and close the semaphore
	p.cancel()
	close(p.semaphore)
}

// Stats returns pool statistics
func (p *BoundedWorkerPool) GetStats() (submitted, completed, rejected int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stats.submitted, p.stats.completed, p.stats.rejected
}

// QueueSize returns the current queue size
func (p *BoundedWorkerPool) QueueSize() int {
	return len(p.jobs)
}

// ActiveWorkers returns the number of active workers
func (p *BoundedWorkerPool) ActiveWorkers() int {
	return len(p.semaphore)
}

// CircuitBreaker provides circuit breaker functionality
type CircuitBreaker struct {
	maxFailures   int
	timeout       time.Duration
	failures      int64
	lastFail      time.Time
	state         int32 // 0=closed, 1=open, 2=half-open
	mu            sync.RWMutex
	successCount  int64
	halfOpenLimit int
}

// CircuitBreakerConfig configures the circuit breaker
type CircuitBreakerConfig struct {
	MaxFailures   int
	Timeout       time.Duration
	HalfOpenLimit int
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.HalfOpenLimit == 0 {
		cfg.HalfOpenLimit = 5
	}

	return &CircuitBreaker{
		maxFailures:   cfg.MaxFailures,
		timeout:       cfg.Timeout,
		halfOpenLimit: cfg.HalfOpenLimit,
	}
}

// Execute executes a function with circuit breaker protection
func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.state

	switch state {
	case 1: // open
		if time.Since(cb.lastFail) > cb.timeout {
			cb.state = 2 // half-open
			cb.successCount = 0
			state = 2
		} else {
			return errors.New("circuit breaker is open")
		}
	}

	err := fn()

	if err != nil {
		cb.failures++
		cb.lastFail = time.Now()

		if cb.failures >= int64(cb.maxFailures) {
			cb.state = 1 // open
		}

		return err
	}

	// Success
	cb.failures = 0

	if state == 2 { // half-open
		cb.successCount++
		if cb.successCount >= int64(cb.halfOpenLimit) {
			cb.state = 0 // closed
		}
	} else {
		cb.state = 0 // closed
	}

	return nil
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case 0:
		return "closed"
	case 1:
		return "open"
	case 2:
		return "half-open"
	default:
		return "unknown"
	}
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() (failures, successes int64, state string) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	var stateStr string
	switch cb.state {
	case 0:
		stateStr = "closed"
	case 1:
		stateStr = "open"
	case 2:
		stateStr = "half-open"
	default:
		stateStr = "unknown"
	}

	return cb.failures, cb.successCount, stateStr
}

// RateLimiter provides rate limiting functionality
type RateLimiter struct {
	limit    int
	interval time.Duration
	tokens   int
	lastTime time.Time
	mu       sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:    limit,
		interval: interval,
		tokens:   limit,
		lastTime: time.Now(),
	}
}

// Allow checks if a request is allowed under the rate limit
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastTime)

	// Add tokens based on elapsed time
	tokensToAdd := int(elapsed / rl.interval)
	if tokensToAdd > 0 {
		rl.tokens = min(rl.limit, rl.tokens+tokensToAdd)
		rl.lastTime = now
	}

	if rl.tokens > 0 {
		rl.tokens--
		return true
	}

	return false
}

// Wait waits until a request is allowed
func (rl *RateLimiter) Wait(ctx context.Context) error {
	for !rl.Allow() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(rl.interval / time.Duration(rl.limit)):
			// Continue trying
		}
	}
	return nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Global instances for common use cases
var (
	// DefaultHTTPCircuitBreaker for HTTP requests
	DefaultHTTPCircuitBreaker = NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:   5,
		Timeout:       30 * time.Second,
		HalfOpenLimit: 3,
	})

	// DefaultScrapingRateLimiter for scraping operations
	DefaultScrapingRateLimiter = NewRateLimiter(10, time.Second)
)
