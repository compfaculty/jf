package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ApplyForm struct {
	FirstName            string `yaml:"first_name"`
	LastName             string `yaml:"last_name"`
	Email                string `yaml:"email"`
	Phone                string `yaml:"phone"`
	CVPath               string `yaml:"cv_path"`
	AgreeTOS             bool   `yaml:"agree_tos"`
	ForwardToHeadhunters bool   `yaml:"forward_to_headhunters"`
}

type Config struct {
	ApplyForm ApplyForm `yaml:"apply_form"`

	CVRoles []string `yaml:"cv_roles"`

	GoodKeywords []string `yaml:"good_keywords"`
	BadKeywords  []string `yaml:"bad_keywords"`

	CVProfile          string  `yaml:"cv_profile"`
	HeuristicThreshold float64 `yaml:"heuristic_threshold"`
	StrongThreshold    float64 `yaml:"strong_threshold"`
	LLMModel           string  `yaml:"llm_model"`
	LLMEnabled         bool    `yaml:"llm_enabled"`
	HardExcludeOnBad   bool    `yaml:"hard_exclude_on_bad"`

	MaxLLM         int `yaml:"max_llm"`
	MaxPromptChars int `yaml:"max_prompt_chars"`
	MaxConcurrency int `yaml:"max_concurrency"`

	// compiled
	goodSet map[string]struct{} `yaml:"-"`
	badSet  map[string]struct{} `yaml:"-"`
}

func (p *Config) CompileKeywordSets() {
	p.goodSet = make(map[string]struct{}, len(p.GoodKeywords)+len(p.CVRoles))

	// merge GoodKeywords + CVRoles
	merged := append(append([]string{}, p.GoodKeywords...), p.CVRoles...)
	for _, s := range merged {
		if t := strings.TrimSpace(strings.ToLower(s)); t != "" {
			p.goodSet[t] = struct{}{}
		}
	}

	p.badSet = make(map[string]struct{}, len(p.BadKeywords))
	for _, s := range p.BadKeywords {
		if t := strings.TrimSpace(strings.ToLower(s)); t != "" {
			p.badSet[t] = struct{}{}
		}
	}
}

func (p *Config) GoodBadKeywordSets() (map[string]struct{}, map[string]struct{}) {
	if p.goodSet == nil || p.badSet == nil {
		p.CompileKeywordSets()
	}
	return p.goodSet, p.badSet
}

func (p *Config) GoodSet() map[string]struct{} { return p.goodSet }
func (p *Config) BadSet() map[string]struct{}  { return p.badSet }
func MergeFromApplyForm(base *Config, form url.Values) *Config {
	// copy base so we don't mutate it
	p := *base

	// text lists: if the textarea is empty, keep existing from YAML
	p.GoodKeywords = pickList(form.Get("good_keywords"), base.GoodKeywords)
	p.BadKeywords = pickList(form.Get("bad_keywords"), base.BadKeywords)
	p.CVRoles = pickList(form.Get("cv_roles"), base.CVRoles)

	// strings
	if v := strings.TrimSpace(form.Get("cv_profile")); v != "" {
		p.CVProfile = v
	}
	if v := strings.TrimSpace(form.Get("llm_model")); v != "" {
		p.LLMModel = v
	}

	// numbers (only override if provided)
	if v := strings.TrimSpace(form.Get("heuristic_threshold")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.HeuristicThreshold = f
		}
	}
	if v := strings.TrimSpace(form.Get("strong_threshold")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.StrongThreshold = f
		}
	}

	// checkboxes: present => true, absent => false
	p.LLMEnabled = form.Has("llm_enabled")
	p.HardExcludeOnBad = form.Has("hard_exclude_on_bad")

	// nested ApplyForm
	// TODO apply form is empty in the end , fix
	p.ApplyForm.FirstName = strings.TrimSpace(form.Get("apply_form.first_name"))
	p.ApplyForm.LastName = strings.TrimSpace(form.Get("apply_form.last_name"))
	p.ApplyForm.Email = strings.TrimSpace(form.Get("apply_form.email"))
	p.ApplyForm.Phone = strings.TrimSpace(form.Get("apply_form.phone"))
	p.ApplyForm.CVPath = strings.TrimSpace(form.Get("apply_form.cv_path"))
	p.ApplyForm.AgreeTOS = form.Has("apply_form.agree_tos")
	p.ApplyForm.ForwardToHeadhunters = form.Has("apply_form.forward_to_headhunters")

	return &p
}

func pickList(s string, fallback []string) []string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	// Split on newlines, commas, semicolons, tabs; trim and drop empties
	parts := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case '\n', '\r', ',', ';', '\t':
			return true
		}
		return false
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func LoadPreferences(path string) (*Config, error) {
	// defaults that match your YAML semantics
	p := &Config{
		HeuristicThreshold: 0.28,
		StrongThreshold:    0.50,
		LLMModel:           "gpt-4o-mini",
		LLMEnabled:         true,
		HardExcludeOnBad:   true,
		MaxLLM:             50,
		MaxPromptChars:     3500,
		MaxConcurrency:     16,
	}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := yaml.Unmarshal(b, p); err != nil {
			return nil, err
		}
	}
	p.CompileKeywordSets()
	return p, nil
}

// FromForm Parse from form values (Web GUI)
func FromForm(f url.Values) *Config {
	get := func(k string) string { return strings.TrimSpace(f.Get(k)) }
	getBool := func(k string) bool { return f.Get(k) == "on" || strings.EqualFold(f.Get(k), "true") }
	lines := func(k string) []string {
		raw := get(k)
		var out []string
		for _, ln := range strings.Split(raw, "\n") {
			ln = strings.TrimSpace(ln)
			if ln != "" {
				out = append(out, ln)
			}
		}
		return out
	}
	p := &Config{
		ApplyForm: ApplyForm{
			FirstName:            get("apply_form.first_name"),
			LastName:             get("apply_form.last_name"),
			Email:                get("apply_form.email"),
			Phone:                get("apply_form.phone"),
			CVPath:               get("apply_form.cv_path"),
			AgreeTOS:             getBool("apply_form.agree_tos"),
			ForwardToHeadhunters: getBool("apply_form.forward_to_headhunters"),
		},
		CVRoles: lines("cv_roles"),

		GoodKeywords: lines("good_keywords"),
		BadKeywords:  lines("bad_keywords"),

		CVProfile:          get("cv_profile"),
		HeuristicThreshold: ParseFloat(get("heuristic_threshold"), 0.28),
		StrongThreshold:    ParseFloat(get("strong_threshold"), 0.50),
		LLMModel:           get("llm_model"),
		LLMEnabled:         getBool("llm_enabled"),
		HardExcludeOnBad:   getBool("hard_exclude_on_bad"),

		MaxLLM:         50,
		MaxPromptChars: 3500,
		MaxConcurrency: 16,
	}
	return p
}

func ParseFloat(s string, def float64) float64 {
	if s == "" {
		return def
	}
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	if err != nil {
		return def
	}
	return f
}
