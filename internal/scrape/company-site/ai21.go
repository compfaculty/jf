package companysite

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/scrape/common"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"
)

var (
	ai21JobHrefRe = regexp.MustCompile(`^/careers/[a-z0-9-]+/[A-Z0-9.]+/?$`)
)

// Ai21Scraper scrapes https://www.ai21.com/careers/
type Ai21Scraper struct {
	company models.Company
	client  common.Doer
}

func NewAi21(c models.Company, client common.Doer) *Ai21Scraper {
	return &Ai21Scraper{
		company: c,
		client:  common.EnsureClient(client),
	}
}

func (s *Ai21Scraper) GetJobs(ctx context.Context, cfg *config.Config) ([]models.ScrapedJob, error) {
	root := strings.TrimSpace(s.company.CareersURL)
	if root == "" {
		return nil, nil
	}
	baseURL, err := url.Parse(root)
	if err != nil {
		return nil, err
	}

	// 1) Fetch careers page (static HTML is enough).
	doc, err := s.fetchDoc(ctx, root)
	if err != nil {
		return nil, err
	}

	// 2) Collect canonical job URLs on same host under /careers/<slug>/<code>/
	urlSet := make(map[string]struct{})
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		href = strings.TrimSpace(href)
		if href == "" || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "javascript:") {
			return
		}
		ref, err := url.Parse(href)
		if err != nil {
			return
		}
		u := baseURL.ResolveReference(ref)
		// host & scheme must match ai21.com
		if !strings.EqualFold(u.Host, baseURL.Host) || u.Scheme != baseURL.Scheme {
			return
		}
		// keep only /careers/... pattern that looks like a job page
		if ai21JobHrefRe.MatchString(u.Path) {
			u.Fragment = ""
			u.RawQuery = ""
			abs := u.String()
			urlSet[abs] = struct{}{}
		}
	})

	if len(urlSet) == 0 {
		return nil, nil
	}

	// 3) Visit each job page and extract title + Requirements
	type out struct {
		j   models.ScrapedJob
		err error
	}
	results := make([]out, 0, len(urlSet))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(6) // be polite

	ch := make(chan out, len(urlSet))
	for abs := range urlSet {
		abs := abs
		g.Go(func() error {
			j, err := s.scrapeJob(gctx, abs)
			ch <- out{j: j, err: err}
			return nil
		})
	}

	go func() {
		_ = g.Wait()
		close(ch)
	}()

	for r := range ch {
		if r.err != nil {
			// non-fatal; just skip this job
			continue
		}
		results = append(results, r)
	}

	jobs := make([]models.ScrapedJob, 0, len(results))
	for _, r := range results {
		jobs = append(jobs, r.j)
	}
	return jobs, nil
}

func (s *Ai21Scraper) fetchDoc(ctx context.Context, u string) (*goquery.Document, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("status %d for %s", resp.StatusCode, u)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

func (s *Ai21Scraper) scrapeJob(ctx context.Context, jobURL string) (models.ScrapedJob, error) {
	doc, err := s.fetchDoc(ctx, jobURL)
	if err != nil {
		return models.ScrapedJob{}, err
	}

	// Title: try <h1>, then og:title
	title := strings.TrimSpace(doc.Find("h1, h1.career__title").First().Text())
	if title == "" {
		if og := strings.TrimSpace(metaContent(doc, "meta[property='og:title']")); og != "" {
			title = og
		}
	}
	if title == "" {
		title = "(no title)"
	}

	// Location (best-effort; if present near details or in meta)
	loc := strings.TrimSpace(doc.Find(".career__location, .details__location, .job__location").First().Text())

	// Requirements: find the section whose H2 text == "Requirements" (case-insensitive),
	// then concatenate <li> items; if no <li>, use the raw text.
	var requirements []string
	doc.Find(".career__details-item").Each(func(_ int, sec *goquery.Selection) {
		heading := strings.TrimSpace(sec.Find("h2, .details__item-title").First().Text())
		if strings.EqualFold(heading, "Requirements") {
			// Prefer bullet points
			sec.Find(".details__item-text li").Each(func(_ int, li *goquery.Selection) {
				txt := strings.TrimSpace(li.Text())
				if txt != "" {
					requirements = append(requirements, txt)
				}
			})
			if len(requirements) == 0 {
				raw := strings.TrimSpace(sec.Find(".details__item-text").First().Text())
				if raw != "" {
					requirements = append(requirements, strings.Join(strings.Fields(raw), " "))
				}
			}
		}
	})

	desc := strings.Join(requirements, "\n")

	return models.ScrapedJob{
		Title:       title,
		URL:         jobURL,
		Location:    emptyIfDash(loc),
		Description: desc, // <= Requirements go here
	}, nil
}

// helpers

func metaContent(doc *goquery.Document, sel string) string {
	if n := doc.Find(sel).First(); n.Length() > 0 {
		if v, ok := n.Attr("content"); ok {
			return v
		}
	}
	return ""
}

func emptyIfDash(s string) string {
	s = strings.TrimSpace(s)
	if s == "-" {
		return ""
	}
	return s
}

// GetJobPosted extracts the posted date from a job URL.
// Stub implementation - returns empty string until instructed where/how to find the date.
func (s *Ai21Scraper) GetJobPosted(ctx context.Context, jobURL string) (string, error) {
	return "", nil
}
