package utils

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics provides application metrics collection
type Metrics struct {
	// HTTP metrics
	HTTPRequestsTotal   int64
	HTTPRequestDuration int64 // in milliseconds
	HTTPErrorsTotal     int64

	// Job scraping metrics
	JobsScrapedTotal   int64
	JobsProcessedTotal int64
	ScrapingDuration   int64 // in milliseconds

	// Database metrics
	DBQueriesTotal  int64
	DBQueryDuration int64 // in milliseconds
	DBErrorsTotal   int64

	// Memory metrics
	MemoryAllocated  int64
	MemoryFreed      int64
	ObjectPoolHits   int64
	ObjectPoolMisses int64

	// Cache metrics
	CacheHits   int64
	CacheMisses int64

	// Error metrics
	PanicsRecovered int64

	// Timing metrics
	StartTime time.Time
	mu        sync.RWMutex
}

// Global metrics instance
var globalMetrics = &Metrics{
	StartTime: time.Now(),
}

// IncrementHTTPRequests increments the HTTP requests counter
func IncrementHTTPRequests() {
	atomic.AddInt64(&globalMetrics.HTTPRequestsTotal, 1)
}

// AddHTTPRequestDuration adds to the HTTP request duration
func AddHTTPRequestDuration(duration time.Duration) {
	atomic.AddInt64(&globalMetrics.HTTPRequestDuration, int64(duration.Milliseconds()))
}

// IncrementHTTPErrors increments the HTTP errors counter
func IncrementHTTPErrors() {
	atomic.AddInt64(&globalMetrics.HTTPErrorsTotal, 1)
}

// IncrementJobsScraped increments the jobs scraped counter
func IncrementJobsScraped() {
	atomic.AddInt64(&globalMetrics.JobsScrapedTotal, 1)
}

// IncrementJobsProcessed increments the jobs processed counter
func IncrementJobsProcessed() {
	atomic.AddInt64(&globalMetrics.JobsProcessedTotal, 1)
}

// AddScrapingDuration adds to the scraping duration
func AddScrapingDuration(duration time.Duration) {
	atomic.AddInt64(&globalMetrics.ScrapingDuration, int64(duration.Milliseconds()))
}

// IncrementDBQueries increments the database queries counter
func IncrementDBQueries() {
	atomic.AddInt64(&globalMetrics.DBQueriesTotal, 1)
}

// AddDBQueryDuration adds to the database query duration
func AddDBQueryDuration(duration time.Duration) {
	atomic.AddInt64(&globalMetrics.DBQueryDuration, int64(duration.Milliseconds()))
}

// IncrementDBErrors increments the database errors counter
func IncrementDBErrors() {
	atomic.AddInt64(&globalMetrics.DBErrorsTotal, 1)
}

// AddMemoryAllocated adds to the memory allocated counter
func AddMemoryAllocated(bytes int64) {
	atomic.AddInt64(&globalMetrics.MemoryAllocated, bytes)
}

// AddMemoryFreed adds to the memory freed counter
func AddMemoryFreed(bytes int64) {
	atomic.AddInt64(&globalMetrics.MemoryFreed, bytes)
}

// IncrementObjectPoolHits increments the object pool hits counter
func IncrementObjectPoolHits() {
	atomic.AddInt64(&globalMetrics.ObjectPoolHits, 1)
}

// IncrementObjectPoolMisses increments the object pool misses counter
func IncrementObjectPoolMisses() {
	atomic.AddInt64(&globalMetrics.ObjectPoolMisses, 1)
}

// IncrementCacheHits increments the cache hits counter
func IncrementCacheHits() {
	atomic.AddInt64(&globalMetrics.CacheHits, 1)
}

// IncrementCacheMisses increments the cache misses counter
func IncrementCacheMisses() {
	atomic.AddInt64(&globalMetrics.CacheMisses, 1)
}

// IncrementPanicsRecovered increments the panics recovered counter
func IncrementPanicsRecovered() {
	atomic.AddInt64(&globalMetrics.PanicsRecovered, 1)
}

// MetricsSnapshot represents a snapshot of metrics at a point in time
type MetricsSnapshot struct {
	Timestamp time.Time `json:"timestamp"`
	Uptime    int64     `json:"uptime_seconds"`

	// HTTP metrics
	HTTPRequestsTotal   int64 `json:"http_requests_total"`
	HTTPRequestDuration int64 `json:"http_request_duration_ms"`
	HTTPErrorsTotal     int64 `json:"http_errors_total"`

	// Job scraping metrics
	JobsScrapedTotal   int64 `json:"jobs_scraped_total"`
	JobsProcessedTotal int64 `json:"jobs_processed_total"`
	ScrapingDuration   int64 `json:"scraping_duration_ms"`

	// Database metrics
	DBQueriesTotal  int64 `json:"db_queries_total"`
	DBQueryDuration int64 `json:"db_query_duration_ms"`
	DBErrorsTotal   int64 `json:"db_errors_total"`

	// Memory metrics
	MemoryAllocated  int64 `json:"memory_allocated_bytes"`
	MemoryFreed      int64 `json:"memory_freed_bytes"`
	ObjectPoolHits   int64 `json:"object_pool_hits"`
	ObjectPoolMisses int64 `json:"object_pool_misses"`

	// Cache metrics
	CacheHits   int64 `json:"cache_hits"`
	CacheMisses int64 `json:"cache_misses"`

	// Error metrics
	PanicsRecovered int64 `json:"panics_recovered"`

	// Computed metrics
	HTTPRequestsPerSecond   float64 `json:"http_requests_per_second"`
	JobsScrapedPerSecond    float64 `json:"jobs_scraped_per_second"`
	CacheHitRate            float64 `json:"cache_hit_rate"`
	ObjectPoolHitRate       float64 `json:"object_pool_hit_rate"`
	AverageHTTPDuration     float64 `json:"average_http_duration_ms"`
	AverageScrapingDuration float64 `json:"average_scraping_duration_ms"`
	AverageDBDuration       float64 `json:"average_db_duration_ms"`
}

// GetMetricsSnapshot returns a snapshot of current metrics
func GetMetricsSnapshot() MetricsSnapshot {
	now := time.Now()
	uptime := now.Sub(globalMetrics.StartTime).Seconds()

	httpRequests := atomic.LoadInt64(&globalMetrics.HTTPRequestsTotal)
	httpDuration := atomic.LoadInt64(&globalMetrics.HTTPRequestDuration)
	httpErrors := atomic.LoadInt64(&globalMetrics.HTTPErrorsTotal)

	jobsScraped := atomic.LoadInt64(&globalMetrics.JobsScrapedTotal)
	jobsProcessed := atomic.LoadInt64(&globalMetrics.JobsProcessedTotal)
	scrapingDuration := atomic.LoadInt64(&globalMetrics.ScrapingDuration)

	dbQueries := atomic.LoadInt64(&globalMetrics.DBQueriesTotal)
	dbDuration := atomic.LoadInt64(&globalMetrics.DBQueryDuration)
	dbErrors := atomic.LoadInt64(&globalMetrics.DBErrorsTotal)

	memoryAllocated := atomic.LoadInt64(&globalMetrics.MemoryAllocated)
	memoryFreed := atomic.LoadInt64(&globalMetrics.MemoryFreed)
	poolHits := atomic.LoadInt64(&globalMetrics.ObjectPoolHits)
	poolMisses := atomic.LoadInt64(&globalMetrics.ObjectPoolMisses)

	cacheHits := atomic.LoadInt64(&globalMetrics.CacheHits)
	cacheMisses := atomic.LoadInt64(&globalMetrics.CacheMisses)

	panicsRecovered := atomic.LoadInt64(&globalMetrics.PanicsRecovered)

	// Compute derived metrics
	var httpRequestsPerSecond, jobsScrapedPerSecond float64
	if uptime > 0 {
		httpRequestsPerSecond = float64(httpRequests) / uptime
		jobsScrapedPerSecond = float64(jobsScraped) / uptime
	}

	var cacheHitRate float64
	if cacheHits+cacheMisses > 0 {
		cacheHitRate = float64(cacheHits) / float64(cacheHits+cacheMisses)
	}

	var poolHitRate float64
	if poolHits+poolMisses > 0 {
		poolHitRate = float64(poolHits) / float64(poolHits+poolMisses)
	}

	var avgHTTPDuration float64
	if httpRequests > 0 {
		avgHTTPDuration = float64(httpDuration) / float64(httpRequests)
	}

	var avgScrapingDuration float64
	if jobsScraped > 0 {
		avgScrapingDuration = float64(scrapingDuration) / float64(jobsScraped)
	}

	var avgDBDuration float64
	if dbQueries > 0 {
		avgDBDuration = float64(dbDuration) / float64(dbQueries)
	}

	return MetricsSnapshot{
		Timestamp:               now,
		Uptime:                  int64(uptime),
		HTTPRequestsTotal:       httpRequests,
		HTTPRequestDuration:     httpDuration,
		HTTPErrorsTotal:         httpErrors,
		JobsScrapedTotal:        jobsScraped,
		JobsProcessedTotal:      jobsProcessed,
		ScrapingDuration:        scrapingDuration,
		DBQueriesTotal:          dbQueries,
		DBQueryDuration:         dbDuration,
		DBErrorsTotal:           dbErrors,
		MemoryAllocated:         memoryAllocated,
		MemoryFreed:             memoryFreed,
		ObjectPoolHits:          poolHits,
		ObjectPoolMisses:        poolMisses,
		CacheHits:               cacheHits,
		CacheMisses:             cacheMisses,
		PanicsRecovered:         panicsRecovered,
		HTTPRequestsPerSecond:   httpRequestsPerSecond,
		JobsScrapedPerSecond:    jobsScrapedPerSecond,
		CacheHitRate:            cacheHitRate,
		ObjectPoolHitRate:       poolHitRate,
		AverageHTTPDuration:     avgHTTPDuration,
		AverageScrapingDuration: avgScrapingDuration,
		AverageDBDuration:       avgDBDuration,
	}
}

// ResetMetrics resets all metrics to zero
func ResetMetrics() {
	atomic.StoreInt64(&globalMetrics.HTTPRequestsTotal, 0)
	atomic.StoreInt64(&globalMetrics.HTTPRequestDuration, 0)
	atomic.StoreInt64(&globalMetrics.HTTPErrorsTotal, 0)
	atomic.StoreInt64(&globalMetrics.JobsScrapedTotal, 0)
	atomic.StoreInt64(&globalMetrics.JobsProcessedTotal, 0)
	atomic.StoreInt64(&globalMetrics.ScrapingDuration, 0)
	atomic.StoreInt64(&globalMetrics.DBQueriesTotal, 0)
	atomic.StoreInt64(&globalMetrics.DBQueryDuration, 0)
	atomic.StoreInt64(&globalMetrics.DBErrorsTotal, 0)
	atomic.StoreInt64(&globalMetrics.MemoryAllocated, 0)
	atomic.StoreInt64(&globalMetrics.MemoryFreed, 0)
	atomic.StoreInt64(&globalMetrics.ObjectPoolHits, 0)
	atomic.StoreInt64(&globalMetrics.ObjectPoolMisses, 0)
	atomic.StoreInt64(&globalMetrics.CacheHits, 0)
	atomic.StoreInt64(&globalMetrics.CacheMisses, 0)
	atomic.StoreInt64(&globalMetrics.PanicsRecovered, 0)

	globalMetrics.StartTime = time.Now()
}

// Timer provides a simple timer for measuring operation duration
type Timer struct {
	start time.Time
}

// NewTimer creates a new timer
func NewTimer() *Timer {
	return &Timer{
		start: time.Now(),
	}
}

// Elapsed returns the elapsed time since the timer was created
func (t *Timer) Elapsed() time.Duration {
	return time.Since(t.start)
}

// Record records the elapsed time for a specific metric type
func (t *Timer) Record(metricType string) {
	duration := t.Elapsed()

	switch metricType {
	case "http":
		AddHTTPRequestDuration(duration)
	case "scraping":
		AddScrapingDuration(duration)
	case "db":
		AddDBQueryDuration(duration)
	}
}
