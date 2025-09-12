package scrape

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"jf/internal/config"
	"jf/internal/models"

	"github.com/PuerkitoBio/goquery"
)

// Agrematch scraper for https://www.agrematch.com/careers
// Each job appears in a tab pane with class .job_pos_div_f.
type Agrematch struct {
	company models.Company
	client  Doer
}

func NewAgrematch(c models.Company, client Doer) *Agrematch {
	return &Agrematch{
		company: c,
		client:  ensureClient(client),
	}
}

func (s *Agrematch) GetJobs(ctx context.Context, _ *config.Config) ([]models.ScrapedJob, error) {
	start := strings.TrimSpace(s.company.CareersURL)
	if start == "" {
		return nil, nil
	}

	doc, err := s.fetchDoc(ctx, start)
	if err != nil {
		return nil, err
	}

	var out []models.ScrapedJob

	// Every job block on the page (hidden or active panes are still in DOM on Webflow pages)
	doc.Find("div.job_pos_div_f").Each(func(_ int, blk *goquery.Selection) {
		title := normWS(blk.Find("h2.open_pos_title.sub_pos").First().Text())
		if title == "" {
			return
		}

		// Merge bullets from both sections:
		//  - "Job description & key responsibilities:"
		//  - "Requirements:"
		desc := extractMergedBulletsAgrematch(blk)

		// Build a stable anchor URL (page uses tabs, not per-job pages)
		// Build a stable per-job URL that dedup logic will treat as distinct
		baseURL, _ := url.Parse(start)
		jobURL := start
		if baseURL != nil {
			sl := slug(title)
			u := *baseURL
			q := u.Query()
			q.Set("_jf", sl) // synthetic, unique per job; NOT removed by canonicalizeURL
			u.RawQuery = q.Encode()
			u.Fragment = sl // keeps the nice scroll-in-page behavior
			jobURL = u.String()
		}

		out = append(out, models.ScrapedJob{
			Title:       title,
			URL:         jobURL,
			Description: desc,
			Company:     s.company.Name,
		})
	})

	// polite
	select {
	case <-time.After(250 * time.Millisecond):
	case <-ctx.Done():
		return out, ctx.Err()
	}

	return out, nil
}

func (s *Agrematch) fetchDoc(ctx context.Context, u string) (*goquery.Document, error) {
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
		return nil, context.Canceled
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

// extractMergedBulletsAgrematch collects bullets from both sections if present:
// 1) "Job description & key responsibilities:" (or contains "responsibilities")
// 2) "Requirements:"
func extractMergedBulletsAgrematch(jobBlk *goquery.Selection) string {
	var respItems, reqItems []string

	jobBlk.Find("div.pos_points").Each(func(_ int, pp *goquery.Selection) {
		heading := strings.ToLower(normWS(pp.Find("h3.career-h3").First().Text()))
		if heading == "" {
			return
		}
		var items []string
		pp.Find("div.career-point-wrap > div").Each(func(_ int, d *goquery.Selection) {
			t := normWS(d.Text()) // collapses <br/> into spaces
			if t != "" {
				items = append(items, t)
			}
		})
		switch {
		case strings.Contains(heading, "responsibil"):
			respItems = append(respItems, items...)
		case strings.Contains(heading, "require"):
			reqItems = append(reqItems, items...)
		}
	})

	// Prefer: Responsibilities first, then Requirements
	switch {
	case len(respItems) > 0 && len(reqItems) > 0:
		return strings.Join(append(respItems, reqItems...), " • ")
	case len(reqItems) > 0:
		return strings.Join(reqItems, " • ")
	case len(respItems) > 0:
		return strings.Join(respItems, " • ")
	default:
		return "" // no bullets found
	}
}
