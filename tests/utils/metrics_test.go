package utils_test

import (
	"testing"
	"time"

	"jf/internal/utils"
)

func TestMetrics(t *testing.T) {
	// Reset metrics before each test
	utils.ResetMetrics()

	t.Run("HTTP Metrics", func(t *testing.T) {
		// Increment HTTP requests
		utils.IncrementHTTPRequests()
		utils.IncrementHTTPRequests()
		utils.IncrementHTTPRequests()

		snapshot := utils.GetMetricsSnapshot()
		if snapshot.HTTPRequestsTotal != 3 {
			t.Errorf("Expected 3 HTTP requests, got %d", snapshot.HTTPRequestsTotal)
		}

		// Add HTTP request duration
		utils.AddHTTPRequestDuration(100 * time.Millisecond)
		utils.AddHTTPRequestDuration(200 * time.Millisecond)

		snapshot = utils.GetMetricsSnapshot()
		if snapshot.HTTPRequestDuration != 300 {
			t.Errorf("Expected 300ms total HTTP duration, got %d", snapshot.HTTPRequestDuration)
		}

		// Increment HTTP errors
		utils.IncrementHTTPErrors()

		snapshot = utils.GetMetricsSnapshot()
		if snapshot.HTTPErrorsTotal != 1 {
			t.Errorf("Expected 1 HTTP error, got %d", snapshot.HTTPErrorsTotal)
		}
	})

	t.Run("Job Scraping Metrics", func(t *testing.T) {
		utils.ResetMetrics()

		// Increment jobs scraped
		utils.IncrementJobsScraped()
		utils.IncrementJobsScraped()
		utils.IncrementJobsScraped()

		snapshot := utils.GetMetricsSnapshot()
		if snapshot.JobsScrapedTotal != 3 {
			t.Errorf("Expected 3 jobs scraped, got %d", snapshot.JobsScrapedTotal)
		}

		// Increment jobs processed
		utils.IncrementJobsProcessed()
		utils.IncrementJobsProcessed()

		snapshot = utils.GetMetricsSnapshot()
		if snapshot.JobsProcessedTotal != 2 {
			t.Errorf("Expected 2 jobs processed, got %d", snapshot.JobsProcessedTotal)
		}

		// Add scraping duration
		utils.AddScrapingDuration(500 * time.Millisecond)

		snapshot = utils.GetMetricsSnapshot()
		if snapshot.ScrapingDuration != 500 {
			t.Errorf("Expected 500ms scraping duration, got %d", snapshot.ScrapingDuration)
		}
	})

	t.Run("Database Metrics", func(t *testing.T) {
		utils.ResetMetrics()

		// Increment DB queries
		utils.IncrementDBQueries()
		utils.IncrementDBQueries()
		utils.IncrementDBQueries()
		utils.IncrementDBQueries()

		snapshot := utils.GetMetricsSnapshot()
		if snapshot.DBQueriesTotal != 4 {
			t.Errorf("Expected 4 DB queries, got %d", snapshot.DBQueriesTotal)
		}

		// Add DB query duration
		utils.AddDBQueryDuration(50 * time.Millisecond)

		snapshot = utils.GetMetricsSnapshot()
		if snapshot.DBQueryDuration != 50 {
			t.Errorf("Expected 50ms DB query duration, got %d", snapshot.DBQueryDuration)
		}

		// Increment DB errors
		utils.IncrementDBErrors()

		snapshot = utils.GetMetricsSnapshot()
		if snapshot.DBErrorsTotal != 1 {
			t.Errorf("Expected 1 DB error, got %d", snapshot.DBErrorsTotal)
		}
	})

	t.Run("Memory Metrics", func(t *testing.T) {
		utils.ResetMetrics()

		// Add memory allocated
		utils.AddMemoryAllocated(1024)
		utils.AddMemoryAllocated(2048)

		snapshot := utils.GetMetricsSnapshot()
		if snapshot.MemoryAllocated != 3072 {
			t.Errorf("Expected 3072 bytes allocated, got %d", snapshot.MemoryAllocated)
		}

		// Add memory freed
		utils.AddMemoryFreed(1024)

		snapshot = utils.GetMetricsSnapshot()
		if snapshot.MemoryFreed != 1024 {
			t.Errorf("Expected 1024 bytes freed, got %d", snapshot.MemoryFreed)
		}
	})

	t.Run("Object Pool Metrics", func(t *testing.T) {
		utils.ResetMetrics()

		// Increment object pool hits
		utils.IncrementObjectPoolHits()
		utils.IncrementObjectPoolHits()

		snapshot := utils.GetMetricsSnapshot()
		if snapshot.ObjectPoolHits != 2 {
			t.Errorf("Expected 2 object pool hits, got %d", snapshot.ObjectPoolHits)
		}

		// Increment object pool misses
		utils.IncrementObjectPoolMisses()

		snapshot = utils.GetMetricsSnapshot()
		if snapshot.ObjectPoolMisses != 1 {
			t.Errorf("Expected 1 object pool miss, got %d", snapshot.ObjectPoolMisses)
		}

		// Check hit rate
		if snapshot.ObjectPoolHitRate < 0.6 || snapshot.ObjectPoolHitRate > 0.7 {
			t.Errorf("Expected hit rate around 0.66, got %f", snapshot.ObjectPoolHitRate)
		}
	})

	t.Run("Cache Metrics", func(t *testing.T) {
		utils.ResetMetrics()

		// Increment cache hits
		utils.IncrementCacheHits()
		utils.IncrementCacheHits()
		utils.IncrementCacheHits()

		snapshot := utils.GetMetricsSnapshot()
		if snapshot.CacheHits != 3 {
			t.Errorf("Expected 3 cache hits, got %d", snapshot.CacheHits)
		}

		// Increment cache misses
		utils.IncrementCacheMisses()

		snapshot = utils.GetMetricsSnapshot()
		if snapshot.CacheMisses != 1 {
			t.Errorf("Expected 1 cache miss, got %d", snapshot.CacheMisses)
		}

		// Check hit rate
		if snapshot.CacheHitRate != 0.75 {
			t.Errorf("Expected cache hit rate 0.75, got %f", snapshot.CacheHitRate)
		}
	})

	t.Run("Error Metrics", func(t *testing.T) {
		utils.ResetMetrics()

		// Increment panics recovered
		utils.IncrementPanicsRecovered()
		utils.IncrementPanicsRecovered()

		snapshot := utils.GetMetricsSnapshot()
		if snapshot.PanicsRecovered != 2 {
			t.Errorf("Expected 2 panics recovered, got %d", snapshot.PanicsRecovered)
		}
	})
}

func TestMetricsConcurrency(t *testing.T) {
	utils.ResetMetrics()

	const numGoroutines = 50
	const numOperations = 100

	done := make(chan bool)

	// Test concurrent access to metrics
	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < numOperations; j++ {
				utils.IncrementHTTPRequests()
				utils.IncrementJobsScraped()
				utils.IncrementDBQueries()
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	snapshot := utils.GetMetricsSnapshot()

	expectedTotal := int64(numGoroutines * numOperations)

	// Verify that metrics were incremented
	if snapshot.HTTPRequestsTotal != expectedTotal {
		t.Errorf("Expected %d HTTP requests, got %d", expectedTotal, snapshot.HTTPRequestsTotal)
	}

	if snapshot.JobsScrapedTotal != expectedTotal {
		t.Errorf("Expected %d jobs scraped, got %d", expectedTotal, snapshot.JobsScrapedTotal)
	}

	if snapshot.DBQueriesTotal != expectedTotal {
		t.Errorf("Expected %d DB queries, got %d", expectedTotal, snapshot.DBQueriesTotal)
	}
}

func TestMetricsSnapshot(t *testing.T) {
	utils.ResetMetrics()

	// Set some initial values
	utils.IncrementHTTPRequests()
	utils.IncrementJobsScraped()
	utils.IncrementDBQueries()

	// Get snapshot
	snapshot1 := utils.GetMetricsSnapshot()

	// Modify metrics
	utils.IncrementHTTPRequests()
	utils.IncrementJobsScraped()

	// Get another snapshot
	snapshot2 := utils.GetMetricsSnapshot()

	// Snapshots should be different
	if snapshot1.HTTPRequestsTotal == snapshot2.HTTPRequestsTotal {
		t.Error("Expected snapshots to be different")
	}

	if snapshot1.JobsScrapedTotal == snapshot2.JobsScrapedTotal {
		t.Error("Expected snapshots to be different")
	}
}

func TestMetricsReset(t *testing.T) {
	// Set some values
	utils.IncrementHTTPRequests()
	utils.IncrementJobsScraped()
	utils.IncrementDBQueries()
	utils.IncrementHTTPErrors()
	utils.IncrementDBErrors()

	// Reset metrics
	utils.ResetMetrics()

	snapshot := utils.GetMetricsSnapshot()

	// All metrics should be reset to 0
	if snapshot.HTTPRequestsTotal != 0 {
		t.Errorf("Expected HTTP requests to be 0 after reset, got %d", snapshot.HTTPRequestsTotal)
	}

	if snapshot.JobsScrapedTotal != 0 {
		t.Errorf("Expected jobs scraped to be 0 after reset, got %d", snapshot.JobsScrapedTotal)
	}

	if snapshot.DBQueriesTotal != 0 {
		t.Errorf("Expected DB queries to be 0 after reset, got %d", snapshot.DBQueriesTotal)
	}

	if snapshot.HTTPErrorsTotal != 0 {
		t.Errorf("Expected HTTP errors to be 0 after reset, got %d", snapshot.HTTPErrorsTotal)
	}

	if snapshot.DBErrorsTotal != 0 {
		t.Errorf("Expected DB errors to be 0 after reset, got %d", snapshot.DBErrorsTotal)
	}
}

func TestMetricsComputedValues(t *testing.T) {
	utils.ResetMetrics()

	// Add some requests
	for i := 0; i < 10; i++ {
		utils.IncrementHTTPRequests()
		utils.AddHTTPRequestDuration(100 * time.Millisecond)
	}

	// Wait a bit to ensure some time has passed
	time.Sleep(50 * time.Millisecond)

	snapshot := utils.GetMetricsSnapshot()

	// Check computed values
	if snapshot.HTTPRequestsPerSecond == 0 {
		t.Error("Expected non-zero HTTP requests per second")
	}

	if snapshot.AverageHTTPDuration == 0 {
		t.Error("Expected non-zero average HTTP duration")
	}

	// Uptime is in seconds as int64, so after 50ms it will be 0
	// Just check it's non-negative
	if snapshot.Uptime < 0 {
		t.Error("Expected non-negative uptime")
	}
}

func TestTimer(t *testing.T) {
	t.Run("Basic Timer", func(t *testing.T) {
		timer := utils.NewTimer()

		// Wait a bit
		time.Sleep(10 * time.Millisecond)

		elapsed := timer.Elapsed()
		if elapsed < 10*time.Millisecond {
			t.Errorf("Expected at least 10ms elapsed, got %v", elapsed)
		}
	})

	t.Run("Timer Record HTTP", func(t *testing.T) {
		utils.ResetMetrics()

		timer := utils.NewTimer()
		time.Sleep(10 * time.Millisecond)
		timer.Record("http")

		snapshot := utils.GetMetricsSnapshot()
		if snapshot.HTTPRequestDuration == 0 {
			t.Error("Expected non-zero HTTP request duration after timer record")
		}
	})

	t.Run("Timer Record Scraping", func(t *testing.T) {
		utils.ResetMetrics()

		timer := utils.NewTimer()
		time.Sleep(10 * time.Millisecond)
		timer.Record("scraping")

		snapshot := utils.GetMetricsSnapshot()
		if snapshot.ScrapingDuration == 0 {
			t.Error("Expected non-zero scraping duration after timer record")
		}
	})

	t.Run("Timer Record DB", func(t *testing.T) {
		utils.ResetMetrics()

		timer := utils.NewTimer()
		time.Sleep(10 * time.Millisecond)
		timer.Record("db")

		snapshot := utils.GetMetricsSnapshot()
		if snapshot.DBQueryDuration == 0 {
			t.Error("Expected non-zero DB query duration after timer record")
		}
	})
}

func BenchmarkMetrics(b *testing.B) {
	utils.ResetMetrics()

	b.Run("IncrementHTTPRequests", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			utils.IncrementHTTPRequests()
		}
	})

	b.Run("IncrementJobsScraped", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			utils.IncrementJobsScraped()
		}
	})

	b.Run("IncrementDBQueries", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			utils.IncrementDBQueries()
		}
	})

	b.Run("GetMetricsSnapshot", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			utils.GetMetricsSnapshot()
		}
	})
}

func BenchmarkMetricsConcurrency(b *testing.B) {
	utils.ResetMetrics()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			switch i % 8 {
			case 0:
				utils.IncrementHTTPRequests()
			case 1:
				utils.IncrementJobsScraped()
			case 2:
				utils.IncrementJobsProcessed()
			case 3:
				utils.IncrementDBQueries()
			case 4:
				utils.IncrementHTTPErrors()
			case 5:
				utils.IncrementDBErrors()
			case 6:
				utils.IncrementCacheHits()
			case 7:
				utils.IncrementCacheMisses()
			}
			i++
		}
	})
}
