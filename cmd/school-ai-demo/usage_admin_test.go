package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/liangzh77/go-sites/internal/modelusage"
)

func TestUsageAdmin(t *testing.T) {
	now := time.Now()
	db, err := modelusage.Open(t.TempDir(), now.AddDate(0, 0, -2))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, at := range []time.Time{now.AddDate(0, 0, -2), now, now} {
		if err := modelusage.Record(db, at); err != nil {
			t.Fatal(err)
		}
	}
	h := newUsageAdmin(db, "test-only-password")
	call := func(method, password, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.test/ai-demo/admin/", strings.NewReader(url.Values{"password": {password}}.Encode()))
		if method == http.MethodPost {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", origin)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := call("GET", "", "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "polyline") || !strings.Contains(w.Body.String(), `type="password"`) || strings.Contains(w.Body.String(), `name="username"`) || w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("expected password-only form", w.Code)
	}
	if w := call("POST", "test-only-password", "https://other.test", nil); w.Code != 403 {
		t.Fatal("cross-site post", w.Code)
	}
	w = call("POST", "test-only-password", "https://example.test", nil)
	cookies := w.Result().Cookies()
	if w.Code != 303 || w.Header().Get("Location") != "./" || len(cookies) != 1 {
		t.Fatal("login", w.Code)
	}
	c := cookies[0]
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/ai-demo/admin/" {
		t.Fatal("unsafe cookie settings")
	}
	tampered := *c
	tampered.Value += "a"
	if w := call("GET", "", "", &tampered); strings.Contains(w.Body.String(), "polyline") {
		t.Fatal("tampered cookie accepted")
	}
	w = call("GET", "", "", c)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code)
	}
	for _, needle := range []string{`points="48,144 324,184 600,104"`, `今日成功<strong>2</strong>`, `<td>0</td>`, `合计</th><td>3</td>`} {
		if !strings.Contains(w.Body.String(), needle) {
			t.Errorf("missing %s", needle)
		}
	}
	if !strings.Contains(w.Body.String(), `value="logout"`) {
		t.Fatal("logout button missing")
	}
	r := httptest.NewRequest("POST", "https://example.test/ai-demo/admin/", strings.NewReader("action=logout"))
	r.Header.Set("Origin", "https://example.test")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(c)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	cleared := w.Result().Cookies()
	if w.Code != 303 || len(cleared) != 1 || cleared[0].MaxAge != -1 || cleared[0].Path != c.Path {
		t.Fatal("logout did not clear cookie", w.Code)
	}
	if w := call("GET", "", "", c); strings.Contains(w.Body.String(), "polyline") {
		t.Fatal("logged-out session still accepted")
	}
	for i := 0; i < 8; i++ {
		if w := call("POST", "wrong", "https://example.test", nil); w.Code != 401 {
			t.Fatalf("wrong attempt %d: %d", i, w.Code)
		}
	}
	if w := call("POST", "wrong", "https://example.test", nil); w.Code != 429 {
		t.Fatal("not throttled", w.Code)
	}
}
