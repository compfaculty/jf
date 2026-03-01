package models

import "time"

type Company struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CareersURL string    `json:"careers_url"`
	Active     bool      `json:"active"`
	ApplyEmail string    `json:"apply_email"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Job struct {
	ID               string     `json:"id"`
	CompanyID        string     `json:"company_id"`
	CompanyName      string     `json:"company_name,omitempty"`
	AggregatorName   string     `json:"aggregator_name,omitempty"` // Name of aggregator (for display)
	Title            string     `json:"title"`
	URL              string     `json:"url"`
	ApplyURL         string     `json:"apply_url,omitempty"`        // HR portal URL for redirect
	ApplyViaPortal   bool       `json:"apply_via_portal,omitempty"` // Flag for portal jobs
	Location         string     `json:"location"`
	Description      string     `json:"description"`
	HREmail          string     `json:"hr_email,omitempty"`
	HRPhone          string     `json:"hr_phone,omitempty"`
	DiscoveredAt     time.Time  `json:"discovered_at"`
	PostedAt         string     `json:"posted_at,omitempty"` // When the job was posted on the internet (ISO format string)
	Applied          bool       `json:"applied"`
	AppliedAt        *time.Time `json:"applied_at,omitempty"`
	ApplyPending429  *time.Time `json:"apply_pending_429,omitempty"` // When to retry if rate-limited (429)
}

// RateLimitedEntry represents a job queued for retry after HTTP 429.
type RateLimitedEntry struct {
	JobID      string    `json:"job_id"`
	URL        string    `json:"url"`
	RetryAfter time.Time `json:"retry_after"`
	CreatedAt  time.Time `json:"created_at"`
}

// JobQuery Filters for GET /api/jobs
type JobQuery struct {
	CompanyID   string `json:"company_id,omitempty"`
	Q           string `json:"q,omitempty"`
	HideApplied bool   `json:"hide_applied,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Offset      int    `json:"offset,omitempty"`
}

type JobPage struct {
	Items  []Job `json:"items"`
	Total  int   `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

type AppliedResult struct {
	JobID   string `json:"job_id"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Status  int    `json:"status"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type Aggregator struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SourceURL string    `json:"source_url"`
	Type      string    `json:"type"` // 'scraper' or 'rss_feed'
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ScrapedJob struct {
	Title       string
	URL         string
	Location    string
	Description string
	Company     string
	DatePosted  string
	HREmail     string
	HRPhone     string
}
