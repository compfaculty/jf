package common

import (
	"context"
	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/pool"
	"net/http"
	"time"
)

// JobScraper is the interface all scrapers must implement.
type JobScraper interface {
	GetJobs(ctx context.Context, prefs *config.Config) ([]models.ScrapedJob, error)
	// GetJobPosted extracts the posted date from a job URL.
	// Returns empty string if date cannot be determined.
	// Should return date in ISO format (RFC3339) or simple date string like "2025-01-15".
	GetJobPosted(ctx context.Context, jobURL string) (string, error)
}

// Doer is satisfied by *http.Client and httpx.Client.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Browser is the minimal ability we need from a browser pool (optional).
type Browser interface {
	FetchHTML(ctx context.Context, url, selector string, wait time.Duration) (string, error)
	FetchAnchors(ctx context.Context, url string, wait time.Duration) ([]pool.Anchor, error)
}
