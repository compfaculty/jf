package utils

import (
	"runtime/metrics"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// jfCollector exports our internal metrics snapshot to Prometheus.
type jfCollector struct {
	namespace string

	// Gauges/Counters defined as Desc; values provided at collect time.
	httpRequestsTotal     *prometheus.Desc
	httpErrorsTotal       *prometheus.Desc
	httpRequestDurationMs *prometheus.Desc

	jobsScrapedTotal   *prometheus.Desc
	jobsProcessedTotal *prometheus.Desc
	scrapingDurationMs *prometheus.Desc

	dbQueriesTotal    *prometheus.Desc
	dbErrorsTotal     *prometheus.Desc
	dbQueryDurationMs *prometheus.Desc

	memoryAllocatedBytes *prometheus.Desc
	memoryFreedBytes     *prometheus.Desc
	objectPoolHits       *prometheus.Desc
	objectPoolMisses     *prometheus.Desc

	cacheHits   *prometheus.Desc
	cacheMisses *prometheus.Desc

	panicsRecovered *prometheus.Desc

	uptimeSeconds *prometheus.Desc
}

// NewJFCollector creates a Prometheus collector for jf metrics.
func NewJFCollector() prometheus.Collector {
	ns := "jf"
	return &jfCollector{
		namespace:             ns,
		httpRequestsTotal:     prometheus.NewDesc(prometheus.BuildFQName(ns, "http", "requests_total"), "Total HTTP requests handled", nil, nil),
		httpErrorsTotal:       prometheus.NewDesc(prometheus.BuildFQName(ns, "http", "errors_total"), "Total HTTP errors returned", nil, nil),
		httpRequestDurationMs: prometheus.NewDesc(prometheus.BuildFQName(ns, "http", "request_duration_ms_total"), "Cumulative HTTP request handling duration in milliseconds", nil, nil),

		jobsScrapedTotal:   prometheus.NewDesc(prometheus.BuildFQName(ns, "jobs", "scraped_total"), "Total jobs scraped", nil, nil),
		jobsProcessedTotal: prometheus.NewDesc(prometheus.BuildFQName(ns, "jobs", "processed_total"), "Total jobs processed", nil, nil),
		scrapingDurationMs: prometheus.NewDesc(prometheus.BuildFQName(ns, "jobs", "scraping_duration_ms_total"), "Cumulative jobs scraping duration in milliseconds", nil, nil),

		dbQueriesTotal:    prometheus.NewDesc(prometheus.BuildFQName(ns, "db", "queries_total"), "Total database queries executed", nil, nil),
		dbErrorsTotal:     prometheus.NewDesc(prometheus.BuildFQName(ns, "db", "errors_total"), "Total database errors", nil, nil),
		dbQueryDurationMs: prometheus.NewDesc(prometheus.BuildFQName(ns, "db", "query_duration_ms_total"), "Cumulative DB query duration in milliseconds", nil, nil),

		memoryAllocatedBytes: prometheus.NewDesc(prometheus.BuildFQName(ns, "memory", "allocated_bytes_total"), "Cumulative allocated bytes tracked by app", nil, nil),
		memoryFreedBytes:     prometheus.NewDesc(prometheus.BuildFQName(ns, "memory", "freed_bytes_total"), "Cumulative freed bytes tracked by app", nil, nil),
		objectPoolHits:       prometheus.NewDesc(prometheus.BuildFQName(ns, "objectpool", "hits_total"), "Object pool hits", nil, nil),
		objectPoolMisses:     prometheus.NewDesc(prometheus.BuildFQName(ns, "objectpool", "misses_total"), "Object pool misses", nil, nil),

		cacheHits:   prometheus.NewDesc(prometheus.BuildFQName(ns, "cache", "hits_total"), "Cache hits", nil, nil),
		cacheMisses: prometheus.NewDesc(prometheus.BuildFQName(ns, "cache", "misses_total"), "Cache misses", nil, nil),

		panicsRecovered: prometheus.NewDesc(prometheus.BuildFQName(ns, "runtime", "panics_recovered_total"), "Recovered panics count", nil, nil),

		uptimeSeconds: prometheus.NewDesc(prometheus.BuildFQName(ns, "process", "uptime_seconds"), "Application uptime in seconds", nil, nil),
	}
}

func (c *jfCollector) Describe(ch chan<- *prometheus.Desc) {
	// Send all descs
	ch <- c.httpRequestsTotal
	ch <- c.httpErrorsTotal
	ch <- c.httpRequestDurationMs
	ch <- c.jobsScrapedTotal
	ch <- c.jobsProcessedTotal
	ch <- c.scrapingDurationMs
	ch <- c.dbQueriesTotal
	ch <- c.dbErrorsTotal
	ch <- c.dbQueryDurationMs
	ch <- c.memoryAllocatedBytes
	ch <- c.memoryFreedBytes
	ch <- c.objectPoolHits
	ch <- c.objectPoolMisses
	ch <- c.cacheHits
	ch <- c.cacheMisses
	ch <- c.panicsRecovered
	ch <- c.uptimeSeconds
}

func (c *jfCollector) Collect(ch chan<- prometheus.Metric) {
	snap := GetMetricsSnapshot()

	// Counters
	ch <- prometheus.MustNewConstMetric(c.httpRequestsTotal, prometheus.CounterValue, float64(snap.HTTPRequestsTotal))
	ch <- prometheus.MustNewConstMetric(c.httpErrorsTotal, prometheus.CounterValue, float64(snap.HTTPErrorsTotal))
	ch <- prometheus.MustNewConstMetric(c.httpRequestDurationMs, prometheus.CounterValue, float64(snap.HTTPRequestDuration))

	ch <- prometheus.MustNewConstMetric(c.jobsScrapedTotal, prometheus.CounterValue, float64(snap.JobsScrapedTotal))
	ch <- prometheus.MustNewConstMetric(c.jobsProcessedTotal, prometheus.CounterValue, float64(snap.JobsProcessedTotal))
	ch <- prometheus.MustNewConstMetric(c.scrapingDurationMs, prometheus.CounterValue, float64(snap.ScrapingDuration))

	ch <- prometheus.MustNewConstMetric(c.dbQueriesTotal, prometheus.CounterValue, float64(snap.DBQueriesTotal))
	ch <- prometheus.MustNewConstMetric(c.dbErrorsTotal, prometheus.CounterValue, float64(snap.DBErrorsTotal))
	ch <- prometheus.MustNewConstMetric(c.dbQueryDurationMs, prometheus.CounterValue, float64(snap.DBQueryDuration))

	ch <- prometheus.MustNewConstMetric(c.memoryAllocatedBytes, prometheus.CounterValue, float64(snap.MemoryAllocated))
	ch <- prometheus.MustNewConstMetric(c.memoryFreedBytes, prometheus.CounterValue, float64(snap.MemoryFreed))
	ch <- prometheus.MustNewConstMetric(c.objectPoolHits, prometheus.CounterValue, float64(snap.ObjectPoolHits))
	ch <- prometheus.MustNewConstMetric(c.objectPoolMisses, prometheus.CounterValue, float64(snap.ObjectPoolMisses))

	ch <- prometheus.MustNewConstMetric(c.cacheHits, prometheus.CounterValue, float64(snap.CacheHits))
	ch <- prometheus.MustNewConstMetric(c.cacheMisses, prometheus.CounterValue, float64(snap.CacheMisses))

	ch <- prometheus.MustNewConstMetric(c.panicsRecovered, prometheus.CounterValue, float64(snap.PanicsRecovered))

	// Gauges
	ch <- prometheus.MustNewConstMetric(c.uptimeSeconds, prometheus.GaugeValue, float64(snap.Uptime))

	// Optionally publish selected runtime counters using Go runtime/metrics API (best-effort)
	// This avoids adding process exporter duplicates; default collectors already handle many.
	_ = metrics.All() // keep import referenced; no-op for now.

	_ = time.Now() // avoid linter complaining about unused imports in some setups
}
