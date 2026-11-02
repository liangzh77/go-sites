package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestGateway(t *testing.T) (*gateway, http.Handler) {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.close() })
	g := &gateway{
		st:       st,
		localTok: "local-secret",
		cloudTok: "cloud-secret",
		basePath: "/relay",
		client:   &http.Client{Timeout: 5 * time.Second},
		now:      time.Now,
	}
	return g, g.routes()
}

func do(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAuthRejectsMissingAndWrongTokens(t *testing.T) {
	_, h := newTestGateway(t)

	cases := []struct{ method, path, token string }{
		{"POST", "/relay/from-cloud", ""},
		{"POST", "/relay/from-cloud", "wrong"},
		{"POST", "/relay/from-cloud", "local-secret"}, // wrong role
		{"GET", "/relay/to-local", "local-secret2"},
		{"POST", "/relay/to-cloud", ""},
		{"GET", "/relay/result/abc", "local-secret"},
	}
	for _, c := range cases {
		w := do(t, h, c.method, c.path, c.token, `{"text":"x"}`)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s token=%q: got %d, want 401", c.method, c.path, c.token, w.Code)
		}
	}
}

func TestFromCloudThenLongPollThenAck(t *testing.T) {
	_, h := newTestGateway(t)

	w := do(t, h, "POST", "/relay/from-cloud", "cloud-secret",
		`{"id":"2026-11-02-do-thing","text":"跑一下测试","meta":{"cwd":"D:/p","agent":"paseo"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("enqueue: got %d %s", w.Code, w.Body.String())
	}

	poll := do(t, h, "GET", "/relay/to-local?wait=0", "local-secret", "")
	if poll.Code != http.StatusOK {
		t.Fatalf("poll: got %d", poll.Code)
	}
	var got task
	if err := json.Unmarshal(poll.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "2026-11-02-do-thing" || got.Text != "跑一下测试" {
		t.Fatalf("unexpected task: %+v", got)
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", got.Attempts)
	}

	ack := do(t, h, "POST", "/relay/ack", "local-secret",
		`{"id":"2026-11-02-do-thing","status":"done","summary":"已完成"}`)
	if ack.Code != http.StatusOK {
		t.Fatalf("ack: got %d %s", ack.Code, ack.Body.String())
	}

	res := do(t, h, "GET", "/relay/result/2026-11-02-do-thing", "cloud-secret", "")
	if res.Code != http.StatusOK {
		t.Fatalf("result: got %d", res.Code)
	}
	var r result
	if err := json.Unmarshal(res.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Status != "done" || r.Summary != "已完成" {
		t.Fatalf("unexpected result: %+v", r)
	}

	// Acked work must not come back on the next poll.
	empty := do(t, h, "GET", "/relay/to-local?wait=0", "local-secret", "")
	if empty.Code != http.StatusNoContent {
		t.Fatalf("after ack: got %d, want 204", empty.Code)
	}
}

func TestFromCloudIsIdempotent(t *testing.T) {
	_, h := newTestGateway(t)

	body := `{"id":"dup-1","text":"only once"}`
	for i := 0; i < 3; i++ {
		if w := do(t, h, "POST", "/relay/from-cloud", "cloud-secret", body); w.Code != http.StatusOK {
			t.Fatalf("enqueue %d: got %d", i, w.Code)
		}
	}

	q := do(t, h, "GET", "/relay/queue", "cloud-secret", "")
	var out struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(q.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Tasks) != 1 {
		t.Fatalf("queue has %d tasks, want 1", len(out.Tasks))
	}
}

func TestLongPollReturnsAsSoonAsWorkArrives(t *testing.T) {
	_, h := newTestGateway(t)

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- do(t, h, "GET", "/relay/to-local?wait=5", "local-secret", "")
	}()

	time.Sleep(300 * time.Millisecond)
	do(t, h, "POST", "/relay/from-cloud", "cloud-secret", `{"id":"late-1","text":"刚到的"}`)

	select {
	case w := <-done:
		if w.Code != http.StatusOK {
			t.Fatalf("long poll: got %d, want 200", w.Code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("long poll did not return after work arrived")
	}
}

func TestLeaseExpiryReclaimsAbandonedTask(t *testing.T) {
	g, h := newTestGateway(t)
	g.lease = 50 * time.Millisecond

	do(t, h, "POST", "/relay/from-cloud", "cloud-secret", `{"id":"abandoned","text":"x"}`)

	first := do(t, h, "GET", "/relay/to-local?wait=0", "local-secret", "")
	if first.Code != http.StatusOK {
		t.Fatalf("first claim: got %d", first.Code)
	}
	// A second immediate poll must find nothing while the lease holds.
	if again := do(t, h, "GET", "/relay/to-local?wait=0", "local-secret", ""); again.Code != http.StatusNoContent {
		t.Fatalf("during lease: got %d, want 204", again.Code)
	}

	time.Sleep(120 * time.Millisecond)
	third := do(t, h, "GET", "/relay/to-local?wait=0", "local-secret", "")
	if third.Code != http.StatusOK {
		t.Fatalf("after lease: got %d, want 200", third.Code)
	}
	var got task
	_ = json.Unmarshal(third.Body.Bytes(), &got)
	if got.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", got.Attempts)
	}
}

func TestConcurrentClaimsNeverHandOutTheSameTask(t *testing.T) {
	g, h := newTestGateway(t)
	g.lease = time.Minute

	const n = 5
	for i := 0; i < n; i++ {
		do(t, h, "POST", "/relay/from-cloud", "cloud-secret",
			fmt.Sprintf(`{"id":"task-%d","text":"x"}`, i))
	}

	var wg sync.WaitGroup
	seen := make([]string, 0, n)
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := do(t, h, "GET", "/relay/to-local?wait=0", "local-secret", "")
			if w.Code != http.StatusOK {
				return
			}
			var tk task
			if err := json.Unmarshal(w.Body.Bytes(), &tk); err != nil {
				return
			}
			mu.Lock()
			seen = append(seen, tk.ID)
			mu.Unlock()
		}()
	}
	wg.Wait()

	uniq := map[string]bool{}
	for _, id := range seen {
		if uniq[id] {
			t.Fatalf("task %s was handed out twice", id)
		}
		uniq[id] = true
	}
	if len(uniq) == 0 {
		t.Fatal("no task was handed out")
	}
}

func TestToCloudForwardsToWebhook(t *testing.T) {
	var gotAuth, gotBody string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	g, h := newTestGateway(t)
	g.hookURL = hook.URL
	g.hookTok = "hook-secret"

	w := do(t, h, "POST", "/relay/to-cloud", "local-secret", `{"id":"m1","text":"云端你好"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("to-cloud: got %d %s", w.Code, w.Body.String())
	}
	if gotAuth != "Bearer hook-secret" {
		t.Fatalf("webhook auth = %q", gotAuth)
	}
	if !strings.Contains(gotBody, "云端你好") || !strings.Contains(gotBody, `"m1"`) {
		t.Fatalf("webhook body = %s", gotBody)
	}
}

func TestToCloudWithoutWebhookConfigFailsClosed(t *testing.T) {
	_, h := newTestGateway(t)
	w := do(t, h, "POST", "/relay/to-cloud", "local-secret", `{"text":"x"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
}

func TestValidation(t *testing.T) {
	_, h := newTestGateway(t)

	bad := []struct{ path, body string }{
		{"/relay/from-cloud", `{"text":""}`},
		{"/relay/from-cloud", `{"id":"Bad ID","text":"x"}`},
		{"/relay/from-cloud", `{"id":"ok","text":"x","extra":1}`},
		{"/relay/from-cloud", `not json`},
		{"/relay/ack", `{"id":"x","status":"weird"}`},
	}
	for _, c := range bad {
		token := "cloud-secret"
		if strings.Contains(c.path, "ack") {
			token = "local-secret"
		}
		if w := do(t, h, "POST", c.path, token, c.body); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s: got %d, want 400", c.path, c.body, w.Code)
		}
	}
}

func TestHealthDoesNotLeakSecrets(t *testing.T) {
	_, h := newTestGateway(t)
	w := do(t, h, "GET", "/relay/health", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("health: got %d", w.Code)
	}
	body := w.Body.String()
	for _, secret := range []string{"local-secret", "cloud-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("health body leaks %q: %s", secret, body)
		}
	}
}

func TestResultFallsBackTo404WhenMissing(t *testing.T) {
	_, h := newTestGateway(t)
	if w := do(t, h, "GET", "/relay/result/never-ran", "cloud-secret", ""); w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}
