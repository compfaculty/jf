package feed

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"jf/internal/models"
)

// RSS feed structures
type RSSFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel RSSChannel `xml:"channel"`
}

type RSSChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Items       []RSSItem `xml:"item"`
}

type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
	// Note: Go's xml package doesn't easily handle custom XML namespaces
	// We'll parse these fields manually if needed, but for now feed name is used as company
	// JobListingCompany  string `xml:"http://jobicy.com company"`
	// JobListingLocation string `xml:"http://jobicy.com location"`
}

// HTTPDoer is an interface for making HTTP requests
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Parser handles RSS feed parsing
type Parser struct {
	httpClient HTTPDoer
}

// NewParser creates a new RSS feed parser
func NewParser(httpClient HTTPDoer) *Parser {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Parser{
		httpClient: httpClient,
	}
}

// ParseFeed fetches and parses an RSS feed from the given URL
func (p *Parser) ParseFeed(ctx context.Context, url string) ([]RSSItem, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "JobFinder/1.0")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// Sanitize XML to fix common issues (unescaped ampersands, etc.)
	data = sanitizeXML(data)

	var feed RSSFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("parse XML: %w", err)
	}

	return feed.Channel.Items, nil
}

// ConvertItemsToJobs converts RSS items to Job models
// It creates a company entry if it doesn't exist based on the feed source name
func ConvertItemsToJobs(items []RSSItem, feedName string, feedURL string) []models.Job {
	jobs := make([]models.Job, 0, len(items))

	// Extract company name from feed name or use a default
	companyName := strings.TrimSpace(feedName)
	if companyName == "" {
		companyName = "RSS Feed"
	}

	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		link := strings.TrimSpace(item.Link)
		rawDescription := strings.TrimSpace(item.Description)

		if title == "" || link == "" {
			continue
		}

		// Strip HTML from description
		description := StripHTML(rawDescription)

		// Parse publication date
		var discoveredAt time.Time
		if item.PubDate != "" {
			// Try common RSS date formats
			formats := []string{
				time.RFC1123Z,
				time.RFC1123,
				time.RFC822Z,
				time.RFC822,
				"Mon, 02 Jan 2006 15:04:05 MST",
				"Mon, 02 Jan 2006 15:04:05 -0700",
			}
			for _, format := range formats {
				if t, err := time.Parse(format, item.PubDate); err == nil {
					discoveredAt = t.UTC()
					break
				}
			}
		}
		if discoveredAt.IsZero() {
			discoveredAt = time.Now().UTC()
		}

		// Extract location from description if available
		location := ExtractLocation(description)

		job := models.Job{
			CompanyName:  companyName,
			Title:        title,
			URL:          link,
			Location:     location,
			Description:  description, // Store stripped description
			DiscoveredAt: discoveredAt,
			Applied:      false,
		}

		jobs = append(jobs, job)
	}

	return jobs
}

// StripHTML removes HTML tags from text
func StripHTML(html string) string {
	// Remove HTML tags using regex (simple approach)
	// This regex matches <...> tags including attributes
	htmlTagRegex := regexp.MustCompile(`<[^>]*>`)

	// Remove all HTML tags
	text := htmlTagRegex.ReplaceAllString(html, "")

	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&apos;", "'")

	// Clean up extra whitespace
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}

	return strings.Join(cleaned, "\n")
}

// ExtractLocation attempts to extract location information from description
func ExtractLocation(description string) string {
	// Common patterns in job descriptions
	patterns := []string{
		"Location:",
		"📍",
		"Location :",
		"Локація:",
		"Локація :",
	}

	descLower := strings.ToLower(description)
	for _, pattern := range patterns {
		idx := strings.Index(descLower, strings.ToLower(pattern))
		if idx != -1 {
			// Extract text after the pattern
			start := idx + len(pattern)
			rest := description[start:]
			// Take up to first newline, HTML tag start, or end
			// Split by newline first
			lines := strings.Split(rest, "\n")
			if len(lines) > 0 {
				loc := strings.TrimSpace(lines[0])
				// Stop at HTML tag if present (shouldn't be after stripHTML, but be safe)
				if tagIdx := strings.Index(loc, "<"); tagIdx != -1 {
					loc = loc[:tagIdx]
				}
				// Remove trailing punctuation
				loc = strings.TrimRight(loc, ",.")
				if loc != "" && len(loc) < 100 { // Reasonable location length limit
					return loc
				}
			}
		}
	}

	return ""
}

// sanitizeXML fixes common XML issues before parsing
// Specifically handles unescaped ampersands that aren't part of valid entities
// This handles cases like "&type" in text content which should be "&amp;type"
func sanitizeXML(data []byte) []byte {
	xmlStr := string(data)

	// Go's regexp doesn't support lookaheads, so we manually process the string
	// Strategy: Find & followed by letters/numbers, check if followed by ; or #

	var result strings.Builder
	result.Grow(len(xmlStr) + len(xmlStr)/10) // Pre-allocate some extra space

	i := 0
	for i < len(xmlStr) {
		if xmlStr[i] == '&' {
			// Check if this is a numeric entity &#...
			if i+1 < len(xmlStr) && xmlStr[i+1] == '#' {
				// This is a numeric entity, copy it as-is (will end with ;)
				// Find the semicolon
				semicolonIdx := strings.IndexByte(xmlStr[i:], ';')
				if semicolonIdx != -1 {
					result.WriteString(xmlStr[i : i+semicolonIdx+1])
					i += semicolonIdx + 1
					continue
				}
				// No semicolon found, treat as malformed but don't escape the #
				result.WriteByte('&')
				i++
				continue
			}

			// Check if this is a named entity &word;
			// Find where the word ends (non-alphanumeric or end of string)
			j := i + 1
			for j < len(xmlStr) && ((xmlStr[j] >= 'a' && xmlStr[j] <= 'z') ||
				(xmlStr[j] >= 'A' && xmlStr[j] <= 'Z') ||
				(xmlStr[j] >= '0' && xmlStr[j] <= '9')) {
				j++
			}

			// Check what comes after the word
			if j < len(xmlStr) && xmlStr[j] == ';' {
				// This is a valid entity (ends with semicolon), copy as-is
				result.WriteString(xmlStr[i : j+1])
				i = j + 1
			} else {
				// No semicolon, this is an unescaped ampersand - fix it
				word := xmlStr[i+1 : j]
				result.WriteString("&amp;" + word)
				i = j
			}
		} else {
			result.WriteByte(xmlStr[i])
			i++
		}
	}

	return []byte(result.String())
}
