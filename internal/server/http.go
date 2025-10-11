package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alitto/pond"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/repo"
	"jf/internal/scanner"
	"jf/internal/scrape"
	"jf/internal/utils"
)

//go:embed gui.html
var content embed.FS

func NewRouter(r repo.Repo, sm *scanner.Scanner, cfg *config.Config, wp *pond.WorkerPool) http.Handler {
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

	// Metrics endpoint
	mux.Get("/api/metrics", func(w http.ResponseWriter, _ *http.Request) {
		metrics := utils.GetMetricsSnapshot()
		writeJSON(w, http.StatusOK, metrics)
	})

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

		var mu sync.Mutex
		for i := range jobs {
			j := jobs[i] // capture
			group.Submit(func() error {
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
				}

				host := ""
				if u, err := url.Parse(strings.TrimSpace(j.URL)); err == nil && u != nil {
					host = strings.ToLower(u.Host)
				}
				log.Printf("[APPLY] start job id=%s host=%s url=%s title=%q", j.ID, host, j.URL, j.Title)

				ok := false
				errMsg := ""

				switch {
				case strings.Contains(host, "jobs.secrettelaviv.com"):
					sta := scrape.NewSecretTelAviv(models.Company{Name: "Secret Tel Aviv"}, http.DefaultClient)
					rr, err := sta.ApplyJobs(req.Context(), []models.Job{j}, cfg)
					if err != nil {
						errMsg = "ApplyJobs error: " + err.Error()
						log.Printf("[APPLY][STA] id=%s err=%s", j.ID, errMsg)
					} else if len(rr) > 0 {
						ok = rr[0].OK
						if !ok && rr[0].Message != "" {
							errMsg = rr[0].Message
						}
						log.Printf("[APPLY][STA] id=%s ok=%v status=%d msg=%q", j.ID, rr[0].OK, rr[0].Status, rr[0].Message)
					} else {
						errMsg = "ApplyJobs returned empty results"
						log.Printf("[APPLY][STA] id=%s err=%s", j.ID, errMsg)
					}
				default:
					errMsg = "apply not supported for host"
					log.Printf("[APPLY] id=%s host=%s unsupported", j.ID, host)
				}

				// Collect results
				mu.Lock()
				if ok {
					okIDs = append(okIDs, j.ID)
				} else {
					fail++
					if errMsg != "" {
						log.Printf("[APPLY] failed id=%s host=%s url=%s err=%s", j.ID, host, j.URL, errMsg)
					} else {
						log.Printf("[APPLY] failed id=%s host=%s url=%s err=%s", j.ID, host, j.URL, "unknown")
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
