package models

import "time"

type Company struct {
	ID         string    `json:"id"` // UUID
	Name       string    `json:"name"`
	CareersURL string    `json:"careers_url"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Job struct {
	ID           string     `json:"id"` // UUID
	CompanyID    string     `json:"company_id"`
	Title        string     `json:"title"`
	URL          string     `json:"url"`
	Location     string     `json:"location"`
	Description  string     `json:"description"`
	DiscoveredAt time.Time  `json:"discovered_at"`
	Applied      bool       `json:"applied"`
	AppliedAt    *time.Time `json:"applied_at,omitempty"`
}

// Filters for GET /api/jobs
type JobQuery struct {
	CompanyID string
	Q         string // search in title/description
	Limit     int
	Offset    int
}
