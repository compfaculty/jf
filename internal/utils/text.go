package utils

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

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

func RandID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
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

func MultipartWithFile(fields url.Values, filePath string) (io.Reader, string, error) {
	pr, pw := io.Pipe()
	w := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		defer w.Close()

		// regular fields
		for k, vs := range fields {
			for _, v := range vs {
				_ = w.WriteField(k, v)
			}
		}

		// file part
		fn := filepath.Base(filePath)
		_ = mime.TypeByExtension(filepath.Ext(fn)) // best-effort mime registration
		fw, err := w.CreateFormFile("file", fn)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}

		f, err := os.Open(filePath)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		defer f.Close()

		if _, err := io.Copy(fw, f); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
	}()
	return pr, w.FormDataContentType(), nil
}

func HostHasSuffix(host string, suffixes []string) bool {
	for _, s := range suffixes {
		if strings.EqualFold(host, s) || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

func HasToken(s string, tokens []string) bool {
	for _, t := range tokens {
		if strings.Contains(s, t) {
			return true
		}
	}
	return false
}

// GetEnv retrieves an environment variable with a default fallback
func GetEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
