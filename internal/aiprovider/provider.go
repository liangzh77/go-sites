// Package aiprovider holds the OpenAI-compatible upstreams shared by the
// school demos, plus the Beijing-time schedule that chooses between them.
//
// Two accounts, two windows: DeepSeek's own API from 20:00 to 02:00 Beijing
// time, Ollama Cloud for the rest of the day. The window is decided per
// request from the wall clock, never by a fallback: a failing upstream is
// reported, not silently replaced by the other model.
package aiprovider

import (
	"os"
	"strings"
	"time"
)

const (
	// OfficialStartHour and OfficialEndHour bound the DeepSeek window as
	// [20:00, 02:00) on the Beijing wall clock.
	OfficialStartHour = 20
	OfficialEndHour   = 2
	// Window is the human-readable form used in status responses and docs.
	Window = "北京时间 20:00–02:00 用 DeepSeek 官方模型，其余时段用 Ollama Cloud"
)

// Beijing is where both the schedule and the daily quota day cut over. The
// server clock is UTC, so nothing here may use time.Local.
var Beijing = time.FixedZone("Asia/Shanghai", 8*60*60)

// Provider is one OpenAI-compatible upstream and the account that pays for it.
type Provider struct {
	Name    string
	BaseURL string
	Model   string
	APIKey  string
}

// Endpoint joins the configured base with the chat completions path. The base
// is a base, not a full URL: a trailing slash or an already-appended
// "/chat/completions" is tolerated, so the same value works for every gateway.
func (p Provider) Endpoint() string {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	base = strings.TrimSuffix(base, "/chat/completions")
	if base == "" {
		return ""
	}
	return base + "/chat/completions"
}

// Configured reports whether this upstream can actually be called.
func (p Provider) Configured() bool { return p.APIKey != "" && p.Endpoint() != "" }

// Set is the pair of accounts. Neither is a backup for the other: Active
// decides, and only Active is used.
type Set struct {
	Official Provider // DeepSeek's own API, 20:00-02:00 Beijing
	Cloud    Provider // Ollama Cloud, every other hour
}

// FromEnv reads the two account blocks. Model names fall back to the ones the
// accounts are subscribed to so a missing variable is not a silent blank.
func FromEnv() Set {
	return Set{
		Official: Provider{
			Name:    "DeepSeek",
			BaseURL: os.Getenv("DEEPSEEK_BASE_URL"),
			Model:   envOrDefault("DEEPSEEK_MODEL", "deepseek-flash"),
			APIKey:  strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")),
		},
		Cloud: Provider{
			Name:    "Ollama Cloud",
			BaseURL: os.Getenv("OLLAMA_BASE_URL"),
			Model:   envOrDefault("OLLAMA_MODEL", "deepseek-v4.1-flash"),
			APIKey:  strings.TrimSpace(os.Getenv("OLLAMA_API_KEY")),
		},
	}
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// Active picks the account for one request, by the Beijing wall clock.
func (s Set) Active(now time.Time) Provider {
	hour := now.In(Beijing).Hour()
	if hour >= OfficialStartHour || hour < OfficialEndHour {
		return s.Official
	}
	return s.Cloud
}

// Describe is the non-secret status summary: who will serve the next request.
func (s Set) Describe(now time.Time) map[string]any {
	active := s.Active(now)
	return map[string]any{
		"configured": active.Configured(),
		"provider":   active.Name,
		"model":      active.Model,
		"window":     Window,
		"hours":      []int{OfficialStartHour, OfficialEndHour},
	}
}
