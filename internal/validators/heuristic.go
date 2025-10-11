package validators

import "jf/internal/strutil"

func HeuristicMatchScore(text string, good, bad map[string]struct{}) float64 {
	tok := strutil.Tokens(text)
	if len(tok) == 0 {
		return 0
	}

	// bad penalty
	badHits := 0
	for _, t := range tok {
		if _, ok := bad[t]; ok {
			badHits++
		}
	}
	badPenalty := strutil.Min(0.60, 0.15*float64(badHits))

	// good precision/recall
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
		recall = float64(len(uniq)) / strutil.Max(1, float64(len(good)))
	}

	score := 0.65*precision + 0.35*recall - badPenalty
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
