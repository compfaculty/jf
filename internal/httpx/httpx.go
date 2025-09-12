package httpx

import (
	"math"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// DefaultUserAgent – realistic desktop UA (can override via config/env).
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) " +
	"Chrome/139.0.0.0 Safari/537.36"

// Config controls behavior of the Client.
type Config struct {
	Timeout      time.Duration
	RPS          float64
	Burst        int
	RetryMax     int
	RetryWaitMin time.Duration
	RetryWaitMax time.Duration
	UserAgent    string
}

// Client is a robust Doer with pooled http.Client, optional rate-limit,
// default User-Agent, and automatic retries for transient errors.
type Client struct {
	http   *http.Client
	lim    *rate.Limiter
	retryN int
	bwMin  time.Duration
	bwMax  time.Duration
}

func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.Burst <= 0 {
		cfg.Burst = 10
	}
	if cfg.RetryMax <= 0 {
		cfg.RetryMax = 4
	}
	if cfg.RetryWaitMin <= 0 {
		cfg.RetryWaitMin = 250 * time.Millisecond
	}
	if cfg.RetryWaitMax <= 0 {
		cfg.RetryWaitMax = 5 * time.Second
	}
	ua := strings.TrimSpace(cfg.UserAgent)
	if ua == "" {
		ua = DefaultUserAgent
	}

	tr := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,

		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,

		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	hc := &http.Client{
		Transport: userAgentRoundTripper{ua: ua, rt: tr},
		Timeout:   cfg.Timeout,
	}

	var lim *rate.Limiter
	if cfg.RPS > 0 {
		lim = rate.NewLimiter(rate.Limit(cfg.RPS), cfg.Burst)
	}

	return &Client{
		http:   hc,
		lim:    lim,
		retryN: cfg.RetryMax,
		bwMin:  cfg.RetryWaitMin,
		bwMax:  cfg.RetryWaitMax,
	}
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if c.lim != nil {
		if err := c.lim.Wait(req.Context()); err != nil {
			return nil, err
		}
	}

	var resp *http.Response
	var err error

	for attempt := 1; attempt <= c.retryN; attempt++ {
		resp, err = c.http.Do(req)
		if err == nil && !shouldRetryStatus(resp.StatusCode) {
			return resp, nil
		}

		if resp != nil && shouldRetryStatus(resp.StatusCode) {
			if d := retryAfterDelay(resp); d > 0 {
				select {
				case <-time.After(d):
				case <-req.Context().Done():
					_ = resp.Body.Close()
					return nil, req.Context().Err()
				}
			}
			_ = resp.Body.Close()
		}

		if attempt == c.retryN {
			break
		}
		backoff := c.backoffDelay(attempt)
		select {
		case <-time.After(backoff):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}

	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) backoffDelay(attempt int) time.Duration {
	pow := math.Pow(2, float64(attempt-1))
	d := time.Duration(float64(c.bwMin) * pow)
	if d > c.bwMax {
		d = c.bwMax
	}
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(d)))
}

func shouldRetryStatus(code int) bool {
	switch code {
	case 408, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func retryAfterDelay(r *http.Response) time.Duration {
	v := strings.TrimSpace(r.Header.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(v); err == nil {
		d := time.Until(when)
		if d > 0 && d < 10*time.Minute {
			return d
		}
	}
	return 0
}

type userAgentRoundTripper struct {
	ua string
	rt http.RoundTripper
}

func (u userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" && u.ua != "" {
		req.Header.Set("User-Agent", u.ua)
	}
	return u.rt.RoundTrip(req)
}

// RLClient Back-compat helpers (optional to keep around)
type RLClient struct {
	*http.Client
	lim *rate.Limiter
}

func (c *RLClient) Do(req *http.Request) (*http.Response, error) {
	if c.lim != nil {
		if err := c.lim.Wait(req.Context()); err != nil {
			return nil, err
		}
	}
	return c.Client.Do(req)
}
