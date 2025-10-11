package strutil

import (
	"crypto/sha1"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// Normalize normalizes text for comparison
func Normalize(s string) string {
	// Convert to lowercase and trim whitespace
	s = strings.TrimSpace(strings.ToLower(s))

	// Remove extra whitespace
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")

	return s
}

// ContainsFold checks if s contains substr (case-insensitive)
func ContainsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// Tokens splits text into tokens
func Tokens(text string) []string {
	// Simple tokenization - split on non-alphanumeric characters
	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				tokens = append(tokens, strings.ToLower(current.String()))
				current.Reset()
			}
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, strings.ToLower(current.String()))
	}

	return tokens
}

// CanonURL creates a canonical URL for deduplication
func CanonURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return Normalize(rawURL) // Fallback to simple normalization
	}

	u.Fragment = "" // Remove fragment
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(strings.ToLower(k), "utm_") ||
			strings.ToLower(k) == "gclid" ||
			strings.ToLower(k) == "fbclid" {
			q.Del(k) // Remove tracking parameters
		}
	}
	u.RawQuery = q.Encode()
	u.Host = strings.ToLower(u.Host) // Normalize host to lowercase

	// Remove trailing slash if not root path
	if u.Path != "/" && strings.HasSuffix(u.Path, "/") {
		u.Path = strings.TrimSuffix(u.Path, "/")
	}

	// Handle root path case - if path is empty, set it to "/"
	if u.Path == "" {
		u.Path = "/"
	}

	return u.String()
}

// SHA16 returns the first 16 characters of SHA1 hash
func SHA16(s string) string {
	h := sha1.Sum([]byte(s))
	return fmt.Sprintf("%x", h)[:16]
}

// Min returns the minimum of two floats
func Min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// Max returns the maximum of two floats
func Max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// HasPathPrefixSafe safely checks if path has prefix
func HasPathPrefixSafe(path, prefix string) bool {
	if prefix == "" {
		return true
	}
	if path == "" {
		return false
	}

	// Normalize paths
	path = strings.TrimSuffix(path, "/")
	prefix = strings.TrimSuffix(prefix, "/")

	return strings.HasPrefix(path, prefix)
}
