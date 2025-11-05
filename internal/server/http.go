package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
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

func NewRouter(r repo.Repo, sm *scanner.Scanner, aggregatorReg *aggregators.Registry, fm *feed.Monitor, cfg *config.Config, wp *pond.WorkerPool, broker *Broker) http.Handler {
	mux := chi.NewRouter()
	mux.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

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

		q := models.JobQuery{
			CompanyID: strings.TrimSpace(req.URL.Query().Get("company_id")),
			Q:         strings.TrimSpace(req.URL.Query().Get("q")),
			Limit:     limit,
			Offset:    offset,
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
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		log.Printf("[APPLY] request ids=%v", body.IDs)

		if len(body.IDs) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"updated": 0})
			return
		}

		jobs, err := r.ListJobsByIDs(req.Context(), body.IDs)
		if err != nil {
			log.Printf("[APPLY] db ListJobsByIDs error: %v", err)
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		log.Printf("[APPLY] loaded %d jobs", len(jobs))
		if len(jobs) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"updated": 0})
			return
		}

		okIDs := make([]string, 0, len(jobs))
		fail := 0

		// Per-request group bound to the client's context (deadline/cancel-friendly)
		group, gctx := wp.GroupContext(req.Context())

		// Load companies and aggregators for source lookup
		companies, err := r.ListCompanies(req.Context())
		if err != nil {
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

		// Create feed parser for RSS sources (use default HTTP client)
		feedParser := feed.NewParser(http.DefaultClient)
		var bp commonpkg.Browser // nil - browser pool not available in router, fallback will work

		var mu sync.Mutex
		for i := range jobs {
			j := jobs[i] // capture
			group.Submit(func() error {
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
				}

				log.Printf("[APPLY] start job id=%s url=%s title=%q", j.ID, j.URL, j.Title)

				ok := false
				errMsg := ""

				// Determine source type and create appropriate JobSource
				var source scrape.JobSource
				if j.AggregatorName != "" {
					// Job is from an aggregator (board or RSS)
					if agg, exists := aggregatorMap[j.AggregatorName]; exists {
						// Get or create company for aggregator
						company := models.Company{
							Name:       agg.Name,
							CareersURL: agg.SourceURL,
							Active:     agg.Active,
						}
						if err := r.UpsertCompanyByName(gctx, &company); err == nil {
							source = scrape.NewJobSource(company, &agg, http.DefaultClient, bp, wp, feedParser, r)
						}
					}
				} else if j.CompanyID != "" {
					// Job is from a direct company
					if company, exists := companyMap[j.CompanyID]; exists {
						source = scrape.NewJobSource(company, nil, http.DefaultClient, bp, wp, feedParser, r)
					}
				}

				// Fallback: if source not found, try to match by job URL pattern
				if source == nil && j.URL != "" {
					if agg := findAggregatorByURL(j.URL, aggregators); agg != nil {
						// Get or create company for aggregator
						company := models.Company{
							Name:       agg.Name,
							CareersURL: agg.SourceURL,
							Active:     agg.Active,
						}
						if err := r.UpsertCompanyByName(gctx, &company); err == nil {
							source = scrape.NewJobSource(company, agg, http.DefaultClient, bp, wp, feedParser, r)
							log.Printf("[APPLY] id=%s found source via URL fallback: %s", j.ID, agg.Name)
						}
					}
				}

				// If we have HR email on the job, prefer emailing CV directly
				if strings.TrimSpace(j.HREmail) != "" {
					ok, errMsg = applyViaEmail(gctx, j, cfg)
					if ok {
						log.Printf("[APPLY][EMAIL] id=%s to=%s ok", j.ID, j.HREmail)
					} else {
						log.Printf("[APPLY][EMAIL] id=%s err=%s", j.ID, errMsg)
					}
				} else if source != nil {
					// Use JobSource.ApplyJob method
					result, err := source.ApplyJob(gctx, j, cfg)
					if err != nil {
						errMsg = "ApplyJob error: " + err.Error()
						log.Printf("[APPLY] id=%s err=%s", j.ID, errMsg)
					} else if result != nil {
						ok = result.OK
						if !ok && result.Message != "" {
							errMsg = result.Message
						}
						log.Printf("[APPLY] id=%s ok=%v status=%d msg=%q", j.ID, result.OK, result.Status, result.Message)
					} else {
						// ApplyJob returned nil - not supported (graceful degradation)
						errMsg = "apply not supported for this source"
						log.Printf("[APPLY] id=%s unsupported", j.ID)
					}
				} else {
					errMsg = "could not determine source for job"
					log.Printf("[APPLY] id=%s err=%s", j.ID, errMsg)
				}

				// Collect results
				mu.Lock()
				if ok {
					okIDs = append(okIDs, j.ID)
				} else {
					fail++
					if errMsg != "" {
						log.Printf("[APPLY] failed id=%s url=%s err=%s", j.ID, j.URL, errMsg)
					} else {
						log.Printf("[APPLY] failed id=%s url=%s err=unknown", j.ID, j.URL)
					}
				}
				mu.Unlock()
				return nil
			})
		}

		// Wait for all tasks or client cancel/timeout
		_ = group.Wait()

		var updated int64
		if len(okIDs) > 0 {
			n, err := r.ApplyJobs(req.Context(), okIDs)
			if err != nil {
				log.Printf("[APPLY] db ApplyJobs error: %v", err)
			} else {
				updated = n
			}
		}

		log.Printf("[APPLY] done attempted=%d success=%d fail=%d updated=%d",
			len(jobs), len(okIDs), fail, updated)

		writeJSON(w, http.StatusOK, map[string]any{"updated": updated})
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
func applyViaEmail(ctx context.Context, job models.Job, cfg *config.Config) (bool, string) {
	mailer := emailx.BuildSMTPMailer(&cfg.Mail)
	applicant := emailx.Applicant{
		FullName: strings.TrimSpace(cfg.ApplyForm.FirstName + " " + cfg.ApplyForm.LastName),
		Email:    cfg.ApplyForm.Email,
		Phone:    cfg.ApplyForm.Phone,
	}

	subj := "Application: " + job.Title + " — " + applicant.FullName
	cvPath, _ := emailx.ChooseResume(job.Title, &cfg.Mail)

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

func serveIndex(w http.ResponseWriter, r *http.Request) {
	_, err := content.Open("gui.html")
	if err != nil {
		http.Error(w, "missing UI", http.StatusInternalServerError)
		return
	}
	statics, _ := fs.ReadFile(content, "gui.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(statics)
}
