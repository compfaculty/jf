package scrape

import (
	"context"
	"fmt"
	"jf/internal/scrape/common"
	"net/url"
	"strings"
	"time"

	"jf/internal/config"
	"jf/internal/extract"
	"jf/internal/models"
)

// BoardSource implements JobSource for job board aggregators.
// Handles boards like Secret Tel Aviv (direct apply) and Telfed (external links).
type BoardSource struct {
	aggregator models.Aggregator
	scraper    common.JobScraper // Existing board scraper (SecretTelAviv, TelfedScraper, etc.)
	browser    common.Browser    // Optional, for extracting company info from external links
	client     common.Doer
}

// NewBoardSource creates a new board source from an existing scraper.
func NewBoardSource(agg models.Aggregator, scraper common.JobScraper, client common.Doer, browser common.Browser) *BoardSource {
	return &BoardSource{
		aggregator: agg,
		scraper:    scraper,
		client:     common.EnsureClient(client),
		browser:    browser,
	}
}

// FindJobs uses the existing board scraper to discover job listings.
func (b *BoardSource) FindJobs(ctx context.Context, cfg *config.Config) ([]JobListing, error) {
	// Use existing GetJobs method
	scrapedJobs, err := b.scraper.GetJobs(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("get jobs: %w", err)
	}

	// Convert ScrapedJob to JobListing
	listings := make([]JobListing, 0, len(scrapedJobs))
	for _, sj := range scrapedJobs {
		if strings.TrimSpace(sj.URL) == "" {
			continue
		}

		// Store raw data for metadata parsing
		rawData := map[string]string{
			"title":       sj.Title,
			"description": sj.Description,
			"location":    sj.Location,
			"hr_email":    sj.HREmail,
			"hr_phone":    sj.HRPhone,
			"date_posted": sj.DatePosted,
		}

		listings = append(listings, JobListing{
			URL:     sj.URL,
			Title:   sj.Title,
			Source:  b.aggregator.Name,
			RawData: rawData,
		})
	}

	return listings, nil
}

// ParseJobMetadata extracts detailed metadata and detects external links.
// For boards that link to external sites, extracts company info and detects HR portals.
func (b *BoardSource) ParseJobMetadata(ctx context.Context, listing JobListing) (*JobMetadata, error) {
	metadata := &JobMetadata{
		Title:    listing.RawData["title"],
		URL:      listing.URL,
		Source:   b.aggregator.Name,
		Location: listing.RawData["location"],
		HREmail:  listing.RawData["hr_email"],
		HRPhone:  listing.RawData["hr_phone"],
	}

	// Parse date posted if available
	if dateStr := listing.RawData["date_posted"]; dateStr != "" {
		// Secret Tel Aviv uses relative dates like "2 days ago"
		if t, ok := parseRelativeDate(dateStr); ok {
			metadata.DatePosted = t
		} else {
			// Try standard formats
			formats := []string{
				time.RFC3339,
				"2006-01-02",
				"02/01/2006",
				"January 2, 2006",
			}
			for _, format := range formats {
				if t, err := time.Parse(format, dateStr); err == nil {
					metadata.DatePosted = t.UTC()
					break
				}
			}
		}
	}

	// Check if this job URL links to an external site (not the board itself)
	parsedURL, err := url.Parse(listing.URL)
	if err == nil {
		boardURL, _ := url.Parse(b.aggregator.SourceURL)

		// If job URL is on a different domain, it's an external link
		if boardURL != nil && parsedURL.Hostname() != boardURL.Hostname() {
			// This is an external link - extract company and check if it's a portal
			host := strings.ToLower(parsedURL.Hostname())
			isPortal := extract.DetectHRPortal(host)

			companyName, applyURL, _, err := extract.ExtractCompanyFromJob(ctx, listing.URL, b.browser)
			if err == nil {
				if companyName != "" {
					metadata.Company = companyName
				}
				if applyURL != "" {
					metadata.ApplyURL = applyURL
				} else {
					metadata.ApplyURL = listing.URL
				}
				metadata.ApplyViaPortal = isPortal
			} else {
				// Fallback: use URL hostname as company hint
				hostname := parsedURL.Hostname()
				hostname = strings.TrimPrefix(hostname, "www.")
				hostname = strings.TrimPrefix(hostname, "jobs.")
				hostname = strings.TrimPrefix(hostname, "careers.")
				if parts := strings.Split(hostname, "."); len(parts) > 0 {
					metadata.Company = strings.Title(parts[0])
				}
				metadata.ApplyURL = listing.URL
				metadata.ApplyViaPortal = isPortal
			}
		} else {
			// Job is on the board itself - use board name as company
			metadata.Company = b.aggregator.Name
			// Secret Tel Aviv supports direct apply, so ApplyURL is the same as job URL
			// We'll determine this in ApplyJob method
		}
	}

	// Fallback company name
	if metadata.Company == "" {
		metadata.Company = b.aggregator.Name
	}

	// Use description from raw data if available
	if desc := listing.RawData["description"]; desc != "" {
		metadata.Description = desc
	} else if metadata.Title != "" {
		metadata.Description = metadata.Title
	}

	return metadata, nil
}

// ApplyJob handles job applications for board-sourced jobs.
// Secret Tel Aviv supports direct apply via form POST.
// Other boards may link to external sites (handled by ApplyURL).
func (b *BoardSource) ApplyJob(ctx context.Context, job models.Job, cfg *config.Config) (*models.AppliedResult, error) {
	// Check if scraper implements ApplyJobs (Secret Tel Aviv does)
	if applier, ok := b.scraper.(interface {
		ApplyJobs(ctx context.Context, jobs []models.Job, cfg *config.Config) ([]models.AppliedResult, error)
	}); ok {
		results, err := applier.ApplyJobs(ctx, []models.Job{job}, cfg)
		if err != nil {
			return nil, err
		}
		if len(results) > 0 {
			return &results[0], nil
		}
		return nil, fmt.Errorf("apply returned no results")
	}

	// For boards that link externally, the ApplyURL should be set
	// But we can't directly apply to external sites from here
	// Return nil to indicate not supported (graceful degradation)
	// The apply endpoint can handle this by redirecting to ApplyURL if set
	return nil, nil
}

// parseRelativeDate parses relative date strings like "2 days ago", "3 weeks ago", "1 month ago".
func parseRelativeDate(s string) (time.Time, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	now := time.Now()

	fields := strings.Fields(s)
	if len(fields) >= 3 && fields[2] == "ago" {
		n := 0
		for _, r := range fields[0] {
			if r >= '0' && r <= '9' {
				n = n*10 + int(r-'0')
			} else {
				return time.Time{}, false
			}
		}

		unit := strings.TrimSuffix(fields[1], "s")
		switch unit {
		case "day":
			return now.AddDate(0, 0, -n), true
		case "week":
			return now.AddDate(0, 0, -7*n), true
		case "month":
			return now.AddDate(0, -n, 0), true
		case "year":
			return now.AddDate(-n, 0, 0), true
		}
	}

	return time.Time{}, false
}
