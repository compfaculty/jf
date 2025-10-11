package utils

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBoundedWorkerPool(t *testing.T) {
	t.Run("Basic Functionality", func(t *testing.T) {
		pool := NewBoundedWorkerPool(BoundedWorkerPoolConfig{
			Workers: 2,
			Queue:   10,
			Timeout: time.Second,
		})
		defer pool.Close()

		var results []int
		var mu sync.Mutex

		// Submit some jobs
		for i := 0; i < 5; i++ {
			i := i // Capture loop variable
			err := pool.Submit(func() {
				mu.Lock()
				results = append(results, i)
				mu.Unlock()
			})
			if err != nil {
				t.Errorf("Failed to submit job %d: %v", i, err)
			}
		}

		// Wait a bit for jobs to complete
		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		if len(results) != 5 {
			t.Errorf("Expected 5 results, got %d", len(results))
		}
		mu.Unlock()
	})

	t.Run("Queue Full", func(t *testing.T) {
		pool := NewBoundedWorkerPool(BoundedWorkerPoolConfig{
			Workers: 1,
			Queue:   2,
			Timeout: time.Second,
		}) // Small queue
		defer pool.Close()

		// Fill the queue
		for i := 0; i < 3; i++ {
			err := pool.Submit(func() {
				time.Sleep(100 * time.Millisecond)
			})
			if i < 2 && err != nil {
				t.Errorf("Expected job %d to be accepted", i)
			}
			if i == 2 && err == nil {
				t.Error("Expected job 2 to be rejected due to full queue")
			}
		}
	})

	t.Run("Stop and Wait", func(t *testing.T) {
		pool := NewBoundedWorkerPool(BoundedWorkerPoolConfig{
			Workers: 2,
			Queue:   10,
			Timeout: time.Second,
		})

		var completed int
		var mu sync.Mutex

		// Submit jobs that take time
		for i := 0; i < 5; i++ {
			pool.Submit(func() {
				time.Sleep(50 * time.Millisecond)
				mu.Lock()
				completed++
				mu.Unlock()
			})
		}

		// Stop and wait
		pool.Close()

		mu.Lock()
		if completed != 5 {
			t.Errorf("Expected 5 completed jobs, got %d", completed)
		}
		mu.Unlock()
	})
}

func TestCircuitBreaker(t *testing.T) {
	t.Run("Closed State", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:   3,
			Timeout:       time.Second,
			HalfOpenLimit: 5,
		})

		// Should allow calls in closed state
		for i := 0; i < 3; i++ {
			err := cb.Execute(func() error {
				return nil // Success
			})
			if err != nil {
				t.Errorf("Expected call %d to be allowed in closed state, got error: %v", i, err)
			}
		}
	})

	t.Run("Open State", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:   2,
			Timeout:       time.Second,
			HalfOpenLimit: 5,
		})

		// Trigger failures to open circuit
		for i := 0; i < 2; i++ {
			cb.Execute(func() error {
				return errors.New("test error") // Failure
			})
		}

		// Should reject calls in open state
		err := cb.Execute(func() error {
			return nil // This should be rejected
		})
		if err == nil {
			t.Error("Expected call to be rejected in open state")
		}
	})

	t.Run("Half-Open State", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:   1,
			Timeout:       50 * time.Millisecond,
			HalfOpenLimit: 5,
		})

		// Trigger failure to open circuit
		cb.Execute(func() error {
			return errors.New("test error")
		})

		// Wait for timeout
		time.Sleep(60 * time.Millisecond)

		// Should allow one call in half-open state
		err := cb.Execute(func() error {
			return nil // Success
		})
		if err != nil {
			t.Error("Expected call to be allowed in half-open state")
		}
	})

	t.Run("Recovery", func(t *testing.T) {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:   2,
			Timeout:       50 * time.Millisecond,
			HalfOpenLimit: 5,
		})

		// Trigger failures
		cb.Execute(func() error {
			return errors.New("test error")
		})
		cb.Execute(func() error {
			return errors.New("test error")
		})

		// Wait for timeout
		time.Sleep(60 * time.Millisecond)

		// Record success
		cb.Execute(func() error {
			return nil // Success
		})

		// Should allow calls again
		err := cb.Execute(func() error {
			return nil // Success
		})
		if err != nil {
			t.Error("Expected call to be allowed after recovery")
		}
	})
}

func TestRateLimiter(t *testing.T) {
	t.Run("Basic Rate Limiting", func(t *testing.T) {
		rl := NewRateLimiter(10, time.Second) // 10 requests per second

		// Should allow initial requests
		for i := 0; i < 10; i++ {
			allowed := rl.Allow()
			if !allowed {
				t.Errorf("Expected request %d to be allowed", i)
			}
		}

		// Should reject excess requests
		allowed := rl.Allow()
		if allowed {
			t.Error("Expected excess request to be rejected")
		}
	})

	t.Run("Rate Limiting with Time", func(t *testing.T) {
		rl := NewRateLimiter(5, 100*time.Millisecond) // 5 requests per 100ms

		// Should allow initial requests
		for i := 0; i < 5; i++ {
			allowed := rl.Allow()
			if !allowed {
				t.Errorf("Expected request %d to be allowed", i)
			}
		}

		// Should reject excess requests
		allowed := rl.Allow()
		if allowed {
			t.Error("Expected excess request to be rejected")
		}

		// Wait for window to reset
		time.Sleep(110 * time.Millisecond)

		// Should allow requests again
		allowed = rl.Allow()
		if !allowed {
			t.Error("Expected request to be allowed after window reset")
		}
	})
}

func TestBoundedWorkerPoolConcurrency(t *testing.T) {
	pool := NewBoundedWorkerPool(BoundedWorkerPoolConfig{
		Workers: 10,
		Queue:   100,
		Timeout: time.Second,
	})
	defer pool.Close()

	const numJobs = 100
	var wg sync.WaitGroup
	wg.Add(numJobs)

	var results []int
	var mu sync.Mutex

	// Submit jobs concurrently
	for i := 0; i < numJobs; i++ {
		i := i
		go func() {
			defer wg.Done()
			err := pool.Submit(func() {
				mu.Lock()
				results = append(results, i)
				mu.Unlock()
			})
			if err != nil {
				t.Errorf("Failed to submit job %d: %v", i, err)
			}
		}()
	}

	wg.Wait()

	// Wait for jobs to complete
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(results) != numJobs {
		t.Errorf("Expected %d results, got %d", numJobs, len(results))
	}
	mu.Unlock()
}

func TestCircuitBreakerConcurrency(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:   5,
		Timeout:       time.Second,
		HalfOpenLimit: 5,
	})

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Test concurrent access
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				cb.Execute(func() error {
					return errors.New("test error")
				})
			}
		}()
	}

	wg.Wait()
	// If we get here without race conditions, the test passes
}

func TestRateLimiterConcurrency(t *testing.T) {
	rl := NewRateLimiter(100, time.Second)

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Test concurrent access
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				rl.Allow()
			}
		}()
	}

	wg.Wait()
	// If we get here without race conditions, the test passes
}

func BenchmarkBoundedWorkerPool(b *testing.B) {
	pool := NewBoundedWorkerPool(BoundedWorkerPoolConfig{
		Workers: 10,
		Queue:   1000,
		Timeout: time.Second,
	})
	defer pool.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			// Simple work
			_ = i * 2
		})
	}
}

func BenchmarkCircuitBreaker(b *testing.B) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:   1000,
		Timeout:       time.Second,
		HalfOpenLimit: 5,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Execute(func() error {
			return nil // Success
		})
	}
}

func BenchmarkRateLimiter(b *testing.B) {
	rl := NewRateLimiter(1000000, time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow()
	}
}

func TestBoundedWorkerPoolInvalidParams(t *testing.T) {
	// Test with invalid parameters
	pool := NewBoundedWorkerPool(BoundedWorkerPoolConfig{
		Workers: 0,
		Queue:   0,
		Timeout: time.Second,
	}) // Should use defaults
	defer pool.Close()

	// Should still work with default values
	err := pool.Submit(func() {
		// Simple job
	})
	if err != nil {
		t.Errorf("Expected pool with invalid params to work with defaults: %v", err)
	}
}

func TestCircuitBreakerStateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:   2,
		Timeout:       100 * time.Millisecond,
		HalfOpenLimit: 5,
	})

	// Start in closed state
	err := cb.Execute(func() error {
		return nil // Success
	})
	if err != nil {
		t.Error("Expected to start in closed state")
	}

	// Record failures to open
	cb.Execute(func() error {
		return errors.New("test error")
	})
	cb.Execute(func() error {
		return errors.New("test error")
	})

	// Should be open now
	err = cb.Execute(func() error {
		return nil // This should fail
	})
	if err == nil {
		t.Error("Expected to be in open state")
	}

	// Wait for timeout
	time.Sleep(110 * time.Millisecond)

	// Should be half-open now
	err = cb.Execute(func() error {
		return nil // Success
	})
	if err != nil {
		t.Error("Expected to be in half-open state")
	}

	// Should be closed again
	err = cb.Execute(func() error {
		return nil // Success
	})
	if err != nil {
		t.Error("Expected to be in closed state after success")
	}
}
