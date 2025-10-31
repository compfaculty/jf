package scrape

import (
	"bytes"
	"context"
	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/pool"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alitto/pond"
)

// TelfedScraper extracts jobs from https://www.telfed.org.il/job-board/
// It uses BrowserPool to bypass Cloudflare protection and extract job links.
type TelfedScraper struct {
	company models.Company
	browser Browser // required - Telfed uses Cloudflare protection
	wp      *pond.WorkerPool
}

func NewTelfed(c models.Company, browser Browser, wp *pond.WorkerPool) *TelfedScraper {
	if browser == nil {
		panic("TelfedScraper requires BrowserPool (site has Cloudflare protection)")
	}
	return &TelfedScraper{
		company: c,
		browser: browser,
		wp:      wp,
	}
}

func (t *TelfedScraper) GetJobs(ctx context.Context, cfg *config.Config) ([]models.ScrapedJob, error) {
	start := strings.TrimSpace(t.company.CareersURL)
	if start == "" {
		return nil, nil
	}

	log.Printf("[TELFED] start url=%s", start)

	baseURL, err := url.Parse(start)
	if err != nil {
		return nil, err
	}

	var all []models.ScrapedJob
	next := start
	seenPages := make(map[string]struct{})

	// Walk pagination
	for next != "" {
		// Prevent infinite loops
		if _, visited := seenPages[next]; visited {
			break
		}
		seenPages[next] = struct{}{}

		// Try rendered HTML first to target UAEL post grid
		html, err := t.browser.FetchHTML(ctx, next, "body", 2*time.Second)
		var jobs []models.ScrapedJob
		var nextPage string
		if err == nil && strings.TrimSpace(html) != "" {
			j2, np := t.extractFromHTML(html, next, baseURL)
			jobs = append(jobs, j2...)
			nextPage = np
			log.Printf("[TELFED] page parsed via HTML jobs=%d next=%s", len(j2), nextPage)
		}

		// Fallback to anchors API if HTML yielded nothing
		if len(jobs) == 0 {
			anchors, err := t.browser.FetchAnchors(ctx, next, 2*time.Second)
			if err != nil {
				// Best effort: return what we have so far
				if len(all) > 0 {
					return dedupeScraped(all), nil
				}
				return nil, err
			}
			if len(anchors) == 0 {
				log.Printf("[TELFED] anchors=0 at %s", next)
				break
			}
			j2, np := t.extractJobsFromAnchors(ctx, anchors, baseURL)
			jobs = append(jobs, j2...)
			if nextPage == "" {
				nextPage = np
			}
			log.Printf("[TELFED] page parsed via anchors jobs=%d next=%s", len(j2), nextPage)
		}

		all = append(all, jobs...)
		log.Printf("[TELFED] accumulated jobs=%d", len(all))

		// Find next page link
		if nextPage == "" {
			// fallback heuristic from anchors inside HTML
			anchors, _ := t.browser.FetchAnchors(ctx, next, 1*time.Second)
			nextPage = t.findNextPageFromAnchors(anchors, next)
		}

		next = nextPage

		// Polite delay between pages
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return dedupeScraped(all), ctx.Err()
		}
	}

	// Now visit each job URL to extract detailed info (title, description)
	out := t.enrichJobsWithDetails(ctx, all)
	log.Printf("[TELFED] enrich done total=%d", len(out))
	return out, nil
}

// enrichJobsWithDetails visits each job URL to extract title and description.
func (t *TelfedScraper) enrichJobsWithDetails(ctx context.Context, jobs []models.ScrapedJob) []models.ScrapedJob {
	if len(jobs) == 0 {
		return jobs
	}

	group, _ := t.wp.GroupContext(ctx)
	results := make([]models.ScrapedJob, len(jobs))
	var mu sync.Mutex

	for i, job := range jobs {
		i, job := i, job
		group.Submit(func() error {
			details := t.fetchJobDetails(ctx, job.URL)
			enriched := job
			if details.Title != "" {
				enriched.Title = details.Title
			}
			if details.Description != "" {
				enriched.Description = details.Description
			}
			if details.Location != "" {
				enriched.Location = details.Location
			}
			if details.HREmail != "" {
				enriched.HREmail = details.HREmail
			}
			if details.HRPhone != "" {
				enriched.HRPhone = details.HRPhone
			}
			mu.Lock()
			results[i] = enriched
			mu.Unlock()
			return nil
		})
	}

	_ = group.Wait()
	return results
}

// fetchJobDetails visits a job page and extracts title and description.
func (t *TelfedScraper) fetchJobDetails(ctx context.Context, jobURL string) models.ScrapedJob {
	html, err := t.browser.FetchHTML(ctx, jobURL, "body", 2*time.Second)
	if err != nil || strings.TrimSpace(html) == "" {
		return models.ScrapedJob{}
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewBufferString(html))
	if err != nil {
		return models.ScrapedJob{}
	}

	var title, description, hrEmail, hrPhone string

	// Extract title from h1.elementor-heading-title
	titleSel := doc.Find("h1.elementor-heading-title").First()
	if titleSel.Length() > 0 {
		title = strings.TrimSpace(joinWS(titleSel.Text()))
	}

	// Extract description from elementor-widget-theme-post-content
	contentSel := doc.Find(".elementor-widget-theme-post-content .elementor-widget-container").First()
	if contentSel.Length() > 0 {
		// Get all text content, preserving paragraph structure
		parts := make([]string, 0)
		contentSel.Find("p").Each(func(_ int, p *goquery.Selection) {
			text := strings.TrimSpace(joinWS(p.Text()))
			if text != "" {
				parts = append(parts, text)
				// Check each paragraph for email and phone patterns - be aggressive, check all paragraphs
				if hrEmail == "" {
					if m := extractEmailFromParagraph(text); m != "" {
						hrEmail = m
						log.Printf("[TELFED] found email in paragraph: %s", m)
					}
				}
				if hrPhone == "" {
					if p := extractPhoneFromParagraph(text); p != "" {
						hrPhone = p
						log.Printf("[TELFED] found phone in paragraph: %s", p)
					}
				}
			}
		})
		// If no paragraphs, get all text
		if len(parts) == 0 {
			description = strings.TrimSpace(joinWS(contentSel.Text()))
		} else {
			description = strings.Join(parts, "\n\n")
		}

		// Try to find HR email inside the content (mailto links first)
		if hrEmail == "" {
			contentSel.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
				if hrEmail != "" {
					return
				}
				if href, ok := a.Attr("href"); ok {
					h := strings.TrimSpace(href)
					hh := strings.ToLower(h)
					if strings.Contains(hh, "mailto:") {
						email := normalizeMailto(h)
						if email == "" {
							// fallback to anchor text
							email = findEmailInText(a.Text())
						}
						if email != "" {
							hrEmail = email
							log.Printf("[TELFED] found email via content anchor: %s", email)
						}
					}
				}
			})
		}
		// Fallback: scan entire content text for email and phone patterns
		if hrEmail == "" {
			text := contentSel.Text()
			if m := extractEmailFromParagraph(text); m != "" {
				hrEmail = m
			}
		}
		if hrPhone == "" {
			text := contentSel.Text()
			if p := extractPhoneFromParagraph(text); p != "" {
				hrPhone = p
			}
		}
	}

	// Global fallbacks if still empty: look for any mailto or plain email across the entire document
	if strings.TrimSpace(hrEmail) == "" {
		doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			if hrEmail != "" {
				return
			}
			if href, ok := a.Attr("href"); ok {
				h := strings.TrimSpace(href)
				if strings.Contains(strings.ToLower(h), "mailto:") {
					email := normalizeMailto(h)
					if email == "" {
						email = findEmailInText(a.Text())
					}
					if email != "" {
						hrEmail = email
						log.Printf("[TELFED] found email via global anchor: %s", email)
					}
				}
			}
		})
	}
	if strings.TrimSpace(hrEmail) == "" {
		// Last resort: scan entire page text for email patterns
		pageText := doc.Text()
		if m := extractEmailFromParagraph(pageText); m != "" {
			hrEmail = m
			log.Printf("[TELFED] found email via page text: %s", m)
		}
	}
	if strings.TrimSpace(hrPhone) == "" {
		// Last resort: scan entire page text for phone patterns
		pageText := doc.Text()
		if p := extractPhoneFromParagraph(pageText); p != "" {
			hrPhone = p
			log.Printf("[TELFED] found phone via page text: %s", p)
		}
	}

	return models.ScrapedJob{
		Title:       title,
		Description: description,
		URL:         jobURL,
		HREmail:     strings.TrimSpace(hrEmail),
		HRPhone:     strings.TrimSpace(hrPhone),
	}
}

// extractEmailFromParagraph extracts email from text, handling patterns like "send CV to: email@domain.com"
func extractEmailFromParagraph(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Look for common patterns: "to:", "email:", "contact:", "at:", etc. followed by email
	lower := strings.ToLower(s)
	markers := []string{"to:", "email:", "contact:", "at:", "send to", "send your cv", "send cv"}
	for _, marker := range markers {
		idx := strings.Index(lower, marker)
		if idx >= 0 {
			// Extract text after the marker
			afterMarker := s[idx+len(marker):]
			afterMarker = strings.TrimSpace(afterMarker)
			// Find email in the text after marker
			if email := findEmailInText(afterMarker); email != "" {
				return email
			}
		}
	}
	// Fallback: scan entire text for any email
	return findEmailInText(s)
}

// findEmailInText finds the first valid-looking email address in text
func findEmailInText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Split by common delimiters
	parts := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\n', '\r', '\t', ',', ';', '(', ')', '[', ']', '{', '}', '<', '>', '"', '\'':
			return true
		}
		return false
	})
	for _, p := range parts {
		p = strings.TrimSpace(p)
		// Remove leading colons or other punctuation
		p = strings.TrimLeft(p, ":")
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Basic email validation: contains @, has characters before and after @
		if strings.Contains(p, "@") && !strings.HasPrefix(strings.ToLower(p), "mailto:") {
			// Strip trailing punctuation
			p = strings.TrimRight(p, ".,;:!?")
			// Basic check: at least something@something
			parts := strings.Split(p, "@")
			if len(parts) == 2 && len(parts[0]) > 0 && len(parts[1]) > 0 {
				// Check domain has at least one dot
				if strings.Contains(parts[1], ".") {
					return p
				}
			}
		}
	}
	return ""
}

// extractPhoneFromParagraph extracts phone number from text, handling various formats
func extractPhoneFromParagraph(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Look for tel: links first
	if strings.Contains(strings.ToLower(s), "tel:") {
		lower := strings.ToLower(s)
		idx := strings.Index(lower, "tel:")
		if idx >= 0 {
			afterTel := s[idx+len("tel:"):]
			afterTel = strings.TrimSpace(afterTel)
			// Split by space or other delimiters to get the phone
			parts := strings.FieldsFunc(afterTel, func(r rune) bool {
				return r == ' ' || r == '\n' || r == '\r' || r == '\t' || r == '<' || r == '>'
			})
			if len(parts) > 0 {
				phone := strings.TrimSpace(parts[0])
				phone = strings.Trim(phone, ".,;:!?\"'()[]{}")
				if isValidPhone(phone) {
					return phone
				}
			}
		}
	}
	// Look for common markers: "phone:", "tel:", "call:", "contact:", etc.
	lower := strings.ToLower(s)
	markers := []string{"phone:", "tel:", "call:", "contact:", "call us", "phone us", "tel us", "ליצור קשר", "טלפון"}
	for _, marker := range markers {
		idx := strings.Index(lower, marker)
		if idx >= 0 {
			afterMarker := s[idx+len(marker):]
			afterMarker = strings.TrimSpace(afterMarker)
			if phone := findPhoneInText(afterMarker); phone != "" {
				return phone
			}
		}
	}
	// Fallback: scan entire text for phone patterns
	return findPhoneInText(s)
}

// findPhoneInText finds the first valid-looking phone number in text
func findPhoneInText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// First, try to find tel: links (most reliable)
	if strings.Contains(strings.ToLower(s), "tel:") {
		lower := strings.ToLower(s)
		idx := strings.Index(lower, "tel:")
		if idx >= 0 {
			afterTel := s[idx+len("tel:"):]
			// Extract until space, newline, or quote
			phone := ""
			for _, r := range afterTel {
				if r == ' ' || r == '\n' || r == '\r' || r == '\t' || r == '"' || r == '\'' || r == '<' || r == '>' {
					break
				}
				phone += string(r)
			}
			phone = strings.TrimSpace(phone)
			if phone != "" && isValidPhone(phone) {
				return normalizePhone(phone)
			}
		}
	}
	// Remove common punctuation but keep digits, dashes, spaces, and plus
	cleaned := ""
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '-' || r == ' ' || r == '(' || r == ')' || r == '+' {
			cleaned += string(r)
		} else if r == '\n' || r == '\r' || r == '\t' {
			cleaned += " "
		}
	}
	// Split into potential phone number tokens
	parts := strings.Fields(cleaned)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		// Skip if starts with minus
		if strings.HasPrefix(p, "-") {
			continue
		}
		// Remove parentheses and normalize
		p = strings.ReplaceAll(p, "(", "")
		p = strings.ReplaceAll(p, ")", "")
		p = strings.TrimSpace(p)
		if p != "" && isValidPhone(p) {
			return normalizePhone(p)
		}
	}
	// Try extracting sequences of digits with separators (more strict)
	// Look for patterns like: 050-123-4567, 050 123 4567, etc.
	phone := extractPhonePattern(s)
	if phone != "" && isValidPhone(phone) {
		return normalizePhone(phone)
	}
	return ""
}

// isValidPhone checks if a string looks like a valid phone number
func isValidPhone(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Reject if starts with minus (likely a negative number or ID)
	if strings.HasPrefix(s, "-") {
		return false
	}
	// Count digits and extract digit-only version
	digits := 0
	digitsOnly := ""
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
			digitsOnly += string(r)
		}
	}
	// Israeli phone numbers: 9-10 digits local, or 12-13 with country code
	if digits < 8 || digits > 13 {
		return false
	}
	// Reject suspicious patterns
	if len(digitsOnly) >= 6 {
		// Reject if all digits are the same (like "1111111")
		allSame := true
		for i := 1; i < len(digitsOnly); i++ {
			if digitsOnly[i] != digitsOnly[0] {
				allSame = false
				break
			}
		}
		if allSame {
			return false
		}
		// Reject repeating patterns (like "10241024")
		if len(digitsOnly)%2 == 0 {
			half := len(digitsOnly) / 2
			if digitsOnly[:half] == digitsOnly[half:] {
				return false
			}
		}
		// Reject if too many zeros (likely not a phone)
		zeroCount := 0
		for _, d := range digitsOnly {
			if d == '0' {
				zeroCount++
			}
		}
		if zeroCount > len(digitsOnly)/2 {
			return false
		}
	}
	// Israeli mobile format: should start with 05X where X is 0-9
	if digits == 9 || digits == 10 {
		// Remove leading + or country code to check prefix
		checkStr := digitsOnly
		if strings.HasPrefix(checkStr, "972") && len(checkStr) >= 12 {
			checkStr = checkStr[3:] // Remove country code
		}
		if len(checkStr) >= 10 {
			// Israeli mobile should start with 05
			if strings.HasPrefix(checkStr, "05") {
				// Check second digit is 0-9
				if len(checkStr) >= 3 && checkStr[2] >= '0' && checkStr[2] <= '9' {
					return true
				}
			}
			// Israeli landline: starts with 02, 03, 04, 08, 09, or 077
			if strings.HasPrefix(checkStr, "02") || strings.HasPrefix(checkStr, "03") ||
				strings.HasPrefix(checkStr, "04") || strings.HasPrefix(checkStr, "08") ||
				strings.HasPrefix(checkStr, "09") || strings.HasPrefix(checkStr, "077") {
				return true
			}
		}
		// International format with country code
		if len(checkStr) >= 9 && strings.HasPrefix(checkStr, "972") {
			return true
		}
	}
	// For international numbers (with +), be more lenient but still validate
	if strings.HasPrefix(s, "+") && digits >= 10 && digits <= 13 {
		return true
	}
	return false
}

// normalizePhone normalizes phone number format
func normalizePhone(s string) string {
	s = strings.TrimSpace(s)
	// Remove all non-digit characters except + at the start
	normalized := ""
	hasPlus := strings.HasPrefix(s, "+")
	for _, r := range s {
		if r >= '0' && r <= '9' {
			normalized += string(r)
		} else if r == '+' && !hasPlus {
			normalized += string(r)
			hasPlus = true
		}
	}
	if normalized == "" {
		return s // fallback to original
	}
	// Format: if starts with +972 or 0, format as Israeli number
	if strings.HasPrefix(normalized, "+972") || strings.HasPrefix(normalized, "972") {
		// Israeli international format: +972-XX-XXX-XXXX
		if len(normalized) >= 12 {
			num := normalized[len(normalized)-9:]                      // last 9 digits
			area := normalized[len(normalized)-11 : len(normalized)-9] // area code
			return "+972-" + area + "-" + num[:3] + "-" + num[3:]
		}
	} else if strings.HasPrefix(normalized, "0") && len(normalized) == 10 {
		// Israeli local format: 0XX-XXX-XXXX
		return normalized[:3] + "-" + normalized[3:6] + "-" + normalized[6:]
	}
	// Return as-is if we can't normalize
	return s
}

// extractPhonePattern tries to extract phone patterns from text with separators
func extractPhonePattern(s string) string {
	// Look for patterns like: 050-123-4567, 050 123 4567, (050) 123-4567
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Try to find sequences that look like phone numbers with separators
	// Israeli format: 050-123-4567 or 050 123 4567
	// Extract digit groups separated by dashes/spaces
	inDigitGroup := false
	digitGroups := []string{}
	currentGroup := ""
	for _, r := range s {
		if r >= '0' && r <= '9' {
			currentGroup += string(r)
			inDigitGroup = true
		} else if (r == '-' || r == ' ' || r == '(' || r == ')') && inDigitGroup {
			if currentGroup != "" {
				digitGroups = append(digitGroups, currentGroup)
				currentGroup = ""
			}
			inDigitGroup = false
		} else if inDigitGroup {
			// End of digit group
			if currentGroup != "" {
				digitGroups = append(digitGroups, currentGroup)
				currentGroup = ""
			}
			inDigitGroup = false
		}
	}
	if currentGroup != "" {
		digitGroups = append(digitGroups, currentGroup)
	}
	// Look for patterns: 3 groups (area-code-number) - most common Israeli format
	if len(digitGroups) == 3 {
		// Israeli mobile: 050-XXX-XXXX (3-3-4)
		// Israeli landline: 02-XXX-XXXX or 03-XXX-XXXX (2-3-4 or 2-3-5)
		if len(digitGroups[0]) == 3 && len(digitGroups[1]) == 3 && len(digitGroups[2]) >= 4 {
			// Check if first group starts with 05 (mobile) or 02/03/04/08/09 (landline)
			if strings.HasPrefix(digitGroups[0], "05") || strings.HasPrefix(digitGroups[0], "02") ||
				strings.HasPrefix(digitGroups[0], "03") || strings.HasPrefix(digitGroups[0], "04") ||
				strings.HasPrefix(digitGroups[0], "08") || strings.HasPrefix(digitGroups[0], "09") {
				combined := strings.Join(digitGroups, "")
				if isValidPhone(combined) {
					return digitGroups[0] + "-" + digitGroups[1] + "-" + digitGroups[2]
				}
			}
		}
		// Landline format: 02-XXX-XXXX (2-3-4)
		if len(digitGroups[0]) == 2 && len(digitGroups[1]) == 3 && len(digitGroups[2]) >= 4 {
			if strings.HasPrefix(digitGroups[0], "02") || strings.HasPrefix(digitGroups[0], "03") ||
				strings.HasPrefix(digitGroups[0], "04") || strings.HasPrefix(digitGroups[0], "08") ||
				strings.HasPrefix(digitGroups[0], "09") {
				combined := strings.Join(digitGroups, "")
				if isValidPhone(combined) {
					return digitGroups[0] + "-" + digitGroups[1] + "-" + digitGroups[2]
				}
			}
		}
	}
	// Try 2 groups (less common but possible)
	if len(digitGroups) == 2 {
		combined := strings.Join(digitGroups, "")
		if isValidPhone(combined) {
			return combined
		}
	}
	return ""
}

// normalizeMailto takes an href string and extracts the email, removing mailto: and query strings
func normalizeMailto(href string) string {
	h := strings.TrimSpace(href)
	if h == "" {
		return ""
	}
	// strip leading scheme variations like MAILTO:
	idx := strings.Index(strings.ToLower(h), "mailto:")
	if idx >= 0 {
		h = h[idx+len("mailto:"):]
	}
	// drop query part if present
	if q := strings.Index(h, "?"); q >= 0 {
		h = h[:q]
	}
	h = strings.TrimSpace(h)
	// basic sanitation
	h = strings.Trim(h, "<> \t\r\n")
	if findEmailInText(h) != "" { // reuse validator
		return findEmailInText(h)
	}
	return ""
}

// extractJobsFromAnchors processes anchors in parallel and extracts valid job links.
func (t *TelfedScraper) extractJobsFromAnchors(
	ctx context.Context,
	anchors []pool.Anchor,
	baseURL *url.URL,
) ([]models.ScrapedJob, string) {
	group, _ := t.wp.GroupContext(ctx)

	type item struct {
		idx int
		job models.ScrapedJob
	}

	items := make([]item, 0, len(anchors))
	var mu sync.Mutex

	var foundNextPage string

	for i, a := range anchors {
		i, a := i, a
		href := strings.TrimSpace(a.Href)
		if href == "" {
			continue
		}

		group.Submit(func() error {
			// Resolve URL
			u := t.resolve(baseURL, href)
			if u == nil {
				return nil // skip invalid URLs
			}

			// pagination detection
			lowerTitle := strings.ToLower(strings.TrimSpace(joinWS(a.Text)))
			lowerHref := strings.ToLower(href)
			if strings.Contains(lowerTitle, "next") || strings.Contains(lowerTitle, "הבא") || strings.Contains(lowerHref, "page=") || strings.Contains(lowerHref, "/page/") {
				mu.Lock()
				if foundNextPage == "" {
					foundNextPage = u.String()
				}
				mu.Unlock()
				return nil
			}

			// Only accept job links under /jobs/
			if !strings.Contains(strings.ToLower(u.Path), "/jobs/") {
				return nil
			}

			title := strings.TrimSpace(joinWS(a.Text))
			if title == "" {
				title = t.fallbackTitle(u)
			}

			mu.Lock()
			items = append(items, item{
				idx: i,
				job: models.ScrapedJob{
					Title:       title,
					URL:         u.String(),
					Description: title,
					Company:     t.company.Name,
				},
			})
			mu.Unlock()
			return nil
		})
	}

	_ = group.Wait() // collect results even if some failed

	jobs := make([]models.ScrapedJob, 0, len(items))
	for _, it := range items {
		jobs = append(jobs, it.job)
	}

	return jobs, foundNextPage
}

// findNextPageFromAnchors looks for pagination links.
func (t *TelfedScraper) findNextPageFromAnchors(anchors []pool.Anchor, currentURL string) string {
	currentBase, _ := url.Parse(currentURL)

	for _, a := range anchors {
		href := strings.TrimSpace(a.Href)
		if href == "" {
			continue
		}

		text := strings.ToLower(strings.TrimSpace(joinWS(a.Text)))
		hrefLower := strings.ToLower(href)

		// Check for "next" indicators
		if strings.Contains(text, "next") ||
			strings.Contains(text, "הבא") ||
			strings.Contains(hrefLower, "page=") ||
			strings.Contains(hrefLower, "/page/") {
			if u := t.resolve(currentBase, href); u != nil {
				// Only return if it's different from current
				if u.String() != currentURL {
					return u.String()
				}
			}
		}
	}
	return ""
}

func (t *TelfedScraper) resolve(base *url.URL, href string) *url.URL {
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return nil
	}
	u := base.ResolveReference(ref)
	u.Fragment = ""
	return u
}

func (t *TelfedScraper) fallbackTitle(u *url.URL) string {
	if u == nil {
		return ""
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path == "" {
		return ""
	}
	// Extract last segment
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	// Clean up
	last = strings.ReplaceAll(last, "-", " ")
	last = strings.ReplaceAll(last, "_", " ")
	return strings.TrimSpace(last)
}

// extractFromHTML parses rendered HTML and extracts job links within UAEL post grid.
func (t *TelfedScraper) extractFromHTML(html string, current string, baseURL *url.URL) ([]models.ScrapedJob, string) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewBufferString(html))
	if err != nil {
		return nil, ""
	}

	// Prefer links under the UAEL post grid
	sel := doc.Find(".uael-post-grid__inner a[href], .uael-post-wrapper a[href]")
	seen := map[string]struct{}{}
	out := make([]models.ScrapedJob, 0, sel.Length())

	for i := range sel.Nodes {
		a := sel.Eq(i)
		href, ok := a.Attr("href")
		if !ok {
			continue
		}
		href = strings.TrimSpace(href)
		if href == "" {
			continue
		}
		if _, dup := seen[href]; dup {
			continue
		}
		seen[href] = struct{}{}

		u := t.resolve(baseURL, href)
		if u == nil {
			continue
		}
		// Only consider job posts under /jobs/
		if !strings.Contains(strings.ToLower(u.Path), "/jobs/") {
			continue
		}

		title := strings.TrimSpace(joinWS(a.Text()))
		if title == "" {
			parent := a.ParentsFiltered(".uael-post__content-wrap").First()
			if parent.Length() > 0 {
				if t2 := strings.TrimSpace(joinWS(parent.Find("h3.uael-post__title a").First().Text())); t2 != "" {
					title = t2
				}
			}
		}
		if title == "" {
			title = t.fallbackTitle(u)
		}

		out = append(out, models.ScrapedJob{
			Title:       title,
			URL:         u.String(),
			Description: title,
			Company:     t.company.Name,
		})
	}

	// Find next page link via common patterns or UAEL pagination
	var next string
	if a := doc.Find("a[rel=next], a.next, a.pagination-next").First(); a.Length() > 0 {
		if href, ok := a.Attr("href"); ok && strings.TrimSpace(href) != "" {
			next = resolveURLMust(current, href)
		}
	}
	if next == "" {
		// heuristic: any anchor with /page/ or page=
		doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			if next != "" {
				return
			}
			if href, ok := a.Attr("href"); ok {
				hl := strings.ToLower(strings.TrimSpace(href))
				if strings.Contains(hl, "/page/") || strings.Contains(hl, "page=") {
					next = resolveURLMust(current, href)
				}
			}
		})
	}

	return out, next
}
