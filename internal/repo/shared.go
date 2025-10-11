package repo

import (
	"database/sql"
	"fmt"
	"jf/internal/models"
	"net/url"
	"strings"

	"context"
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
}

// SeedCompanies loads the embedded list and upserts by name (engine-agnostic).
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
