package scrape

import (
	"jf/internal/httpx"
	"time"
)

// EnsureClient returns the provided Doer or a robust httpx.Client with sane defaults.
// This is a shared utility used by all scraper implementations.
func EnsureClient(c Doer) Doer {
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
