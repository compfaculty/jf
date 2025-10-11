package validators

import (
	"context"
	"jf/internal/config"
	"jf/internal/strutil"
	"strings"
	"time"

	"github.com/openai/openai-go"
)

type Heuristics struct {
	Good        map[string]struct{}
	Bad         map[string]struct{}
	Threshold   float64 // heuristic accept cutoff
	Strong      float64 // strong accept cutoff
	HardExclude bool    // drop immediately on bad hit
}

func DefaultHeuristics(cfg *config.Config) *Heuristics {
	good, bad := cfg.GoodBadKeywordSets()
	return &Heuristics{
		Good:        good,
		Bad:         bad,
		Threshold:   cfg.HeuristicThreshold,
		Strong:      cfg.StrongThreshold,
		HardExclude: cfg.HardExcludeOnBad,
	}
}

// ClassifyResult is a tiny, storage-friendly result.
type ClassifyResult struct {
	IsTechJob  bool    `json:"is_tech_job"`
	Confidence float64 `json:"confidence"` // [0..1]
	Reason     string  `json:"reason"`
	Source     string  `json:"source"` // "heuristic-accept" | "heuristic-reject" | "llm" | "empty"
}

// ClassifyYesNoOpts allows light tuning without complexity.
type ClassifyYesNoOpts struct {
	// Optional: override thresholds via DefaultHeuristics() if you already do that globally.
	Heuristics *Heuristics

	// Optional: timeout for the LLM call (default: 20s). Keep short; we only need YES/NO.
	Timeout time.Duration
}

// ClassifyTechJob keeps your Yes/No flavor:
//  1. Heuristic short-circuit (accept/reject bands).
//  2. Otherwise the LLM gatekeeper asked to reply strictly YES/NO.
//
// It never panics. If the API key/model/client is missing or errors, you’ll get Source="empty".
func ClassifyTechJob(
	ctx context.Context,
	client *openai.Client,
	model openai.ChatModel, // note: typed alias in your SDK
	jobText string,
	opts *ClassifyYesNoOpts,
	cfg *config.Config,
) (ClassifyResult, error) {
	h := DefaultHeuristics(cfg)
	timeout := 20 * time.Second
	if opts != nil {
		if opts.Heuristics != nil {
			h = opts.Heuristics
		}
		if opts.Timeout > 0 {
			timeout = opts.Timeout
		}
	}

	t := strings.TrimSpace(jobText)
	if t == "" {
		return ClassifyResult{
			IsTechJob:  false,
			Confidence: 0.0,
			Reason:     "Empty text",
			Source:     "empty",
		}, nil
	}

	// --- 1) Heuristic prefilter
	hs := HeuristicScore(t, "description", h)
	fastAccept := FastAcceptBand(h) // max(0.80, strong_threshold)
	fastReject := FastRejectBand(h) // min(0.20, heuristic_threshold/2)

	if hs >= fastAccept {
		return ClassifyResult{
			IsTechJob:  true,
			Confidence: clamp01(hs),
			Reason:     "Heuristic strong match",
			Source:     "heuristic-accept",
		}, nil
	}
	if hs <= fastReject {
		return ClassifyResult{
			IsTechJob:  false,
			Confidence: clamp01(1.0 - hs),
			Reason:     "Heuristic strong non-match",
			Source:     "heuristic-reject",
		}, nil
	}

	// --- 2) Ambiguous → LLM YES/NO gate (like your YesNoGate) ---
	// If client/model not provided, behave like your Python "no API" path.
	if client == nil || (string(model) == "") {
		return ClassifyResult{
			IsTechJob:  false,
			Confidence: 0.0,
			Reason:     "",
			Source:     "empty",
		}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// A tight prompt that mirrors your gate:
	sys := "You are a strict gatekeeper. Reply only with YES or NO."
	user := "Does the following text clearly describe a technology job (software, data, security, DevOps/SRE, infra/platform, QA/automation, ML/AI, backend/fullstack)?\n\n" +
		"TEXT:\n" + t + "\n"

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(sys),
			openai.UserMessage(user),
		},
		Temperature: openai.Float(0),
		// Keep it minimal; no streaming, no extra fields needed.
	})
	if err != nil {
		// Mirror lightweight behavior: no result if LLM fails.
		return ClassifyResult{
			IsTechJob:  false,
			Confidence: 0.0,
			Reason:     "",
			Source:     "empty",
		}, nil
	}

	out := ""
	if len(resp.Choices) > 0 {
		out = strings.TrimSpace(strings.ToLower(resp.Choices[0].Message.Content))
	}

	yes := containsFold(out, "yes")
	// Gentle calibration (same spirit as your Python bands):
	// if LLM says YES, but we got here (ambiguous heuristics), set a modest confidence 0.60
	// else a modest 0.40.
	conf := 0.40
	reason := "LLM NO"
	if yes {
		conf = 0.60
		reason = "LLM YES"
	}

	return ClassifyResult{
		IsTechJob:  yes,
		Confidence: conf,
		Reason:     reason,
		Source:     "llm",
	}, nil
}

// HeuristicScore computes a score in [0,1] using your good/bad sets.
// mode: "title" | "href" | "description" (default)
func HeuristicScore(text, mode string, h *Heuristics) float64 {
	if h == nil {
		// Defensive default: empty heuristics behave like "reject".
		return 0
	}
	t := strings.TrimSpace(text)
	if t == "" {
		return 0
	}
	toks := strutil.Tokens(t) //  regex-based tokenizer
	if len(toks) == 0 {
		return 0
	}

	// Fast membership helpers
	inGood := func(tok string) bool {
		_, ok := h.Good[strings.ToLower(tok)]
		return ok
	}
	inBad := func(tok string) bool {
		_, ok := h.Bad[strings.ToLower(tok)]
		return ok
	}

	// Count hits
	badHits := 0
	goodHits := 0
	uniq := make(map[string]struct{}, len(toks))
	for _, tok := range toks {
		lc := strings.ToLower(tok)
		if inBad(lc) {
			badHits++
		}
		if inGood(lc) {
			goodHits++
		}
		uniq[lc] = struct{}{}
	}

	// Penalty for bad density (cap at 0.60)
	badPenalty := strutil.Min(0.60, 0.15*float64(badHits))

	// Precision / recall-ish terms
	var precision, recall float64
	if len(uniq) > 0 {
		precision = float64(goodHits) / float64(len(uniq))
	}
	if lg := float64(len(h.Good)); lg > 0 {
		// count unique good tokens present
		uniqueGood := 0
		for g := range h.Good {
			if _, ok := uniq[g]; ok {
				uniqueGood++
			}
		}
		recall = float64(uniqueGood) / lg
	}

	// Mode shaping
	var base float64
	switch mode {
	case "title":
		// concise text: prefer precision, reduce max penalty effect a bit
		// base weighting + slight boost
		base = 0.75*precision + 0.25*recall
		if badPenalty > 0.45 {
			badPenalty = 0.45
		}
		return clamp01(base*1.15 - badPenalty)
	case "href":
		// noisier signal: lower recall weight
		base = 0.60*precision + 0.40*recall
		return clamp01(base*1.05 - badPenalty)
	default: // "description"
		base = 0.65*precision + 0.35*recall
		return clamp01(base*1.25 - badPenalty)
	}
}

// FastAcceptBand mirrors the Python: max(0.80, strong_threshold)
func FastAcceptBand(h *Heuristics) float64 {
	if h == nil {
		return 0.80
	}
	if h.Strong < 0.80 {
		return 0.80
	}
	return h.Strong
}

// FastRejectBand mirrors the Python: min(0.20, heuristic_threshold/2)
func FastRejectBand(h *Heuristics) float64 {
	if h == nil {
		return 0.20
	}
	half := h.Threshold / 2.0
	if half > 0.20 {
		return 0.20
	}
	return half
}

// ---- tiny local helpers (kept here to avoid cross-package dependencies) ----

func containsFold(s, needle string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(needle))
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
