package extract

import (
	"context"
	"jf/internal/pool"
	"net/url"
	"strings"
	"time"
)

// Browser is the minimal ability we need from a browser pool.
// Matches the interface from internal/scrape/factory.go
type Browser interface {
	FetchHTML(ctx context.Context, urlStr, selector string, wait time.Duration) (string, error)
	FetchAnchors(ctx context.Context, urlStr string, wait time.Duration) ([]pool.Anchor, error)
}

// CompanyExtractor is a generic interface for extracting company names from job URLs.
// Each job-board can implement its own extractor with site-specific logic.
type CompanyExtractor interface {
	FindCompanyName(ctx context.Context, jobURL string, browser Browser) (companyName string, err error)
}

// GenericCompanyExtractor provides default extraction logic.
// It attempts to extract company name from URL patterns, meta tags, and page content.
type GenericCompanyExtractor struct{}

func NewGenericCompanyExtractor() *GenericCompanyExtractor {
	return &GenericCompanyExtractor{}
}

// FindCompanyName implements CompanyExtractor interface.
// Tries multiple strategies: URL parsing, HTTP headers, then page scraping.
func (g *GenericCompanyExtractor) FindCompanyName(ctx context.Context, jobURL string, browser Browser) (string, error) {
	if jobURL == "" {
		return "", nil
	}

	u, err := url.Parse(jobURL)
	if err != nil {
		return "", err
	}

	// Strategy 1: Try to extract from URL patterns
	if company := extractCompanyFromURL(u); company != "" {
		return company, nil
	}

	// Strategy 2: If browser is available, try page scraping
	if browser != nil {
		if company, err := extractCompanyFromPage(ctx, jobURL, browser); err == nil && company != "" {
			return company, nil
		}
	}

	return "", nil
}

// extractCompanyFromURL attempts to extract company name from URL structure.
func extractCompanyFromURL(u *url.URL) string {
	host := strings.ToLower(u.Hostname())

	// Remove common prefixes and suffixes
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "jobs.")
	host = strings.TrimPrefix(host, "careers.")
	host = strings.TrimPrefix(host, "boards.")

	// For subdomains like company.lever.co, extract the subdomain
	parts := strings.Split(host, ".")
	if len(parts) > 2 {
		// Example: company.lever.co -> company
		return strings.Title(parts[0])
	}

	// For paths like lever.co/company/job, try to extract from path
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) > 0 && pathParts[0] != "" {
		// Could be company name in path
		return strings.Title(pathParts[0])
	}

	return ""
}

// extractCompanyFromPage attempts to extract company name from page metadata.
// Uses browser to fetch and parse HTML.
func extractCompanyFromPage(ctx context.Context, jobURL string, browser Browser) (string, error) {
	// Try fetching common meta selectors
	selectors := []string{
		`meta[property="og:site_name"]`,
		`meta[name="application-name"]`,
		`meta[property="article:author"]`,
	}

	for _, selector := range selectors {
		html, err := browser.FetchHTML(ctx, jobURL, selector, 2*time.Second)
		if err == nil && html != "" {
			// Parse content attribute from meta tag
			if company := extractFromMetaTag(html); company != "" {
				return company, nil
			}
		}
	}

	// Fallback: fetch full page and look for common patterns
	html, err := browser.FetchHTML(ctx, jobURL, "title", 2*time.Second)
	if err == nil && html != "" {
		// Try to extract from title or other common patterns
		if company := extractFromPageContent(html); company != "" {
			return company, nil
		}
	}

	return "", nil
}

// extractFromMetaTag extracts company name from meta tag HTML.
func extractFromMetaTag(html string) string {
	// Simple regex or string parsing for meta content
	// For now, basic implementation
	if idx := strings.Index(html, `content="`); idx != -1 {
		start := idx + 9
		if end := strings.Index(html[start:], `"`); end != -1 {
			return strings.TrimSpace(html[start : start+end])
		}
	}
	return ""
}

// extractFromPageContent attempts to extract company from page content.
func extractFromPageContent(html string) string {
	// Look for common patterns like "at Company Name" or "Company Name Careers"
	// This is a placeholder - real implementation would use proper HTML parsing
	return ""
}

// DetectHRPortal checks if the host is a known HR portal.
func DetectHRPortal(host string) bool {
	host = strings.ToLower(host)
	portalHosts := []string{
		"lever.co",
		"jobs.lever.co",
		"workable.com",
		"apply.workable.com",
		"greenhouse.io",
		"boards.greenhouse.io",
		"ashbyhq.com",
		"jobs.ashbyhq.com",
		"smartrecruiters.com",
		"jobs.smartrecruiters.com",
		"recruitee.com",
		"comeet.co",
		"bamboohr.com",
	}

	for _, portal := range portalHosts {
		if strings.Contains(host, portal) {
			return true
		}
	}
	return false
}

// GetExtractorForPortal returns a portal-specific extractor or the generic one.
func GetExtractorForPortal(host string) CompanyExtractor {
	host = strings.ToLower(host)

	switch {
	case strings.Contains(host, "lever.co"):
		return NewLeverExtractor()
	case strings.Contains(host, "workable.com"):
		return NewWorkableExtractor()
	case strings.Contains(host, "greenhouse.io"):
		return NewGreenhouseExtractor()
	default:
		return NewGenericCompanyExtractor()
	}
}

// ExtractCompanyFromJob extracts company name and portal info from a job URL.
// Returns: companyName, applyURL, isPortal, error
func ExtractCompanyFromJob(ctx context.Context, jobURL string, browser Browser) (companyName, applyURL string, isPortal bool, err error) {
	if jobURL == "" {
		return "", "", false, nil
	}

	u, err := url.Parse(jobURL)
	if err != nil {
		return "", "", false, err
	}

	isPortal = DetectHRPortal(u.Hostname())

	var extractor CompanyExtractor
	if isPortal {
		extractor = GetExtractorForPortal(u.Hostname())
		applyURL = jobURL // Use the job URL as apply URL for portals
	} else {
		extractor = NewGenericCompanyExtractor()
	}

	companyName, err = extractor.FindCompanyName(ctx, jobURL, browser)
	if err != nil {
		return "", "", isPortal, err
	}

	return companyName, applyURL, isPortal, nil
}

// Portal-specific extractors (implementations can be added incrementally)

type LeverExtractor struct{}

func NewLeverExtractor() *LeverExtractor {
	return &LeverExtractor{}
}

func (l *LeverExtractor) FindCompanyName(ctx context.Context, jobURL string, browser Browser) (string, error) {
	u, err := url.Parse(jobURL)
	if err != nil {
		return "", err
	}

	// Lever URLs: https://jobs.lever.co/company-name/job-id
	// Extract company from path
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) > 0 && pathParts[0] != "" {
		return strings.Title(strings.ReplaceAll(pathParts[0], "-", " ")), nil
	}

	// Fallback to generic
	gen := NewGenericCompanyExtractor()
	return gen.FindCompanyName(ctx, jobURL, browser)
}

type WorkableExtractor struct{}

func NewWorkableExtractor() *WorkableExtractor {
	return &WorkableExtractor{}
}

func (w *WorkableExtractor) FindCompanyName(ctx context.Context, jobURL string, browser Browser) (string, error) {
	u, err := url.Parse(jobURL)
	if err != nil {
		return "", err
	}

	// Workable URLs: https://apply.workable.com/company-name/j/job-id
	// Extract company from path
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) > 1 && pathParts[0] == "j" {
		// Skip "j" and get company from subdomain or other path segment
		if len(pathParts) > 1 {
			return strings.Title(strings.ReplaceAll(pathParts[1], "-", " ")), nil
		}
	}

	// Fallback to generic
	gen := NewGenericCompanyExtractor()
	return gen.FindCompanyName(ctx, jobURL, browser)
}

type GreenhouseExtractor struct{}

func NewGreenhouseExtractor() *GreenhouseExtractor {
	return &GreenhouseExtractor{}
}

func (g *GreenhouseExtractor) FindCompanyName(ctx context.Context, jobURL string, browser Browser) (string, error) {
	u, err := url.Parse(jobURL)
	if err != nil {
		return "", err
	}

	// Greenhouse URLs: https://boards.greenhouse.io/company-name/jobs/job-id
	// Extract company from path
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, part := range pathParts {
		if part == "jobs" && i > 0 {
			return strings.Title(strings.ReplaceAll(pathParts[i-1], "-", " ")), nil
		}
	}

	// Fallback to generic
	gen := NewGenericCompanyExtractor()
	return gen.FindCompanyName(ctx, jobURL, browser)
}
