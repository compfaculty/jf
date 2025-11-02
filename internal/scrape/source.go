package scrape

import (
	"context"
	"jf/internal/config"
	"jf/internal/models"
	"time"
)

// JobSource is the unified interface for all job sources (companies, job boards, RSS feeds).
// All sources must implement FindJobs and ParseJobMetadata.
// ApplyJob is optional - sources that don't support applying should return nil.
type JobSource interface {
	// FindJobs discovers job listings from the source.
	// Returns a list of JobListing objects with minimal information (typically just URLs).
	FindJobs(ctx context.Context, cfg *config.Config) ([]JobListing, error)

	// ParseJobMetadata extracts detailed metadata from a job listing.
	// This is called for each job discovered by FindJobs to get full details.
	ParseJobMetadata(ctx context.Context, listing JobListing) (*JobMetadata, error)

	// ApplyJob attempts to apply to a job using source-specific logic.
	// Returns nil if applying is not supported for this source (graceful degradation).
	// Supported strategies: direct form POST, HR portal redirect, email.
	ApplyJob(ctx context.Context, job models.Job, cfg *config.Config) (*models.AppliedResult, error)
}

// JobListing represents a minimal job listing discovered by FindJobs.
// Contains just enough information to fetch full metadata later.
type JobListing struct {
	URL     string            // Raw URL from source
	Title   string            // Optional: title if available from listing
	Source  string            // Source identifier (company name, board name, etc.)
	RawData map[string]string // Optional: source-specific raw data for metadata parsing
}

// JobMetadata contains detailed information extracted from a job listing.
type JobMetadata struct {
	Title          string    // Job title
	URL            string    // Canonical job URL
	Description    string    // Full job description
	Company        string    // Company name
	Location       string    // Job location
	DatePosted     time.Time // When the job was posted
	ApplyURL       string    // URL to apply (may differ from job URL for boards)
	ApplyViaPortal bool      // True if ApplyURL points to an HR portal (greenhouse, workable, etc.)
	HREmail        string    // HR contact email (if available)
	HRPhone        string    // HR contact phone (if available)
	Source         string    // Source identifier
}

// ToScrapedJob converts JobMetadata to the existing ScrapedJob model format.
func (jm *JobMetadata) ToScrapedJob() models.ScrapedJob {
	datePosted := ""
	if !jm.DatePosted.IsZero() {
		datePosted = jm.DatePosted.Format("2006-01-02")
	}
	return models.ScrapedJob{
		Title:       jm.Title,
		URL:         jm.URL,
		Location:    jm.Location,
		Description: jm.Description,
		Company:     jm.Company,
		DatePosted:  datePosted,
		HREmail:     jm.HREmail,
		HRPhone:     jm.HRPhone,
	}
}
