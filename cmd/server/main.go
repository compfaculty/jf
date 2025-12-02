package main

import (
	"context"
	"errors"
	"flag"
	"jf/internal/aggregators"
	"jf/internal/config"
	"jf/internal/httpx"
	"jf/internal/models"
	"jf/internal/pool"
	"jf/internal/repo"
	"jf/internal/scanner"
	"jf/internal/server"
	"jf/internal/utils"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alitto/pond"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

func main() {
	// Parse verbose flag before other initialization
	verbose := flag.Bool("v", false, "Enable verbose logging")
	flag.Parse()

	// Set global verbose state
	utils.SetVerbose(*verbose)
	if *verbose {
		log.Printf("[BOOT] verbose logging enabled")
	}

	// OS signals → context
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- HttpClientConfig ---
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// --- DB (auto-migrate inside) ---
	// Temporarily using SQLite due to DuckDB Windows binding issues
	r, err := repo.NewSQLite("data/jobs.db")
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer func() { _ = r.Close() }()

	// Optional one-time seed
	if err := repo.SeedCompanies(r); err != nil {
		log.Printf("seed companies: %v", err)
	}

	// Initialize aggregator registry (in-memory, not from DB)
	aggregatorReg := aggregators.NewRegistry()

	httpDoer := httpx.New(cfg.HTTPX())
	bp := pool.NewBrowserPool(cfg.BrowserPoolConfig())
	defer bp.Close()
	// --- Shared worker pool
	w, q := cfg.WorkerPoolConfig()
	wp := pond.New(w, q, pond.Context(rootCtx))
	defer wp.StopAndWait()

	// --- SSE Event Broker ---
	broker := server.NewBroker()
	defer broker.Close()

	// --- Scanner manager
	sm := scanner.NewScanner(r, aggregatorReg, cfg, httpDoer, bp, wp)

	// Wire broker callbacks to scanner
	sm.SetJobFoundCallback(func(job models.Job) {
		broker.SendJobFound(job)
	})
	sm.SetStatusCallback(func(running bool, percent, found, total int, errMsg string) {
		broker.SendScanStatus(running, percent, found, total, errMsg)
	})
	sm.SetCompleteCallback(func(totalFound int, duration time.Duration) {
		broker.SendScanComplete(totalFound, duration)
	})

	// --- HTTP server ---
	// RSS feeds are now processed on-demand during scan, no background monitoring
	router := server.NewRouter(r, sm, aggregatorReg, nil, cfg, wp, broker)
	h := WithRecovery(WithRequestLogger(router, cfg.Debug))

	// Prometheus collectors (register safely to avoid duplicate registration on hot-reload/tests)
	safeRegister := func(c prometheus.Collector) {
		if err := prometheus.Register(c); err != nil {
			var alreadyRegistered prometheus.AlreadyRegisteredError
			if errors.As(err, &alreadyRegistered) {
				// Ignore duplicates; another part of the app may have registered this already.
				if *verbose {
					log.Printf("[METRICS] collector already registered: %T", c)
				}
				return
			}
			log.Printf("[METRICS] register error for %T: %v", c, err)
		}
	}

	// Default runtime/process collectors plus our custom jf collector
	safeRegister(collectors.NewGoCollector())
	safeRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	safeRegister(utils.NewJFCollector())

	httpSrv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: h,
	}

	go func() {
		log.Printf("[BOOT] addr=%s", cfg.Addr())
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	// Wait for SIGINT/SIGTERM
	<-rootCtx.Done()
	log.Printf("[SHUTDOWN] start")

	// graceful shutdown window
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeoutDuration())
	defer cancel()

	_ = httpSrv.Shutdown(ctx)
	log.Printf("[SHUTDOWN] done")
}

func WithRequestLogger(next http.Handler, debug bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(lrw, r)
		dur := time.Since(start)
		log.Printf("[REQ] %s %s -> %d bytes=%d dur=%s ua=%q",
			r.Method, r.URL.RequestURI(), lrw.status, lrw.bytes, dur, r.Header.Get("User-Agent"))

		// metrics
		utils.IncrementHTTPRequests()
		utils.AddHTTPRequestDuration(dur)
		if lrw.status >= 400 {
			utils.IncrementHTTPErrors()
		}
		if debug {
			if q := r.URL.Query().Encode(); q != "" {
				log.Printf("[REQ][debug] query: %s", q)
			}
			for k, v := range r.Header {
				log.Printf("[REQ][debug] %s: %s", k, strings.Join(v, ", "))
			}
		}
	})
}

func WithRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[PANIC] %v", rec)
				utils.IncrementPanicsRecovered()
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *loggingResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
func (w *loggingResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}
