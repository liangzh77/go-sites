package aiprovider

import (
	"testing"
	"time"
)

func TestEndpointJoinsThePathOnce(t *testing.T) {
	cases := map[string]string{
		"https://api.deepseek.com":                   "https://api.deepseek.com/chat/completions",
		"https://api.deepseek.com/":                  "https://api.deepseek.com/chat/completions",
		"https://api.deepseek.com/v1":                "https://api.deepseek.com/v1/chat/completions",
		"https://ollama.com/v1":                      "https://ollama.com/v1/chat/completions",
		"https://opencode.ai/inference/go/openai/v1": "https://opencode.ai/inference/go/openai/v1/chat/completions",
		"https://api.deepseek.com/chat/completions":  "https://api.deepseek.com/chat/completions",
		"": "",
	}
	for base, want := range cases {
		if got := (Provider{BaseURL: base}).Endpoint(); got != want {
			t.Errorf("Endpoint(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestScheduleBoundaries(t *testing.T) {
	set := Set{
		Official: Provider{Name: "DeepSeek", BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", APIKey: "a"},
		Cloud:    Provider{Name: "Ollama Cloud", BaseURL: "https://ollama.com/v1", Model: "deepseek-v4.1-flash", APIKey: "b"},
	}
	cases := []struct {
		beijing string
		want    string
	}{
		{"2026-11-28T19:59:59+08:00", "Ollama Cloud"},
		{"2026-11-28T20:00:00+08:00", "DeepSeek"},
		{"2026-11-29T00:00:00+08:00", "DeepSeek"},
		{"2026-11-29T01:59:59+08:00", "DeepSeek"},
		{"2026-11-29T02:00:00+08:00", "Ollama Cloud"},
		{"2026-11-29T12:00:00+08:00", "Ollama Cloud"},
	}
	for _, tc := range cases {
		clock, err := time.Parse(time.RFC3339, tc.beijing)
		if err != nil {
			t.Fatal(err)
		}
		if got := set.Active(clock).Name; got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.beijing, got, tc.want)
		}
	}
	// The same instant expressed in UTC must land in the same window: the
	// server clock is UTC and must not shift the schedule.
	utc := time.Date(2026, 11, 28, 13, 0, 0, 0, time.UTC) // 21:00 Beijing
	if got := set.Active(utc).Name; got != "DeepSeek" {
		t.Errorf("UTC clock: got %q, want DeepSeek", got)
	}
}

func TestDescribeHasNoKeyAndReportsReadiness(t *testing.T) {
	set := Set{
		Official: Provider{Name: "DeepSeek", BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", APIKey: ""},
		Cloud:    Provider{Name: "Ollama Cloud", BaseURL: "https://ollama.com/v1", Model: "deepseek-v4.1-flash", APIKey: "cloud-key"},
	}
	night, _ := time.Parse(time.RFC3339, "2026-11-28T22:00:00+08:00")
	out := set.Describe(night)
	if out["configured"] != false || out["provider"] != "DeepSeek" || out["model"] != "deepseek-flash" {
		t.Fatalf("describe = %v", out)
	}
	for _, value := range out {
		if s, ok := value.(string); ok && s == "cloud-key" {
			t.Fatal("describe leaked a key")
		}
	}
	day, _ := time.Parse(time.RFC3339, "2026-11-29T09:00:00+08:00")
	out = set.Describe(day)
	if out["configured"] != true || out["provider"] != "Ollama Cloud" {
		t.Fatalf("describe = %v", out)
	}
}
