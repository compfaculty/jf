package scrape

import (
	"context"
	"jf/internal/scrape"
	"jf/internal/scrape/common"
	"net/url"
	"regexp"
	"strings"
	"time"

	"jf/internal/extract"

	"github.com/PuerkitoBio/goquery"
)

// RealWorkFromAnywhereParser handles parsing job pages from realworkfromanywhere.com.
type RealWorkFromAnywhereParser struct {
	browser common.Browser
}

// NewRealWorkFromAnywhereParser creates a new parser for realworkfromanywhere.com.
func NewRealWorkFromAnywhereParser(browser common.Browser) *RealWorkFromAnywhereParser {
	return &RealWorkFromAnywhereParser{
		browser: browser,
	}
}

// ParseJobPage extracts metadata from a realworkfromanywhere.com job page.
func (p *RealWorkFromAnywhereParser) ParseJobPage(ctx context.Context, jobURL string) (*scrape.JobMetadata, error) {
	metadata := &scrape.JobMetadata{}

	// Fetch and parse the HTML page
	doc, err := FetchJobPage(ctx, jobURL, p.browser)
	if err != nil {
		return nil, err
	}

	// Extract title from h1
	p.extractTitle(doc, metadata)

	// Extract company name
	p.extractCompany(doc, metadata)

	// Extract date posted
	p.extractDatePosted(doc, metadata)

	// Extract Apply URL
	p.extractApplyURL(doc, metadata)

	// Extract description
	p.extractDescription(doc, metadata)

	// Extract location
	p.extractLocation(doc, metadata)

	return metadata, nil
}

// extractTitle extracts job title from h1, splitting by " at " to separate title and company.
func (p *RealWorkFromAnywhereParser) extractTitle(doc *goquery.Document, metadata *scrape.JobMetadata) {
	titleSel := doc.Find("h1").First()
	if titleSel.Length() > 0 {
		fullTitle := strings.TrimSpace(titleSel.Text())
		// Split by " at " to separate title and company
		if strings.Contains(fullTitle, " at ") {
			parts := strings.SplitN(fullTitle, " at ", 2)
			metadata.Title = strings.TrimSpace(parts[0])
			if len(parts) > 1 {
				metadata.Company = strings.TrimSpace(parts[1])
			}
		} else {
			metadata.Title = fullTitle
		}
	}
}

// extractCompany extracts company name from various page sections.
func (p *RealWorkFromAnywhereParser) extractCompany(doc *goquery.Document, metadata *scrape.JobMetadata) {
	// If already extracted from title, skip
	if metadata.Company != "" {
		return
	}

	// Method 1: Look for company name in the company profile section
	companyLink := doc.Find(`a[href*="/companies/"]`).First()
	if companyLink.Length() > 0 {
		container := companyLink.Closest("div, section, article")
		if container.Length() > 0 {
			container.Find("p").Each(func(_ int, p *goquery.Selection) {
				text := strings.TrimSpace(p.Text())
				// Company name is typically short and doesn't contain certain keywords
				if len(text) > 1 && len(text) < 80 &&
					!strings.Contains(strings.ToLower(text), "view") &&
					!strings.Contains(strings.ToLower(text), "company profile") &&
					!strings.Contains(strings.ToLower(text), "regulated") &&
					!strings.Contains(strings.ToLower(text), "offering") {
					metadata.Company = text
					return
				}
			})
		}
	}

	// Method 2: Look in "About the job" section for company name
	if metadata.Company == "" {
		doc.Find("h2").Each(func(_ int, h *goquery.Selection) {
			if strings.Contains(strings.ToLower(h.Text()), "about the job") {
				section := h.NextUntil("h2, h3")
				if section.Length() == 0 {
					section = h.Parent()
				}
				section.Find("p").Each(func(_ int, p *goquery.Selection) {
					text := strings.TrimSpace(p.Text())
					if len(text) > 1 && len(text) < 80 &&
						!strings.Contains(strings.ToLower(text), "view") &&
						!strings.Contains(strings.ToLower(text), "posted") &&
						!strings.Contains(strings.ToLower(text), "apply") &&
						!strings.Contains(strings.ToLower(text), "job type") &&
						!strings.Contains(strings.ToLower(text), "location") {
						if metadata.Company == "" {
							metadata.Company = text
						}
					}
				})
			}
		})
	}
}

// extractDatePosted extracts the date the job was posted.
func (p *RealWorkFromAnywhereParser) extractDatePosted(doc *goquery.Document, metadata *scrape.JobMetadata) {
	doc.Find("*").Each(func(_ int, s *goquery.Selection) {
		text := s.Text()
		if strings.Contains(text, "Posted on") {
			// Look for date pattern like "Oct 13, 2025"
			dateRegex := regexp.MustCompile(`Posted on\s*([A-Za-z]{3,9}\s+\d{1,2},\s+\d{4})`)
			matches := dateRegex.FindStringSubmatch(text)
			if len(matches) > 1 {
				dateStr := matches[1]
				// Parse date in format "Oct 13, 2025"
				formats := []string{
					"Jan 2, 2006",
					"January 2, 2006",
					"Jan 02, 2006",
					"January 02, 2006",
				}
				for _, format := range formats {
					if t, err := time.Parse(format, dateStr); err == nil {
						metadata.DatePosted = t.UTC()
						return
					}
				}
			}
		}
	})
}

// extractApplyURL extracts the Apply URL from links containing HR portal domains.
func (p *RealWorkFromAnywhereParser) extractApplyURL(doc *goquery.Document, metadata *scrape.JobMetadata) {
	applySelectors := []string{
		`a[href*="greenhouse.io"]`,
		`a[href*="workable.com"]`,
		`a[href*="lever.co"]`,
		`a[href*="jobs.lever.co"]`,
		`a[href*="apply.workable.com"]`,
		`button[onclick*="greenhouse"], a[href*="apply"]`,
	}

	for _, selector := range applySelectors {
		applyLink := doc.Find(selector).First()
		if applyLink.Length() > 0 {
			if href, exists := applyLink.Attr("href"); exists && href != "" {
				metadata.ApplyURL = href
				// Check if it's an HR portal
				parsedApplyURL, err := url.Parse(href)
				if err == nil {
					host := strings.ToLower(parsedApplyURL.Hostname())
					metadata.ApplyViaPortal = extract.DetectHRPortal(host)
				}
				break
			}
		}
	}
}

// extractDescription extracts job description from the job description section.
func (p *RealWorkFromAnywhereParser) extractDescription(doc *goquery.Document, metadata *scrape.JobMetadata) {
	doc.Find("h2, h3").Each(func(_ int, h *goquery.Selection) {
		if strings.Contains(strings.ToLower(h.Text()), "job description") {
			// Get all text in the article or section after this heading
			section := h.Next()
			if section.Length() == 0 {
				section = h.Parent()
			}
			var descParts []string
			section.Find("p, li").Each(func(_ int, p *goquery.Selection) {
				text := strings.TrimSpace(p.Text())
				if text != "" && len(text) > 20 {
					descParts = append(descParts, text)
				}
			})
			if len(descParts) > 0 {
				metadata.Description = strings.Join(descParts, "\n\n")
			}
		}
	})
}

// extractLocation extracts location from "About the job" section.
func (p *RealWorkFromAnywhereParser) extractLocation(doc *goquery.Document, metadata *scrape.JobMetadata) {
	doc.Find("h2").Each(func(_ int, h *goquery.Selection) {
		if strings.Contains(strings.ToLower(h.Text()), "about the job") {
			// Look for location in this section
			section := h.NextUntil("h2, h3")
			if section.Length() == 0 {
				section = h.Parent()
			}
			section.Find("*").Each(func(_ int, s *goquery.Selection) {
				text := s.Text()
				if strings.Contains(text, "Location") {
					// Find the next sibling or child that contains the location value
					next := s.Next()
					if next.Length() > 0 {
						locationText := strings.TrimSpace(next.Text())
						// Location is typically "Worldwide", "Remote", city name, etc.
						if locationText != "" && len(locationText) < 100 {
							metadata.Location = locationText
							return
						}
					}
					// Alternative: extract from the same element's text
					locationRegex := regexp.MustCompile(`Location\s+([^\n]+)`)
					matches := locationRegex.FindStringSubmatch(text)
					if len(matches) > 1 {
						metadata.Location = strings.TrimSpace(matches[1])
					}
				}
			})
		}
	})
}
