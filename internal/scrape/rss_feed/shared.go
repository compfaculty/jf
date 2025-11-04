package scrape

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"jf/internal/scrape/common"

	"github.com/PuerkitoBio/goquery"
)

// FetchJobPage fetches and parses HTML from a job page URL.
// Uses browser if available, otherwise falls back to HTTP client.
func FetchJobPage(ctx context.Context, jobURL string, browser common.Browser) (*goquery.Document, error) {
	var doc *goquery.Document

	if browser != nil {
		// Use browser to fetch HTML (handles JS-rendered content)
		html, err := browser.FetchHTML(ctx, jobURL, "", 3*time.Second)
		if err != nil {
			return nil, fmt.Errorf("fetch HTML with browser: %w", err)
		}
		doc, err = goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			return nil, fmt.Errorf("parse HTML: %w", err)
		}
	} else {
		// Fallback: try HTTP request
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, jobURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36 Edg/142.0.0.0")

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("HTTP request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		doc, err = goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("parse document: %w", err)
		}
	}

	return doc, nil
}
