package utils

import (
	"net/url"
	"path"
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

func ResolveURL(base *url.URL, href string) string {
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return base.ResolveReference(ref).String()
}

func ResolveURLMust(baseStr, href string) string {
	b, _ := url.Parse(baseStr)
	return ResolveURL(b, href)
}

// HasPathPrefix enforces a segment-safe prefix match:
// "/careers/" matches "/careers/eng/.." but not "/careers-old/".
func HasPathPrefix(gotPath, basePath string) bool {
	got := EnsureTrailingSlash(path.Clean(gotPath))
	base := EnsureTrailingSlash(path.Clean(basePath))
	return strings.HasPrefix(got, base)
}

// HasPathPrefixSafe returns true if child is the same path as base, or under it.
// Treats "/careers" and "/careers/" as equivalent.
func HasPathPrefixSafe(child, base string) bool {
	bc := path.Clean("/" + strings.TrimSpace(base)) // always starts with /
	cc := path.Clean("/" + strings.TrimSpace(child))
	// allow equality with/without trailing slash
	if cc == bc {
		return true
	}
	// allow child strictly under base (with a boundary slash)
	if strings.HasSuffix(bc, "/") {
		bc = strings.TrimSuffix(bc, "/")
	}
	return strings.HasPrefix(cc, bc+"/")
}

func EnsureTrailingSlash(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasSuffix(p, "/") {
		return p + "/"
	}
	return p
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

func DomainContains(raw, substr string) bool {
	u, _ := url.Parse(raw)
	return strings.Contains(strings.ToLower(u.Host), strings.ToLower(substr))
}

func HostFromURL(rawURL string) string {
	// add scheme if missing (e.g., "example.com" -> "https://example.com")
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL // fallback: return input unchanged
	}

	host := strings.ToLower(u.Hostname()) // strip port and normalize case
	return host
}
