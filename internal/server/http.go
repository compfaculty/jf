package server

import (
	"compress/gzip"
	"context"
	"crypto/md5"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alitto/pond"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	promhttp "github.com/prometheus/client_golang/prometheus/promhttp"

	"jf/internal/aggregators"
	"jf/internal/config"
	"jf/internal/emailx"
	"jf/internal/feed"
	"jf/internal/models"
	"jf/internal/repo"
	"jf/internal/scanner"
	"jf/internal/scrape"
	commonpkg "jf/internal/scrape/common"
	"jf/internal/utils"
)

//go:embed gui.html
var content embed.FS

// applyMu guards applyRunning and applyStatus.
var applyMu sync.Mutex
var applyRunning bool

// applyStatus holds progress for the current apply run (for GET /api/apply/status).
var applyStatus struct {
	mu      sync.RWMutex
	Total   int `json:"total"`
	Sent    int `json:"sent"`
	Failed  int `json:"failed"`
	Waiting int `json:"waiting"` // jobs queued for 429 retry
}

const applyDelayMinSec = 8
const applyDelayMaxSec = 20

// guiETag stores the computed ETag for the embedded GUI
var guiETag string
var guiContent []byte

func init() {
	// Pre-compute ETag for embedded GUI
	data, _ := fs.ReadFile(content, "gui.html")
	if len(data) > 0 {
		guiContent = data
		hash := md5.Sum(data)
		guiETag = `"` + hex.EncodeToString(hash[:]) + `"`
	}
}

// gzipResponseWriter wraps http.ResponseWriter with gzip compression
type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

// GzipMiddleware compresses responses for clients that support gzip.
// Skips compression for SSE streams (/api/scan/stream) because gzip buffers
// output and blocks real-time delivery of apply_progress/apply_complete events.
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/scan/stream" || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		next.ServeHTTP(gzipResponseWriter{Writer: gz, ResponseWriter: w}, r)
	})
}

func NewRouter(r repo.Repo, sm *scanner.Scanner, aggregatorReg *aggregators.Registry, fm *feed.Monitor, cfg *config.Config, wp *pond.WorkerPool, broker *Broker) http.Handler {
	mux := chi.NewRouter()
	mux.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	mux.Use(GzipMiddleware)

	// Static SPA
	mux.Get("/", serveIndex)
	mux.Get("/index.html", serveIndex)

	// API
	mux.Post("/api/scan/start", func(w http.ResponseWriter, _ *http.Request) {
		_ = sm.StartScan()
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
	})
	mux.Get("/api/scan/status", func(w http.ResponseWriter, _ *http.Request) {
		st := sm.Status()
		writeJSON(w, http.StatusOK, ScanStatus{
			Running:   st.Running,
			StartedAt: st.StartedAt.Format(time.RFC3339),
			Percent:   st.Percent,
			Found:     st.Found,
			Total:     st.Total,
			Error:     st.Error,
		})
	})

	mux.Get("/api/apply/status", func(w http.ResponseWriter, _ *http.Request) {
		applyMu.Lock()
		running := applyRunning
		applyMu.Unlock()
		applyStatus.mu.RLock()
		sent := applyStatus.Sent
		failed := applyStatus.Failed
		waiting := applyStatus.Waiting
		total := applyStatus.Total
		applyStatus.mu.RUnlock()
		queued := total - sent - failed - waiting
		if queued < 0 {
			queued = 0
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"running": running,
			"total":   total,
			"sent":    sent,
			"failed":  failed,
			"waiting": waiting,
			"queued":  queued,
		})
	})

	// SSE stream endpoint for real-time updates
	if broker != nil {
		mux.Get("/api/scan/stream", func(w http.ResponseWriter, req *http.Request) {
			handleSSEStream(w, req, broker, sm)
		})
	}

	// Metrics endpoint
	mux.Get("/api/metrics", func(w http.ResponseWriter, _ *http.Request) {
		metrics := utils.GetMetricsSnapshot()
		writeJSON(w, http.StatusOK, metrics)
	})

	// Prometheus metrics endpoint (standard path)
	mux.Method("GET", "/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		promhttp.Handler().ServeHTTP(w, r)
	}))

	// CV list endpoint - scan /data folder for PDF files
	mux.Get("/api/cv/list", func(w http.ResponseWriter, _ *http.Request) {
		cvs, err := listCVFiles("data")
		if err != nil {
			log.Printf("[CV] list error: %v", err)
			http.Error(w, fmt.Sprintf("failed to list CV files: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"cvs": cvs})
	})

	// CV upload endpoint - upload PDF file to /data folder
	mux.Post("/api/cv/upload", func(w http.ResponseWriter, req *http.Request) {
		// Parse multipart form (max 10MB)
		if err := req.ParseMultipartForm(10 << 20); err != nil {
			log.Printf("[CV] upload parse error: %v", err)
			http.Error(w, fmt.Sprintf("failed to parse form: %v", err), http.StatusBadRequest)
			return
		}

		file, header, err := req.FormFile("file")
		if err != nil {
			log.Printf("[CV] upload form file error: %v", err)
			http.Error(w, fmt.Sprintf("no file provided: %v", err), http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Validate file extension
		filename := header.Filename
		if !strings.EqualFold(filepath.Ext(filename), ".pdf") {
			http.Error(w, "only PDF files are allowed", http.StatusBadRequest)
			return
		}

		// Sanitize filename (remove path components, keep only safe characters)
		filename = filepath.Base(filename)
		filename = strings.ReplaceAll(filename, "..", "")
		filename = strings.TrimSpace(filename)
		if filename == "" {
			http.Error(w, "invalid filename", http.StatusBadRequest)
			return
		}

		// Create /data directory if it doesn't exist
		dataDir := "data"
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			log.Printf("[CV] upload mkdir error: %v", err)
			http.Error(w, fmt.Sprintf("failed to create data directory: %v", err), http.StatusInternalServerError)
			return
		}

		// Save file to /data folder
		dstPath := filepath.Join(dataDir, filename)
		dstFile, err := os.Create(dstPath)
		if err != nil {
			log.Printf("[CV] upload create file error: %v", err)
			http.Error(w, fmt.Sprintf("failed to save file: %v", err), http.StatusInternalServerError)
			return
		}
		defer dstFile.Close()

		if _, err := dstFile.ReadFrom(file); err != nil {
			dstFile.Close()
			os.Remove(dstPath) // Clean up on error
			log.Printf("[CV] upload write error: %v", err)
			http.Error(w, fmt.Sprintf("failed to write file: %v", err), http.StatusInternalServerError)
			return
		}

		// Normalize path for response
		normalizedPath := strings.ReplaceAll(dstPath, "\\", "/")

		log.Printf("[CV] uploaded: %s", normalizedPath)
		writeJSON(w, http.StatusOK, map[string]any{
			"filename": filename,
			"path":     normalizedPath,
		})
	})

	// Config CV endpoint - return current CV path from config
	mux.Get("/api/config/cv", func(w http.ResponseWriter, _ *http.Request) {
		cvPath := strings.TrimSpace(cfg.ApplyForm.CVPath)
		writeJSON(w, http.StatusOK, map[string]any{"cv_path": cvPath})
	})

	// RSS Feed endpoints
	if fm != nil {
		mux.Get("/api/feed/status", func(w http.ResponseWriter, _ *http.Request) {
			status := fm.GetStatus()
			writeJSON(w, http.StatusOK, status)
		})
		mux.Get("/api/feed/updates", func(w http.ResponseWriter, req *http.Request) {
			limit := atoi(req.URL.Query().Get("limit"))
			if limit <= 0 || limit > 100 {
				limit = 20
			}
			updates := fm.GetUpdates(limit)
			writeJSON(w, http.StatusOK, map[string]any{
				"updates": updates,
			})
		})
	}

	// Jobs (paginated)
	mux.Get("/api/jobs", func(w http.ResponseWriter, req *http.Request) {
		limit := atoi(req.URL.Query().Get("limit"))
		offset := atoi(req.URL.Query().Get("offset"))
		if limit <= 0 || limit > 200 {
			limit = 25
		}
		if offset < 0 {
			offset = 0
		}

		hideApplied := req.URL.Query().Get("hide_applied") == "true"

		q := models.JobQuery{
			CompanyID:   strings.TrimSpace(req.URL.Query().Get("company_id")),
			Q:           strings.TrimSpace(req.URL.Query().Get("q")),
			HideApplied: hideApplied,
			Limit:       limit,
			Offset:      offset,
		}
		items, total, err := r.ListJobsPage(req.Context(), q)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"items":  items,
			"total":  total,
			"limit":  q.Limit,
			"offset": q.Offset,
		})
	})

	mux.Post("/api/apply", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			IDs    []string `json:"ids"`
			CVPath string   `json:"cv_path"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		log.Printf("[APPLY] request ids=%v cv_path=%q", body.IDs, body.CVPath)

		if len(body.IDs) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"updated": 0})
			return
		}

		applyMu.Lock()
		if applyRunning {
			applyMu.Unlock()
			writeJSON(w, http.StatusConflict, map[string]any{"error": "apply already in progress"})
			return
		}
		applyRunning = true
		applyMu.Unlock()

		jobs, err := r.ListJobsByIDs(req.Context(), body.IDs)
		if err != nil {
			applyMu.Lock()
			applyRunning = false
			applyMu.Unlock()
			log.Printf("[APPLY] db ListJobsByIDs error: %v", err)
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		log.Printf("[APPLY] loaded %d jobs", len(jobs))
		if len(jobs) == 0 {
			applyMu.Lock()
			applyRunning = false
			applyMu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"updated": 0})
			return
		}

		companies, err := r.ListCompanies(req.Context())
		if err != nil {
			applyMu.Lock()
			applyRunning = false
			applyMu.Unlock()
			log.Printf("[APPLY] ListCompanies error: %v", err)
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		companyMap := make(map[string]models.Company)
		for _, c := range companies {
			companyMap[c.ID] = c
		}

		aggregators := aggregatorReg.GetAll()
		aggregatorMap := make(map[string]models.Aggregator)
		for _, a := range aggregators {
			aggregatorMap[a.Name] = a
		}

		feedParser := feed.NewParser(http.DefaultClient)
		var bp commonpkg.Browser

		writeJSON(w, http.StatusAccepted, map[string]any{"status": "started", "total": len(jobs)})

		go func() {
			total := len(jobs)
			applyMu.Lock()
			applyRunning = true
			applyMu.Unlock()
			applyStatus.mu.Lock()
			applyStatus.Total = total
			applyStatus.Sent = 0
			applyStatus.Failed = 0
			applyStatus.Waiting = 0
			applyStatus.mu.Unlock()
			defer func() {
				applyMu.Lock()
				applyRunning = false
				applyMu.Unlock()
			}()

			ctx := context.Background()
			okIDs := make([]string, 0, total)
			sent := 0
			fail := 0
			waiting := 0

			waitHours := 2
			if v := os.Getenv("JF_RATE_LIMIT_WAIT_HOURS"); v != "" {
				if h, err := strconv.Atoi(v); err == nil && h > 0 {
					waitHours = h
				}
			}
			rateLimitBackoff := time.Duration(waitHours) * time.Hour

			for i := range jobs {
				j := jobs[i]
				queued := total - sent - fail

				log.Printf("[APPLY] start job id=%s url=%s title=%q", j.ID, j.URL, j.Title)

				ok := false
				errMsg := ""
				var result *models.AppliedResult

				var source scrape.JobSource
				if j.AggregatorName != "" {
					if agg, exists := aggregatorMap[j.AggregatorName]; exists {
						company := models.Company{
							Name:       agg.Name,
							CareersURL: agg.SourceURL,
							Active:     agg.Active,
						}
						if err := r.UpsertCompanyByName(ctx, &company); err == nil {
							source = scrape.NewJobSource(company, &agg, http.DefaultClient, bp, wp, feedParser, r)
						}
					}
				} else if j.CompanyID != "" {
					if company, exists := companyMap[j.CompanyID]; exists {
						source = scrape.NewJobSource(company, nil, http.DefaultClient, bp, wp, feedParser, r)
					}
				}

				if source == nil && j.URL != "" {
					if agg := findAggregatorByURL(j.URL, aggregators); agg != nil {
						company := models.Company{
							Name:       agg.Name,
							CareersURL: agg.SourceURL,
							Active:     agg.Active,
						}
						if err := r.UpsertCompanyByName(ctx, &company); err == nil {
							source = scrape.NewJobSource(company, agg, http.DefaultClient, bp, wp, feedParser, r)
							log.Printf("[APPLY] id=%s found source via URL fallback: %s", j.ID, agg.Name)
						}
					}
				}

				cvPath := strings.TrimSpace(body.CVPath)
				if strings.TrimSpace(j.HREmail) != "" {
					ok, errMsg = applyViaEmail(ctx, j, cfg, cvPath)
					if ok {
						log.Printf("[APPLY][EMAIL] id=%s to=%s ok", j.ID, j.HREmail)
					} else {
						log.Printf("[APPLY][EMAIL] id=%s err=%s", j.ID, errMsg)
					}
				} else if source != nil {
					var err error
					result, err = source.ApplyJob(ctx, j, cfg)
					if err != nil {
						errMsg = "ApplyJob error: " + err.Error()
						log.Printf("[APPLY] id=%s err=%s", j.ID, errMsg)
					} else if result != nil {
						ok = result.OK
						if !ok && result.Message != "" {
							errMsg = result.Message
						}
						log.Printf("[APPLY] id=%s ok=%v status=%d msg=%q", j.ID, result.OK, result.Status, result.Message)
						// 429: enqueue for retry, do not count as fail
						if !ok && result.Status == 429 {
							retryAfter := time.Now().UTC().Add(rateLimitBackoff)
							if enqErr := r.EnqueueRateLimited(ctx, j.ID, j.URL, retryAfter); enqErr != nil {
								log.Printf("[APPLY] id=%s EnqueueRateLimited error: %v", j.ID, enqErr)
							} else {
								waiting++
								log.Printf("[APPLY] id=%s rate-limited (429), queued for retry at %s", j.ID, retryAfter.Format(time.RFC3339))
							}
						}
					} else {
						errMsg = "apply not supported for this source"
						log.Printf("[APPLY] id=%s unsupported", j.ID)
					}
				} else {
					errMsg = "could not determine source for job"
					log.Printf("[APPLY] id=%s err=%s", j.ID, errMsg)
				}

				if ok {
					okIDs = append(okIDs, j.ID)
					sent++
				} else if result != nil && result.Status == 429 {
					// Already handled above (waiting++), nothing more
				} else {
					fail++
					if errMsg != "" {
						log.Printf("[APPLY] failed id=%s url=%s err=%s", j.ID, j.URL, errMsg)
					} else {
						log.Printf("[APPLY] failed id=%s url=%s err=unknown", j.ID, j.URL)
					}
				}

				queued = total - sent - fail - waiting
				applyStatus.mu.Lock()
				applyStatus.Sent = sent
				applyStatus.Failed = fail
				applyStatus.Waiting = waiting
				applyStatus.mu.Unlock()
				if broker != nil {
					broker.SendApplyProgress(queued, sent, fail, waiting, j.Title)
				}

				if i < len(jobs)-1 {
					delaySec := applyDelayMinSec + rand.Intn(applyDelayMaxSec-applyDelayMinSec+1)
					select {
					case <-time.After(time.Duration(delaySec) * time.Second):
					case <-ctx.Done():
						log.Printf("[APPLY] cancelled")
						return
					}
				}
			}

			var updated int64
			if len(okIDs) > 0 {
				n, err := r.ApplyJobs(ctx, okIDs)
				if err != nil {
					log.Printf("[APPLY] db ApplyJobs error: %v", err)
				} else {
					updated = n
				}
			}

			log.Printf("[APPLY] done attempted=%d success=%d fail=%d updated=%d", total, len(okIDs), fail, updated)
			if broker != nil {
				broker.SendApplyComplete(int(updated), fail)
			}
		}()
	})

	mux.Post("/api/delete", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		n, err := r.DeleteJobs(req.Context(), body.IDs)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
	})

	return mux
}

func atoi(s string) int {
	i, _ := strconv.Atoi(s)
	return i
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// applyViaEmail sends application via email to HR email.
// If cvPathOverride is provided and non-empty, it will be used instead of automatic selection.
func applyViaEmail(ctx context.Context, job models.Job, cfg *config.Config, cvPathOverride string) (bool, string) {
	mailer := emailx.BuildSMTPMailer(&cfg.Mail)
	applicant := emailx.Applicant{
		FullName: strings.TrimSpace(cfg.ApplyForm.FirstName + " " + cfg.ApplyForm.LastName),
		Email:    cfg.ApplyForm.Email,
		Phone:    cfg.ApplyForm.Phone,
	}

	subj := "Application: " + job.Title + " — " + applicant.FullName

	// Use override if provided, otherwise use automatic selection
	var cvPath string
	if strings.TrimSpace(cvPathOverride) != "" {
		cvPath = strings.TrimSpace(cvPathOverride)
		log.Printf("[APPLY][EMAIL] using override CV: %q", cvPath)
	} else {
		cvPath, _ = emailx.ChooseResume(job.Title, &cfg.Mail)
		log.Printf("[APPLY][EMAIL] using auto-selected CV: %q", cvPath)
	}

	body := strings.Builder{}
	if strings.TrimSpace(applicant.FullName) != "" {
		body.WriteString("Hi,\n\n")
	}
	body.WriteString("I'm applying for the " + strings.TrimSpace(job.Title) + " role.\n")
	if strings.TrimSpace(job.URL) != "" {
		body.WriteString("Job link: " + strings.TrimSpace(job.URL) + "\n")
	}
	if strings.TrimSpace(applicant.LinkedIn) != "" {
		body.WriteString("LinkedIn: " + applicant.LinkedIn + "\n")
	}
	if strings.TrimSpace(applicant.Portfolio) != "" {
		body.WriteString("Portfolio: " + applicant.Portfolio + "\n")
	}
	body.WriteString("\nBest,\n" + applicant.FullName + "\n")

	atts := []string{}
	if strings.TrimSpace(cvPath) != "" {
		// Verify CV file exists before adding as attachment
		if st, err := os.Stat(cvPath); err == nil && !st.IsDir() && strings.EqualFold(filepath.Ext(cvPath), ".pdf") {
			atts = append(atts, cvPath)
		} else if err != nil {
			log.Printf("[APPLY][EMAIL] CV file not found or invalid: %q (error: %v)", cvPath, err)
			// Continue without attachment - email will still be sent
		}
	}

	if err := mailer.Send([]string{strings.TrimSpace(job.HREmail)}, subj, body.String(), atts); err != nil {
		return false, fmt.Sprintf("email send error: %v", err)
	}
	return true, ""
}

func handleSSEStream(w http.ResponseWriter, req *http.Request, broker *Broker, sm *scanner.Scanner) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Flush headers immediately
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Generate client ID
	clientID := fmt.Sprintf("client-%d", time.Now().UnixNano())

	// Register client
	client := broker.Register(clientID)
	if client == nil {
		http.Error(w, "broker closed", http.StatusServiceUnavailable)
		return
	}
	defer broker.Unregister(clientID)

	// Send initial status
	st := sm.Status()
	broker.SendScanStatus(st.Running, st.Percent, st.Found, st.Total, st.Error)

	// Stream messages until client disconnects
	ctx := req.Context()
	ticker := time.NewTicker(30 * time.Second) // Keep-alive ping
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			return
		case <-client.Done:
			// Broker closed client
			return
		case msg, ok := <-client.Messages:
			if !ok {
				// Channel closed
				return
			}
			if _, err := w.Write(msg); err != nil {
				log.Printf("[SSE] write error for client %s: %v", clientID, err)
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		case <-ticker.C:
			// Send keep-alive comment
			if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
				log.Printf("[SSE] keep-alive error for client %s: %v", clientID, err)
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

// findAggregatorByURL attempts to find an aggregator by matching the job URL
// hostname with aggregator SourceURL patterns. Returns the first matching aggregator
// or nil if no match is found.
func findAggregatorByURL(jobURL string, aggregators []models.Aggregator) *models.Aggregator {
	parsedURL, err := url.Parse(jobURL)
	if err != nil {
		return nil
	}

	jobHostname := strings.ToLower(parsedURL.Hostname())
	if jobHostname == "" {
		return nil
	}

	// Remove common prefixes for better matching
	jobHostname = strings.TrimPrefix(jobHostname, "www.")
	jobHostname = strings.TrimPrefix(jobHostname, "jobs.")

	// Check each aggregator's SourceURL for a hostname match
	for i := range aggregators {
		aggURL, err := url.Parse(aggregators[i].SourceURL)
		if err != nil {
			continue
		}

		aggHostname := strings.ToLower(aggURL.Hostname())
		aggHostname = strings.TrimPrefix(aggHostname, "www.")
		aggHostname = strings.TrimPrefix(aggHostname, "jobs.")

		// Match if hostnames are equal (exact match after trimming prefixes)
		// This handles cases like "jobs.secrettelaviv.com" matching "jobs.secrettelaviv.com"
		// or "secrettelaviv.com" matching "jobs.secrettelaviv.com"
		if jobHostname == aggHostname {
			return &aggregators[i]
		}
	}

	return nil
}

// listCVFiles scans the specified directory for PDF files and returns them with their paths.
func listCVFiles(dir string) ([]map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %q: %w", dir, err)
	}

	var cvs []map[string]string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Check if file has .pdf extension (case-insensitive)
		if !strings.EqualFold(filepath.Ext(name), ".pdf") {
			continue
		}
		// Use relative path from project root
		path := filepath.Join(dir, name)
		// Normalize path separators to forward slashes for consistency
		path = strings.ReplaceAll(path, "\\", "/")
		cvs = append(cvs, map[string]string{
			"filename": name,
			"path":     path,
		})
	}
	return cvs, nil
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if len(guiContent) == 0 {
		http.Error(w, "missing UI", http.StatusInternalServerError)
		return
	}

	// Set caching headers
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600, stale-while-revalidate=86400")
	w.Header().Set("Vary", "Accept-Encoding")

	// ETag support for conditional requests
	if guiETag != "" {
		w.Header().Set("ETag", guiETag)
		if r.Header.Get("If-None-Match") == guiETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(guiContent)
}
