package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"jf/internal/models"
)

type SQLiteRepo struct {
	db    *sql.DB
	log   *log.Logger
	debug bool
}

// NewSQLite opens the DB, falling back to DELETE journal if WAL doesn't work
func NewSQLite(path string) (*SQLiteRepo, error) {
	logger := log.Default()
	debug := isTruthy(os.Getenv("JF_DB_DEBUG"))

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

	// health check (already done in openSQLiteWithFallback for the chosen mode, but keep a full timeout)
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
	r.infof("DB migrate ok")
	return r, nil
}

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

func (r *SQLiteRepo) Close() error {
	r.infof("DB close")
	return r.db.Close()
}

func (r *SQLiteRepo) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS companies(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  careers_url TEXT NOT NULL,
  active INTEGER NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS companies_name_uq ON companies(name);

CREATE TABLE IF NOT EXISTS jobs(
  id TEXT PRIMARY KEY,
  company_id TEXT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  url TEXT NOT NULL,
  location TEXT,
  description TEXT,
  discovered_at TIMESTAMP NOT NULL,
  applied INTEGER NOT NULL DEFAULT 0,
  applied_at TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS jobs_url_uq ON jobs(url);
`
	start := time.Now()
	_, err := r.exec(context.Background(), schema)
	r.debugf("migrate took %s", time.Since(start))
	return err
}

// --- Companies ---

// UpsertCompany keeps conflict target on id (useful if you already know IDs).
func (r *SQLiteRepo) UpsertCompany(ctx context.Context, c *models.Company) error {
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now

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
	return nil
}

func (r *SQLiteRepo) ListCompanies(ctx context.Context) ([]models.Company, error) {
	q := `SELECT id,name,careers_url,active,created_at,updated_at FROM companies WHERE active=1 ORDER BY name`
	start := time.Now()
	rows, err := r.query(ctx, q)
	if err != nil {
		r.infof("ListCompanies err err=%v", err)
		return nil, err
	}
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

// --- Jobs ---

func (r *SQLiteRepo) UpsertJob(ctx context.Context, j *models.Job) error {
	if j.ID == "" {
		j.ID = uuid.NewString()
	}
	if j.DiscoveredAt.IsZero() {
		j.DiscoveredAt = time.Now().UTC()
	}
	q := `
INSERT INTO jobs(id,company_id,title,url,location,description,discovered_at,applied,applied_at)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(url) DO UPDATE SET
  title=excluded.title,
  location=excluded.location,
  description=excluded.description
`
	start := time.Now()
	res, err := r.exec(ctx, q,
		j.ID, j.CompanyID, j.Title, j.URL, j.Location, j.Description, j.DiscoveredAt, boolToInt(j.Applied), j.AppliedAt)
	dur := time.Since(start)
	if err != nil {
		r.infof("UpsertJob err url=%q company_id=%s dur=%s err=%v", j.URL, j.CompanyID, dur, err)
		return err
	}
	ra := rowsAffected(res)
	r.debugf("UpsertJob ok url=%q company_id=%s rows=%d dur=%s", j.URL, j.CompanyID, ra, dur)
	return nil
}

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

func (r *SQLiteRepo) ListJobs(ctx context.Context, q models.JobQuery) ([]models.Job, error) {
	where := []string{"1=1"}
	args := []any{}
	if q.CompanyID != "" {
		where = append(where, "company_id=?")
		args = append(args, q.CompanyID)
	}
	if q.Q != "" {
		where = append(where, "(title LIKE ? OR description LIKE ?)")
		args = append(args, "%"+q.Q+"%", "%"+q.Q+"%")
	}
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	query := fmt.Sprintf(`
SELECT id,company_id,title,url,location,description,discovered_at,applied,applied_at
FROM jobs
WHERE %s
ORDER BY discovered_at DESC
LIMIT ? OFFSET ?`, strings.Join(where, " AND "))
	args = append(args, limit, offset)

	start := time.Now()
	rows, err := r.query(ctx, query, args...)
	if err != nil {
		r.infof("ListJobs err q=%q company_id=%s err=%v", q.Q, q.CompanyID, err)
		return nil, err
	}
	defer rows.Close()

	var out []models.Job
	var n int
	for rows.Next() {
		var j models.Job
		var appliedInt int
		var appliedAt sql.NullTime
		if err := rows.Scan(&j.ID, &j.CompanyID, &j.Title, &j.URL, &j.Location, &j.Description, &j.DiscoveredAt, &appliedInt, &appliedAt); err != nil {
			return nil, err
		}
		j.Applied = appliedInt == 1
		if appliedAt.Valid {
			t := appliedAt.Time
			j.AppliedAt = &t
		}
		out = append(out, j)
		n++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	r.debugf("ListJobs ok count=%d dur=%s params={company_id=%s q=%q limit=%d offset=%d}", n, time.Since(start), q.CompanyID, q.Q, limit, offset)
	return out, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SeedCompanies loads the embedded JSON list and upserts by name.
func SeedCompanies(r *SQLiteRepo) error {
	type entry struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}

	var list []entry
	if err := json.Unmarshal([]byte(embeddedCompaniesJSON), &list); err != nil {
		return fmt.Errorf("seed: bad embedded JSON: %w", err)
	}

	seen := make(map[string]struct{}, len(list))
	ctx := context.Background()
	added, skipped := 0, 0

	for _, e := range list {
		name := strings.TrimSpace(e.Name)
		url := strings.TrimSpace(e.URL)
		if name == "" || url == "" {
			skipped++
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue // dedupe JSON
		}
		seen[key] = struct{}{}

		c := models.Company{
			Name:       name,
			CareersURL: url,
			Active:     true,
		}
		if err := r.UpsertCompanyByName(ctx, &c); err != nil {
			return err
		}
		added++
	}

	r.infof("SeedCompanies loaded=%d skipped=%d total_in_json=%d", added, skipped, len(list))
	return nil
}

// --- internal helpers ---

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

func (r *SQLiteRepo) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	start := time.Now()
	rows, err := r.db.QueryContext(ctx, query, args...)
	dur := time.Since(start)
	if err != nil {
		r.infof("SQL query err dur=%s sql=%q args=%s err=%v", dur, minifySQL(query), previewArgs(args), err)
		return nil, err
	}
	r.debugf("SQL query ok  dur=%s sql=%q args=%s", dur, minifySQL(query), previewArgs(args))
	return rows, nil
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

func isTruthy(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// embeddedCompaniesJSON is compiled into the binary and not served anywhere.
const embeddedCompaniesJSON = `[
  { "name": "Secret TLV", "url": "https://jobs.secrettelaviv.com/" },
  { "name": "AI21", "url": "https://www.ai21.com/careers/" },
  { "name": "Agora Real", "url": "https://agorareal.com/career" },
  { "name": "Arbe Robotics", "url": "https://arberobotics.com/career/" },
  { "name": "Buildots", "url": "https://buildots.com/careers/co/development/" },
  { "name": "C2A Security", "url": "https://c2a-sec.com/careers/" },
  { "name": "Carbyne", "url": "https://carbyne.com/company/careers/" },
  { "name": "Check Point", "url": "https://careers.checkpoint.com/index.php?m=cpcareers&a=search" },
  { "name": "Kornit", "url": "https://careers.kornit.com/all-positions/" },
  { "name": "Mobileye", "url": "https://careers.mobileye.com/jobs" },
  { "name": "Kaltura", "url": "https://corp.kaltura.com/company/careers/" },
  { "name": "Double AI", "url": "https://doubleai.com/careers/" },
  { "name": "DriveNets", "url": "https://drivenets.com/careers/" },
  { "name": "Hailo", "url": "https://hailo.ai/company-overview/careers/" },
  { "name": "JFrog", "url": "https://join.jfrog.com/positions/" },
  { "name": "Noma Security", "url": "https://noma.security/careers/" },
  { "name": "ParaZero", "url": "https://parazero.com/careers/" },
  { "name": "Pentera", "url": "https://pentera.io/careers/" },
  { "name": "Perion", "url": "https://perion.com/careers/" },
  { "name": "40Seas", "url": "https://www.40seas.com/careers#positions" },
  { "name": "Agrematch", "url": "https://www.agrematch.com/careers" },
  { "name": "Aidoc", "url": "https://www.aidoc.com/about/careers/" },
  { "name": "Akeyless", "url": "https://www.akeyless.io/careers/#positions" },
  { "name": "Allot", "url": "https://www.allot.com/careers/search/" },
  { "name": "Anecdotes", "url": "https://www.anecdotes.ai/careers" }
]`
