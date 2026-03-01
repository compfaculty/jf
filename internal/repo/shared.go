package repo

import (
	"context"
	"database/sql"
	"fmt"
	"jf/internal/models"
	"jf/internal/utils"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Repo is the storage contract your app depends on.
// Both SQLiteRepo and DuckRepo must implement this.
type Repo interface {
	Close() error

	// Companies
	UpsertCompany(ctx context.Context, c *models.Company) error
	UpsertCompanyByName(ctx context.Context, c *models.Company) error
	ListCompanies(ctx context.Context) ([]models.Company, error)

	// Jobs
	UpsertJob(ctx context.Context, j *models.Job) error
	ApplyJobs(ctx context.Context, ids []string) (int64, error)
	ListJobsPage(ctx context.Context, q models.JobQuery) ([]models.Job, int, error)
	ListJobs(ctx context.Context, q models.JobQuery) ([]models.Job, error)
	DeleteJobs(ctx context.Context, ids []string) (int64, error)
	ListJobsByIDs(ctx context.Context, ids []string) ([]models.Job, error)
	JobURLExists(ctx context.Context, url string) (bool, error)

	// Rate limit queue (429 retry)
	EnqueueRateLimited(ctx context.Context, jobID, url string, retryAfter time.Time) error
	ListRateLimitedReady(ctx context.Context) ([]models.RateLimitedEntry, error)
	DequeueRateLimited(ctx context.Context, jobID string) error
	UpdateRateLimitedRetry(ctx context.Context, jobID string, retryAfter time.Time) error
}

// SeedCompanies loads the embedded list and upserts by name (engine-agnostic).
// This seeds pure companies (not job boards/aggregators).
func SeedCompanies(r Repo) error {
	seen := make(map[string]struct{}, len(embeddedCompanies))
	ctx := context.Background()
	added, skipped := 0, 0

	for _, e := range embeddedCompanies {
		name := strings.TrimSpace(e.Name)
		url := strings.TrimSpace(e.URL)
		email := strings.TrimSpace(e.Email)
		if name == "" || url == "" {
			skipped++
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		c := models.Company{
			Name:       name,
			CareersURL: url,
			ApplyEmail: email,
			Active:     true,
		}
		if err := r.UpsertCompanyByName(ctx, &c); err != nil {
			return err
		}
		added++
	}

	// optional: if your package has a logger, you can log here.
	// log.Printf("[DB] SeedCompanies loaded=%d skipped=%d total_in_list=%d", added, skipped, len(embeddedCompanies))
	_ = added
	_ = skipped
	return nil
}

func rowsAffected(res sql.Result) int64 {
	if res == nil {
		return 0
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0
	}
	return n
}

func minifySQL(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func previewArgs(args []any) string {
	if len(args) == 0 {
		return "[]"
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		s := fmt.Sprintf("%v", a)
		if len(s) > 256 {
			s = s[:256] + "…"
		}
		out = append(out, s)
	}
	return "[" + strings.Join(out, ", ") + "]"
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func anySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func canonicalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	uu, err := url.Parse(u)
	if err != nil {
		return strings.ToLower(u)
	}
	uu.Fragment = ""

	q := uu.Query()
	for k := range q {
		if strings.HasPrefix(strings.ToLower(k), "utm_") {
			q.Del(k)
		}
	}
	uu.RawQuery = q.Encode()
	uu.Host = strings.ToLower(uu.Host)
	if strings.HasSuffix(uu.Path, "/") {
		uu.Path = strings.TrimRight(uu.Path, "/")
	}
	return uu.String()
}

// Common repository initialization helpers

// getDebugFlag reads the debug flag from environment variable and verbose state.
func getDebugFlag() bool {
	v := os.Getenv("JF_DB_DEBUG")
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "yes" || v == "on" || utils.IsVerbose()
}

// ensureParentDir creates the parent directory for a database path if needed.
func ensureParentDir(path string) {
	if dir := filepath.Dir(path); dir != "" && dir != "." && dir != "/" {
		_ = os.MkdirAll(dir, 0o755)
	}
}

// pingDB performs a health check on the database connection.
func pingDB(ctx context.Context, db *sql.DB) error {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.PingContext(pingCtx)
}

// repoLogger provides common logging methods for repository implementations.
type repoLogger struct {
	log   *log.Logger
	debug bool
}

// infof logs an info message.
func (rl *repoLogger) infof(format string, args ...any) {
	if rl.log != nil {
		rl.log.Printf("[DB] "+format, args...)
	}
}

// debugf logs a debug message if debug mode is enabled.
func (rl *repoLogger) debugf(format string, args ...any) {
	if rl.debug && rl.log != nil {
		rl.log.Printf("[DB][debug] "+format, args...)
	}
}
