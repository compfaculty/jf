package scrape

import (
	"context"
	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/pool"
	"net/http"
	"strings"
	"time"

	"jf/internal/httpx"

	"github.com/alitto/pond"
)

// ---- Public types used by scanner adapter ----

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

// NewJobScraper chooses a concrete scraper by careers host.
// Pass nil for client to use the default robust httpx client.
// Pass nil for browser if you don't want JS fallback in Generic.
func NewJobScraper(c models.Company, client Doer, browser Browser, wp *pond.WorkerPool) JobScraper {
	switch {
	case strings.Contains(c.CareersURL, "telfed.org.il/job-board") || strings.Contains(c.CareersURL, "telfed.org.il/job"):
		// Telfed requires BrowserPool due to Cloudflare protection
		if browser == nil {
			// Fallback to generic if no browser (will likely fail due to Cloudflare)
			return NewGeneric(c, client, browser, wp)
		}
		// Note: avoid log import here to keep factory lean; rely on Telfed scraper logs
		return NewTelfed(c, browser, wp)
	case strings.Contains(c.CareersURL, "secrettelaviv.com"):

		return NewSecretTelAviv(c, client)
	case strings.Contains(c.CareersURL, "40seas.com"):

		return NewFortySeas(c, client)
	case strings.Contains(c.CareersURL, "agrematch.com"):
		return NewAgrematch(c, client)
	case strings.Contains(c.CareersURL, "ai21.com"):
		return NewAi21(c, client)
	case strings.Contains(c.CareersURL, "akeyless.io"):
		return NewAkeyless(c, client, wp)
	case strings.Contains(c.CareersURL, "audiocodes.com"):
		return NewAudioCodes(c, client)
	default:
		// generic path: static first, optional browser fallback
		return NewGeneric(c, client, browser, wp)
	}
}

// ensureClient returns the provided Doer or a robust httpx.Client with sane defaults.
func ensureClient(c Doer) Doer {
	if c != nil {
		return c
	}
	return httpx.New(httpx.HttpClientConfig{
		Timeout:      30 * time.Second,
		RPS:          1.5, // polite default; tweak per your needs
		Burst:        4,   // small burst to smooth bursts of links
		RetryMax:     4,   // transient resiliency
		RetryWaitMin: 250 * time.Millisecond,
		RetryWaitMax: 5 * time.Second,
		UserAgent:    httpx.DefaultUserAgent,
	})
}
