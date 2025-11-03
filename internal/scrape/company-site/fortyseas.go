package companysite

import (
	"context"
	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/scrape/common"
	"jf/internal/utils"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"
)

// FortySeas scraper for https://www.40seas.com/careers#positions
// Flow:
//  1. Collect anchors to /open-positions/* on the careers page.
//  2. Visit each position page (in parallel) and extract:
//     - Title:  h1.heading-11 (fallbacks: first h1, then slug)
//     - Requirements (compact): items of the <ul> that follows the <p><strong>Requirements</strong></p>
type FortySeas struct {
	company models.Company
	client  common.Doer
}

func NewFortySeas(c models.Company, client common.Doer) *FortySeas {
	return &FortySeas{
		company: c,
		client:  common.EnsureClient(client),
	}
}

func (s *FortySeas) GetJobs(ctx context.Context, _ *config.Config) ([]models.ScrapedJob, error) {
	start := strings.TrimSpace(s.company.CareersURL)
	if start == "" {
		return nil, nil
	}
	baseURL, _ := url.Parse(start)

	// 1) Load the careers page
	doc, err := s.fetchDoc(ctx, start)
	if err != nil {
		return nil, err
	}

	// 2) Collect absolute links to /open-positions/*
	links := collectOpenPositionsLinks(doc, baseURL)
	if len(links) == 0 {
		return nil, nil
	}

	// 3) Fetch details in parallel with a small cap
	const maxConcurrent = 6
	sem := make(chan struct{}, maxConcurrent)

	var (
		mu  sync.Mutex
		out []models.ScrapedJob
	)

	g, gctx := errgroup.WithContext(ctx)
	for _, jobURL := range links {
		jobURL := jobURL
		sem <- struct{}{} // acquire
		g.Go(func() error {
			defer func() { <-sem }() // release

			// Respect cancellation
			select {
			case <-gctx.Done():
				return gctx.Err()
			default:
			}

			jdoc, err := s.fetchDoc(gctx, jobURL)
			if err != nil {
				return nil // best-effort: skip this one
			}

			title := utils.NormWS(jdoc.Find("h1.heading-11").First().Text())
			if title == "" {
				title = utils.NormWS(jdoc.Find("h1").First().Text())
				if title == "" {
					if u, _ := url.Parse(jobURL); u != nil {
						title = utils.SlugToTitle(strings.TrimPrefix(u.Path, "/open-positions/"))
					}
				}
			}

			req := extractRequirementsCompact(jdoc)

			mu.Lock()
			out = append(out, models.ScrapedJob{
				Title:       title,
				URL:         jobURL,
				Description: req, // compact "Requirements"
				Company:     s.company.Name,
			})
			mu.Unlock()

			// tiny politeness pause between details
			select {
			case <-time.After(100 * time.Millisecond):
			case <-gctx.Done():
				return gctx.Err()
			}

			return nil
		})
	}

	_ = g.Wait() // ignore per-item skips

	return utils.DedupeScraped(out), nil
}

func (s *FortySeas) fetchDoc(ctx context.Context, u string) (*goquery.Document, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := s.client.Do(req)
	if err != nil || resp == nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// simple non-OK; treat as skip to keep pipeline resilient
		return nil, context.Canceled
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

func collectOpenPositionsLinks(doc *goquery.Document, base *url.URL) []string {
	if base == nil {
		return nil
	}
	seen := make(map[string]struct{}, 16)
	var links []string

	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok || strings.TrimSpace(href) == "" {
			return
		}
		ref, err := url.Parse(href)
		if err != nil {
			return
		}
		u := base.ResolveReference(ref)
		u.Fragment = "" // normalize

		// same host + /open-positions/ path
		if !strings.EqualFold(u.Host, base.Host) || u.Scheme != base.Scheme {
			return
		}
		if !strings.HasPrefix(u.Path, "/open-positions/") {
			return
		}
		abs := u.String()
		if _, dup := seen[abs]; dup {
			return
		}
		seen[abs] = struct{}{}
		links = append(links, abs)
	})

	return links
}

// extractRequirementsCompact finds the first <p> containing "requirements" (case-insensitive),
// then takes the next <ul> and flattens its <li> items into a single "•"-separated line.
func extractRequirementsCompact(doc *goquery.Document) string {
	rich := doc.Find("div.position-rich.w-richtext")
	if rich.Length() == 0 {
		rich = doc.Find(".position-rich")
	}
	var compact string
	found := false

	rich.Find("p").Each(func(_ int, p *goquery.Selection) {
		if found {
			return
		}
		pt := strings.ToLower(strings.TrimSpace(p.Text()))
		if pt == "" {
			return
		}
		if strings.Contains(pt, "requirements") {
			ul := p.NextAllFiltered("ul").First()
			if ul.Length() == 0 {
				return
			}
			var items []string
			ul.Find("li").Each(func(_ int, li *goquery.Selection) {
				t := utils.NormWS(li.Text())
				if t != "" {
					items = append(items, t)
				}
			})
			if len(items) > 0 {
				compact = strings.Join(items, " • ")
				found = true
			}
		}
	})

	return compact
}

// GetJobPosted extracts the posted date from a job URL.
// Stub implementation - returns empty string until instructed where/how to find the date.
func (s *FortySeas) GetJobPosted(ctx context.Context, jobURL string) (string, error) {
	return "", nil
}
