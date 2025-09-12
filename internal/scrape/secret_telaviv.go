package scrape

import (
	"context"
	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/validators"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// SecretTelAviv scraper for jobs.secrettelaviv.com pages.
type SecretTelAviv struct {
	company models.Company
	client  Doer
}

func NewSecretTelAviv(c models.Company, client Doer) *SecretTelAviv {
	return &SecretTelAviv{
		company: c,
		client:  ensureClient(client),
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
		doc.Find("div.wpjb-grid-row").Each(func(_ int, row *goquery.Selection) {
			a := row.Find("div.wpjb-col-title a").First()
			if a.Length() == 0 {
				return
			}
			title := strings.TrimSpace(joinWS(a.Text()))
			href, _ := a.Attr("href")
			if title == "" || strings.TrimSpace(href) == "" {
				return
			}
			if baseURL != nil && !validators.MustJobLink(title, href, baseURL, good, bad, thr, hardExcl) {
				return
			}
			date := strings.TrimSpace(row.Find("div.wpjb-grid-col-right span.wpjb-line-major").First().Text())
			out = append(out, models.ScrapedJob{
				Title:       title,
				URL:         href,
				Description: title,
				Company:     s.company.Name,
				DatePosted:  date,
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
		all = append(all, parsePage(doc)...)
		// polite tiny delay to avoid hammering
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return dedupeScraped(all), ctx.Err()
		}
		next = findNext(doc, next)
	}

	return dedupeScraped(all), nil
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
	return resolveURLMust(base, href)
}

// fetchDoc gets the URL and returns a parsed document or an error.
func (s *SecretTelAviv) fetchDoc(ctx context.Context, u string) (*goquery.Document, error) {
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
		return nil, context.Canceled // simple non-OK error; caller treats as skip for non-first pages
	}
	return goquery.NewDocumentFromReader(resp.Body)
}
