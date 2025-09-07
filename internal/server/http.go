package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"jf/internal/models"
	"jf/internal/repo"
	"jf/internal/scanner"
)

func NewRouter(r *repo.SQLiteRepo, sm *scanner.Manager) http.Handler {
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
	mux.Get("/api/jobs", func(w http.ResponseWriter, req *http.Request) {
		q := models.JobQuery{
			CompanyID: req.URL.Query().Get("company_id"),
			Q:         strings.TrimSpace(req.URL.Query().Get("q")),
			Limit:     atoi(req.URL.Query().Get("limit")),
			Offset:    atoi(req.URL.Query().Get("offset")),
		}
		list, err := r.ListJobs(req.Context(), q)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.Post("/api/apply", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		n, err := r.ApplyJobs(req.Context(), body.IDs)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated": n})
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

//go:embed gui.html
var content embed.FS

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
