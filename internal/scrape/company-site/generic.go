package companysite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/pool"
	"jf/internal/utils"
	"jf/internal/validators"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alitto/pond"

	"jf/internal/scrape/common"
)

// GenericScraper scans static anchors; if no results and a Browser is provided,
// it retries with HTML from a headless page (selector-driven wait).
type GenericScraper struct {
	company models.Company
	client  common.Doer
	browser common.Browser   // optional
	wp      *pond.WorkerPool // shared pool (recommended)
}

func NewGeneric(c models.Company, client common.Doer, browser common.Browser, wp *pond.WorkerPool) common.JobScraper {
	return &GenericScraper{
		company: c,
		client:  common.EnsureClient(client),
		browser: browser,
		wp:      wp,
	}
}

func (g *GenericScraper) GetJobs(ctx context.Context, cfg *config.Config) ([]models.ScrapedJob, error) {
	root := strings.TrimSpace(g.company.CareersURL)
	if root == "" {
		return nil, nil
	}

	// 1) Static first (cheap)
	if out, err := g.fetchStatic(ctx, root, cfg); err == nil && len(out) > 0 {
		return out, nil
	}

	// 2) Headless fallback (with iframe traversal inside BrowserPool)
	if g.browser == nil {
		return nil, nil
	}
	anchors, err := g.browser.FetchAnchors(ctx, root, 1*time.Second)
	if err != nil {
		return nil, err
	}
	if len(anchors) == 0 {
		return nil, nil
	}

	found, err := g.extractFromAnchorsParallel(ctx, root, anchors, cfg)
	if err != nil {
		return found, err
	}
	return found, nil
}

// Parallel anchor processing with backpressure and stable ordering, using a GROUP on pond.
func (g *GenericScraper) extractFromAnchorsParallel(
	ctx context.Context,
	root string,
	anchors []pool.Anchor,
	cfg *config.Config,
) ([]models.ScrapedJob, error) {
	base, err := url.Parse(root)
	if err != nil {
		return nil, err
	}

	good, bad := cfg.GoodBadKeywordSets()
	thr := cfg.HeuristicThreshold
	hardExcl := cfg.HardExcludeOnBad

	// Create a per-call group bound to ctx (supports timeout/cancel)
	group, _ := g.wp.GroupContext(ctx)

	type item struct {
		idx int
		job models.ScrapedJob
	}

	items := make([]item, 0, len(anchors))
	var mu sync.Mutex

	for i, a := range anchors {
		i, a := i, a
		txt := strings.TrimSpace(a.Text)
		href := strings.TrimSpace(a.Href)
		if href == "" {
			continue
		}

		group.Submit(func() error {
			// Resolve for title fallback and to build combined string
			u := g.resolve(base, href)
			if u == nil {
				return errors.New("failed to resolve url")
			}

			// Title: prefer text, fallback to last path segment
			title := g.pickTitle(strings.TrimSpace(utils.JoinWS(txt)), u)

			combined := title + " " + u.String()
			ok, absCanon := validators.MustJobLinkURL(combined, href, base, good, bad, thr, hardExcl)
			if !ok || strings.TrimSpace(absCanon) == "" {
				return errors.New("failed to resolve url")
			}

			mu.Lock()
			items = append(items, item{
				idx: i,
				job: models.ScrapedJob{
					Title:       title,
					URL:         absCanon, // canonical absolute URL
					Description: title,
					Company:     g.company.Name,
				},
			})
			mu.Unlock()
			return nil
		})
	}

	// Wait for the group to complete (or ctx deadline/cancel)
	if err := group.Wait(); err != nil {
		// Return whatever we collected + error so caller can decide
		// (common pattern in your codebase)
	}

	// Stable order (by original anchor index), then dedupe by canonical URL
	sort.SliceStable(items, func(i, j int) bool { return items[i].idx < items[j].idx })

	seen := make(map[string]struct{}, len(items))
	out := make([]models.ScrapedJob, 0, len(items))
	for _, it := range items {
		u := strings.ToLower(strings.TrimSpace(it.job.URL))
		if u == "" {
			continue
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, it.job)
	}
	return out, nil
}

func (g *GenericScraper) fetchStatic(ctx context.Context, root string, cfg *config.Config) ([]models.ScrapedJob, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, root, nil)
	if err != nil {
		return nil, err
	}
	// No UA set here; httpx injects UA if needed via shared client.
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return g.extractFromHTML(root, resp.Body, cfg)
}

func (g *GenericScraper) resolve(base *url.URL, href string) *url.URL {
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return nil
	}
	u := base.ResolveReference(ref)
	u.Fragment = ""
	return u
}

func (g *GenericScraper) pickTitle(text string, u *url.URL) string {
	if t := strings.TrimSpace(text); t != "" {
		return t
	}
	seg := path.Base(strings.TrimSuffix(u.Path, "/"))
	seg = strings.ReplaceAll(seg, "-", " ")
	seg = strings.ReplaceAll(seg, "_", " ")
	return strings.TrimSpace(seg)
}

func (g *GenericScraper) extractFromHTML(root string, r io.ReadCloser, cfg *config.Config) ([]models.ScrapedJob, error) {
	defer r.Close()

	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(root)
	if err != nil {
		return nil, err
	}

	good, bad := cfg.GoodBadKeywordSets()
	thr := cfg.HeuristicThreshold
	hardExcl := cfg.HardExcludeOnBad

	seen := map[string]struct{}{}
	var out []models.ScrapedJob

	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		if _, dup := seen[href]; dup {
			return
		}
		seen[href] = struct{}{}

		text := strings.TrimSpace(utils.JoinWS(a.Text()))

		// IMPORTANT: produce a canonical ABSOLUTE URL (fixes localhost-relative links in GUI)
		ok2, absCanon := validators.MustJobLinkURL(text, href, base, good, bad, thr, hardExcl)
		if !ok2 || strings.TrimSpace(absCanon) == "" {
			return
		}

		title := text
		if title == "" {
			if u := g.resolve(base, href); u != nil {
				title = g.pickTitle("", u)
			}
		}

		out = append(out, models.ScrapedJob{
			Title:       title,
			URL:         absCanon, // canonical absolute
			Description: title,
			Company:     g.company.Name,
		})
	})

	return out, nil
}

// GetJobPosted extracts the posted date from a job URL.
// Stub implementation - returns empty string until instructed where/how to find the date.
func (g *GenericScraper) GetJobPosted(ctx context.Context, jobURL string) (string, error) {
	return "", nil
}

/********** tiny helper **********/

//func joinWS(s string) string {
//	var b strings.Builder
//	b.Grow(len(s))
//	prevSpace := false
//	for _, r := range s {
//		if r == '\n' || r == '\t' || r == '\r' || r == ' ' {
//			if !prevSpace {
//				b.WriteByte(' ')
//			}
//			prevSpace = true
//			continue
//		}
//		b.WriteRune(r)
//		prevSpace = false
//	}
//	return strings.TrimSpace(b.String())
//}
