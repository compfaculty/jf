package scrape

import (
	"net/url"
	"regexp"
	"strings"

	"jf/internal/models"
)

func ensureTrailingSlash(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasSuffix(p, "/") {
		return p + "/"
	}
	return p
}

func normWS(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

var nonSlug = regexp.MustCompile(`[^a-z0-9\-]+`)

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " & ", " and ")
	s = strings.ReplaceAll(s, "/", " ")
	s = strings.Join(strings.Fields(s), "-")
	s = nonSlug.ReplaceAllString(s, "")
	return s
}

func slugToTitle(slug string) string {
	slug = strings.Trim(slug, "/")
	if slug == "" {
		return ""
	}
	parts := strings.Split(slug, "/")
	last := parts[len(parts)-1]
	last = strings.ReplaceAll(last, "-", " ")
	return strings.Title(last) // simple fallback; acceptable here
}

// joinWS compacts whitespace to single spaces.
func joinWS(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

// resolveURLMust resolves rel against baseRaw, returns a best-effort string.
func resolveURLMust(baseRaw, rel string) string {
	b, err := url.Parse(baseRaw)
	if err != nil {
		return rel
	}
	r, err := url.Parse(rel)
	if err != nil {
		return baseRaw
	}
	return b.ResolveReference(r).String()
}

// dedupeScraped removes duplicates by Title|URL (case-insensitive).
func dedupeScraped(in []models.ScrapedJob) []models.ScrapedJob {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]models.ScrapedJob, 0, len(in))
	for _, j := range in {
		k := strings.ToLower(strings.TrimSpace(j.Title)) + "|" + strings.ToLower(strings.TrimSpace(j.URL))
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, j)
	}
	return out
}

func EnsureTrailingSlash(p string) string {
	if p == "" || strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}

// BadHref returns true for empty/anchor/scheme-junk and for common
// non-navigational assets (docs, images, media, archives).
var badHrefExtRe = regexp.MustCompile(`(?i)\.(pdf|docx?|xlsx?|csv|zip|rar|7z|png|jpe?g|gif|svg|webp|mp4|mp3|avi|mov|wmv)(?:[?#].*)?$`)

func BadHref(href string) bool {
	h := strings.TrimSpace(href)
	if h == "" {
		return true
	}
	hl := strings.ToLower(h)

	// obvious non-navigational schemes / anchors
	if strings.HasPrefix(hl, "#") ||
		strings.HasPrefix(hl, "javascript:") ||
		strings.HasPrefix(hl, "mailto:") ||
		strings.HasPrefix(hl, "tel:") ||
		strings.HasPrefix(hl, "sms:") ||
		strings.HasPrefix(hl, "data:") {
		return true
	}

	// common file extensions we don't want to follow
	return badHrefExtRe.MatchString(hl)
}
