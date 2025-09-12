package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"jf/internal/httpx"
	"jf/internal/pool"
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

type ServerCfg struct {
	Addr            string `yaml:"addr"`
	ShutdownTimeout string `yaml:"shutdown_timeout"` // "10s"
}

type HTTPClientCfg struct {
	Timeout      string  `yaml:"timeout"` // "30s"
	RPS          float64 `yaml:"rps"`     // 0 → disabled
	Burst        int     `yaml:"burst"`
	RetryMax     int     `yaml:"retry_max"`
	RetryWaitMin string  `yaml:"retry_wait_min"` // "250ms"
	RetryWaitMax string  `yaml:"retry_wait_max"` // "5s"
	UserAgent    string  `yaml:"user_agent"`
}

type BrowserPoolCfg struct {
	Workers    int    `yaml:"workers"`
	Headless   bool   `yaml:"headless"`
	Queue      int    `yaml:"queue"`
	NavWait    string `yaml:"nav_wait"`    // "800ms"
	NavTimeout string `yaml:"nav_timeout"` // "15s"
}

type WorkerPoolCfg struct {
	Workers int `yaml:"workers"`
	Queue   int `yaml:"queue"`
}

type Config struct {
	// runtime
	Server      ServerCfg      `yaml:"server"`
	HTTPClient  HTTPClientCfg  `yaml:"http_client"`
	BrowserPool BrowserPoolCfg `yaml:"browser_pool"`
	WorkerPool  WorkerPoolCfg  `yaml:"worker_pool"`

	// business
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
	Debug   bool                `yaml:"debug"`
}

// ---- Derived helpers ----

func (p *Config) Addr() string {
	if strings.TrimSpace(p.Server.Addr) == "" {
		return ":8080"
	}
	return p.Server.Addr
}

func (p *Config) ShutdownTimeoutDuration() time.Duration {
	d := strings.TrimSpace(p.Server.ShutdownTimeout)
	if d == "" {
		return 10 * time.Second
	}
	if v, err := time.ParseDuration(d); err == nil {
		return v
	}
	return 10 * time.Second
}

func (p *Config) HTTPX() httpx.Config {
	// sensible defaults if missing
	timeout := parseDur(p.HTTPClient.Timeout, 30*time.Second)
	rmin := parseDur(p.HTTPClient.RetryWaitMin, 250*time.Millisecond)
	rmax := parseDur(p.HTTPClient.RetryWaitMax, 5*time.Second)
	ua := strings.TrimSpace(p.HTTPClient.UserAgent)
	if ua == "" {
		ua = httpx.DefaultUserAgent
	}
	return httpx.Config{
		Timeout:      timeout,
		RPS:          p.HTTPClient.RPS,
		Burst:        nz(p.HTTPClient.Burst, 6),
		RetryMax:     nz(p.HTTPClient.RetryMax, 4),
		RetryWaitMin: rmin,
		RetryWaitMax: rmax,
		UserAgent:    ua,
	}
}

func (p *Config) BrowserPoolConfig() pool.BrowserPoolConfig {
	return pool.BrowserPoolConfig{
		Workers:    nz(p.BrowserPool.Workers, 4),
		Headless:   p.BrowserPool.Headless,
		Queue:      nz(p.BrowserPool.Queue, 256),
		NavWait:    parseDur(p.BrowserPool.NavWait, 1000*time.Millisecond),
		NavTimeout: parseDur(p.BrowserPool.NavTimeout, 15*time.Second),
	}
}

func (p *Config) WorkerPoolConfig() (workers, queue int) {
	return nz(p.WorkerPool.Workers, 8), nz(p.WorkerPool.Queue, 1024)
}

func parseDur(s string, def time.Duration) time.Duration {
	if strings.TrimSpace(s) == "" {
		return def
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	return def
}

func nz(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// ---- Keywords compilation ----

func (p *Config) CompileKeywordSets() {
	p.goodSet = make(map[string]struct{})

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

// ---- ApplyForm merge from GUI (fixed: preserve existing values) ----

func MergeFromApplyForm(base *Config, form url.Values) *Config {
	p := *base // shallow copy of top-level

	// lists: if empty keep existing
	p.GoodKeywords = pickList(form.Get("good_keywords"), base.GoodKeywords)
	p.BadKeywords = pickList(form.Get("bad_keywords"), base.BadKeywords)
	p.CVRoles = pickList(form.Get("cv_roles"), base.CVRoles)

	// scalars (only override if provided)
	if v := strings.TrimSpace(form.Get("cv_profile")); v != "" {
		p.CVProfile = v
	}
	if v := strings.TrimSpace(form.Get("llm_model")); v != "" {
		p.LLMModel = v
	}
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
	if _, ok := form["llm_enabled"]; ok {
		p.LLMEnabled = true
	} else {
		p.LLMEnabled = false
	}
	if _, ok := form["hard_exclude_on_bad"]; ok {
		p.HardExcludeOnBad = true
	} else {
		p.HardExcludeOnBad = false
	}

	// nested ApplyForm: update only fields provided, otherwise keep existing
	if v := strings.TrimSpace(form.Get("apply_form.first_name")); v != "" {
		p.ApplyForm.FirstName = v
	}
	if v := strings.TrimSpace(form.Get("apply_form.last_name")); v != "" {
		p.ApplyForm.LastName = v
	}
	if v := strings.TrimSpace(form.Get("apply_form.email")); v != "" {
		p.ApplyForm.Email = v
	}
	if v := strings.TrimSpace(form.Get("apply_form.phone")); v != "" {
		p.ApplyForm.Phone = v
	}
	if v := strings.TrimSpace(form.Get("apply_form.cv_path")); v != "" {
		p.ApplyForm.CVPath = v
	}
	// checkboxes inside nested struct
	if _, ok := form["apply_form.agree_tos"]; ok {
		p.ApplyForm.AgreeTOS = true
	} else if form.Has("apply_form.agree_tos") {
		p.ApplyForm.AgreeTOS = false
	}
	if _, ok := form["apply_form.forward_to_headhunters"]; ok {
		p.ApplyForm.ForwardToHeadhunters = true
	} else if form.Has("apply_form.forward_to_headhunters") {
		p.ApplyForm.ForwardToHeadhunters = false
	}

	// recompile keyword sets
	p.CompileKeywordSets()
	return &p
}

// ---- FromForm (GUI → Config) ----

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

	cfg := &Config{
		Server: ServerCfg{
			Addr:            get("server.addr"),
			ShutdownTimeout: get("server.shutdown_timeout"),
		},
		HTTPClient: HTTPClientCfg{
			Timeout:      get("http_client.timeout"),
			RPS:          ParseFloat(get("http_client.rps"), 0),
			Burst:        int(ParseFloat(get("http_client.burst"), 0)),
			RetryMax:     int(ParseFloat(get("http_client.retry_max"), 0)),
			RetryWaitMin: get("http_client.retry_wait_min"),
			RetryWaitMax: get("http_client.retry_wait_max"),
			UserAgent:    get("http_client.user_agent"),
		},
		BrowserPool: BrowserPoolCfg{
			Workers:    int(ParseFloat(get("browser_pool.workers"), 0)),
			Headless:   getBool("browser_pool.headless"),
			Queue:      int(ParseFloat(get("browser_pool.queue"), 0)),
			NavWait:    get("browser_pool.nav_wait"),
			NavTimeout: get("browser_pool.nav_timeout"),
		},
		WorkerPool: WorkerPoolCfg{
			Workers: int(ParseFloat(get("worker_pool.workers"), 0)),
			Queue:   int(ParseFloat(get("worker_pool.queue"), 0)),
		},
		ApplyForm: ApplyForm{
			FirstName:            get("apply_form.first_name"),
			LastName:             get("apply_form.last_name"),
			Email:                get("apply_form.email"),
			Phone:                get("apply_form.phone"),
			CVPath:               get("apply_form.cv_path"),
			AgreeTOS:             getBool("apply_form.agree_tos"),
			ForwardToHeadhunters: getBool("apply_form.forward_to_headhunters"),
		},
		CVRoles:            lines("cv_roles"),
		GoodKeywords:       lines("good_keywords"),
		BadKeywords:        lines("bad_keywords"),
		CVProfile:          get("cv_profile"),
		HeuristicThreshold: ParseFloat(get("heuristic_threshold"), 0.28),
		StrongThreshold:    ParseFloat(get("strong_threshold"), 0.50),
		LLMModel:           get("llm_model"),
		LLMEnabled:         getBool("llm_enabled"),
		HardExcludeOnBad:   getBool("hard_exclude_on_bad"),
		MaxLLM:             50,
		MaxPromptChars:     3500,
		MaxConcurrency:     16,
	}
	cfg.CompileKeywordSets()
	return cfg
}

// ---- Load with defaults ----

func Load(path string) (*Config, error) {
	p := &Config{
		Server: ServerCfg{
			Addr:            ":8080",
			ShutdownTimeout: "10s",
		},
		HTTPClient: HTTPClientCfg{
			Timeout:      "30s",
			RPS:          2.0,
			Burst:        6,
			RetryMax:     4,
			RetryWaitMin: "250ms",
			RetryWaitMax: "5s",
			UserAgent:    "",
		},
		BrowserPool: BrowserPoolCfg{
			Workers:    4,
			Headless:   true,
			Queue:      256,
			NavWait:    "800ms",
			NavTimeout: "15s",
		},
		WorkerPool: WorkerPoolCfg{
			Workers: 8,
			Queue:   1024,
		},
		HeuristicThreshold: 0.28,
		StrongThreshold:    0.50,
		LLMModel:           "gpt-4o-mini",
		LLMEnabled:         true,
		HardExcludeOnBad:   true,
		MaxLLM:             50,
		MaxPromptChars:     3500,
		MaxConcurrency:     16,
	}
	if b, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(b, p); err != nil {
			return nil, err
		}
	}
	p.CompileKeywordSets()
	return p, nil
}

// ---- small helpers ----

func pickList(s string, fallback []string) []string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
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
