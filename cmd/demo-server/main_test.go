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

func TestListWikiFilesReturnsMetadataOnly(t *testing.T) {
	wikiRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(wikiRoot, "note.md"), []byte("# Note\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{wikiRoot: wikiRoot}

	listReq := httptest.NewRequest(http.MethodGet, "/api/wiki/files", nil)
	listRec := httptest.NewRecorder()
	a.handleListWikiFiles(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var listPayload struct {
		Files []wikiFileItem `json:"files"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Files) != 1 {
		t.Fatalf("file count = %d, want 1", len(listPayload.Files))
	}
	if listPayload.Files[0].Content != "" || listPayload.Files[0].HTML != "" {
		t.Fatalf("list returned heavy fields: content=%q html=%q", listPayload.Files[0].Content, listPayload.Files[0].HTML)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/wiki/files/note.md", nil)
	getRec := httptest.NewRecorder()
	a.handleGetWikiFile(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	var file wikiFileItem
	if err := json.NewDecoder(getRec.Body).Decode(&file); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(file.Content, "body") || !strings.Contains(file.HTML, "<h1") {
		t.Fatalf("single file did not include content/html: %#v", file)
	}
}

func TestServeDemoRedirectsSlugToDirectory(t *testing.T) {
	dataDir := t.TempDir()
	a := &app{
		dataDir:      dataDir,
		demosDir:     filepath.Join(dataDir, "demos"),
		manifestPath: filepath.Join(dataDir, "manifest.json"),
		publicOrigin: "https://example.test",
	}
	if err := os.MkdirAll(filepath.Join(a.demosDir, "persona-box"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.demosDir, "persona-box", "index.html"), []byte(`<img src="assets/product.jpg">`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.saveManifestLocked(manifest{Demos: []demoItem{{Title: "Persona Box", Slug: "persona-box"}}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/demo/persona-box", nil)
	rec := httptest.NewRecorder()
	a.handleServeDemo(rec, req)

	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusPermanentRedirect)
	}
	if location := rec.Header().Get("Location"); location != "/demo/persona-box/" {
		t.Fatalf("Location = %q, want /demo/persona-box/", location)
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
