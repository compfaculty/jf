package scrape

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
