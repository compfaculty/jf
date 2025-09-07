package httpx

import (
	"net"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// Build once per config and reuse.
func NewHTTPClient() *http.Client {
	tr := &http.Transport{
		// Dialer timeouts
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,

		// Connection pooling
		MaxIdleConns:        200, // total
		MaxIdleConnsPerHost: 100, // per host
		IdleConnTimeout:     90 * time.Second,

		// TLS / HTTP behavior
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true, // explicit
	}

	return &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second, // whole request deadline (headers+body)
	}
}

// Optional: a simple wrapper for rate-limiting requests.
type RLClient struct {
	*http.Client
	lim *rate.Limiter
}

func NewRateLimited(c *http.Client, rps float64, burst int) *RLClient {
	return &RLClient{Client: c, lim: rate.NewLimiter(rate.Limit(rps), burst)}
}

func (c *RLClient) Do(req *http.Request) (*http.Response, error) {
	// Per-request context timeout should be set by caller via context.WithTimeout.
	if err := c.lim.Wait(req.Context()); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}
