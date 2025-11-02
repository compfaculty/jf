package scrape

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"jf/internal/config"
	"jf/internal/extract"
	"jf/internal/feed"
	"jf/internal/models"
)

// RSSSource implements JobSource for RSS feed aggregators.
type RSSSource struct {
	aggregator models.Aggregator
	parser     *feed.Parser
	browser    Browser // Optional, for extracting company info from job URLs
}

// NewRSSSource creates a new RSS feed source.
func NewRSSSource(agg models.Aggregator, parser *feed.Parser, browser Browser) *RSSSource {
	return &RSSSource{
		aggregator: agg,
		parser:     parser,
		browser:    browser,
	}
}

// FindJobs parses the RSS feed and returns job listings.
func (r *RSSSource) FindJobs(ctx context.Context, cfg *config.Config) ([]JobListing, error) {
	items, err := r.parser.ParseFeed(ctx, r.aggregator.SourceURL)
	if err != nil {
		return nil, fmt.Errorf("parse RSS feed: %w", err)
	}

	listings := make([]JobListing, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Link) == "" {
			continue
		}

		// Store raw RSS item data for metadata parsing
		rawData := map[string]string{
			"title":       item.Title,
			"description": item.Description,
			"pubDate":     item.PubDate,
			"guid":        item.GUID,
		}

		listings = append(listings, JobListing{
			URL:     strings.TrimSpace(item.Link),
			Title:   strings.TrimSpace(item.Title),
			Source:  r.aggregator.Name,
			RawData: rawData,
		})
	}

	return listings, nil
}

// ParseJobMetadata extracts detailed metadata from an RSS item.
func (r *RSSSource) ParseJobMetadata(ctx context.Context, listing JobListing) (*JobMetadata, error) {
	metadata := &JobMetadata{
		Title:       listing.Title,
		URL:         listing.URL,
		Source:      listing.Source,
		Description: listing.RawData["description"],
	}

	// Parse publication date
	if pubDateStr := listing.RawData["pubDate"]; pubDateStr != "" {
		formats := []string{
			time.RFC1123Z,
			time.RFC1123,
			time.RFC822Z,
			time.RFC822,
			"Mon, 02 Jan 2006 15:04:05 MST",
			"Mon, 02 Jan 2006 15:04:05 -0700",
		}
		for _, format := range formats {
			if t, err := time.Parse(format, pubDateStr); err == nil {
				metadata.DatePosted = t.UTC()
				break
			}
		}
	}

	// Strip HTML from description
	if metadata.Description != "" {
		metadata.Description = feed.StripHTML(metadata.Description)
	}

	// Extract location from description
	metadata.Location = feed.ExtractLocation(metadata.Description)

	// Try to extract company name from URL or page
	// RSS feeds often link to external company sites or HR portals
	if listing.URL != "" {
		// Check if URL is an HR portal
		parsedURL, err := url.Parse(listing.URL)
		if err == nil {
			host := strings.ToLower(parsedURL.Hostname())
			isPortal := extract.DetectHRPortal(host)

			if isPortal {
				// For HR portals, extract company name and set ApplyURL
				companyName, applyURL, _, err := extract.ExtractCompanyFromJob(ctx, listing.URL, r.browser)
				if err == nil && companyName != "" {
					metadata.Company = companyName
					metadata.ApplyURL = applyURL
					metadata.ApplyViaPortal = true
				}
			} else {
				// For regular company sites, try to extract company name
				companyName, _, _, err := extract.ExtractCompanyFromJob(ctx, listing.URL, r.browser)
				if err == nil && companyName != "" {
					metadata.Company = companyName
				}
			}
		}
	}

	// Fallback to aggregator name if company not found
	if metadata.Company == "" {
		metadata.Company = r.aggregator.Name
	}

	// If title is empty, try to extract from URL
	if metadata.Title == "" {
		if parsedURL, err := url.Parse(listing.URL); err == nil {
			path := strings.Trim(parsedURL.Path, "/")
			parts := strings.Split(path, "/")
			if len(parts) > 0 {
				lastPart := parts[len(parts)-1]
				metadata.Title = strings.ReplaceAll(lastPart, "-", " ")
				metadata.Title = strings.ReplaceAll(metadata.Title, "_", " ")
			}
		}
	}

	return metadata, nil
}

// ApplyJob handles job applications for RSS-sourced jobs.
// RSS feeds typically link to external sites, so we extract the ApplyURL
// and return nil (not supported) to let the apply endpoint handle routing.
func (r *RSSSource) ApplyJob(ctx context.Context, job models.Job, cfg *config.Config) (*models.AppliedResult, error) {
	// RSS feeds don't typically support direct application
	// The ApplyURL should already be set in the job metadata
	// Return nil to indicate not supported (graceful degradation)
	return nil, nil
}
