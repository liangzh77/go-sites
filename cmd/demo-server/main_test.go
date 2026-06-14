package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
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

func TestMarkdownDemoPageUsesSitePalette(t *testing.T) {
	page := renderMarkdownPage("调色测试", "# 标题\n\n正文")

	for _, want := range []string{
		`href="/favicon.svg?v=20260609-mist-palette"`,
		`--ink: #213642`,
		`--line: #b8c6ce`,
		`class="md-brand-seal"`,
		`灵感书架`,
		`>标题</h1>`,
		`<p>正文</p>`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("rendered markdown page missing %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "#756b59") || strings.Contains(page, "20260528") {
		t.Fatalf("rendered markdown page contains old theme values:\n%s", page)
	}
}

func TestRefreshStoredMarkdownDemoPagesUpdatesShellAndKeepsBody(t *testing.T) {
	dataDir := t.TempDir()
	a := &app{
		dataDir:      dataDir,
		demosDir:     filepath.Join(dataDir, "demos"),
		manifestPath: filepath.Join(dataDir, "manifest.json"),
		publicOrigin: "https://example.test",
	}
	if err := os.MkdirAll(filepath.Join(a.demosDir, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPage := strings.Replace(renderMarkdownPage("演示", "# 保留正文\n\n- one"), markdownDemoFaviconVersion, "20260528", 1)
	oldPage = strings.ReplaceAll(oldPage, "#213642", "#27312b")
	if err := os.WriteFile(filepath.Join(a.demosDir, "demo", "index.html"), []byte(oldPage), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.saveManifestLocked(manifest{Demos: []demoItem{{Title: "演示", Slug: "demo", Kind: "markdown"}}}); err != nil {
		t.Fatal(err)
	}

	if err := a.refreshStoredMarkdownDemoPages(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(a.demosDir, "demo", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	nextPage := string(data)
	for _, want := range []string{
		`href="/favicon.svg?v=20260609-mist-palette"`,
		`--ink: #213642`,
		`>保留正文</h1>`,
		`<li>one</li>`,
	} {
		if !strings.Contains(nextPage, want) {
			t.Fatalf("refreshed page missing %q:\n%s", want, nextPage)
		}
	}
	if strings.Contains(nextPage, "20260528") || strings.Contains(nextPage, "#27312b") {
		t.Fatalf("refreshed page contains old shell values:\n%s", nextPage)
	}
}

func TestSessionCookieLastsOneWeek(t *testing.T) {
	a := &app{
		adminPassword: "test-password",
		sessionSecret: []byte("test-secret"),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/demo/session", strings.NewReader(`{"password":"test-password"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	a.handleSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(cookies))
	}
	if cookies[0].MaxAge != 60*60*24*7 {
		t.Fatalf("MaxAge = %d, want one week", cookies[0].MaxAge)
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

	sourceReq := httptest.NewRequest(http.MethodGet, "/api/wiki/files/note.md?render=false", nil)
	sourceRec := httptest.NewRecorder()
	a.handleGetWikiFile(sourceRec, sourceReq)
	if sourceRec.Code != http.StatusOK {
		t.Fatalf("source status = %d, body = %s", sourceRec.Code, sourceRec.Body.String())
	}
	var sourceOnly wikiFileItem
	if err := json.NewDecoder(sourceRec.Body).Decode(&sourceOnly); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sourceOnly.Content, "body") || sourceOnly.HTML != "" {
		t.Fatalf("source response should include content without html: %#v", sourceOnly)
	}
}

func TestUpdateWikiFileReturnsRenderedHTML(t *testing.T) {
	wikiRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(wikiRoot, "note.md"), []byte("# Old\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{wikiRoot: wikiRoot}

	body := strings.NewReader(`{"content":"# Note\n\n## Parent\n\n- Item\n  - Child"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/wiki/files/note.md", body)
	rec := httptest.NewRecorder()
	a.handleUpdateWikiFile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var file wikiFileItem
	if err := json.NewDecoder(rec.Body).Decode(&file); err != nil {
		t.Fatal(err)
	}
	if file.HTML == "" {
		t.Fatalf("saved wiki file response did not include rendered html: %#v", file)
	}
	for _, want := range []string{"<h1", "<h2", "<ul>", "<li>Child</li>"} {
		if !strings.Contains(file.HTML, want) {
			t.Fatalf("rendered html missing %q: %s", want, file.HTML)
		}
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

func TestExtractZipDemoUsesOnlyHTMLFileAsEntry(t *testing.T) {
	targetDir := t.TempDir()
	data := zipArchive(t, zipEntry{name: "prototype/手机竖版UI原型.html", body: `<link rel="stylesheet" href="./手机竖版UI原型.css">`}, zipEntry{name: "prototype/手机竖版UI原型.css", body: `body { color: red; }`})

	if err := extractZipDemo(data, targetDir); err != nil {
		t.Fatal(err)
	}

	index, err := os.ReadFile(filepath.Join(targetDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "./%E6%89%8B%E6%9C%BA%E7%AB%96%E7%89%88UI%E5%8E%9F%E5%9E%8B.html") {
		t.Fatalf("generated entry page does not point at the only HTML file:\n%s", string(index))
	}
	if _, err := os.Stat(filepath.Join(targetDir, "手机竖版UI原型.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "手机竖版UI原型.css")); err != nil {
		t.Fatal(err)
	}
}

func TestExtractZipDemoRequiresIndexForMultipleHTMLFiles(t *testing.T) {
	targetDir := t.TempDir()
	data := zipArchive(t, zipEntry{name: "a.html", body: `a`}, zipEntry{name: "b.html", body: `b`})

	err := extractZipDemo(data, targetDir)
	if err == nil || !strings.Contains(err.Error(), "index.html or exactly one HTML file") {
		t.Fatalf("error = %v, want multiple HTML entry error", err)
	}
}

func TestExtractZipDemoDecodesGB18030Names(t *testing.T) {
	targetDir := t.TempDir()
	data := zipArchive(t,
		zipEntry{name: gb18030Name(t, "手机竖版UI原型.html"), body: `<img src="./assets/专业摄影.jpg">`, nonUTF8: true},
		zipEntry{name: gb18030Name(t, "assets/专业摄影.jpg"), body: `image`, nonUTF8: true},
	)

	if err := extractZipDemo(data, targetDir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "手机竖版UI原型.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "assets", "专业摄影.jpg")); err != nil {
		t.Fatal(err)
	}
}

func TestTopLevelAppRoutesServeIndex(t *testing.T) {
	staticRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticRoot, "index.html"), []byte("app shell"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{staticRoot: staticRoot}
	handler := a.routes()

	for _, routePath := range []string{"/wiki", "/search", "/demo"} {
		req := httptest.NewRequest(http.MethodGet, routePath, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", routePath, rec.Code, http.StatusOK)
		}
		if body := rec.Body.String(); body != "app shell" {
			t.Fatalf("%s body = %q, want app shell", routePath, body)
		}
	}
}

func TestWikiAssetRoutesStillRequireAuth(t *testing.T) {
	staticRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticRoot, "index.html"), []byte("app shell"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{
		staticRoot:    staticRoot,
		wikiRoot:      t.TempDir(),
		sessionSecret: []byte("test-secret"),
	}
	handler := a.routes()

	req := httptest.NewRequest(http.MethodGet, "/wiki/asset.png", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

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

type zipEntry struct {
	name    string
	body    string
	nonUTF8 bool
}

func zipArchive(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate, NonUTF8: entry.nonUTF8}
		part, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gb18030Name(t *testing.T, name string) string {
	t.Helper()
	encoded, err := simplifiedchinese.GB18030.NewEncoder().String(name)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
