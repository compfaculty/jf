package companysite

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path"
	"regexp"
	_ "sort"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/alitto/pond" // or your local pond with NewPool/NewGroupContext

	"jf/internal/scrape/common"
	util "jf/internal/utils"
)

/********** Comeet client **********/

type ComeetClient struct {
	http common.Doer
	wp   *pond.WorkerPool // shared pool; create a group per bulk call
}

func NewComeet(client common.Doer) *ComeetClient {
	return &ComeetClient{http: common.EnsureClient(client)}
}

func (c *ComeetClient) WithPool(wp *pond.WorkerPool) *ComeetClient {
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
		if href == "" || base == nil {
			return
		}
		ref, err := url.Parse(href)
		if err != nil {
			return
		}
		u := base.ResolveReference(ref)
		u.Fragment = ""
		if !isComeetJobURL(u) {
			return
		}
		abs := util.CanonURL(u.String())
		k := strings.ToLower(abs)
		if _, dup := seen[k]; dup {
			return
		}
		seen[k] = struct{}{}
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

	// 1) Requirements block near heading
	var reqHTML string
	doc.Find("h3.smallTitle, h3.positionSubTitle, h3, strong, b").EachWithBreak(func(_ int, h *goquery.Selection) bool {
		ht := strings.ToLower(strings.TrimSpace(h.Text()))
		if strings.Contains(ht, "requirement") {
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

	// 2) JSON-LD → slice "Requirements"
	if strings.TrimSpace(reqHTML) == "" {
		if full := descriptionFromJSONLD(doc); strings.TrimSpace(full) != "" {
			if sl := sliceRequirementsFromHTML(full); strings.TrimSpace(sl) != "" {
				reqHTML = sl
			}
		}
	}

	// 3) POSITION_DATA → "Requirements"
	if strings.TrimSpace(reqHTML) == "" {
		if s := requirementsFromPositionData(doc); strings.TrimSpace(s) != "" {
			reqHTML = s
		}
	}

	// 4) First .company-description
	if strings.TrimSpace(reqHTML) == "" {
		if n := doc.Find("div.userDesignedContent.company-description").First(); n.Length() > 0 {
			if s, _ := n.Html(); strings.TrimSpace(s) != "" {
				reqHTML = s
			}
		}
	}

	// Plain text
	plain := strings.TrimSpace(reqHTML)
	if plain != "" {
		plain = reTags.ReplaceAllString(plain, " ")
		plain = html.UnescapeString(plain)
		plain = strings.Join(strings.Fields(plain), " ")
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

	return title, plain, nil
}

/********** Bulk APIs — shared pool + GROUPS **********/

// DiscoverAll scans multiple careers pages concurrently and returns unique Comeet job links.
// Uses a *group* on the shared pool so you can set deadlines/cancel per operation.
func (c *ComeetClient) DiscoverAll(ctx context.Context, careersURLs []string) ([]string, error) {
	if len(careersURLs) == 0 {
		return nil, nil
	}

	// Create a group bound to the provided ctx (supports timeout/cancel)
	group, _ := c.wp.GroupContext(ctx)

	var mu sync.Mutex
	seen := make(map[string]struct{}, len(careersURLs)*8)
	out := make([]string, 0, len(careersURLs)*8)

	for _, root := range careersURLs {
		root := root
		group.Submit(func() error {
			links, err := c.Discover(ctx, root)
			if err != nil || len(links) == 0 {
				return err
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
			return nil
		})
	}

	// Wait until all tasks finish OR ctx cancels (deadline/timeout/etc.)
	if err := group.Wait(); err != nil {
		// We still return whatever we accumulated; caller can check err if needed.
		return out, err
	}
	return out, nil
}

type FetchResult struct {
	URL   string
	Title string
	HTML  string
	Err   error
}

// FetchAll fetches many Comeet job pages concurrently.
// Uses a *group* on the shared pool for per-call cancellation/timeout.
func (c *ComeetClient) FetchAll(ctx context.Context, jobURLs []string) []FetchResult {
	if len(jobURLs) == 0 {
		return nil
	}

	group, _ := c.wp.GroupContext(ctx)

	results := make([]FetchResult, 0, len(jobURLs))
	var mu sync.Mutex

	for _, link := range jobURLs {
		link := link
		group.Submit(func() error {
			title, htmlText, err := c.Fetch(ctx, link)
			if err != nil {
				return err
			}
			mu.Lock()
			results = append(results, FetchResult{
				URL:   util.CanonURL(link),
				Title: title,
				HTML:  strings.TrimSpace(htmlText),
				Err:   err,
			})
			mu.Unlock()
			return nil
		})
	}

	_ = group.Wait() // ignore error here; callers typically inspect per-item Err
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
		return strings.TrimSpace(util.SlugToTitle(s))
	}
	return strings.TrimSpace(u.Hostname())
}

var reComeetCode = regexp.MustCompile(`(?i)^[a-z0-9]{1,4}\.[a-z0-9]{2,}$`)

func looksLikeComeetCode(seg string) bool { return reComeetCode.MatchString(strings.TrimSpace(seg)) }

/********** helpers **********/

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
