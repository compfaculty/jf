package validators

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/openai/openai-go"
)

func SHA16(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:8]) }
func ContainsFold(hay, needle string) bool {
	return strings.Contains(strings.ToLower(hay), strings.ToLower(needle))
}

func Set(in []string) map[string]struct{} {
	m := make(map[string]struct{}, len(in))
	for _, x := range in {
		m[x] = struct{}{}
	}
	return m
}

func Normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

var tokenRe = regexp.MustCompile(`[a-zA-Z0-9_#+]+`)

func Tokens(s string) []string { return tokenRe.FindAllString(Normalize(s), -1) }

var skipExt = regexp.MustCompile(`(?i)\.(pdf|docx?|xlsx?|csv|zip|png|jpe?g|gif|svg|mp4|mp3)(?:[?#].*)?$`)

func HeuristicMatchScore(text string, good, bad map[string]struct{}) float64 {
	tok := Tokens(text)
	if len(tok) == 0 {
		return 0
	}
	badHits := 0
	for _, t := range tok {
		if _, ok := bad[t]; ok {
			badHits++
		}
	}
	badPenalty := Min(0.60, 0.15*float64(badHits))
	jobGood := 0
	uniq := map[string]struct{}{}
	for _, t := range tok {
		if _, ok := good[t]; ok {
			jobGood++
			uniq[t] = struct{}{}
		}
	}
	var precision, recall float64
	if jobGood > 0 {
		precision = float64(jobGood) / float64(len(tok))
		recall = float64(len(uniq)) / Max(1, float64(len(good)))
	}
	score := 0.65*precision + 0.35*recall - badPenalty
	if score < 0 {
		score = 0
	} else if score > 1 {
		score = 1
	}
	return score
}

func BadHref(href string) bool {
	if strings.TrimSpace(href) == "" {
		return true
	}
	h := strings.TrimSpace(href)
	if strings.HasPrefix(h, "#") || strings.HasPrefix(h, "mailto:") || strings.HasPrefix(h, "javascript:") ||
		strings.HasPrefix(h, "tel:") || strings.HasPrefix(h, "sms:") {
		return true
	}
	return skipExt.MatchString(h)
}

func ShouldConsider(text string, good, bad map[string]struct{}, heuristicThr float64, hardExcludeOnBad bool) bool {
	n := Normalize(text)
	for p := range bad {
		if p != "" && ContainsFold(n, p) {
			return false
		}
	}
	for p := range good {
		if p != "" && ContainsFold(n, p) {
			return true
		}
	}
	if hardExcludeOnBad {
		st := Set(Tokens(text))
		for t := range st {
			if _, hit := bad[t]; hit {
				return false
			}
		}
	}
	return HeuristicMatchScore(text, good, bad) >= heuristicThr
}

func YesNoGate(ctx context.Context, client *openai.Client, model, cvProfile, jobText string) (bool, error) {
	prompt := "Is this candidate a good match for the job? Reply strictly YES or NO.\n\n" +
		"Candidate:\n" + cvProfile + "\n\nJob:\n" + jobText + "\n"

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		// NOTE: Model is a typed alias, not an Opt[string]
		Model: model,

		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are a careful gatekeeper; reply only 'YES' or 'NO'."),
			openai.UserMessage(prompt),
		},
		Temperature: openai.Float(0),
	})
	if err != nil {
		return false, err
	}

	text := ""
	if len(resp.Choices) > 0 {
		text = Normalize(resp.Choices[0].Message.Content)
	}
	return ContainsFold(text, "yes"), nil
}

func CanonURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		if isTracking(k) {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Path = path.Clean(u.Path)
	return strings.ToLower(u.String())
}

func isTracking(k string) bool {
	switch strings.ToLower(k) {
	case "utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content", "utm_id", "gclid", "fbclid", "mc_cid", "mc_eid", "igshid", "msclkid", "pk_campaign", "pk_kwd", "ref", "ref_src", "ref_url", "s", "spm", "mkt_tok":
		return true
	}
	return false
}
func Min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func Max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func HasPathPrefix(gotPath, basePath string) bool {
	// Normalize both and ensure prefix match on segment boundary
	got := EnsureTrailingSlash(path.Clean(gotPath))
	base := EnsureTrailingSlash(path.Clean(basePath))
	return strings.HasPrefix(got, base)
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
