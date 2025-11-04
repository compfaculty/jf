package repo

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"jf/internal/models"
	"jf/internal/utils"
)

var _ Repo = (*SQLiteRepo)(nil)

type SQLiteRepo struct {
	db    *sql.DB
	log   *log.Logger
	debug bool
}

// NewSQLite opens the DB with preferred journal mode (default WAL) and falls
// back to DELETE if needed. No migrations are performed—schema is created fresh.
func NewSQLite(path string) (*SQLiteRepo, error) {
	logger := log.Default()
	var v string = os.Getenv("JF_DB_DEBUG")
	v = strings.ToLower(strings.TrimSpace(v))
	// Check both environment variable and global verbose flag
	debug := v == "1" || v == "true" || v == "yes" || v == "on" || utils.IsVerbose()

	// ensure parent dir exists (useful if path points into a mount)
	if dir := filepath.Dir(path); dir != "" && dir != "." && dir != "/" {
		_ = os.MkdirAll(dir, 0o755)
	}

	preferred := journalFromEnv() // WAL by default; override via JF_SQLITE_JOURNAL
	db, usedMode, err := openSQLiteWithFallback(path, preferred)
	if err != nil {
		return nil, err
	}

	r := &SQLiteRepo{db: db, log: logger, debug: debug}
	r.infof("DB open path=%q driver=modernc.org/sqlite journal=%s debug=%v", path, usedMode, debug)

	// health check
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}
	r.infof("DB ping ok")

	if err := r.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	r.infof("DB schema ensured")
	return r, nil
}

func (r *SQLiteRepo) Close() error {
	r.infof("DB close")
	return r.db.Close()
}

// ----------------------------------------------------------------------------
// Configuration & open helpers
// ----------------------------------------------------------------------------

func journalFromEnv() string {
	s := strings.ToUpper(strings.TrimSpace(os.Getenv("JF_SQLITE_JOURNAL")))
	switch s {
	case "WAL", "DELETE", "TRUNCATE", "PERSIST", "MEMORY", "OFF":
		return s
	}
	return "WAL"
}

func openSQLiteWithFallback(path, preferred string) (*sql.DB, string, error) {
	modes := []string{preferred}
	// Always try a safe fallback if preferred isn't DELETE
	if strings.ToUpper(preferred) != "DELETE" {
		modes = append(modes, "DELETE")
	}

	var lastErr error
	for _, m := range modes {
		db, err := openSQLiteWithMode(path, m)
		if err != nil {
			lastErr = err
			continue
		}
		// quick probe
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pingErr := db.PingContext(ctx)
		cancel()
		if pingErr == nil {
			return db, m, nil
		}
		_ = db.Close()
		lastErr = pingErr
	}
	return nil, "", fmt.Errorf("sqlite open failed (modes=%v): %w", modes, lastErr)
}

func openSQLiteWithMode(path, journal string) (*sql.DB, error) {
	// busy_timeout + FKs + chosen journal + sane durability
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=journal_mode(%s)&_pragma=synchronous(NORMAL)",
		path, journal,
	)
	return sql.Open("sqlite", dsn)
}

// ----------------------------------------------------------------------------
// Schema (fresh DB, no migrations/backfills)
// ----------------------------------------------------------------------------

func (r *SQLiteRepo) migrate() error {
	// Base schema for a fresh DB (no migrations/backfills).
	//goland:noinspection SqlResolve,SqlNoDataSourceInspection,SqlDialectInspection
	const schema = `
CREATE TABLE IF NOT EXISTS companies(
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  careers_url TEXT NOT NULL,
  active      INTEGER NOT NULL DEFAULT 1,
  apply_email TEXT NOT NULL DEFAULT '', 
  created_at  TIMESTAMP NOT NULL,
  updated_at  TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS companies_name_uq ON companies(name);

CREATE TABLE IF NOT EXISTS jobs(
  id             TEXT PRIMARY KEY,
  company_id     TEXT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
  aggregator_name TEXT NOT NULL DEFAULT '',
  title          TEXT NOT NULL,
  url            TEXT NOT NULL,
  apply_url      TEXT NOT NULL DEFAULT '',
  apply_via_portal INTEGER NOT NULL DEFAULT 0,
  canonical_url  TEXT NOT NULL DEFAULT '',
  location       TEXT,
  description    TEXT,
  hr_email       TEXT NOT NULL DEFAULT '',
  hr_phone       TEXT NOT NULL DEFAULT '',
  discovered_at  TIMESTAMP NOT NULL,
  posted_at      TEXT,
  applied        INTEGER NOT NULL DEFAULT 0,
  applied_at     TIMESTAMP
);

/* One canonicalized posting per company */
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_company_canon_uq
  ON jobs(company_id, canonical_url);

/* Helpful read performance indices */
CREATE INDEX IF NOT EXISTS idx_jobs_discovered_at ON jobs(discovered_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_company_id    ON jobs(company_id);
`
	start := time.Now()
	if _, err := r.exec(context.Background(), schema); err != nil {
		return err
	}
	r.debugf("migrate took %s", time.Since(start))
	return nil
}

// ----------------------------------------------------------------------------
// Companies
// ----------------------------------------------------------------------------

func (r *SQLiteRepo) UpsertCompany(ctx context.Context, c *models.Company) error {
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now

	//goland:noinspection SqlResolve
	q := `
INSERT INTO companies(id,name,careers_url,active,created_at,updated_at)
VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name,
  careers_url=excluded.careers_url,
  active=excluded.active,
  updated_at=excluded.updated_at
`
	start := time.Now()
	res, err := r.exec(ctx, q, c.ID, c.Name, c.CareersURL, boolToInt(c.Active), c.CreatedAt, c.UpdatedAt)
	dur := time.Since(start)
	if err != nil {
		r.infof("UpsertCompany err company=%q id=%s dur=%s err=%v", c.Name, c.ID, dur, err)
		return err
	}
	ra := rowsAffected(res)
	r.debugf("UpsertCompany ok company=%q id=%s rows=%d dur=%s", c.Name, c.ID, ra, dur)
	return nil
}

// UpsertCompanyByName upserts using unique(name) as the conflict target.
func (r *SQLiteRepo) UpsertCompanyByName(ctx context.Context, c *models.Company) error {
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now

	//goland:noinspection SqlResolve
	q := `
INSERT INTO companies(id,name,careers_url,active,created_at,updated_at)
VALUES(?,?,?,?,?,?)
ON CONFLICT(name) DO UPDATE SET
  careers_url=excluded.careers_url,
  active=excluded.active,
  updated_at=excluded.updated_at
`
	start := time.Now()
	res, err := r.exec(ctx, q, c.ID, c.Name, c.CareersURL, boolToInt(c.Active), c.CreatedAt, c.UpdatedAt)
	dur := time.Since(start)
	if err != nil {
		r.infof("UpsertCompanyByName err name=%q dur=%s err=%v", c.Name, dur, err)
		return err
	}
	r.debugf("UpsertCompanyByName ok name=%q rows=%d dur=%s", c.Name, rowsAffected(res), dur)

	// Always fetch the ID to ensure we have the correct one after upsert
	idQ := `SELECT id FROM companies WHERE name = ?`
	if err := r.db.QueryRowContext(ctx, idQ, c.Name).Scan(&c.ID); err != nil {
		r.infof("UpsertCompanyByName failed to fetch ID for name=%q err=%v", c.Name, err)
		return err
	}
	return nil
}

func (r *SQLiteRepo) ListCompanies(ctx context.Context) ([]models.Company, error) {
	//goland:noinspection SqlResolve
	const q = `SELECT id,name,careers_url,active,created_at,updated_at FROM companies WHERE active=1 ORDER BY name`
	start := time.Now()
	rows, err := r.db.QueryContext(ctx, q)
	dur := time.Since(start)
	if err != nil {
		r.infof("SQL query err dur=%s sql=%q err=%v", dur, minifySQL(q), err)
		r.infof("ListCompanies err err=%v", err)
		return nil, err
	}
	r.debugf("SQL query ok  dur=%s sql=%q", dur, minifySQL(q))
	defer rows.Close()
	var out []models.Company
	var n int
	for rows.Next() {
		var c models.Company
		var activeInt int
		if err := rows.Scan(&c.ID, &c.Name, &c.CareersURL, &activeInt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.Active = activeInt == 1
		out = append(out, c)
		n++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	r.debugf("ListCompanies ok count=%d dur=%s", n, time.Since(start))
	return out, nil
}

// ----------------------------------------------------------------------------
// Jobs
// ----------------------------------------------------------------------------

func (r *SQLiteRepo) UpsertJob(ctx context.Context, j *models.Job) error {
	if j.ID == "" {
		j.ID = uuid.NewString()
	}
	if j.DiscoveredAt.IsZero() {
		j.DiscoveredAt = time.Now().UTC()
	}

	// compute canonical URL for uniqueness
	canon := canonicalizeURL(j.URL)

	//goland:noinspection SqlResolve
	q := `
INSERT INTO jobs(id,company_id,aggregator_name,title,url,apply_url,apply_via_portal,canonical_url,location,description,hr_email,hr_phone,discovered_at,posted_at,applied,applied_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(company_id, canonical_url) DO UPDATE SET
  title=excluded.title,
  url=excluded.url,
  aggregator_name=excluded.aggregator_name,
  apply_url=excluded.apply_url,
  apply_via_portal=excluded.apply_via_portal,
  location=excluded.location,
  description=excluded.description,
  hr_email=excluded.hr_email,
  hr_phone=excluded.hr_phone,
  posted_at=excluded.posted_at
`
	start := time.Now()
	res, err := r.exec(ctx, q,
		j.ID, j.CompanyID, j.AggregatorName, j.Title, j.URL, j.ApplyURL, boolToInt(j.ApplyViaPortal), canon, j.Location, j.Description, j.HREmail, j.HRPhone, j.DiscoveredAt, j.PostedAt, boolToInt(j.Applied), j.AppliedAt)
	dur := time.Since(start)
	if err != nil {
		r.infof("UpsertJob err url=%q canon=%q company_id=%s dur=%s err=%v", j.URL, canon, j.CompanyID, dur, err)
		return err
	}
	ra := rowsAffected(res)
	r.debugf("UpsertJob ok url=%q canon=%q company_id=%s rows=%d dur=%s", j.URL, canon, j.CompanyID, ra, dur)
	return nil
}

//goland:noinspection SqlResolve,SqlNoDataSourceInspection,SqlDialectInspection
func (r *SQLiteRepo) ApplyJobs(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		r.debugf("ApplyJobs no-op (empty ids)")
		return 0, nil
	}
	now := time.Now().UTC()
	in := make([]any, 0, len(ids)+1)
	sb := &strings.Builder{}
	sb.WriteString(`UPDATE jobs SET applied=1, applied_at=? WHERE id IN (`)
	in = append(in, now)
	for i, id := range ids {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("?")
		in = append(in, id)
	}
	sb.WriteString(")")

	start := time.Now()
	res, err := r.exec(ctx, sb.String(), in...)
	dur := time.Since(start)
	if err != nil {
		r.infof("ApplyJobs err count=%d dur=%s err=%v", len(ids), dur, err)
		return 0, err
	}
	n := rowsAffected(res)
	r.debugf("ApplyJobs ok updated=%d requested=%d dur=%s", n, len(ids), dur)
	return n, nil
}

// ListJobsPage returns jobs for the query and the total count for pagination.
//
//goland:noinspection SqlResolve,SqlNoDataSourceInspection,SqlDialectInspection
func (r *SQLiteRepo) ListJobsPage(ctx context.Context, q models.JobQuery) ([]models.Job, int, error) {
	// Defaults & bounds
	limit := q.Limit
	offset := q.Offset
	if limit <= 0 || limit > 200 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}

	// WHERE builder
	where := make([]string, 0, 4)
	args := make([]any, 0, 8)

	if q.CompanyID != "" {
		where = append(where, "j.company_id = ?")
		args = append(args, q.CompanyID)
	}

	if q.Q != "" {
		like := "%" + q.Q + "%"
		where = append(where, "(j.title LIKE ? OR j.description LIKE ? OR j.location LIKE ? OR c.name LIKE ?)")
		args = append(args, like, like, like, like)
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	// COUNT(*) for total (join to respect same filters)
	countSQL := fmt.Sprintf(`
SELECT COUNT(*)
FROM jobs j
JOIN companies c ON c.id = j.company_id
%s`, whereSQL)

	var total int
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Page query with company name and aggregator name
	pageSQL := fmt.Sprintf(`
SELECT j.id,
       j.company_id,
       c.name as company_name,
       j.aggregator_name,
       j.title,
       j.url,
       j.apply_url,
       j.apply_via_portal,
       j.location,
       j.description,
       j.hr_email,
       j.hr_phone,
       j.discovered_at,
       j.posted_at,
       j.applied,
       j.applied_at
FROM jobs j
JOIN companies c ON c.id = j.company_id
%s
ORDER BY j.discovered_at DESC
LIMIT ? OFFSET ?`, whereSQL)

	pageArgs := append(append([]any{}, args...), limit, offset)
	rows, err := r.db.QueryContext(ctx, pageSQL, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]models.Job, 0, limit)
	for rows.Next() {
		var j models.Job
		var appliedInt int
		var applyViaPortalInt int
		var appliedAt sql.NullTime
		var postedAt sql.NullString
		var aggregatorName string
		if err = rows.Scan(
			&j.ID, &j.CompanyID, &j.CompanyName, &aggregatorName, &j.Title, &j.URL, &j.ApplyURL, &applyViaPortalInt,
			&j.Location, &j.Description, &j.HREmail, &j.HRPhone,
			&j.DiscoveredAt, &postedAt, &appliedInt, &appliedAt,
		); err != nil {
			return nil, 0, err
		}
		j.Applied = appliedInt == 1
		j.ApplyViaPortal = applyViaPortalInt == 1
		j.AggregatorName = aggregatorName
		if postedAt.Valid {
			j.PostedAt = postedAt.String
		}
		if appliedAt.Valid {
			t := appliedAt.Time
			j.AppliedAt = &t
		}
		out = append(out, j)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}

	return out, total, nil
}

// ListJobs (Optional) Keep the old method for backward compatibility.
func (r *SQLiteRepo) ListJobs(ctx context.Context, q models.JobQuery) ([]models.Job, error) {
	items, _, err := r.ListJobsPage(ctx, q)
	return items, err
}

// DeleteJobs hard-deletes jobs by IDs. Returns number of rows deleted.
func (r *SQLiteRepo) DeleteJobs(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma

	//goland:noinspection SqlResolve,Annotator
	q := fmt.Sprintf(`DELETE FROM jobs WHERE id IN (%s)`, placeholders)

	res, err := r.db.ExecContext(ctx, q, anySlice(ids)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListJobsByIDs returns a minimal view of jobs for given IDs.
// We only need ID, Title, URL, hr_email, apply_url, and apply_via_portal for applying.
func (r *SQLiteRepo) ListJobsByIDs(ctx context.Context, ids []string) ([]models.Job, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := strings.Repeat("?,", len(ids))
	ph = ph[:len(ph)-1]
	q := fmt.Sprintf(`SELECT id, title, url, hr_email, hr_phone, apply_url, apply_via_portal FROM jobs WHERE id IN (%s)`, ph)

	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Job, 0, len(ids))
	for rows.Next() {
		var j models.Job
		var applyViaPortalInt int
		if err := rows.Scan(&j.ID, &j.Title, &j.URL, &j.HREmail, &j.HRPhone, &j.ApplyURL, &applyViaPortalInt); err != nil {
			return nil, err
		}
		j.ApplyViaPortal = applyViaPortalInt == 1
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SeedCompanies loads the embedded list and upserts by name.
//func SeedCompanies(r *SQLiteRepo) error {
//	seen := make(map[string]struct{}, len(embeddedCompanies))
//	ctx := context.Background()
//	added, skipped := 0, 0
//
//	for _, e := range embeddedCompanies {
//		name := strings.TrimSpace(e.Name)
//		url := strings.TrimSpace(e.URL)
//		email := strings.TrimSpace(e.Email)
//		if name == "" || url == "" {
//			skipped++
//			continue
//		}
//		if _, ok := seen[name]; ok {
//			continue
//		}
//		seen[name] = struct{}{}
//
//		c := models.Company{
//			Name:       name,
//			CareersURL: url,
//			ApplyEmail: email,
//			Active:     true,
//		}
//		if err := r.UpsertCompanyByName(ctx, &c); err != nil {
//			return err
//		}
//		added++
//	}
//
//	r.infof("SeedCompanies loaded=%d skipped=%d total_in_list=%d", added, skipped, len(embeddedCompanies))
//	return nil
//}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func (r *SQLiteRepo) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	start := time.Now()
	res, err := r.db.ExecContext(ctx, query, args...)
	dur := time.Since(start)
	if err != nil {
		r.infof("SQL exec err dur=%s sql=%q args=%s err=%v", dur, minifySQL(query), previewArgs(args), err)
		return nil, err
	}
	r.debugf("SQL exec ok  dur=%s sql=%q args=%s", dur, minifySQL(query), previewArgs(args))
	return res, nil
}

func (r *SQLiteRepo) infof(format string, args ...any) {
	if r.log != nil {
		r.log.Printf("[DB] "+format, args...)
	}
}

func (r *SQLiteRepo) debugf(format string, args ...any) {
	if r.debug && r.log != nil {
		r.log.Printf("[DB][debug] "+format, args...)
	}
}
