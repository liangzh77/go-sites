package main

import (
	"encoding/json"
	"golang.org/x/crypto/bcrypt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMachineGrantLifecycle(t *testing.T) {
	s := testStore(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("test"), bcrypt.MinCost)
	auth, _ := newAuthManager(hash, []byte(strings.Repeat("x", 32)), "http://example.test", false)
	a, _ := newApp(s, newDetector(), auth, "http://example.test")
	count := 0
	a.detector.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		count++
		if r.Header.Get("Authorization") != "Bearer synthetic-key" {
			t.Fatal("incorrect bearer")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ready"}}]}`))}, nil
	})}
	pair := grantPair{}
	for _, p := range []string{"ollama", "deepseek"} {
		base := "https://ollama.com/v1"
		if p == "deepseek" {
			base = "https://api.deepseek.com"
		}
		provider, e := s.createProvider(ProviderInput{Name: p, PresetID: p, BaseURL: base, AuthMode: authBearer, Protocol: protocolOpenAIChat})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.createModel(provider.ID, ModelInput{ModelID: "deepseek-synthetic", DefaultTest: true, Source: "manual"}); e != nil {
			t.Fatal(e)
		}
		v := "synthetic-key"
		k, e := s.createAPIKey(provider.ID, APIKeyInput{Alias: p, Secret: &v, TestModel: "deepseek-synthetic"})
		if e != nil {
			t.Fatal(e)
		}
		choice := selection{k.ID, "deepseek-synthetic"}
		if p == "ollama" {
			pair.Day = choice
		} else {
			pair.Night = choice
		}
	}
	srv := httptest.NewServer(a.handler())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	endpoint := srv.URL + "/keys/api/inference-grant"
	data := map[string]any{"action": "enable", "routes": pair, "password": "test"}
	call := func(csrf string) int { return requestJSON(t, client, "POST", endpoint, data, csrf).status }
	if call("") != 401 {
		t.Fatal("anonymous grant")
	}
	login := requestJSON(t, client, "POST", srv.URL+"/keys/api/session", map[string]string{"password": "test"}, "")
	var sess struct{ CSRFToken string }
	json.Unmarshal(login.body, &sess)
	if call("") != 403 {
		t.Fatal("missing CSRF")
	}
	data["password"] = "wrong"
	if call(sess.CSRFToken) != 403 {
		t.Fatal("missing reauth")
	}
	local := func() int {
		w := httptest.NewRecorder()
		a.localChat(w, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"Hi"}]}`)))
		if strings.Contains(w.Body.String(), vForTest) {
			t.Fatal("key leaked")
		}
		return w.Code
	}
	if local() != 428 {
		t.Fatal("no grant")
	}
	data["password"] = "test"
	if call(sess.CSRFToken) != 200 || count != 2 || local() != 200 {
		t.Fatalf("grant or broker failed: probes=%d", count)
	}
	if requestJSON(t, client, "POST", srv.URL+"/v1/chat/completions", map[string]any{}, "").status != 404 {
		t.Fatal("public inference route")
	}
	data["action"] = "revoke"
	if call(sess.CSRFToken) != 200 || local() != 428 {
		t.Fatal("revoke failed")
	}
}

const vForTest = "synthetic-key"
