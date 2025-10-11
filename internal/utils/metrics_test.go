package utils

import (
	"testing"
	"time"
)

func TestMetrics(t *testing.T) {
	t.Run("Initialization", func(t *testing.T) {
		// Reset metrics before testing
		ResetMetrics()

		snapshot := GetMetricsSnapshot()

		// Check that all metrics are zero after reset
		if snapshot.HTTPRequestsTotal != 0 {
			t.Errorf("Expected HTTPRequestsTotal to be 0, got %d", snapshot.HTTPRequestsTotal)
		}
		if snapshot.JobsScrapedTotal != 0 {
			t.Errorf("Expected JobsScrapedTotal to be 0, got %d", snapshot.JobsScrapedTotal)
		}
	})

	t.Run("HTTP Requests", func(t *testing.T) {
		ResetMetrics()

		// Test increment
		IncrementHTTPRequests()
		IncrementHTTPRequests()
		IncrementHTTPRequests()

		snapshot := GetMetricsSnapshot()
		if snapshot.HTTPRequestsTotal != 3 {
			t.Errorf("Expected HTTPRequestsTotal to be 3, got %d", snapshot.HTTPRequestsTotal)
		}
	})

	t.Run("HTTP Errors", func(t *testing.T) {
		ResetMetrics()

		IncrementHTTPErrors()
		IncrementHTTPErrors()

		snapshot := GetMetricsSnapshot()
		if snapshot.HTTPErrorsTotal != 2 {
			t.Errorf("Expected HTTPErrorsTotal to be 2, got %d", snapshot.HTTPErrorsTotal)
		}
	})

	t.Run("Jobs Scraped", func(t *testing.T) {
		ResetMetrics()

		IncrementJobsScraped()
		IncrementJobsScraped()
		IncrementJobsScraped()

		snapshot := GetMetricsSnapshot()
		if snapshot.JobsScrapedTotal != 3 {
			t.Errorf("Expected JobsScrapedTotal to be 3, got %d", snapshot.JobsScrapedTotal)
		}
	})

	t.Run("Jobs Processed", func(t *testing.T) {
		ResetMetrics()

		IncrementJobsProcessed()
		IncrementJobsProcessed()

		snapshot := GetMetricsSnapshot()
		if snapshot.JobsProcessedTotal != 2 {
			t.Errorf("Expected JobsProcessedTotal to be 2, got %d", snapshot.JobsProcessedTotal)
		}
	})

	t.Run("DB Queries", func(t *testing.T) {
		ResetMetrics()

		IncrementDBQueries()
		IncrementDBQueries()
		IncrementDBQueries()
		IncrementDBQueries()

		snapshot := GetMetricsSnapshot()
		if snapshot.DBQueriesTotal != 4 {
			t.Errorf("Expected DBQueriesTotal to be 4, got %d", snapshot.DBQueriesTotal)
		}
	})

	t.Run("DB Errors", func(t *testing.T) {
		ResetMetrics()

		IncrementDBErrors()

		snapshot := GetMetricsSnapshot()
		if snapshot.DBErrorsTotal != 1 {
			t.Errorf("Expected DBErrorsTotal to be 1, got %d", snapshot.DBErrorsTotal)
		}
	})

	t.Run("Request Duration", func(t *testing.T) {
		ResetMetrics()

		AddHTTPRequestDuration(100 * time.Millisecond)
		AddHTTPRequestDuration(200 * time.Millisecond)

		snapshot := GetMetricsSnapshot()
		if snapshot.HTTPRequestDuration < 300 {
			t.Errorf("Expected HTTPRequestDuration to be at least 300ms, got %d", snapshot.HTTPRequestDuration)
		}
	})

	t.Run("Cache Hits and Misses", func(t *testing.T) {
		ResetMetrics()

		IncrementCacheHits()
		IncrementCacheHits()
		IncrementCacheMisses()

		snapshot := GetMetricsSnapshot()
		if snapshot.CacheHits != 2 {
			t.Errorf("Expected CacheHits to be 2, got %d", snapshot.CacheHits)
		}
		if snapshot.CacheMisses != 1 {
			t.Errorf("Expected CacheMisses to be 1, got %d", snapshot.CacheMisses)
		}

		// Check cache hit rate calculation
		expectedRate := 2.0 / 3.0
		if snapshot.CacheHitRate < expectedRate-0.01 || snapshot.CacheHitRate > expectedRate+0.01 {
			t.Errorf("Expected CacheHitRate to be ~%.2f, got %.2f", expectedRate, snapshot.CacheHitRate)
		}
	})

	t.Run("Object Pool Metrics", func(t *testing.T) {
		ResetMetrics()

		IncrementObjectPoolHits()
		IncrementObjectPoolHits()
		IncrementObjectPoolMisses()

		snapshot := GetMetricsSnapshot()
		if snapshot.ObjectPoolHits != 2 {
			t.Errorf("Expected ObjectPoolHits to be 2, got %d", snapshot.ObjectPoolHits)
		}
		if snapshot.ObjectPoolMisses != 1 {
			t.Errorf("Expected ObjectPoolMisses to be 1, got %d", snapshot.ObjectPoolMisses)
		}
	})
}

func TestMetricsConcurrency(t *testing.T) {
	ResetMetrics()

	const numGoroutines = 50
	const numOperations = 100

	// Test concurrent access to metrics
	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < numOperations; j++ {
				IncrementHTTPRequests()
				IncrementJobsScraped()
				IncrementDBQueries()
			}
		}()
	}

	// Wait a bit for operations to complete
	time.Sleep(100 * time.Millisecond)

	snapshot := GetMetricsSnapshot()

	// Verify that metrics were incremented
	if snapshot.HTTPRequestsTotal == 0 {
		t.Error("Expected HTTPRequestsTotal to be greater than 0")
	}
	if snapshot.JobsScrapedTotal == 0 {
		t.Error("Expected JobsScrapedTotal to be greater than 0")
	}
	if snapshot.DBQueriesTotal == 0 {
		t.Error("Expected DBQueriesTotal to be greater than 0")
	}
}

func TestMetricsSnapshot(t *testing.T) {
	ResetMetrics()

	// Set some initial values
	IncrementHTTPRequests()
	IncrementJobsScraped()
	IncrementCacheHits()

	// Get snapshot
	snapshot1 := GetMetricsSnapshot()

	// Modify metrics
	IncrementHTTPRequests()
	IncrementJobsScraped()

	// Get another snapshot
	snapshot2 := GetMetricsSnapshot()

	// Snapshots should be independent
	if snapshot1.HTTPRequestsTotal == snapshot2.HTTPRequestsTotal {
		t.Error("Expected snapshots to be independent")
	}

	if snapshot1.JobsScrapedTotal == snapshot2.JobsScrapedTotal {
		t.Error("Expected snapshots to be independent")
	}
}

func TestMetricsReset(t *testing.T) {
	// Set some values
	IncrementHTTPRequests()
	IncrementJobsScraped()
	IncrementHTTPErrors()
	IncrementDBQueries()
	IncrementDBErrors()
	IncrementCacheHits()

	// Reset metrics
	ResetMetrics()

	snapshot := GetMetricsSnapshot()

	// All metrics should be reset to 0
	if snapshot.HTTPRequestsTotal != 0 {
		t.Errorf("Expected HTTPRequestsTotal to be 0 after reset, got %d", snapshot.HTTPRequestsTotal)
	}
	if snapshot.JobsScrapedTotal != 0 {
		t.Errorf("Expected JobsScrapedTotal to be 0 after reset, got %d", snapshot.JobsScrapedTotal)
	}
	if snapshot.HTTPErrorsTotal != 0 {
		t.Errorf("Expected HTTPErrorsTotal to be 0 after reset, got %d", snapshot.HTTPErrorsTotal)
	}
	if snapshot.DBQueriesTotal != 0 {
		t.Errorf("Expected DBQueriesTotal to be 0 after reset, got %d", snapshot.DBQueriesTotal)
	}
	if snapshot.DBErrorsTotal != 0 {
		t.Errorf("Expected DBErrorsTotal to be 0 after reset, got %d", snapshot.DBErrorsTotal)
	}
	if snapshot.CacheHits != 0 {
		t.Errorf("Expected CacheHits to be 0 after reset, got %d", snapshot.CacheHits)
	}
}

func TestTimer(t *testing.T) {
	timer := NewTimer()

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	elapsed := timer.Elapsed()

	if elapsed < 50*time.Millisecond {
		t.Errorf("Expected elapsed time to be at least 50ms, got %v", elapsed)
	}
}

func BenchmarkMetrics(b *testing.B) {
	ResetMetrics()

	b.Run("IncrementHTTPRequests", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			IncrementHTTPRequests()
		}
	})

	b.Run("IncrementJobsScraped", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			IncrementJobsScraped()
		}
	})

	b.Run("GetMetricsSnapshot", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			GetMetricsSnapshot()
		}
	})
}

func BenchmarkMetricsConcurrency(b *testing.B) {
	ResetMetrics()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			switch i % 8 {
			case 0:
				IncrementHTTPRequests()
			case 1:
				IncrementJobsScraped()
			case 2:
				IncrementJobsProcessed()
			case 3:
				IncrementHTTPErrors()
			case 4:
				IncrementDBQueries()
			case 5:
				IncrementDBErrors()
			case 6:
				IncrementCacheHits()
			case 7:
				IncrementCacheMisses()
			}
			i++
		}
	})
}
