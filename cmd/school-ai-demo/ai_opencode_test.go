package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// --- the gateway contract ---------------------------------------------------

func TestOpencodeRequestCarriesItsHeaders(t *testing.T) {
	var got http.Header
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"fields\":{\"f04\":\"张伟\"}}"}}]}`))
	}))
	defer srv.Close()

	// The base is a base; the client must append the completions path.
	c := &aiClient{
		apiKey:       "primary-key",
		endpoint:     completionsURL(srv.URL + "/inference/go/openai/v1"),
		model:        "deepseek-v4.1-flash",
		http:         http.DefaultClient,
		needsSession: true,
	}
	if _, err := c.fill(context.Background(), testTemplate(t), "姓名：张伟"); err != nil {
		t.Fatalf("fill: %v", err)
	}

	if gotPath != "/inference/go/openai/v1/chat/completions" {
		t.Fatalf("path = %q, want the completions path appended to the base", gotPath)
	}
	if got.Get("Authorization") != "Bearer primary-key" {
		t.Fatalf("authorization = %q", got.Get("Authorization"))
	}
	if got.Get("x-opencode-session") == "" {
		t.Fatal("the gateway rejects requests without x-opencode-session")
	}
	if got.Get("User-Agent") != userAgent {
		t.Fatalf("user-agent = %q, want %q", got.Get("User-Agent"), userAgent)
	}
}

func TestSessionHeaderIsStableWithinOneFill(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("x-opencode-session"))
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"fields\":{\"f04\":\"张伟\"}}"}}]}`))
	}))
	defer srv.Close()
	c := &aiClient{apiKey: "k", endpoint: srv.URL, model: "m", http: http.DefaultClient, needsSession: true}
	if _, err := c.fill(context.Background(), testTemplate(t), "姓名：张伟"); err != nil {
		t.Fatalf("fill: %v", err)
	}
	// A second fill is a new conversation, so it gets a new id.
	if _, err := c.fill(context.Background(), testTemplate(t), "姓名：李四"); err != nil {
		t.Fatalf("second fill: %v", err)
	}
	if len(seen) != 2 || seen[0] == seen[1] {
		t.Fatalf("session ids should differ per fill, got %v", seen)
	}
}

func TestBackupKeyIsUsedWhenThePrimaryIsRejected(t *testing.T) {
	var calls atomic.Int32
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		auth := r.Header.Get("Authorization")
		auths = append(auths, auth)
		if strings.Contains(auth, "primary") {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"fields\":{\"f04\":\"张伟\"}}"}}]}`))
	}))
	defer srv.Close()

	c := &aiClient{apiKey: "primary", backupKey: "backup-key", endpoint: srv.URL, model: "m", http: http.DefaultClient}
	res, err := c.fill(context.Background(), testTemplate(t), "姓名：张伟")
	if err != nil {
		t.Fatalf("fill should have fallen back to the backup key: %v", err)
	}
	if res.Values["f04"] != "张伟" {
		t.Fatalf("values = %v", res.Values)
	}
	if calls.Load() != 2 {
		t.Fatalf("made %d calls, want 2 (primary then backup)", calls.Load())
	}
	if len(auths) != 2 || !strings.Contains(auths[1], "backup") {
		t.Fatalf("second call did not use the backup key: %v", auths)
	}
}

func TestARateLimitedPrimaryFallsBackToo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), "primary") {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"fields\":{\"f04\":\"张伟\"}}"}}]}`))
	}))
	defer srv.Close()
	c := &aiClient{apiKey: "primary", backupKey: "backup", endpoint: srv.URL, model: "m", http: http.DefaultClient}
	if _, err := c.fill(context.Background(), testTemplate(t), "姓名：张伟"); err != nil {
		t.Fatalf("429 on the primary should fall back: %v", err)
	}
}

// A server-side error is not a key problem, so the backup must not be spent.
func TestA500DoesNotTryTheBackup(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := &aiClient{apiKey: "primary", backupKey: "backup", endpoint: srv.URL, model: "m", http: http.DefaultClient}
	if _, err := c.fill(context.Background(), testTemplate(t), "姓名：张伟"); err == nil {
		t.Fatal("a 500 should surface as an error")
	}
	if calls.Load() != 1 {
		t.Fatalf("made %d calls, want 1", calls.Load())
	}
}

func TestNoBackupMeansNoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := &aiClient{apiKey: "only", endpoint: srv.URL, model: "m", http: http.DefaultClient}
	if _, err := c.fill(context.Background(), testTemplate(t), "姓名：张伟"); err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() != 1 {
		t.Fatalf("made %d calls, want 1", calls.Load())
	}
}

// --- reasoning-model output -------------------------------------------------

// The model sometimes emits a stray brace inside a key. That must be repaired,
// not reported as a failure, because the values themselves are fine.
func TestMalformedJSONFromTheModelIsRepaired(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"stray brace in key", `{"fields{": {"f04": "张伟"}}`},
		{"trailing comma", `{"fields": {"f04": "张伟",}}`},
		{"code fence", "```json\n{\"fields\": {\"f04\": \"张伟\"}}\n```"},
		{"prose around it", `好的，填写结果如下：{"fields": {"f04": "张伟"}} 请核对。`},
		{"smart quotes", `{“fields”: {“f04”: “张伟”}}`},
	}
	tpl := testTemplate(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := parseFillJSON(tpl, tc.content, "m")
			if err != nil {
				t.Fatalf("should have been repaired: %v", err)
			}
			if res.Values["f04"] != "张伟" {
				t.Fatalf("values = %v", res.Values)
			}
		})
	}
}

func TestUnrepairableJSONIsAReadableError(t *testing.T) {
	_, err := parseFillJSON(testTemplate(t), `{"fields": {"f04": `, "m")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("error should mention the JSON problem: %v", err)
	}
}

// Reasoning models spend the token budget before writing content; an empty
// content string is that, not an empty answer.
func TestEmptyContentSaysSoInsteadOfReturningNoFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
	}))
	defer srv.Close()
	c := &aiClient{apiKey: "k", endpoint: srv.URL, model: "m", http: http.DefaultClient}
	_, err := c.fill(context.Background(), testTemplate(t), "姓名：张伟")
	if err == nil {
		t.Fatal("an empty answer should be an error, not an empty success")
	}
	if !strings.Contains(err.Error(), "没有返回内容") {
		t.Fatalf("message should tell the user to retry: %v", err)
	}
}

func TestMaxTokensLeavesRoomForReasoning(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"fields\":{}}"}}]}`))
	}))
	defer srv.Close()
	c := &aiClient{apiKey: "k", endpoint: srv.URL, model: "m", http: http.DefaultClient}
	if _, err := c.fill(context.Background(), testTemplate(t), "姓名：张伟"); err != nil {
		t.Fatalf("fill: %v", err)
	}
	got, ok := body["max_tokens"].(float64)
	if !ok {
		t.Fatalf("max_tokens should be sent, body = %v", body)
	}
	if got < 4000 {
		t.Fatalf("max_tokens = %v is small enough to starve the answer", got)
	}
}

func TestNoAPIKeyIsItsOwnError(t *testing.T) {
	c := &aiClient{model: "m", http: http.DefaultClient}
	if _, err := c.fill(context.Background(), testTemplate(t), "姓名：张伟"); err != errNoAPIKey {
		t.Fatalf("err = %v, want errNoAPIKey", err)
	}
}
