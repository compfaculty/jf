package scrape

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// ---- Types used by scanner adapter ----

type Company struct {
	Name string
	URL  string
}

type ScrapedJob struct {
	Title       string
	URL         string
	Location    string
	Description string
}

// Doer is satisfied by *http.Client and httpx.RLClient
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type JobScraper interface {
	GetJobs(ctx context.Context, prefs any) ([]ScrapedJob, error)
}

// ---- Router ----

func NewJobScraper(c Company, client Doer) JobScraper {
	host := hostFromURL(c.URL)
	switch {
	case strings.HasSuffix(host, "jobs.secrettelaviv.com"):
		return &secretTLV{company: c, client: ensureClient(client)}
	// TODO: add dedicated parsers (akeyless/lever/greenhouse etc.)
	default:
		return &generic{company: c, client: ensureClient(client)}
	}
}

func ensureClient(c Doer) Doer {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// ---- Generic scraper (static HTML anchors, strict child URL policy) ----

type generic struct {
	company Company
	client  Doer
}

func (g *generic) GetJobs(ctx context.Context, _ any) ([]ScrapedJob, error) {
	root := strings.TrimSpace(g.company.URL)
	if root == "" {
		return nil, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, root, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(root)
	if err != nil {
		return nil, err
	}
	base.Path = ensureTrailingSlash(path.Clean(base.Path))

	seen := map[string]struct{}{}
	var out []ScrapedJob

	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		text := strings.TrimSpace(joinWS(a.Text()))
		if text == "" {
			return
		}
		href, ok := a.Attr("href")
		if !ok || skipHref(href) {
			return
		}
		ref, err := url.Parse(href)
		if err != nil {
			return
		}
		u := base.ResolveReference(ref)

		if !isAllowedChildURL(base, u) {
			return
		}
		stripTracking(u)

		if !looksLikeJob(text, u.String()) {
			return
		}

		key := strings.ToLower(text) + " | " + strings.ToLower(u.String())
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}

		out = append(out, ScrapedJob{
			Title:       text,
			URL:         u.String(),
			Description: text,
		})
	})

	return out, nil
}

// ---- Secret Tel Aviv scraper (site-specific CSS) ----

type secretTLV struct {
	company Company
	client  Doer
}

func (s *secretTLV) GetJobs(ctx context.Context, _ any) ([]ScrapedJob, error) {
	next := s.company.URL
	var out []ScrapedJob

	for next != "" {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		req.Header.Set("User-Agent", ua)
		resp, err := s.client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			break
		}
		doc, err := goquery.NewDocumentFromReader(resp.Body)
		resp.Body.Close()
		if err != nil {
			break
		}

		doc.Find("div.wpjb-grid-row").Each(func(_ int, row *goquery.Selection) {
			titleA := row.Find("div.wpjb-col-title a").First()
			if titleA.Length() == 0 {
				return
			}
			title := strings.TrimSpace(joinWS(titleA.Text()))
			href, _ := titleA.Attr("href")
			if title == "" || href == "" {
				return
			}
			out = append(out, ScrapedJob{
				Title:       title,
				URL:         href,
				Description: title,
			})
		})

		np := doc.Find("a.next.page-numbers").First()
		if np.Length() == 0 {
			break
		}
		if href, ok := np.Attr("href"); ok && strings.TrimSpace(href) != "" {
			next = resolveURLMust(next, href)
		} else {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// tiny de-dupe
	seen := map[string]struct{}{}
	var dedup []ScrapedJob
	for _, j := range out {
		k := strings.ToLower(j.Title) + "|" + strings.ToLower(j.URL)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		dedup = append(dedup, j)
	}
	return dedup, nil
}

// ---- Helpers ----

const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"

func hostFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

func ensureTrailingSlash(p string) string {
	if p == "" || strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}

func isAllowedChildURL(base, u *url.URL) bool {
	if !strings.EqualFold(base.Host, u.Host) || base.Scheme != u.Scheme {
		return false
	}
	bp := ensureTrailingSlash(strings.TrimSpace(base.Path))
	up := ensureTrailingSlash(strings.TrimSpace(u.Path))
	return strings.HasPrefix(up, bp)
}

func stripTracking(u *url.URL) {
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		kl := strings.ToLower(k)
		if strings.HasPrefix(kl, "utm_") || kl == "gclid" || kl == "fbclid" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
}

func skipHref(href string) bool {
	h := strings.ToLower(strings.TrimSpace(href))
	return h == "" || strings.HasPrefix(h, "javascript:") || strings.HasPrefix(h, "mailto:") ||
		strings.HasPrefix(h, "tel:") || h == "#"
}

func looksLikeJob(text, href string) bool {
	l := strings.ToLower(text + " " + href)
	return strings.Contains(l, "job") || strings.Contains(l, "jobs") || strings.Contains(l, "career") ||
		strings.Contains(l, "careers") || strings.Contains(l, "position") || strings.Contains(l, "positions") ||
		strings.Contains(l, "opening") || strings.Contains(l, "openings")
}

func joinWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func resolveURLMust(baseRaw, rel string) string {
	b, err := url.Parse(baseRaw)
	if err != nil {
		return rel
	}
	r, err := url.Parse(rel)
	if err != nil {
		return baseRaw
	}
	return b.ResolveReference(r).String()
}
