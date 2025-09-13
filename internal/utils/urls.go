package utils

import (
	"net/url"
	"strings"
)

func ResolveAgainst(base *url.URL, href string) (*url.URL, bool) {
	h := strings.TrimSpace(href)
	if h == "" || base == nil {
		return nil, false
	}
	ref, err := url.Parse(h)
	if err != nil {
		return nil, false
	}
	u := base.ResolveReference(ref) // ★ key line: make it absolute
	return u, true
}

func CanonURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return strings.ToLower(raw)
	}
	// Do NOT remove scheme/host; just normalize pieces.
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		if isTracking(k) {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	// lower-case host, keep path as-is
	u.Host = strings.ToLower(u.Host)
	return u.String()
}

func isTracking(k string) bool {
	switch strings.ToLower(k) {
	case "utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content", "utm_id",
		"gclid", "fbclid", "mc_cid", "mc_eid", "igshid", "msclkid",
		"pk_campaign", "pk_kwd", "ref", "ref_src", "ref_url", "s", "spm", "mkt_tok":
		return true
	}
	return false
}
