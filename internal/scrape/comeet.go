package scrape

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"

	"jf/internal/pool"
	util "jf/internal/utils"
)

// Small, reusable Comeet helper kept in the scrape package.
type ComeetClient struct {
	http Doer
	wp   *pool.WorkerPool // optional: used by DiscoverAll/FetchAll
}

// NewComeet creates a client using your shared Doer (falls back to default client).
func NewComeet(client Doer) *ComeetClient {
	return &ComeetClient{
		http: ensureClient(client),
	}
}

// WithPool attaches a worker pool (optional, for bulk ops).
func (c *ComeetClient) WithPool(wp *pool.WorkerPool) *ComeetClient {
	c.wp = wp
	return c
}

/********** Single-item APIs **********/

// Discover returns absolute, canonicalized Comeet job links found on careersURL.
func (c *ComeetClient) Discover(ctx context.Context, careersURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, careersURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %d", careersURL, resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	base, _ := url.Parse(careersURL)
	seen := make(map[string]struct{}, 32)
	out := make([]string, 0, 32)

	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href := strings.TrimSpace(util.Attr(a, "href", ""))
		// Minimal filter: resolve then check host/path
		u, ok := util.ResolveAgainst(base, href)
		if !ok {
			return
		}
		u.Fragment = ""
		if !isComeetJobURL(u) {
			return
		}
		abs := util.CanonURL(u.String())
		key := strings.ToLower(abs)
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		out = append(out, abs)
	})

	return out, nil
}

// tiny tag-stripper (good enough for our small fragments)
var reTags = regexp.MustCompile(`(?s)<[^>]+>`)

// Fetch extracts (title, PLAIN-TEXT Requirements) from a Comeet job URL.
func (c *ComeetClient) Fetch(ctx context.Context, jobURL string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jobURL, nil)
	if err != nil {
		return "", "", err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GET %s: %d", jobURL, resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", "", err
	}

	// 1) Try to find the Requirements block rendered near an <h3>/<b>/<strong> with “Requirements”
	var reqHTML string
	doc.Find("h3.smallTitle, h3.positionSubTitle, h3, strong, b").EachWithBreak(func(_ int, h *goquery.Selection) bool {
		ht := strings.ToLower(strings.TrimSpace(h.Text()))
		if strings.Contains(ht, "requirement") {
			// Comeet often puts content in a sibling/parent .company-description or list/paragraphs after the heading
			if n := h.Parent().Find("div.userDesignedContent.company-description").First(); n.Length() > 0 {
				if s, e := n.Html(); e == nil && strings.TrimSpace(s) != "" {
					reqHTML = s
					return false
				}
			}
			if n := h.NextAllFiltered("div.userDesignedContent.company-description, ul, ol, p").First(); n.Length() > 0 {
				if s, e := n.Html(); e == nil && strings.TrimSpace(s) != "" {
					reqHTML = s
					return false
				}
			}
		}
		return true
	})

	// 2) Fallback for client-side pages: use JSON-LD description and slice only the Requirements section.
	if strings.TrimSpace(reqHTML) == "" {
		if full := descriptionFromJSONLD(doc); strings.TrimSpace(full) != "" {
			if sl := sliceRequirementsFromHTML(full); strings.TrimSpace(sl) != "" {
				reqHTML = sl
			}
		}
	}

	// 3) Very reliable on Comeet: POSITION_DATA inline JSON → take only the "Requirements" detail
	if strings.TrimSpace(reqHTML) == "" {
		if s := requirementsFromPositionData(doc); strings.TrimSpace(s) != "" {
			reqHTML = s
		}
	}

	// 4) Last resort: first .company-description on the page (may include more than Requirements)
	if strings.TrimSpace(reqHTML) == "" {
		if n := doc.Find("div.userDesignedContent.company-description").First(); n.Length() > 0 {
			if s, _ := n.Html(); strings.TrimSpace(s) != "" {
				reqHTML = s
			}
		}
	}

	// Strip tags → plain text
	plain := strings.TrimSpace(reqHTML)
	if plain != "" {
		plain = reTags.ReplaceAllString(plain, " ")
		plain = html.UnescapeString(plain)
		plain = collapseWS(plain)
	}

	// Title (heuristics)
	title := titleFromComeetURL(jobURL)
	if title == "" || looksLikeComeetCode(title) {
		if og, ok := doc.Find(`meta[property="og:title"]`).Attr("content"); ok && strings.TrimSpace(og) != "" {
			title = strings.TrimSpace(og)
		} else {
			if h := strings.TrimSpace(doc.Find("h1, h2").First().Text()); h != "" {
				title = h
			}
		}
	}

	return title, plain, nil
}

// Old fallback kept for reference / parity.
func (c *ComeetClient) FetchOldOld(ctx context.Context, jobURL string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jobURL, nil)
	if err != nil {
		return "", "", err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GET %s: %d", jobURL, resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", "", err
	}

	// Title
	title := titleFromComeetURL(jobURL)
	if title == "" || looksLikeComeetCode(title) {
		if og, ok := doc.Find(`meta[property="og:title"]`).Attr("content"); ok && strings.TrimSpace(og) != "" {
			title = strings.TrimSpace(og)
		} else if h := strings.TrimSpace(doc.Find("h1, h2").First().Text()); h != "" {
			title = h
		}
	}

	// 1) Try static DOM:
	htmlDesc := strings.TrimSpace(firstCompanyDescriptionHTML(doc))

	// 2) POSITION_DATA
	if htmlDesc == "" {
		if s := descriptionFromPositionData(doc); strings.TrimSpace(s) != "" {
			htmlDesc = strings.TrimSpace(s)
		}
	}

	// 3) JSON-LD
	if htmlDesc == "" {
		if s := descriptionFromJSONLD(doc); strings.TrimSpace(s) != "" {
			htmlDesc = strings.TrimSpace(s)
		}
	}

	// 4) Meta description
	if htmlDesc == "" {
		if og, ok := doc.Find(`meta[property="og:description"]`).Attr("content"); ok && strings.TrimSpace(og) != "" {
			htmlDesc = "<p>" + html.EscapeString(strings.TrimSpace(og)) + "</p>"
		} else if md, ok := doc.Find(`meta[name="description"]`).Attr("content"); ok && strings.TrimSpace(md) != "" {
			htmlDesc = "<p>" + html.EscapeString(strings.TrimSpace(md)) + "</p>"
		}
	}

	return title, strings.TrimSpace(htmlDesc), nil
}

/********** helpers **********/

// collapseWS collapses runs of whitespace to single spaces.
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Keep only the content under a "Requirements" heading from an HTML fragment.
// Looks for <h3>/<b>/<strong> with "Requirements" (case-insensitive), then
// captures until the next heading or end.
func sliceRequirementsFromHTML(htmlFrag string) string {
	re := regexp.MustCompile(`(?is)(?:<h3[^>]*>\s*requirements\s*</h3>|<b[^>]*>\s*requirements\s*</b>|<strong[^>]*>\s*requirements\s*</strong>)(.*?)(?:<h3[^>]*>|<b[^>]*>|<strong[^>]*>|$)`)
	m := re.FindStringSubmatch(htmlFrag)
	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// Try the static DOM block (usually empty on Comeet unless pre-rendered)
func firstCompanyDescriptionHTML(doc *goquery.Document) string {
	// Prefer block near an <h3> 'requirement', else first company-description
	var out string
	doc.Find("h3.smallTitle, h3.positionSubTitle, h3").EachWithBreak(func(_ int, h *goquery.Selection) bool {
		ht := strings.ToLower(strings.TrimSpace(h.Text()))
		if strings.Contains(ht, "requirement") {
			if n := h.Parent().Find("div.userDesignedContent.company-description").First(); n.Length() > 0 {
				if s, e := n.Html(); e == nil && strings.TrimSpace(s) != "" {
					out = s
					return false
				}
			}
			if n := h.NextAllFiltered("div.userDesignedContent.company-description").First(); n.Length() > 0 {
				if s, e := n.Html(); e == nil && strings.TrimSpace(s) != "" {
					out = s
					return false
				}
			}
		}
		return true
	})
	if strings.TrimSpace(out) != "" {
		return out
	}
	if n := doc.Find("div.userDesignedContent.company-description").First(); n.Length() > 0 {
		if s, _ := n.Html(); strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// Parse POSITION_DATA = {...}; and build an ordered HTML from custom_fields.details
func descriptionFromPositionData(doc *goquery.Document) string {
	re := regexp.MustCompile(`(?s)POSITION_DATA\s*=\s*(\{.*\})\s*;`)
	var raw string
	doc.Find("script").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		txt := s.Text()
		m := re.FindStringSubmatch(txt)
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			raw = m[1]
			return false
		}
		return true
	})
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	type detail struct {
		Name  string `json:"name"`
		Value string `json:"value"`
		Order int    `json:"order"`
	}
	type customFields struct {
		Details []detail `json:"details"`
	}
	type positionData struct {
		Name         string       `json:"name"`
		CustomFields customFields `json:"custom_fields"`
	}

	var pd positionData
	if err := json.Unmarshal([]byte(raw), &pd); err != nil {
		return ""
	}
	if len(pd.CustomFields.Details) == 0 {
		return ""
	}

	// Order by 'order'
	dets := append([]detail(nil), pd.CustomFields.Details...)
	sort.SliceStable(dets, func(i, j int) bool { return dets[i].Order < dets[j].Order })

	// Build combined HTML: <h3>{Name}</h3>{Value}
	var b strings.Builder
	for _, d := range dets {
		n := strings.TrimSpace(d.Name)
		v := strings.TrimSpace(d.Value)
		if v == "" {
			continue
		}
		if n != "" {
			fmt.Fprintf(&b, "<h3>%s</h3>\n", html.EscapeString(n))
		}
		b.WriteString(v)
		if !strings.HasSuffix(v, "\n") {
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}

// Parse POSITION_DATA and return only the "Requirements" section's HTML.
func requirementsFromPositionData(doc *goquery.Document) string {
	re := regexp.MustCompile(`(?s)POSITION_DATA\s*=\s*(\{.*\})\s*;`)
	var raw string
	doc.Find("script").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		txt := s.Text()
		if m := re.FindStringSubmatch(txt); len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			raw = m[1]
			return false
		}
		return true
	})
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	type detail struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	type customFields struct {
		Details []detail `json:"details"`
	}
	type positionData struct {
		CustomFields customFields `json:"custom_fields"`
	}

	var pd positionData
	if json.Unmarshal([]byte(raw), &pd) != nil || len(pd.CustomFields.Details) == 0 {
		return ""
	}
	for _, d := range pd.CustomFields.Details {
		if strings.Contains(strings.ToLower(strings.TrimSpace(d.Name)), "requirement") {
			return strings.TrimSpace(d.Value)
		}
	}
	return ""
}

// Parse JSON-LD JobPosting.description (already HTML)
func descriptionFromJSONLD(doc *goquery.Document) string {
	var out string
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		txt := strings.TrimSpace(s.Text())
		if txt == "" {
			return true
		}
		// Try object first
		var obj map[string]any
		if json.Unmarshal([]byte(txt), &obj) == nil {
			if strings.EqualFold(fmt.Sprint(obj["@type"]), "JobPosting") {
				if d, ok := obj["description"].(string); ok && strings.TrimSpace(d) != "" {
					out = strings.TrimSpace(d)
					return false
				}
			}
		}
		// Try array of objects
		var arr []map[string]any
		if json.Unmarshal([]byte(txt), &arr) == nil {
			for _, o := range arr {
				if strings.EqualFold(fmt.Sprint(o["@type"]), "JobPosting") {
					if d, ok := o["description"].(string); ok && strings.TrimSpace(d) != "" {
						out = strings.TrimSpace(d)
						return false
					}
				}
			}
		}
		return true
	})
	return out
}

/********** Bulk APIs using WorkerPool **********/

// DiscoverAll scans multiple careers pages concurrently and returns a unique set of Comeet job links.
func (c *ComeetClient) DiscoverAll(ctx context.Context, careersURLs []string) ([]string, error) {
	if len(careersURLs) == 0 {
		return nil, nil
	}

	// Use injected pool if present; otherwise create a short-lived one.
	wp := c.wp
	local := false
	if wp == nil {
		wp = pool.NewWorkerPool(8, len(careersURLs))
		local = true
	}
	if local {
		defer wp.Stop()
	}

	var mu sync.Mutex
	seen := make(map[string]struct{}, len(careersURLs)*8)
	out := make([]string, 0, len(careersURLs)*8)

	var wg sync.WaitGroup
	wg.Add(len(careersURLs))
	for _, root := range careersURLs {
		root := root
		wp.Submit(func() {
			defer wg.Done()
			links, err := c.Discover(ctx, root)
			if err != nil || len(links) == 0 {
				return
			}
			mu.Lock()
			for _, l := range links {
				k := strings.ToLower(l)
				if _, ok := seen[k]; ok {
					continue
				}
				seen[k] = struct{}{}
				out = append(out, l)
			}
			mu.Unlock()
		})
	}
	wg.Wait()
	return out, nil
}

type FetchResult struct {
	URL   string
	Title string
	HTML  string
	Err   error
}

// FetchAll fetches many Comeet job pages concurrently.
func (c *ComeetClient) FetchAll(ctx context.Context, jobURLs []string) []FetchResult {
	if len(jobURLs) == 0 {
		return nil
	}

	wp := c.wp
	local := false
	if wp == nil {
		wp = pool.NewWorkerPool(8, len(jobURLs))
		local = true
	}
	if local {
		defer wp.Stop()
	}

	results := make([]FetchResult, 0, len(jobURLs))
	resultsCh := make(chan FetchResult, len(jobURLs))

	var wg sync.WaitGroup
	wg.Add(len(jobURLs))
	for _, link := range jobURLs {
		link := link
		wp.Submit(func() {
			defer wg.Done()
			title, htmlText, err := c.Fetch(ctx, link)
			resultsCh <- FetchResult{
				URL:   util.CanonURL(link),
				Title: title,
				HTML:  strings.TrimSpace(htmlText),
				Err:   err,
			}
		})
	}

	wg.Wait()
	close(resultsCh)
	for r := range resultsCh {
		results = append(results, r)
	}
	return results
}

/********** internals **********/

func isComeetJobURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if !(host == "comeet.com" || host == "www.comeet.com" || strings.HasSuffix(host, ".comeet.com")) {
		return false
	}
	pp := strings.Trim(strings.ToLower(path.Clean(u.Path)), "/")
	if !strings.HasPrefix(pp, "jobs/") {
		return false
	}
	parts := strings.Split(pp, "/")
	if len(parts) < 3 {
		return false
	}
	last := parts[len(parts)-1]
	return reComeetCode.MatchString(last)
}

func titleFromComeetURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSpace(raw)
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := len(segs) - 1; i >= 0; i-- {
		s := segs[i]
		if s == "" || looksLikeComeetCode(s) {
			continue
		}
		return strings.TrimSpace(slugToTitle(s)) // your helper
	}
	return strings.TrimSpace(u.Hostname())
}

var reComeetCode = regexp.MustCompile(`(?i)^[a-z0-9]{1,4}\.[a-z0-9]{2,}$`)

func looksLikeComeetCode(seg string) bool {
	return reComeetCode.MatchString(strings.TrimSpace(seg))
}
