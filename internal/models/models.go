package models

import "time"

type Company struct {
	ID         string    `json:"id"` // UUID
	Name       string    `json:"name"`
	CareersURL string    `json:"careers_url"`
	Active     bool      `json:"active"`
	ApplyEmail string    `json:"apply_email"` // if set, prefer email-based application
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Job struct {
	ID           string     `json:"id"` // UUID
	CompanyID    string     `json:"company_id"`
	CompanyName  string     `json:"company_name,omitempty"`
	Title        string     `json:"title"`
	URL          string     `json:"url"`
	Location     string     `json:"location"`
	Description  string     `json:"description"`
	DiscoveredAt time.Time  `json:"discovered_at"`
	Applied      bool       `json:"applied"`
	AppliedAt    *time.Time `json:"applied_at,omitempty"`
}

// JobQuery Filters for GET /api/jobs
type JobQuery struct {
	CompanyID string `json:"company_id,omitempty"`
	Q         string `json:"q,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

type JobPage struct {
	Items  []Job `json:"items"`
	Total  int   `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

type AppliedResult struct {
	JobID   int64  `json:"job_id"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Status  int    `json:"status"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type ScrapedJob struct {
	Title       string
	URL         string
	Location    string
	Description string
	Company     string
	DatePosted  string
}
