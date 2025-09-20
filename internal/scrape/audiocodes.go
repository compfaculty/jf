package scrape

import (
	"context"
	"jf/internal/config"
	"jf/internal/models"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alitto/pond"
)

// AudioCodes scraper for https://www.audiocodes.com/careers/positions
type AudioCodes struct {
	company models.Company
	client  Doer
}

func NewAudioCodes(c models.Company, client Doer) *AudioCodes {
	return &AudioCodes{
		company: c,
		client:  ensureClient(client),
	}
}

func (s *AudioCodes) GetJobs(ctx context.Context, _ *config.Config) ([]models.ScrapedJob, error) {
	start := strings.TrimSpace(s.company.CareersURL)
	if start == "" {
		return nil, nil
	}
	baseURL, _ := url.Parse(start)

	// 1) Fetch the root (positions) page
	doc, err := s.fetchDoc(ctx, start)
	if err != nil || doc == nil {
		return nil, err
	}

	// 2) Collect absolute job links: /careers/positions/*
	links := collectAudioCodesLinks(doc, baseURL)
	if len(links) == 0 {
		return nil, nil
	}

	// 3) Concurrency with pond: use GroupContext so tasks respect ctx and we can Wait()
	const (
		maxWorkers  = 8  // a touch higher; pond will keep idle workers minimal via idle timeout
		maxCapacity = 64 // queue depth; larger batches won’t block Submit
	)
	pool := pond.New(
		maxWorkers,
		maxCapacity,
		pond.Context(ctx),               // tie pool lifetime to ctx
		pond.MinWorkers(1),              // keep at least one warm worker
		pond.IdleTimeout(3*time.Second), // shrink quicker when idle
	)
	defer pool.StopAndWait()

	group, gctx := pool.GroupContext(ctx)

	// Results fan-in channel (avoids contention on a shared slice)
	results := make(chan models.ScrapedJob, len(links))

	for _, jobURL := range links {
		jobURL := jobURL
		group.Submit(func() error {
			// Early cancellation check
			select {
			case <-gctx.Done():
				return gctx.Err()
			default:
			}

			jdoc, err := s.fetchDoc(gctx, jobURL)
			if err != nil || jdoc == nil {
				return nil // best-effort skip
			}

			// Title from: <div class="share-component"><h1>...</h1>
			title := normWS(jdoc.Find(".share-component h1").First().Text())
			if title == "" {
				title = "Untitled"
			}

			// Requirements-only, compacted with " • "
			req := extractAudioCodesRequirements(jdoc)

			results <- models.ScrapedJob{
				Title:       title,
				URL:         jobURL,
				Description: req,
				Company:     s.company.Name,
			}

			// tiny politeness pause between details
			select {
			case <-time.After(100 * time.Millisecond):
			case <-gctx.Done():
				return gctx.Err()
			}
			return nil
		})
	}

	// Collector: close results after all tasks finish (regardless of per-task skips)
	var (
		out []models.ScrapedJob
		mu  sync.Mutex
	)
	done := make(chan struct{})

	go func() {
		_ = group.Wait() // wait for all submissions to finish
		close(results)
		close(done)
	}()

	for {
		select {
		case j, ok := <-results:
			if !ok {
				// drained
				goto FINISH
			}
			if j.Title == "" && j.URL == "" {
				continue
			}
			mu.Lock()
			out = append(out, j)
			mu.Unlock()
		case <-done:
			goto FINISH
		case <-ctx.Done():
			goto FINISH
		}
	}

FINISH:
	return dedupeScraped(out), nil
}

func (s *AudioCodes) fetchDoc(ctx context.Context, u string) (*goquery.Document, error) {
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
		// keep pipeline resilient; treat non-200 as skip
		return nil, context.Canceled
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

// collectAudioCodesLinks finds job links like /careers/positions/*
func collectAudioCodesLinks(doc *goquery.Document, base *url.URL) []string {
	if base == nil {
		return nil
	}
	seen := make(map[string]struct{}, 64)
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
		u.Fragment = ""

		if !strings.EqualFold(u.Host, base.Host) || u.Scheme != base.Scheme {
			return
		}
		if !strings.HasPrefix(u.Path, "/careers/positions/") {
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

// extractAudioCodesRequirements locates <p><strong>Requirements</strong></p>
// under .section__body.sec-body, then flattens the following <ul><li> items,
// or (if no <ul>) the following non-empty <p> siblings, into one string
// joined with " • ".
func extractAudioCodesRequirements(doc *goquery.Document) string {
	sec := doc.Find("div.section__body.sec-body")
	if sec.Length() == 0 {
		return ""
	}
	var compact string
	found := false

	sec.Find("p").Each(func(_ int, p *goquery.Selection) {
		if found {
			return
		}
		pt := strings.ToLower(strings.TrimSpace(p.Text()))
		if pt == "" {
			return
		}
		if strings.Contains(pt, "requirements") {
			var items []string

			// Prefer a following <ul> list
			ul := p.NextAllFiltered("ul").First()
			if ul.Length() > 0 {
				ul.Find("li").Each(func(_ int, li *goquery.Selection) {
					t := normWS(li.Text())
					if t != "" {
						items = append(items, t)
					}
				})
			} else {
				// Otherwise consume subsequent non-empty <p> siblings
				p.NextAllFiltered("p").Each(func(_ int, sib *goquery.Selection) {
					t := normWS(sib.Text())
					if t != "" {
						items = append(items, t)
					}
				})
			}

			if len(items) > 0 {
				compact = strings.Join(items, " • ")
				found = true
			}
		}
	})
	return compact
}
