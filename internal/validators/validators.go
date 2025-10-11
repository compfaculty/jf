package validators

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"jf/internal/strutil"

	"github.com/openai/openai-go"
)

// Common non-navigational file extensions (light set used only in this package).
var skipExt = regexp.MustCompile(`(?i)\.(pdf|docx?|xlsx?|csv|zip|rar|7z|png|jpe?g|gif|svg|webp|mp4|mp3|avi|mov|wmv)(?:[?#].*)?$`)

// Local minimal href guard for this package (avoid importing scrape/* and cycles)
func badHrefLite(href string) bool {
	h := strings.TrimSpace(href)
	if h == "" {
		return true
	}
	hl := strings.ToLower(h)
	if strings.HasPrefix(hl, "#") ||
		strings.HasPrefix(hl, "javascript:") ||
		strings.HasPrefix(hl, "mailto:") ||
		strings.HasPrefix(hl, "tel:") ||
		strings.HasPrefix(hl, "sms:") ||
		strings.HasPrefix(hl, "data:") {
		return true
	}
	return skipExt.MatchString(hl)
}

/********** heuristics **********/

// ShouldConsider: quick good/bad gates + heuristic threshold.
func ShouldConsider(text string, good, bad map[string]struct{}, heuristicThr float64, hardExcludeOnBad bool) bool {
	n := strutil.Normalize(text)

	// hard substring hits on bad → immediate reject
	for p := range bad {
		if p != "" && strutil.ContainsFold(n, p) {
			return false
		}
	}
	// any good substring → quick accept
	for p := range good {
		if p != "" && strutil.ContainsFold(n, p) {
			return true
		}
	}
	// optional hard exclude on tokenized bad terms
	if hardExcludeOnBad {
		for _, t := range strutil.Tokens(text) {
			if _, hit := bad[t]; hit {
				return false
			}
		}
	}
	return HeuristicMatchScore(text, good, bad) >= heuristicThr
}

/********** unified decisions **********/

// MustJobLink returns true if (text, href, base) looks like a valid in-scope job link.
func MustJobLink(
	text, href string,
	base *url.URL,
	good, bad map[string]struct{},
	heuristicThr float64,
	hardExcludeOnBad bool,
) bool {
	ok, _ := MustJobLinkURL(text, href, base, good, bad, heuristicThr, hardExcludeOnBad)
	return ok
}

// MustJobLinkURL returns (true, cleanedAbsoluteURL) when link passes structural
// scope checks and heuristics; otherwise (false, "").
func MustJobLinkURL(
	text, href string,
	base *url.URL,
	good, bad map[string]struct{},
	heuristicThr float64,
	hardExcludeOnBad bool,
) (bool, string) {
	// 1) Quick structural rejects
	if base == nil || badHrefLite(href) {
		return false, ""
	}

	// 2) Resolve & normalize
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return false, ""
	}
	u := base.ResolveReference(ref)
	u.Fragment = ""

	// 3) Scope: same host+scheme+path OR allow well-known ATS hosts
	sameScheme := strings.EqualFold(u.Scheme, base.Scheme)
	sameHost := strings.EqualFold(u.Hostname(), base.Hostname())
	//underBase := strutil.HasPathPrefixSafe(u.Path, base.Path)

	//allow := sameScheme && sameHost && underBase
	allow := sameScheme && sameHost
	if !allow {
		// Known ATS domains frequently used from careers pages
		atsHost := strings.ToLower(u.Hostname())
		switch {
		case strings.Contains(atsHost, "greenhouse.io"),
			strings.Contains(atsHost, "boards.greenhouse.io"),
			strings.Contains(atsHost, "lever.co"),
			strings.Contains(atsHost, "jobs.lever.co"),
			strings.Contains(atsHost, "workable.com"),
			strings.Contains(atsHost, "bamboohr.com"),
			strings.Contains(atsHost, "smartrecruiters.com"),
			strings.Contains(atsHost, "recruitee.com"),
			strings.Contains(atsHost, "ashbyhq.com"),
			strings.Contains(atsHost, "comeet.co"):
			allow = true
		}
	}
	if !allow {
		return false, ""
	}

	// 4) Semantic filters on combined text+URL
	combined := strings.TrimSpace(text + " " + u.String())

	if hardExcludeOnBad {
		for _, t := range strutil.Tokens(combined) {
			if _, hit := bad[t]; hit {
				return false, ""
			}
		}
	}

	if HeuristicMatchScore(combined, good, bad) < heuristicThr {
		return false, ""
	}

	// 5) Return canonical absolute URL (stable for dedupe)
	return true, strutil.CanonURL(u.String())
}

/********** optional LLM gate **********/

func YesNoGate(ctx context.Context, client *openai.Client, model, cvProfile, jobText string) (bool, error) {
	prompt := "Is this candidate a good match for the job? Reply strictly YES or NO.\n\n" +
		"Candidate:\n" + cvProfile + "\n\nJob:\n" + jobText + "\n"

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
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
		text = strutil.Normalize(resp.Choices[0].Message.Content)
	}
	return strutil.ContainsFold(text, "yes"), nil
}
