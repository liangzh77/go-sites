package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liangzh77/go-sites/internal/aiprovider"
)

func setup(t *testing.T, fn http.HandlerFunc) *service {
	t.Helper()
	up := httptest.NewServer(fn)
	t.Cleanup(up.Close)
	db, err := openDB(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// A fixed endpoint stands in for whichever account the hour would pick.
	return &service{db: db, client: up.Client(), fixedEndpoint: up.URL, daily: 50, slots: make(chan struct{}, 2), visits: map[string][]time.Time{}, now: time.Now}
}
func completion(w http.ResponseWriter, text string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text}, "finish_reason": "stop"}}})
}
func request(t *testing.T, s *service, mode, text string, img []byte, consent bool) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	form := multipart.NewWriter(&b)
	_ = form.WriteField("mode", mode)
	_ = form.WriteField("text", text)
	if consent {
		_ = form.WriteField("consent", "yes")
	}
	if img != nil {
		f, _ := form.CreateFormFile("image", "essay.png")
		_, _ = f.Write(img)
	}
	_ = form.Close()
	r := httptest.NewRequest("POST", prefix, &b)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("Origin", "https://liangz77.cn")
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	return w
}
func TestTextReviewAndNoContentPersistence(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth or session")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "test-model" {
			t.Error("wrong model")
		}
		raw, _ := json.Marshal(body)
		if !bytes.Contains(raw, []byte(prompt)) {
			t.Error("missing prompt")
		}
		completion(w, "你写出了小猫玩毛线的样子，很生动。建议补一句它扑过去时的动作。")
	})
	w := request(t, s, "text", "测试作文秘密内容：小猫在玩毛线。", nil, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache")
	}
	if n, _ := s.used(); n != 1 {
		t.Fatal("quota", n)
	}
	rows, err := s.db.Query("SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		if name != "quota" {
			t.Fatal("unexpected stored data", name)
		}
	}
}
func TestImageReview(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !bytes.Contains(raw, []byte("data:image/png;base64,")) {
			t.Error("missing image")
		}
		completion(w, "你写的细节很有趣。建议把结尾写完整。")
	})
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	if w := request(t, s, "image", "", b.Bytes(), true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestBadInputsDontSpendQuota(t *testing.T) {
	cases := []struct {
		name, mode, text string
		img              []byte
		consent          bool
	}{
		{"empty", "text", "", nil, true}, {"no consent", "text", "作文", nil, false}, {"too long", "text", strings.Repeat("字", 8001), nil, true},
		{"invalid image", "image", "", []byte("not an image"), true}, {"missing image", "image", "", nil, true}, {"mixed", "text", "作文", []byte("image"), true}, {"bad mode", "unknown", "作文", nil, true},
		{"large image", "image", "", make([]byte, maxImage+1), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected model call") })
			w := request(t, s, tc.mode, tc.text, tc.img, tc.consent)
			if w.Code != 400 {
				t.Fatal(w.Code, w.Body.String())
			}
			if n, _ := s.used(); n != 0 {
				t.Fatal(n)
			}
		})
	}
}
func TestUpstreamFailuresRefundAndRedact(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("secret-upstream-token user作文"))
	})
	w := request(t, s, "text", "小猫", nil, true)
	if w.Code != 502 || strings.Contains(w.Body.String(), "secret-upstream") {
		t.Fatal(w.Code, w.Body.String())
	}
	if n, _ := s.used(); n != 0 {
		t.Fatal(n)
	}
}
func TestSchedulePicksOneAccountPerRequest(t *testing.T) {
	set := aiprovider.Set{
		Official: aiprovider.Provider{Name: "DeepSeek", BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", APIKey: "official"},
		Cloud:    aiprovider.Provider{Name: "Ollama Cloud", BaseURL: "https://ollama.com/v1", Model: "deepseek-v4.1-flash", APIKey: "cloud"},
	}
	var got []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body.Model, r.URL.Path)
		completion(w, "你写得清楚。建议补一个动作。")
	}))
	defer up.Close()
	set.Official.BaseURL, set.Cloud.BaseURL = up.URL, up.URL
	db, err := openDB(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cases := []struct {
		beijing  string
		provider string
		model    string
		path     string
	}{
		{"2026-11-28T20:00:00+08:00", "Bearer official", "deepseek-flash", "/chat/completions"},
		{"2026-11-29T01:59:00+08:00", "Bearer official", "deepseek-flash", "/chat/completions"},
		{"2026-11-29T02:00:00+08:00", "Bearer cloud", "deepseek-v4.1-flash", "/chat/completions"},
		{"2026-11-29T12:00:00+08:00", "Bearer cloud", "deepseek-v4.1-flash", "/chat/completions"},
		{"2026-11-29T19:59:00+08:00", "Bearer cloud", "deepseek-v4.1-flash", "/chat/completions"},
	}
	for _, tc := range cases {
		clock, err := time.Parse(time.RFC3339, tc.beijing)
		if err != nil {
			t.Fatal(err)
		}
		s := &service{db: db, client: up.Client(), providers: set, daily: 50, slots: make(chan struct{}, 2), visits: map[string][]time.Time{}, now: func() time.Time { return clock }}
		got = nil
		if w := request(t, s, "text", "作文", nil, true); w.Code != 200 {
			t.Fatalf("%s: %d %s", tc.beijing, w.Code, w.Body.String())
		}
		if len(got) != 3 || got[0] != tc.provider || got[1] != tc.model || got[2] != tc.path {
			t.Fatalf("%s: %v", tc.beijing, got)
		}
	}
	// One account per request: a 429 from the chosen one is reported, not
	// retried against the other model.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer official" {
			w.WriteHeader(429)
			return
		}
		t.Error("the schedule must not fall back to the other account")
	}))
	defer failing.Close()
	set.Official.BaseURL, set.Cloud.BaseURL = failing.URL, failing.URL
	clock, _ := time.Parse(time.RFC3339, "2026-11-28T21:00:00+08:00")
	s := &service{db: db, client: failing.Client(), providers: set, daily: 50, slots: make(chan struct{}, 2), visits: map[string][]time.Time{}, now: func() time.Time { return clock }}
	if w := request(t, s, "text", "作文", nil, true); w.Code != 503 {
		t.Fatalf("want 503 for the chosen account's quota, got %d %s", w.Code, w.Body.String())
	}
}

func TestStatusReportsActiveAccountWithoutKeys(t *testing.T) {
	set := aiprovider.Set{
		Official: aiprovider.Provider{Name: "DeepSeek", BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", APIKey: "official-secret"},
		Cloud:    aiprovider.Provider{Name: "Ollama Cloud", BaseURL: "https://ollama.com/v1", Model: "deepseek-v4.1-flash", APIKey: "cloud-secret"},
	}
	db, err := openDB(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	clock, _ := time.Parse(time.RFC3339, "2026-11-29T09:00:00+08:00")
	s := &service{db: db, client: http.DefaultClient, providers: set, daily: 50, slots: make(chan struct{}, 2), visits: map[string][]time.Time{}, now: func() time.Time { return clock }}
	for _, path := range []string{prefix + "/status", mathPrefix + "/status"} {
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, `"provider":"Ollama Cloud"`) || !strings.Contains(body, `"model":"deepseek-v4.1-flash"`) || !strings.Contains(body, `"configured":true`) {
			t.Fatalf("%s: %s", path, body)
		}
		if strings.Contains(body, "secret") {
			t.Fatalf("%s leaked a key: %s", path, body)
		}
	}
}

func TestLongAnswerRewrittenNotTruncated(t *testing.T) {
	calls := 0
	s := setup(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			completion(w, strings.Repeat("字", 110))
		} else {
			completion(w, "你写得很有趣。建议补充结尾。")
		}
	})
	w := request(t, s, "text", "作文", nil, true)
	if w.Code != 200 || calls != 2 {
		t.Fatal(w.Code, calls, w.Body.String())
	}
}
func TestPersistentLongAnswerFails(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) { completion(w, strings.Repeat("字", 101)) })
	w := request(t, s, "text", "作文", nil, true)
	if w.Code != 502 {
		t.Fatal(w.Code)
	}
	if n, _ := s.used(); n != 0 {
		t.Fatal(n)
	}
}
func TestQuotaAtomicAndReset(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	s.daily = 7
	var wg sync.WaitGroup
	var accepted atomic.Int64
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.claim("2026-11-24") == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 7 {
		t.Fatal(accepted.Load())
	}
	if err := s.claim("2026-11-25"); err != nil {
		t.Fatal(err)
	}
}
func TestQuotaSurvivesReopen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "quota.db")
	db, _ := openDB(p)
	_, _ = db.Exec("INSERT INTO quota VALUES('2026-11-24',9)")
	db.Close()
	db, err := openDB(p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT used FROM quota").Scan(&n); err != nil || n != 9 {
		t.Fatal(n, err)
	}
}
func TestOriginAndRateLimit(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	r := httptest.NewRequest("POST", prefix, nil)
	r.Header.Set("Origin", "https://evil.invalid")
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	for i := 0; i < 5; i++ {
		if !s.allowIP(r) {
			t.Fatal(i)
		}
	}
	if s.allowIP(r) {
		t.Fatal("rate not bounded")
	}
}
func TestEmptyAndTruncatedResponse(t *testing.T) {
	for _, reason := range []string{"length", "stop"} {
		t.Run(reason, func(t *testing.T) {
			s := setup(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""},"finish_reason":"` + reason + `"}]}`))
			})
			if w := request(t, s, "text", "作文", nil, true); w.Code != 502 {
				t.Fatal(w.Code)
			}
		})
	}
}
func TestCanceledModelRequest(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.complete(ctx, []map[string]any{}, "test")
	var f *failure
	if !errors.As(err, &f) || f.status != 504 {
		t.Fatal("expected timeout response", err)
	}
}

func TestDailyLimitAndConcurrency(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected upstream request") })
	s.daily = 1
	if err := s.claim(s.day()); err != nil {
		t.Fatal(err)
	}
	if w := request(t, s, "text", "作文", nil, true); w.Code != 429 || !strings.Contains(w.Body.String(), "DAILY_LIMIT") {
		t.Fatal(w.Code, w.Body.String())
	}
	s.refund(s.day())
	s.slots <- struct{}{}
	s.slots <- struct{}{}
	if w := request(t, s, "text", "作文", nil, true); w.Code != 429 || !strings.Contains(w.Body.String(), "BUSY") {
		t.Fatal(w.Code, w.Body.String())
	}
	if n, _ := s.used(); n != 0 {
		t.Fatal(n)
	}
}

func TestDatabasePragmas(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatal(mode, err)
	}
	var enabled int
	if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatal(enabled, err)
	}
}
