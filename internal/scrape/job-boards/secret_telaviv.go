package scrape

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/scrape/common"
	"jf/internal/utils"
	"jf/internal/validators"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// SecretTelAviv scraper for jobs.secrettelaviv.com pages.
type SecretTelAviv struct {
	company models.Company
	client  common.Doer
}

func NewSecretTelAviv(c models.Company, client common.Doer) *SecretTelAviv {
	return &SecretTelAviv{
		company: c,
		client:  common.EnsureClient(client),
	}
}

func (s *SecretTelAviv) GetJobs(ctx context.Context, cfg *config.Config) ([]models.ScrapedJob, error) {
	start := strings.TrimSpace(s.company.CareersURL)
	if start == "" {
		return nil, nil
	}

	// prepare filters once
	good, bad := cfg.GoodBadKeywordSets()
	thr := cfg.HeuristicThreshold
	hardExcl := cfg.HardExcludeOnBad
	baseURL, _ := url.Parse(start)

	parsePage := func(doc *goquery.Document) []models.ScrapedJob {
		var out []models.ScrapedJob
		now := time.Now()

		doc.Find("div.wpjb-grid-row").Each(func(_ int, row *goquery.Selection) {
			a := row.Find("div.wpjb-col-title a").First()
			if a.Length() == 0 {
				return
			}
			title := strings.TrimSpace(utils.JoinWS(a.Text()))
			href, _ := a.Attr("href")
			if title == "" || strings.TrimSpace(href) == "" {
				return
			}
			if baseURL != nil && !validators.MustJobLink(title, href, baseURL, good, bad, thr, hardExcl) {
				return
			}

			dateRaw := strings.TrimSpace(row.Find("div.wpjb-grid-col-right span.wpjb-line-major").First().Text())
			// 1) date filter: ignore if older than ~1 month (31 days)
			if ts, ok := parseDateSTA(dateRaw, now.Location()); ok {
				if now.Sub(ts) > 31*24*time.Hour {
					return
				}
			}

			out = append(out, models.ScrapedJob{
				Title:       title,
				URL:         href,
				Description: "", // Will be extracted in fetchJobMetadata or fallback to title in board_source
				Company:     s.company.Name,
				DatePosted:  dateRaw,
			})
		})
		return out
	}

	var all []models.ScrapedJob

	// walk the pagination via "Next"
	next := start
	for next != "" {
		doc, err := s.fetchDoc(ctx, next)
		if err != nil {
			break // best-effort: stop on failure
		}
		items := parsePage(doc)

		// 2) per-job inactive check + metadata extraction (lightweight GET + DOM probe)
		filtered := make([]models.ScrapedJob, 0, len(items))
		for _, j := range items {
			ok, inactive, companyName, postedDate, description, location := s.fetchJobMetadata(ctx, j.URL)
			if !ok || inactive {
				if inactive {
					continue
				}
				// If fetch failed, still include the job but keep original metadata
				filtered = append(filtered, j)
			} else {
				// Update job with extracted metadata
				if companyName != "" {
					j.Company = companyName
				}
				if postedDate != "" {
					j.DatePosted = postedDate
				}
				if description != "" {
					j.Description = description
				}
				if location != "" {
					j.Location = location
				}
				filtered = append(filtered, j)
			}

			// tiny delay to be polite between item checks
			select {
			case <-time.After(120 * time.Millisecond):
			case <-ctx.Done():
				return utils.DedupeScraped(append(all, filtered...)), ctx.Err()
			}
		}

		all = append(all, filtered...)

		// polite tiny delay to avoid hammering
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return utils.DedupeScraped(all), ctx.Err()
		}
		next = findNext(doc, next)
	}

	return utils.DedupeScraped(all), nil
}

// findNext returns the absolute URL of the next page or "" if none.
func findNext(doc *goquery.Document, base string) string {
	a := doc.Find("a.next.page-numbers").First()
	if a.Length() == 0 {
		return ""
	}
	href, ok := a.Attr("href")
	if !ok || strings.TrimSpace(href) == "" {
		return ""
	}
	if strings.HasPrefix(href, "http") {
		return href
	}
	return utils.ResolveURLMust(base, href)
}

// fetchDoc gets the URL and returns a parsed document or an error.
func (s *SecretTelAviv) fetchDoc(ctx context.Context, u string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, utils.NewNetworkError("failed to create request", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, utils.NewNetworkError("failed to fetch document", err)
	}

	if resp == nil {
		return nil, utils.NewNetworkError("received nil response", nil)
	}

	defer utils.SafeClose(resp.Body, "response body")

	if resp.StatusCode != http.StatusOK {
		return nil, utils.NewNetworkError(fmt.Sprintf("HTTP %d", resp.StatusCode), nil)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, utils.NewParsingError("failed to parse HTML document", err)
	}

	return doc, nil
}

// fetchJobMetadata GETs the job page, extracts metadata, and checks if inactive.
// Returns (ok, inactive, companyName, postedDate, description, location).
func (s *SecretTelAviv) fetchJobMetadata(ctx context.Context, jobURL string) (bool, bool, string, string, string, string) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, jobURL, nil)
	resp, err := s.client.Do(req)
	if err != nil || resp == nil {
		return false, false, "", "", "", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// non-200 often means gone/redirect/etc. Treat as inconclusive; keep the job.
		return true, false, "", "", "", ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, false, "", "", "", ""
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return false, false, "", "", "", ""
	}

	// Look inside .post-content for the error box text
	errBox := strings.ToLower(strings.TrimSpace(
		doc.Find(".post-content .wpjb-flash-error").Text(),
	))
	if errBox == "" {
		// Some themes wrap the text inside a <span> — be tolerant:
		errBox = strings.ToLower(strings.TrimSpace(
			doc.Find(".post-content .wpjb-flash-error span").Text(),
		))
	}

	// Check for inactive status
	if errBox != "" {
		// key phrase from the site:
		if strings.Contains(errBox, "selected job is inactive or does not exist") {
			return true, true, "", "", "", ""
		}
	}

	// Check if the page has the job board structure (wpjb-page-single is required for active jobs)
	// Inactive jobs like bluespine-automation-engineer have empty post-content without this div
	hasJobStructure := doc.Find(".post-content .wpjb.wpjb-job.wpjb-page-single").Length() > 0
	if !hasJobStructure {
		// This is likely an inactive job (empty post-content, no job board structure)
		return true, true, "", "", "", ""
	}

	// Extract company name and posted date from .wpjb-top-header-title
	// Format: "Lusha 9 active jobs (view) Published: October 27, 2025"
	headerTitleText := strings.TrimSpace(doc.Find(".wpjb-top-header-title").First().Text())
	var companyName, postedDate string

	if headerTitleText != "" {
		// Extract date: look for "Published: " prefix
		if idx := strings.Index(headerTitleText, "Published: "); idx != -1 {
			postedDate = strings.TrimSpace(headerTitleText[idx+len("Published: "):])
			// Extract company name from the part before "Published: "
			beforePublished := strings.TrimSpace(headerTitleText[:idx])
			// Extract first word (company name) - it's typically before numbers or "active jobs"
			// Split by space and take the first token
			parts := strings.Fields(beforePublished)
			if len(parts) > 0 {
				companyName = parts[0]
			}
		} else {
			// Fallback: if no "Published:" found, try to extract company name from first word
			parts := strings.Fields(headerTitleText)
			if len(parts) > 0 {
				companyName = parts[0]
			}
		}
	}

	// Extract description from .wpjb-text div
	// Only extract the REQUIREMENTS section if it exists, otherwise use full text
	var description string
	descriptionElem := doc.Find(".wpjb-text").First()
	if descriptionElem.Length() > 0 {
		fullText := strings.TrimSpace(utils.JoinWS(descriptionElem.Text()))

		// Check if REQUIREMENTS section exists (case-insensitive)
		upperText := strings.ToUpper(fullText)
		reqsIndex := strings.Index(upperText, "REQUIREMENTS")
		if reqsIndex != -1 {
			// Extract from REQUIREMENTS onwards
			description = strings.TrimSpace(fullText[reqsIndex:])
		} else {
			// No REQUIREMENTS section found, use full text
			description = fullText
		}
	}

	// Extract location from .wpjb-row-meta-location_stlv .wpjb-col-60
	// This selector directly targets the location value (e.g., "Tel Aviv/ Ramat Gan")
	var location string
	locationElem := doc.Find(".wpjb-row-meta-location_stlv .wpjb-col-60").First()
	if locationElem.Length() > 0 {
		location = strings.TrimSpace(utils.JoinWS(locationElem.Text()))
	} else {
		// Fallback: try alternative selector structure
		locationElem = doc.Find(".wpjb-grid-row.wpjb-row-meta-location_stlv .wpjb-col-60").First()
		if locationElem.Length() > 0 {
			location = strings.TrimSpace(utils.JoinWS(locationElem.Text()))
		}
	}

	return true, false, companyName, postedDate, description, location
}

// isInactive GETs the job page and detects the WPJB inactive message.
// Returns (ok, inactive).
func (s *SecretTelAviv) isInactive(ctx context.Context, jobURL string) (bool, bool) {
	ok, inactive, _, _, _, _ := s.fetchJobMetadata(ctx, jobURL)
	return ok, inactive
}

// ApplyJobs: actually submits Secret Tel Aviv application forms for given jobs.
// Required cfg.ApplyForm fields: CVPath, FirstName, LastName, Email, Phone, AgreeTOS, ForwardToHeadhunters.
func (s *SecretTelAviv) ApplyJobs(ctx context.Context, jobs []models.Job, cfg *config.Config) ([]models.AppliedResult, error) {
	p := cfg.ApplyForm

	// --- Path normalization & validation (handles ~, $HOME, backslashes on Linux, etc) ---
	cvRaw := strings.TrimSpace(p.CVPath)
	cv, normErr := normalizePath(cvRaw)
	log.Printf("[STA] config CV raw=%q normalized=%q goos=%s", cvRaw, cv, runtime.GOOS)
	if normErr != nil {
		return nil, fmt.Errorf("cv path invalid: %v", normErr)
	}
	st, err := os.Stat(cv)
	if err != nil {
		return nil, fmt.Errorf("cv not found: %s (stat err: %v)", cv, err)
	}
	if st.IsDir() {
		return nil, fmt.Errorf("cv path is a directory: %s", cv)
	}
	if !p.AgreeTOS {
		return nil, fmt.Errorf("agree_tos must be true for submission")
	}

	// Use whatever client was injected into the scraper (http.Client or your httpx.Doer).
	type doer interface {
		Do(*http.Request) (*http.Response, error)
	}
	cl, _ := s.client.(doer)
	if cl == nil {
		return nil, fmt.Errorf("http client not set")
	}

	timeout := 45 * time.Second
	results := make([]models.AppliedResult, len(jobs))

	for i := range jobs {
		j := jobs[i]
		out := models.AppliedResult{JobID: j.ID, URL: j.URL, Title: j.Title}

		log.Printf("[STA] apply start id=%s url=%s title=%q cv=%q tos=%v headhunters=%v",
			j.ID, j.URL, j.Title, cv, p.AgreeTOS, p.ForwardToHeadhunters)

		u := strings.TrimSpace(j.URL)
		if u == "" {
			out.Message = "missing job url"
			results[i] = out
			log.Printf("[STA] id=%s error=%s", j.ID, out.Message)
			continue
		}

		// GET job page (IMPORTANT: do not cancel before fully reading/parsing)
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
		// Do NOT set headers like User-Agent or Referer here; your client module handles it.

		resp, err := cl.Do(req)
		if err != nil {
			out.Message = "HTTP error: " + err.Error()
			results[i] = out
			log.Printf("[STA] id=%s GET error=%v", j.ID, err)
			continue
		}
		out.Status = resp.StatusCode

		ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
		clen := strings.TrimSpace(resp.Header.Get("Content-Length"))
		log.Printf("[STA] id=%s GET status=%d ct=%q len=%s", j.ID, resp.StatusCode, ct, clen)

		// Read body fully, then parse from memory so context cancel won't break parsing.
		bb, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			out.Message = "read error: " + readErr.Error()
			results[i] = out
			log.Printf("[STA] id=%s GET read error=%v", j.ID, readErr)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			out.Message = fmt.Sprintf("GET failed (%d)", resp.StatusCode)
			results[i] = out
			log.Printf("[STA] id=%s %s", j.ID, out.Message)
			continue
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(bb))
		if err != nil {
			preview := strings.ToLower(string(bb))
			if len(preview) > 240 {
				preview = preview[:240] + "…"
			}
			out.Message = "parse error: " + err.Error()
			results[i] = out
			log.Printf("[STA] id=%s parse error=%v bodyPreview=%q", j.ID, err, preview)
			continue
		}

		form := doc.Find("form#wpjb-apply-form").First()
		if form.Length() == 0 {
			out.Message = "apply form not found"
			results[i] = out
			log.Printf("[STA] id=%s %s", j.ID, out.Message)
			continue
		}

		action := strings.TrimSpace(attr(form, "action", j.URL))
		action = resolve(j.URL, action)
		method := strings.ToLower(strings.TrimSpace(attr(form, "method", "post")))
		log.Printf("[STA] id=%s form action=%s method=%s", j.ID, action, method)
		if method != "post" {
			out.Message = "unexpected form method: " + method
			results[i] = out
			log.Printf("[STA] id=%s %s", j.ID, out.Message)
			continue
		}

		// Collect fields (keep hidden defaults)
		vals := url.Values{}
		form.Find("input").Each(func(_ int, sel *goquery.Selection) {
			name, ok := sel.Attr("name")
			if !ok || strings.TrimSpace(name) == "" {
				return
			}
			typ := strings.ToLower(strings.TrimSpace(attr(sel, "type", "")))
			switch typ {
			case "checkbox", "radio", "file":
				return
			default:
				vals.Set(name, attr(sel, "value", ""))
			}
		})
		if v := strings.TrimSpace(vals.Get("_wpjb_action")); v == "" {
			vals.Set("_wpjb_action", "apply")
		}
		if v := strings.TrimSpace(vals.Get("_job_id")); v == "" {
			if hid := form.Find(`input[name="_job_id"]`).First(); hid.Length() > 0 {
				vals.Set("_job_id", attr(hid, "value", ""))
			}
		}

		// Fill visible fields
		vals.Set("applicant_name", p.FirstName)
		vals.Set("applicant_surname", p.LastName)
		vals.Set("email", p.Email)
		vals.Set("telephone_number_apply_form", p.Phone)

		// Required ToS checkbox (similar_employers_cv[])
		if inp := form.Find(`input[name="similar_employers_cv[]"]`).First(); inp.Length() > 0 {
			vals.Set("similar_employers_cv[]", attr(inp, "value", "1"))
		} else {
			vals.Set("similar_employers_cv[]", "1")
		}
		// Optional headhunters
		if p.ForwardToHeadhunters {
			if inp := form.Find(`input[name="headhunters[]"]`).First(); inp.Length() > 0 {
				vals.Set("headhunters[]", attr(inp, "value", "1"))
			} else {
				vals.Set("headhunters[]", "1")
			}
		}

		// Detect file field name
		fileField := ""
		if f := form.Find(`input[type="file"]`).First(); f.Length() > 0 {
			if n, ok := f.Attr("name"); ok && strings.TrimSpace(n) != "" {
				fileField = n
			}
		}
		candidates := []string{}
		if fileField != "" {
			candidates = append(candidates, fileField)
		}
		candidates = append(candidates, "file", "cv", "resume")
		log.Printf("[STA] id=%s file candidates=%v", j.ID, candidates)

		// Try posting with candidates until one works
		var postErr error
		var ok bool
		var msg string
		for _, ff := range candidates {
			ok, msg, postErr = s.trySTAApply(ctx, action, j.URL, timeout, vals, ff, cv, cl, j.ID)
			if postErr == nil {
				break
			}
			log.Printf("[STA] id=%s POST with field=%q error=%v", j.ID, ff, postErr)
		}
		if postErr != nil {
			out.Message = "HTTP error: " + postErr.Error()
			results[i] = out
			log.Printf("[STA] id=%s final error=%v", j.ID, postErr)
			continue
		}

		out.OK = ok
		out.Message = msg
		results[i] = out
		log.Printf("[STA] apply done id=%s ok=%v msg=%q", j.ID, out.OK, out.Message)
	}

	return results, nil
}

func (s *SecretTelAviv) trySTAApply(
	ctx context.Context,
	actionURL string,
	referer string, // kept for logging only
	timeout time.Duration,
	vals url.Values,
	fileField string,
	cvPath string,
	cl interface {
		Do(*http.Request) (*http.Response, error)
	},
	jobID string,
) (bool, string, error) {
	body, ctype, err := buildMultipart(vals, fileField, cvPath)
	if err != nil {
		return false, "", err
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, actionURL, body)
	// Do NOT set UA/Referer headers here; your client module handles that.
	// We must set Content-Type to include the multipart boundary:
	req.Header.Set("Content-Type", ctype)

	log.Printf("[STA] id=%s POST url=%s fileField=%q size=%d referer=%s", jobID, actionURL, fileField, body.Len(), referer)

	resp, err := cl.Do(req)
	if err != nil {
		return false, "", err
	}
	bb, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	preview := strings.ToLower(string(bb))
	if len(preview) > 320 {
		preview = preview[:320] + "…"
	}
	log.Printf("[STA] id=%s POST status=%d bodyPreview=%q", jobID, resp.StatusCode, preview)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect:
		switch {
		case strings.Contains(preview, "application sent"),
			strings.Contains(preview, "thank you"),
			strings.Contains(preview, "successfully"),
			strings.Contains(preview, "applied"):
			return true, "Submitted", nil
		case strings.Contains(preview, "error"),
			strings.Contains(preview, "invalid"),
			strings.Contains(preview, "required"):
			return false, "Submitted but error text detected", nil
		default:
			return true, "Submitted (no explicit confirmation found)", nil
		}
	default:
		return false, "", fmt.Errorf("POST failed (%d)", resp.StatusCode)
	}
}

func buildMultipart(values url.Values, fileField, filePath string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	for k, vs := range values {
		for _, v := range vs {
			if err := w.WriteField(k, v); err != nil {
				_ = w.Close()
				return nil, "", err
			}
		}
	}

	fw, err := w.CreateFormFile(fileField, filepath.Base(filePath))
	if err != nil {
		_ = w.Close()
		return nil, "", err
	}
	f, err := os.Open(filePath)
	if err != nil {
		_ = w.Close()
		return nil, "", err
	}
	_, err = io.Copy(fw, f)
	_ = f.Close()
	if err != nil {
		_ = w.Close()
		return nil, "", err
	}

	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}

func attr(sel *goquery.Selection, name, def string) string {
	if v, ok := sel.Attr(name); ok {
		return v
	}
	return def
}

func resolve(base, href string) string {
	if href == "" {
		return base
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	bu, err1 := url.Parse(base)
	hu, err2 := url.Parse(href)
	if err1 == nil && err2 == nil {
		return bu.ResolveReference(hu).String()
	}
	return href
}

func drain(rc io.ReadCloser) error {
	_, _ = io.Copy(io.Discard, rc)
	return rc.Close()
}

// normalizePath fixes common issues in user-supplied paths:
// - expands env vars ($HOME) and ~
// - on non-Windows, converts backslashes to slashes
// - Clean + Abs for stability
func normalizePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty")
	}
	p = os.ExpandEnv(p)
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		if home != "" {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	// If running on non-windows and path uses backslashes, flip them.
	if runtime.GOOS != "windows" && strings.Contains(p, `\`) {
		p = strings.ReplaceAll(p, `\`, `/`)
	}
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		if ap, err := filepath.Abs(p); err == nil {
			p = ap
		}
	}
	return p, nil
}

// parseDateSTA parses SecretTelAviv list dates like:
//   - "2 days ago", "3 weeks ago", "1 month ago"
//   - "January 2, 2006", "02/01/2006", "2006-01-02"
//
// Returns (timestamp, true) if parsed, otherwise (zero, false).
func parseDateSTA(s string, loc *time.Location) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	str := strings.ToLower(strings.TrimSpace(s))
	now := time.Now().In(loc)

	// relative forms
	fields := strings.Fields(str)
	if len(fields) >= 3 && fields[2] == "ago" {
		n := toInt(fields[0])
		unit := strings.TrimSuffix(fields[1], "s")
		if n > 0 {
			switch unit {
			case "day":
				return now.AddDate(0, 0, -n), true
			case "week":
				return now.AddDate(0, 0, -7*n), true
			case "month":
				return now.AddDate(0, -n, 0), true
			}
		}
	}

	// absolute common formats
	layouts := []string{
		time.RFC3339,      // 2006-01-02T15:04:05Z07:00
		"2006-01-02",      // 2006-01-02
		"02/01/2006",      // 02/01/2006 (dd/mm/yyyy) — sometimes appears on WP themes
		"01/02/2006",      // 01/02/2006 (mm/dd/yyyy) — be lenient
		"January 2, 2006", // January 2, 2006
		"2 January 2006",  // 2 January 2006
		"Jan 2, 2006",     // Jan 2, 2006
		"02 Jan 2006",     // 02 Jan 2006
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func toInt(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// GetJobPosted extracts the posted date from a job URL.
// Returns the posted date in a human-readable format, or empty string if not found.
func (s *SecretTelAviv) GetJobPosted(ctx context.Context, jobURL string) (string, error) {
	_, _, _, postedDate, _, _ := s.fetchJobMetadata(ctx, jobURL)
	return postedDate, nil
}
