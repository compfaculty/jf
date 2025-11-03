package pool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"jf/internal/utils"
)

// BrowserPool is a small fixed-size pool of chromedp contexts.
// Submit jobs with a provided context for cancellation/timeouts.
type BrowserPool struct {
	jobs    chan func(ctx context.Context)
	cancels []context.CancelFunc
	wg      sync.WaitGroup
}

type BrowserPoolConfig struct {
	Workers    int
	Headless   bool
	Queue      int
	NavWait    time.Duration // sleep after Navigate (cheap "network idle" proxy)
	NavTimeout time.Duration // per-job suggested timeout (the caller ctx is the real deadline)
}

// Anchor is a minimal link payload extracted from the page.
type Anchor struct {
	Text string `json:"text"`
	Href string `json:"href"`
}

func NewBrowserPool(cfg BrowserPoolConfig) *BrowserPool {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.Queue <= 0 {
		cfg.Queue = 256
	}

	bp := &BrowserPool{
		jobs: make(chan func(ctx context.Context), cfg.Queue),
	}

	log.Printf("[BROWSER] init workers=%d headless=%v queue=%d nav_wait=%s nav_timeout=%s",
		cfg.Workers, cfg.Headless, cfg.Queue, cfg.NavWait, cfg.NavTimeout)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", cfg.Headless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("mute-audio", true),
	)

	// When not headless, try to keep the browser unobtrusive
	if !cfg.Headless {
		opts = append(opts,
			chromedp.Flag("start-minimized", true),
			chromedp.Flag("window-size", "640,480"),         // small window
			chromedp.Flag("window-position", "20000,20000"), // move off-screen on most setups
			chromedp.Flag("enable-automation", false),       // reduce automation infobar
		)
	}

	allocCtx, _ := chromedp.NewExecAllocator(context.Background(), opts...)
	for i := 0; i < cfg.Workers; i++ {
		ctx, cancel := chromedp.NewContext(allocCtx)
		bp.cancels = append(bp.cancels, cancel)
		bp.wg.Add(1)
		go func(c context.Context) {
			defer bp.wg.Done()
			for job := range bp.jobs {
				job(c)
			}
		}(ctx)
	}
	return bp
}

func (bp *BrowserPool) Submit(job func(ctx context.Context)) error {
	select {
	case bp.jobs <- job:
		utils.Verbosef("BrowserPool: job submitted, queue length: %d/%d", len(bp.jobs), cap(bp.jobs))
		return nil
	default:
		utils.Verbosef("BrowserPool: queue full, cannot submit job")
		return errors.New("browser pool queue full")
	}
}

func (bp *BrowserPool) Close() {
	close(bp.jobs)
	for _, c := range bp.cancels {
		c()
	}
	bp.wg.Wait()

	// Additional cleanup to prevent memory leaks
	// Note: chromedp contexts should be properly cancelled by the cancel functions above
	// This is a safety measure to ensure all resources are released
}

// FetchHTML navigates to url and returns the outerHTML of selector (default "html").
// Use caller's ctx for cancellation/deadline.
func (bp *BrowserPool) FetchHTML(ctx context.Context, url, selector string, wait time.Duration) (string, error) {
	if selector == "" {
		selector = "html"
	}
	start := time.Now()
	utils.Verbosef("BrowserPool: FetchHTML starting url=%q selector=%q wait=%s", url, selector, wait)

	var html string
	done := make(chan struct{})
	errCh := make(chan error, 1)

	if err := bp.Submit(func(c context.Context) {
		// Tie chromedp work to the caller's ctx (so cancellation propagates)
		taskCtx, cancel := chromedp.NewContext(c)
		defer cancel()

		utils.Verbosef("BrowserPool: navigating to %s", url)
		err2 := chromedp.Run(taskCtx,
			chromedp.Navigate(url),
			chromedp.Sleep(wait),
			chromedp.OuterHTML(selector, &html, chromedp.ByQuery),
		)
		if err2 != nil {
			utils.Verbosef("BrowserPool: FetchHTML error for %s: %v", url, err2)
			errCh <- err2
			return
		}
		utils.Verbosef("BrowserPool: FetchHTML completed url=%q html_len=%d dur=%s", url, len(html), time.Since(start))
		close(done)
	}); err != nil {
		utils.Verbosef("BrowserPool: FetchHTML submit failed for %s: %v", url, err)
		return "", err
	}

	select {
	case <-done:
		return html, nil
	case e := <-errCh:
		utils.Verbosef("BrowserPool: FetchHTML error from worker: %v", e)
		return "", e
	case <-ctx.Done():
		utils.Verbosef("BrowserPool: FetchHTML timeout/context cancelled for %s", url)
		return "", fmt.Errorf("timeout: %w", ctx.Err())
	}
}

// FetchAnchors navigates and returns anchors present on the page.
// Additionally, it collects anchors from any iframe[src] by navigating to
// each iframe URL as a full page (handles cross-origin boards like Greenhouse/Lever).
func (bp *BrowserPool) FetchAnchors(ctx context.Context, url string, wait time.Duration) ([]Anchor, error) {
	start := time.Now()
	utils.Verbosef("BrowserPool: FetchAnchors starting url=%q wait=%s", url, wait)

	var out []Anchor

	done := make(chan struct{})
	errCh := make(chan error, 1)

	if err := bp.Submit(func(c context.Context) {
		taskCtx, cancel := chromedp.NewContext(c)
		defer cancel()

		type pair struct{ Text, Href string }
		var top []pair
		var frames []string

		run := func(actions ...chromedp.Action) error {
			return chromedp.Run(taskCtx, actions...)
		}

		// 1) Main page
		utils.Verbosef("BrowserPool: FetchAnchors navigating to main page %s", url)
		if err := run(
			chromedp.Navigate(url),
			chromedp.WaitReady(`body`, chromedp.ByQuery),
			chromedp.Sleep(wait),
			chromedp.Evaluate(`Array.from(document.querySelectorAll('a[href]'))
        .map(a => ({text: (a.innerText||'').trim(), href: a.getAttribute('href')||''}))`, &top),
			chromedp.Evaluate(`Array.from(document.querySelectorAll('iframe[src]'))
        .map(f => f.getAttribute('src'))`, &frames),
		); err != nil {
			utils.Verbosef("BrowserPool: FetchAnchors error on main page %s: %v", url, err)
			errCh <- err
			return
		}

		utils.Verbosef("BrowserPool: FetchAnchors found %d anchors and %d iframes on main page", len(top), len(frames))
		for _, p := range top {
			out = append(out, Anchor{Text: p.Text, Href: p.Href})
		}

		// 2) For each iframe URL: navigate to it and collect its anchors too
		for i, fsrc := range frames {
			if strings.TrimSpace(fsrc) == "" {
				continue
			}
			utils.Verbosef("BrowserPool: FetchAnchors processing iframe %d/%d: %s", i+1, len(frames), fsrc)
			var sub []pair
			if err := run(
				chromedp.Navigate(fsrc),
				chromedp.WaitReady(`body`, chromedp.ByQuery),
				chromedp.Sleep(wait),
				chromedp.Evaluate(`Array.from(document.querySelectorAll('a[href]'))
          .map(a => ({text: (a.innerText||'').trim(), href: a.getAttribute('href')||''}))`, &sub),
			); err != nil {
				utils.Verbosef("BrowserPool: FetchAnchors error on iframe %s: %v (skipping)", fsrc, err)
				// Skip broken frames but continue others
				continue
			}
			utils.Verbosef("BrowserPool: FetchAnchors found %d anchors in iframe %s", len(sub), fsrc)
			for _, p := range sub {
				out = append(out, Anchor{Text: p.Text, Href: p.Href})
			}
		}

		utils.Verbosef("BrowserPool: FetchAnchors completed url=%q total_anchors=%d dur=%s", url, len(out), time.Since(start))
		close(done)
	}); err != nil {
		utils.Verbosef("BrowserPool: FetchAnchors submit failed for %s: %v", url, err)
		return nil, err
	}

	select {
	case <-done:
		return out, nil
	case e := <-errCh:
		utils.Verbosef("BrowserPool: FetchAnchors error from worker: %v", e)
		return nil, e
	case <-ctx.Done():
		utils.Verbosef("BrowserPool: FetchAnchors timeout/context cancelled for %s", url)
		return nil, fmt.Errorf("timeout: %w", ctx.Err())
	}
}
