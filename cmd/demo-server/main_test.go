package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishDemoOverwritesSameTitle(t *testing.T) {
	dataDir := t.TempDir()
	a := &app{
		dataDir:       dataDir,
		demosDir:      filepath.Join(dataDir, "demos"),
		manifestPath:  filepath.Join(dataDir, "manifest.json"),
		apiKey:        "test-key",
		sessionSecret: []byte("test-secret"),
		publicOrigin:  "https://example.test",
	}
	if err := a.ensureManifest(); err != nil {
		t.Fatal(err)
	}

	first := publishDemo(t, a, `{"title":"演示页面","html":"<h1>first</h1>","feature":"v1"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first publish status = %d, body = %s", first.Code, first.Body.String())
	}

	var created demoItem
	if err := json.NewDecoder(first.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Slug == "" {
		t.Fatal("created slug is empty")
	}

	second := publishDemo(t, a, `{"title":"演示页面","html":"<h1>second</h1>","feature":"v2"}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second publish status = %d, body = %s", second.Code, second.Body.String())
	}

	var updated demoItem
	if err := json.NewDecoder(second.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Slug != created.Slug {
		t.Fatalf("slug changed on overwrite: %q -> %q", created.Slug, updated.Slug)
	}
	if updated.Feature != "v2" {
		t.Fatalf("feature = %q, want v2", updated.Feature)
	}

	page, err := os.ReadFile(filepath.Join(a.demosDir, updated.Slug, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "second") || strings.Contains(string(page), "first") {
		t.Fatalf("page was not overwritten: %s", string(page))
	}

	m, err := a.loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Demos) != 1 {
		t.Fatalf("demo count = %d, want 1", len(m.Demos))
	}
}

func TestPublishDemoRequiresAPIKey(t *testing.T) {
	dataDir := t.TempDir()
	a := &app{
		dataDir:       dataDir,
		demosDir:      filepath.Join(dataDir, "demos"),
		manifestPath:  filepath.Join(dataDir, "manifest.json"),
		apiKey:        "test-key",
		sessionSecret: []byte("test-secret"),
		publicOrigin:  "https://example.test",
	}

	req := httptest.NewRequest(http.MethodPost, "/api/demos/publish", strings.NewReader(`{"title":"x","html":"x"}`))
	rec := httptest.NewRecorder()
	a.requireAPIKey(a.handlePublishDemo)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func publishDemo(t *testing.T, a *app, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/demos/publish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	a.requireAPIKey(a.handlePublishDemo)(rec, req)
	return rec
}
