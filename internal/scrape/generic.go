package scrape

import (
	"context"
	"fmt"
	"io"
	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/pool"
	"jf/internal/validators"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// GenericScraper scans static anchors; if no results and a Browser is provided,
// it retries with HTML from a headless page (selector-driven wait).
type GenericScraper struct {
	company models.Company
	client  Doer
	browser Browser // optional
}

func NewGeneric(c models.Company, client Doer, browser Browser) *GenericScraper {
	return &GenericScraper{
		company: c,
		client:  ensureClient(client),
		browser: browser,
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
	hrefs, err := g.browser.FetchAnchors(ctx, root, 1*time.Second)
	if err != nil {
		return nil, err
	}
	if len(hrefs) == 0 {
		return nil, nil
	}

	found, err := g.extractFromAnchorsParallel(ctx, root, hrefs, cfg)
	if err != nil {
		return found, err
	}
	return found, nil
}

// Parallel anchor processing with backpressure and stable ordering.
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

	// Concurrency controls
	n := cfg.MaxConcurrency
	if n <= 0 {
		n = 8
	}
	if n > 64 {
		n = 64
	}
	queue := len(anchors)
	if queue < 64 {
		queue = 64
	}

	wp := pool.NewWorkerPool(n, queue)
	defer wp.Stop()

	type item struct {
		idx int
		job models.ScrapedJob
	}

	outCh := make(chan item, len(anchors))
	var wg sync.WaitGroup

	for i, a := range anchors {
		if ctx.Err() != nil {
			break
		}
		idx := i
		txt := strings.TrimSpace(a.Text)
		href := strings.TrimSpace(a.Href)

		// Quick local skips (cheap) before scheduling
		if href == "" || BadHref(href) {
			continue
		}

		wg.Add(1)
		wp.Submit(func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Resolve for title fallback and to build combined string
			u := g.resolve(base, href)
			if u == nil {
				return
			}

			// Title: prefer text, fallback to last path segment
			title := g.pickTitle(strings.TrimSpace(joinWS(txt)), u)

			combined := title + " " + u.String()
			ok, ul := validators.MustJobLinkURL(combined, href, base, good, bad, thr, hardExcl)
			if !ok {
				return
			}

			outCh <- item{
				idx: idx,
				job: models.ScrapedJob{
					Title:       title,
					URL:         ul, // canonical absolute URL
					Description: title,
					Company:     g.company.Name,
				},
			}
		})
	}

	go func() {
		wg.Wait()
		close(outCh)
	}()

	// Collect, sort by original order, and dedupe by canonical URL
	items := make([]item, 0, len(anchors))
	for it := range outCh {
		items = append(items, it)
	}
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
	// No UA set here; httpx will inject DefaultUserAgent if header is empty (via shared client).
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

		text := strings.TrimSpace(joinWS(a.Text()))

		// IMPORTANT: produce a canonical ABSOLUTE URL (fixes localhost-relative links in GUI)
		ok2, ul := validators.MustJobLinkURL(text, href, base, good, bad, thr, hardExcl)
		if !ok2 || strings.TrimSpace(ul) == "" {
			return
		}

		out = append(out, models.ScrapedJob{
			Title:       text,
			URL:         ul, // <- canonical absolute
			Description: text,
			Company:     g.company.Name,
		})
	})

	return out, nil
}
