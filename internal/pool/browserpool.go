package pool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
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

	bp := &BrowserPool{jobs: make(chan func(ctx context.Context), cfg.Queue)}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", cfg.Headless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		//chromedp.Flag("user-data-dir", `/mnt/c/Users/compf/chrome-profile-copy`),
	)

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
		return nil
	default:
		return errors.New("browser pool queue full")
	}
}

func (bp *BrowserPool) Close() {
	close(bp.jobs)
	for _, c := range bp.cancels {
		c()
	}
	bp.wg.Wait()
}

// FetchHTML navigates to url and returns the outerHTML of selector (default "html").
// Use caller's ctx for cancellation/deadline.
func (bp *BrowserPool) FetchHTML(ctx context.Context, url, selector string, wait time.Duration) (string, error) {
	if selector == "" {
		selector = "html"
	}
	var html string
	done := make(chan struct{})
	errCh := make(chan error, 1)

	if err := bp.Submit(func(c context.Context) {
		// Tie chromedp work to the caller's ctx (so cancellation propagates)
		taskCtx, cancel := chromedp.NewContext(c)
		defer cancel()

		err2 := chromedp.Run(taskCtx,
			chromedp.Navigate(url),
			chromedp.Sleep(wait),
			chromedp.OuterHTML(selector, &html, chromedp.ByQuery),
		)
		if err2 != nil {
			errCh <- err2
			return
		}
		close(done)
	}); err != nil {
		return "", err
	}

	select {
	case <-done:
		return html, nil
	case e := <-errCh:
		return "", e
	case <-ctx.Done():
		return "", fmt.Errorf("timeout: %w", ctx.Err())
	}
}

// FetchAnchors navigates and returns anchors present on the page.
// Additionally, it collects anchors from any iframe[src] by navigating to
// each iframe URL as a full page (handles cross-origin boards like Greenhouse/Lever).
func (bp *BrowserPool) FetchAnchors(ctx context.Context, url string, wait time.Duration) ([]Anchor, error) {
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
		if err := run(
			chromedp.Navigate(url),
			chromedp.WaitReady(`body`, chromedp.ByQuery),
			chromedp.Sleep(wait),
			chromedp.Evaluate(`Array.from(document.querySelectorAll('a[href]'))
        .map(a => ({text: (a.innerText||'').trim(), href: a.getAttribute('href')||''}))`, &top),
			chromedp.Evaluate(`Array.from(document.querySelectorAll('iframe[src]'))
        .map(f => f.getAttribute('src'))`, &frames),
		); err != nil {
			errCh <- err
			return
		}

		for _, p := range top {
			out = append(out, Anchor{Text: p.Text, Href: p.Href})
		}

		// 2) For each iframe URL: navigate to it and collect its anchors too
		for _, fsrc := range frames {
			if strings.TrimSpace(fsrc) == "" {
				continue
			}
			var sub []pair
			if err := run(
				chromedp.Navigate(fsrc),
				chromedp.WaitReady(`body`, chromedp.ByQuery),
				chromedp.Sleep(wait),
				chromedp.Evaluate(`Array.from(document.querySelectorAll('a[href]'))
          .map(a => ({text: (a.innerText||'').trim(), href: a.getAttribute('href')||''}))`, &sub),
			); err != nil {
				// Skip broken frames but continue others
				continue
			}
			for _, p := range sub {
				out = append(out, Anchor{Text: p.Text, Href: p.Href})
			}
		}

		close(done)
	}); err != nil {
		return nil, err
	}

	select {
	case <-done:
		return out, nil
	case e := <-errCh:
		return nil, e
	case <-ctx.Done():
		return nil, fmt.Errorf("timeout: %w", ctx.Err())
	}
}
