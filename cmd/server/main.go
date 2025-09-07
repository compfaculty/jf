package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	appcfg "jf/internal/config"
	"jf/internal/httpx"
	"jf/internal/repo"
	"jf/internal/scanner"
	"jf/internal/server"
)

// -------- Env/Config helpers --------

type AppConfig struct {
	Addr            string        // HTTP bind address
	DBPath          string        // SQLite path
	LogLevel        string        // debug|info|warn|error (best-effort; std log)
	ShutdownTimeout time.Duration // graceful shutdown timeout
	ConfigPath      string        // YAML preferences path
}

func loadConfig() AppConfig {
	addr := getEnv("JF_ADDR", ":8080")
	db := getEnv("JFV2_DB_PATH", "data/jobs.db")
	level := strings.ToLower(getEnv("JF_LOG_LEVEL", "info"))
	to := parseDur(getEnv("JF_SHUTDOWN_TIMEOUT", "10s"), 10*time.Second)
	cfgPath := getEnv("JF_CONFIG_PATH", "config/config.yaml")

	return AppConfig{
		Addr:            addr,
		DBPath:          db,
		LogLevel:        level,
		ShutdownTimeout: to,
		ConfigPath:      cfgPath,
	}
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func parseDur(s string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d
	}
	return def
}

func isDebug(level string) bool { return level == "debug" }

// -------- main --------

func main() {
	// Verbose log format
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)

	started := time.Now()
	cfg := loadConfig()

	log.Printf("[BOOT] starting job-finder server")
	log.Printf("[BOOT] config: addr=%s db=%s log_level=%s shutdown_timeout=%s cfg_yaml=%s",
		cfg.Addr, cfg.DBPath, cfg.LogLevel, cfg.ShutdownTimeout, cfg.ConfigPath)

	// Load YAML preferences (safe even if file missing: defaults apply)
	prefs, err := appcfg.LoadPreferences(cfg.ConfigPath)
	if err != nil {
		log.Printf("[BOOT] preferences load error (%s): %v (using defaults)", cfg.ConfigPath, err)
	}
	logPrefsSummary(prefs)

	// Init repository (SQLite + schema)
	sqlRepo, err := repo.NewSQLite(cfg.DBPath)
	if err != nil {
		log.Fatalf("[BOOT] repo init failed: %v", err)
	}
	defer func() {
		if err := sqlRepo.Close(); err != nil {
			log.Printf("[SHUTDOWN] repo close error: %v", err)
		}
	}()

	// Seed companies (embedded JSON; idempotent)
	if err := repo.SeedCompanies(sqlRepo); err != nil {
		log.Printf("[BOOT] seed companies error: %v", err)
	} else {
		log.Printf("[BOOT] seed companies ok")
	}

	// Shared HTTP client for all scrapers (keep-alive, timeouts, HTTP/2)
	baseHTTP := httpx.NewHTTPClient()
	var httpDoer interface {
		Do(*http.Request) (*http.Response, error)
	} = baseHTTP
	if rpsStr := os.Getenv("JF_HTTP_RPS"); rpsStr != "" {
		rps, _ := strconv.ParseFloat(rpsStr, 64)
		if rps <= 0 {
			rps = 5
		}
		burst := 10
		if bStr := os.Getenv("JF_HTTP_BURST"); bStr != "" {
			if b, err := strconv.Atoi(bStr); err == nil && b > 0 {
				burst = b
			}
		}
		httpDoer = httpx.NewRateLimited(baseHTTP, rps, burst)
		log.Printf("[BOOT] httpx: rate limit enabled rps=%.2f burst=%d", rps, burst)
	} else {
		log.Printf("[BOOT] httpx: rate limit disabled")
	}

	// Scanner manager (uses repo + prefs + shared HTTP client)
	sm := scanner.NewManager(sqlRepo, prefs, httpDoer)

	// Router + middlewares (request logging + recovery)
	baseHandler := server.NewRouter(sqlRepo, sm)
	h := withRecovery(withRequestLogger(baseHandler, cfg.LogLevel), cfg.LogLevel)

	// HTTP server with sane timeouts
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           h,
		ReadTimeout:       20 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      40 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	// Serve
	go func() {
		log.Printf("[HTTP] listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[HTTP] fatal: %v", err)
		}
	}()

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	sig := <-stop
	log.Printf("[SHUTDOWN] received signal: %s", sig)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	log.Printf("[SHUTDOWN] draining HTTP connections (timeout=%s)...", cfg.ShutdownTimeout)
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[SHUTDOWN] graceful error: %v", err)
	} else {
		log.Printf("[SHUTDOWN] HTTP server closed gracefully")
	}

	log.Printf("[EXIT] uptime=%s", time.Since(started).Truncate(time.Millisecond))
}

// -------- Prefs summary logging (non-sensitive) --------

func logPrefsSummary(p *appcfg.Config) {
	if p == nil {
		log.Printf("[BOOT] prefs: <nil>")
		return
	}
	good, bad := p.GoodBadKeywordSets()
	log.Printf("[BOOT] prefs: roles=%d good=%d bad=%d heur=%.2f strong=%.2f llm_enabled=%t model=%s",
		len(p.CVRoles), len(good), len(bad), p.HeuristicThreshold, p.StrongThreshold, p.LLMEnabled, safeStr(p.LLMModel))
	// Mask ApplyForm contact info in logs
	af := p.ApplyForm
	log.Printf("[BOOT] apply_form: name=%s %s email=%s phone=%s cv=%s agree_tos=%t forward_headhunters=%t",
		safeStr(af.FirstName), safeStr(af.LastName), maskEmail(af.Email), maskPhone(af.Phone), safePath(af.CVPath),
		af.AgreeTOS, af.ForwardToHeadhunters)
}

func safeStr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return s
}
func safePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "-"
	}
	if home, _ := os.UserHomeDir(); home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func maskEmail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || !strings.Contains(s, "@") {
		return "-"
	}
	parts := strings.SplitN(s, "@", 2)
	local := parts[0]
	if len(local) > 2 {
		local = local[:2] + strings.Repeat("*", len(local)-2)
	} else if len(local) == 2 {
		local = local[:1] + "*"
	} else {
		local = "*"
	}
	domain := parts[1]
	return local + "@" + domain
}

func maskPhone(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	var digits []rune
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	if len(digits) <= 4 {
		return "***"
	}
	masked := strings.Repeat("*", len(digits)-4) + string(digits[len(digits)-4:])
	return masked
}

// -------- Middlewares --------

func withRequestLogger(next http.Handler, level string) http.Handler {
	debug := isDebug(level)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w, status: 200}

		next.ServeHTTP(lrw, r)

		dur := time.Since(start)
		ip := clientIP(r)
		ua := r.Header.Get("User-Agent")

		log.Printf("[REQ] %s %s -> %d bytes=%d dur=%s ip=%s ua=%q",
			r.Method, r.URL.RequestURI(), lrw.status, lrw.bytes, dur, ip, ua)

		if debug {
			if q := r.URL.Query().Encode(); q != "" {
				log.Printf("[REQ][debug] query: %s", q)
			}
			if len(r.Header) > 0 {
				log.Printf("[REQ][debug] headers:")
				for k, vals := range r.Header {
					log.Printf("   %s: %s", k, strings.Join(vals, ", "))
				}
			}
		}
	})
}

func withRecovery(next http.Handler, _ string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[PANIC] %v", rec)
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

func (w *loggingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		conn, rw, err := hj.Hijack()
		return conn, rw, err
	}
	return nil, nil, fmt.Errorf("hijacker not supported")
}

func clientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}

// -------- Small helpers --------

func readAllLimit(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = 1 << 20 // 1 MiB
	}
	return io.ReadAll(io.LimitReader(r, limit))
}

func atoi(s string, def int) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return i
}
