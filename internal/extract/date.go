package extract

import (
	"context"
	"net/http"
	"strings"
	"time"

	"jf/internal/scrape/common"
)

// DateExtractor is a generic interface for extracting job posted dates from job URLs.
// Each job-board can implement its own extractor with site-specific logic.
type DateExtractor interface {
	GetJobPostedDate(ctx context.Context, jobURL string, respHeaders http.Header, browser common.Browser) (postedDate time.Time, found bool, err error)
}

// GenericDateExtractor provides default extraction logic.
// It attempts to extract posted date from HTTP headers first, then page parsing.
type GenericDateExtractor struct{}

func NewGenericDateExtractor() *GenericDateExtractor {
	return &GenericDateExtractor{}
}

// GetJobPostedDate implements DateExtractor interface.
// Strategy: 1) Check HTTP headers, 2) Parse page metadata, 3) Parse page content.
func (g *GenericDateExtractor) GetJobPostedDate(ctx context.Context, jobURL string, respHeaders http.Header, browser common.Browser) (time.Time, bool, error) {
	// Strategy 1: Check HTTP response headers first (fastest, no page load needed)
	if respHeaders != nil {
		if date, found := ParseDateFromHeaders(respHeaders); found {
			return date, true, nil
		}
	}

	// Strategy 2: If browser is available, try page scraping
	if browser != nil {
		if date, found, err := extractDateFromPage(ctx, jobURL, browser); err == nil && found {
			return date, true, nil
		}
	}

	return time.Time{}, false, nil
}

// ParseDateFromHeaders extracts date from HTTP response headers.
// Checks: Last-Modified, Date, X-Job-Posted-Date, and other common date headers.
func ParseDateFromHeaders(headers http.Header) (time.Time, bool) {
	// Check common date headers in order of reliability
	headerNames := []string{
		"Last-Modified",
		"X-Job-Posted-Date",
		"X-Posted-Date",
		"Date",
		"X-Publish-Date",
	}

	for _, name := range headerNames {
		value := headers.Get(name)
		if value == "" {
			continue
		}

		// Try multiple date formats
		formats := []string{
			time.RFC1123,
			time.RFC1123Z,
			time.RFC822,
			time.RFC822Z,
			time.RFC3339,
			"2006-01-02",
			"2006-01-02T15:04:05Z",
			"2006-01-02 15:04:05",
		}

		for _, format := range formats {
			if t, err := time.Parse(format, strings.TrimSpace(value)); err == nil {
				return t.UTC(), true
			}
		}
	}

	return time.Time{}, false
}

// extractDateFromPage attempts to extract posted date from page metadata.
// Uses browser to fetch and parse HTML.
func extractDateFromPage(ctx context.Context, jobURL string, browser common.Browser) (time.Time, bool, error) {
	// Try fetching common meta selectors for dates
	selectors := []string{
		`meta[property="article:published_time"]`,
		`meta[name="datePublished"]`,
		`meta[property="og:published_time"]`,
		`meta[name="publishdate"]`,
		`time[datetime]`,
	}

	for _, selector := range selectors {
		html, err := browser.FetchHTML(ctx, jobURL, selector, 2*time.Second)
		if err == nil && html != "" {
			// Parse datetime attribute or content from meta tag
			if date, found := extractDateFromMetaTag(html); found {
				return date, true, nil
			}
		}
	}

	return time.Time{}, false, nil
}

// extractDateFromMetaTag extracts date from meta tag HTML.
func extractDateFromMetaTag(html string) (time.Time, bool) {
	// Look for content="..." or datetime="..."
	patterns := []string{
		`content="`,
		`datetime="`,
	}

	for _, pattern := range patterns {
		if idx := strings.Index(html, pattern); idx != -1 {
			start := idx + len(pattern)
			if end := strings.Index(html[start:], `"`); end != -1 {
				dateStr := strings.TrimSpace(html[start : start+end])

				// Try multiple date formats
				formats := []string{
					time.RFC3339,
					time.RFC3339Nano,
					"2006-01-02T15:04:05Z07:00",
					"2006-01-02",
					"2006-01-02 15:04:05",
				}

				for _, format := range formats {
					if t, err := time.Parse(format, dateStr); err == nil {
						return t.UTC(), true
					}
				}
			}
		}
	}

	return time.Time{}, false
}

// GetExtractorForPortal returns a portal-specific date extractor or the generic one.
// GetDateExtractorForPortal previously selected portal-specific extractors.
// Since portal-specific implementations delegated to the generic logic,
// we simplify by always returning the generic extractor.
func GetDateExtractorForPortal(_ string) DateExtractor { return NewGenericDateExtractor() }
