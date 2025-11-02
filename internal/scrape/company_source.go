package scrape

import (
	"context"
	"fmt"
	"jf/internal/scrape/common"
	"net/http"
	"regexp"
	"strings"
	"time"

	"jf/internal/config"
	"jf/internal/emailx"
	"jf/internal/models"

	"github.com/PuerkitoBio/goquery"
	"github.com/alitto/pond"
)

// CompanySource implements JobSource for direct company career pages.
type CompanySource struct {
	company models.Company
	scraper common.JobScraper // Existing scraper (Generic, SecretTelAviv, etc.)
	client  common.Doer
	browser common.Browser
	wp      *pond.WorkerPool
}

// NewCompanySource creates a new company source from an existing scraper.
func NewCompanySource(c models.Company, scraper common.JobScraper, client common.Doer, browser common.Browser, wp *pond.WorkerPool) *CompanySource {
	return &CompanySource{
		company: c,
		scraper: scraper,
		client:  common.EnsureClient(client),
		browser: browser,
		wp:      wp,
	}
}

// FindJobs uses the existing scraper to discover job listings.
func (c *CompanySource) FindJobs(ctx context.Context, cfg *config.Config) ([]JobListing, error) {
	// Use existing GetJobs method
	scrapedJobs, err := c.scraper.GetJobs(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("get jobs: %w", err)
	}

	// Convert ScrapedJob to JobListing
	listings := make([]JobListing, 0, len(scrapedJobs))
	for _, sj := range scrapedJobs {
		if strings.TrimSpace(sj.URL) == "" {
			continue
		}

		listings = append(listings, JobListing{
			URL:    sj.URL,
			Title:  sj.Title,
			Source: c.company.Name,
		})
	}

	return listings, nil
}

// ParseJobMetadata extracts detailed metadata from a job page.
func (c *CompanySource) ParseJobMetadata(ctx context.Context, listing JobListing) (*JobMetadata, error) {
	metadata := &JobMetadata{
		Title:   listing.Title,
		URL:     listing.URL,
		Company: c.company.Name,
		Source:  c.company.Name,
	}

	// Try to fetch job page for more details
	if listing.URL != "" {
		details, err := c.fetchJobDetails(ctx, listing.URL)
		if err == nil {
			// Use fetched details if available
			if details.Title != "" {
				metadata.Title = details.Title
			}
			if details.Description != "" {
				metadata.Description = details.Description
			}
			if details.Location != "" {
				metadata.Location = details.Location
			}
			if !details.DatePosted.IsZero() {
				metadata.DatePosted = details.DatePosted
			}
			if details.HREmail != "" {
				metadata.HREmail = details.HREmail
			}
			if details.HRPhone != "" {
				metadata.HRPhone = details.HRPhone
			}
		}
	}

	// If we have a title from listing, use it as fallback
	if metadata.Title == "" && listing.Title != "" {
		metadata.Title = listing.Title
	}

	// If description is empty, use title as minimal description
	if metadata.Description == "" {
		metadata.Description = metadata.Title
	}

	return metadata, nil
}

// fetchJobDetails attempts to fetch detailed information from a job page.
func (c *CompanySource) fetchJobDetails(ctx context.Context, jobURL string) (*JobMetadata, error) {
	// Try browser first (better for JS-rendered pages)
	if c.browser != nil {
		html, err := c.browser.FetchHTML(ctx, jobURL, "body", 2*time.Second)
		if err == nil && html != "" {
			return c.parseJobHTML(html, jobURL)
		}
	}

	// Fallback to static HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", jobURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	return c.parseJobDocument(doc, jobURL)
}

// parseJobHTML parses job details from HTML string.
func (c *CompanySource) parseJobHTML(html, jobURL string) (*JobMetadata, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	return c.parseJobDocument(doc, jobURL)
}

// parseJobDocument extracts metadata from a parsed HTML document.
func (c *CompanySource) parseJobDocument(doc *goquery.Document, jobURL string) (*JobMetadata, error) {
	metadata := &JobMetadata{
		URL:     jobURL,
		Company: c.company.Name,
		Source:  c.company.Name,
	}

	// Try to find title from common selectors
	titleSel := doc.Find("h1").First()
	if titleSel.Length() == 0 {
		titleSel = doc.Find("title").First()
	}
	if titleSel.Length() > 0 {
		metadata.Title = strings.TrimSpace(common.JoinWS(titleSel.Text()))
	}

	// Try to find description from common selectors
	descSelectors := []string{
		".job-description",
		".description",
		"article",
		".content",
		"main",
		"#description",
	}
	for _, sel := range descSelectors {
		if elem := doc.Find(sel).First(); elem.Length() > 0 {
			text := strings.TrimSpace(common.JoinWS(elem.Text()))
			if len(text) > 100 { // Only use if substantial content
				metadata.Description = text
				break
			}
		}
	}

	// Try to find location
	locationSelectors := []string{
		".location",
		"[data-location]",
		".job-location",
	}
	for _, sel := range locationSelectors {
		if elem := doc.Find(sel).First(); elem.Length() > 0 {
			metadata.Location = strings.TrimSpace(common.JoinWS(elem.Text()))
			if metadata.Location != "" {
				break
			}
		}
	}

	// Try to find HR email/phone
	text := doc.Text()
	metadata.HREmail = extractEmailFromText(text)
	metadata.HRPhone = extractPhoneFromText(text)

	// Try to find posted date
	dateSelectors := []string{
		"[data-posted-date]",
		".posted-date",
		"time",
	}
	for _, sel := range dateSelectors {
		if elem := doc.Find(sel).First(); elem.Length() > 0 {
			dateStr := elem.AttrOr("datetime", "")
			if dateStr == "" {
				dateStr = strings.TrimSpace(common.JoinWS(elem.Text()))
			}
			if dateStr != "" {
				if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
					metadata.DatePosted = t
				}
			}
		}
	}

	return metadata, nil
}

// ApplyJob handles job applications for company sites.
// Tries company-specific apply logic first, falls back to email if available.
func (c *CompanySource) ApplyJob(ctx context.Context, job models.Job, cfg *config.Config) (*models.AppliedResult, error) {
	// Check if scraper implements ApplyJobs (like SecretTelAviv)
	if applier, ok := c.scraper.(interface {
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

	// Fallback to email if company has ApplyEmail set
	if strings.TrimSpace(c.company.ApplyEmail) != "" {
		return c.applyViaEmail(ctx, job, cfg)
	}

	// Not supported
	return nil, nil
}

// applyViaEmail sends application via email.
func (c *CompanySource) applyViaEmail(ctx context.Context, job models.Job, cfg *config.Config) (*models.AppliedResult, error) {
	mailer := emailx.BuildSMTPMailer(&cfg.Mail)
	applicant := emailx.Applicant{
		FullName: strings.TrimSpace(cfg.ApplyForm.FirstName + " " + cfg.ApplyForm.LastName),
		Email:    cfg.ApplyForm.Email,
		Phone:    cfg.ApplyForm.Phone,
	}

	scrapedJob := models.ScrapedJob{
		Title:       job.Title,
		URL:         job.URL,
		Description: job.Description,
		Company:     c.company.Name,
	}

	company := models.Company{
		Name:       c.company.Name,
		ApplyEmail: c.company.ApplyEmail,
	}

	_, err := emailx.ApplyByEmail(ctx, mailer, &cfg.Mail, applicant, company, scrapedJob)
	if err != nil {
		return nil, err
	}

	return &models.AppliedResult{
		JobID:   job.ID,
		URL:     job.URL,
		Title:   job.Title,
		Status:  200, // Email sent successfully
		OK:      true,
		Message: "Sent via email to " + company.ApplyEmail,
	}, nil
}

// Helper functions for extracting email/phone from text
func extractEmailFromText(text string) string {
	// Simple email regex pattern
	emailRegex := `[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`
	matches := regexp.MustCompile(emailRegex).FindString(text)
	return strings.TrimSpace(matches)
}

func extractPhoneFromText(text string) string {
	// Simple phone pattern - look for sequences of digits with separators
	phoneRegex := `[\+]?[0-9]{1,4}[\s\-\.]?[0-9]{1,4}[\s\-\.]?[0-9]{1,9}`
	matches := regexp.MustCompile(phoneRegex).FindString(text)
	return strings.TrimSpace(matches)
}
