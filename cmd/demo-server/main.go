package main

import (
	"archive/zip"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	cookieName     = "go_sites_demo_session"
	maxUploadBytes = 25 << 20
)

type app struct {
	mu            sync.Mutex
	dataDir       string
	demosDir      string
	manifestPath  string
	staticRoot    string
	adminPassword string
	sessionSecret []byte
	publicOrigin  string
}

type demoItem struct {
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	Address   string `json:"address"`
	Disabled  bool   `json:"disabled"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type manifest struct {
	Demos []demoItem `json:"demos"`
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func main() {
	dataDir := envOr("DEMO_DATA_DIR", filepath.Join(".", "demo-data"))
	staticRoot := envOr("SITE_ROOT", ".")
	password := firstEnv("DEMO_ADMIN_PASSWORD", "GO_SITES_DEMO_PASSWORD")
	secret := firstEnv("DEMO_SESSION_SECRET", "GO_SITES_DEMO_PASSWORD", "DEMO_ADMIN_PASSWORD")
	if secret == "" {
		secret = "local-development-demo-secret"
	}

	a := &app{
		dataDir:       dataDir,
		demosDir:      filepath.Join(dataDir, "demos"),
		manifestPath:  filepath.Join(dataDir, "manifest.json"),
		staticRoot:    staticRoot,
		adminPassword: password,
		sessionSecret: []byte(secret),
		publicOrigin:  strings.TrimRight(envOr("PUBLIC_ORIGIN", "https://liangz77.cn"), "/"),
	}

	if err := os.MkdirAll(a.demosDir, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := a.ensureManifest(); err != nil {
		log.Fatal(err)
	}
	if err := a.normalizeDemoAddresses(); err != nil {
		log.Fatal(err)
	}
	if err := a.pruneStoredMarkdownTemplateTitles(); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/demo/session", a.handleSession)
	mux.HandleFunc("GET /api/demos", a.handleListDemos)
	mux.HandleFunc("POST /api/demos", a.requireAuth(a.handleCreateDemo))
	mux.HandleFunc("PATCH /api/demos/{slug}", a.requireAuth(a.handleUpdateDemo))
	mux.HandleFunc("DELETE /api/demos/{slug}", a.requireAuth(a.handleDeleteDemo))
	mux.HandleFunc("/demo/", a.handleServeDemo)
	mux.HandleFunc("/", a.handleStaticFallback)

	addr := envOr("DEMO_SERVER_ADDR", ":9005")
	log.Printf("demo server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, secureHeaders(mux)))
}

func (a *app) handleSession(w http.ResponseWriter, r *http.Request) {
	if a.adminPassword == "" {
		writeError(w, http.StatusServiceUnavailable, "AUTH_NOT_CONFIGURED", "Demo administration password is not configured.")
		return
	}

	var input struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body.")
		return
	}
	if input.Password != a.adminPassword {
		writeError(w, http.StatusUnauthorized, "INVALID_PASSWORD", "Invalid password.")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    a.sessionToken(),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.Header.Get("X-Forwarded-Proto") == "https",
		MaxAge:   60 * 60 * 24,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleListDemos(w http.ResponseWriter, r *http.Request) {
	m, err := a.loadManifest()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "READ_FAILED", "Unable to read demos.")
		return
	}

	demos := m.Demos
	if !a.isAuthenticated(r) {
		demos = make([]demoItem, 0, len(m.Demos))
		for _, item := range m.Demos {
			if !item.Disabled {
				demos = append(demos, item)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string][]demoItem{"demos": demos})
}

func (a *app) handleCreateDemo(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		a.handleCreateMarkdownDemo(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "UPLOAD_TOO_LARGE", "Upload must be 25MB or smaller.")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "FILE_REQUIRED", "A demo file is required.")
		return
	}
	defer file.Close()

	if title == "" {
		title = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}
	baseSlug := slugify(title)
	if baseSlug == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_TITLE", "Demo title is required.")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	m, err := a.loadManifestLocked()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "READ_FAILED", "Unable to read demos.")
		return
	}
	title, slug := nextDemoNameAndSlug(title, baseSlug, m.Demos)

	targetDir := filepath.Join(a.demosDir, slug)
	tempDir := targetDir + ".tmp-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to prepare demo directory.")
		return
	}
	defer os.RemoveAll(tempDir)

	kind, err := materializeUpload(file, header, tempDir, title)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_UPLOAD", err.Error())
		return
	}
	if err := os.Rename(tempDir, targetDir); err != nil {
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to save demo.")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	item := demoItem{
		Title:     title,
		Slug:      slug,
		Address:   a.demoAddress(slug),
		Kind:      kind,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.Demos = append(m.Demos, item)
	sort.Slice(m.Demos, func(i, j int) bool {
		return m.Demos[i].CreatedAt > m.Demos[j].CreatedAt
	})

	if err := a.saveManifestLocked(m); err != nil {
		_ = os.RemoveAll(targetDir)
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to save demo list.")
		return
	}

	writeJSON(w, http.StatusCreated, item)
}

func (a *app) handleCreateMarkdownDemo(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxUploadBytes)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body.")
		return
	}

	title := strings.TrimSpace(input.Title)
	content := strings.TrimSpace(input.Content)
	if content == "" {
		writeError(w, http.StatusUnprocessableEntity, "EMPTY_MARKDOWN", "Markdown content is required.")
		return
	}
	if title == "" {
		title = markdownTitleFromContent(content)
	}
	if title == "" {
		title = "Markdown 演示"
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	m, err := a.loadManifestLocked()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "READ_FAILED", "Unable to read demos.")
		return
	}

	baseSlug := slugify(title)
	if baseSlug == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_TITLE", "Demo title is required.")
		return
	}
	title, slug := nextDemoNameAndSlug(title, baseSlug, m.Demos)

	targetDir := filepath.Join(a.demosDir, slug)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to prepare demo directory.")
		return
	}
	if err := os.WriteFile(filepath.Join(targetDir, "index.html"), []byte(renderMarkdownPage(title, content)), 0o644); err != nil {
		_ = os.RemoveAll(targetDir)
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to save markdown demo.")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	item := demoItem{
		Title:     title,
		Slug:      slug,
		Address:   a.demoAddress(slug),
		Kind:      "markdown",
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.Demos = append(m.Demos, item)
	sort.Slice(m.Demos, func(i, j int) bool {
		return m.Demos[i].CreatedAt > m.Demos[j].CreatedAt
	})
	if err := a.saveManifestLocked(m); err != nil {
		_ = os.RemoveAll(targetDir)
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to save demo list.")
		return
	}

	writeJSON(w, http.StatusCreated, item)
}

func (a *app) handleUpdateDemo(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var input struct {
		Disabled *bool   `json:"disabled"`
		Title    *string `json:"title"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body.")
		return
	}
	if input.Disabled == nil && input.Title == nil {
		writeError(w, http.StatusBadRequest, "NO_CHANGE", "No supported fields were provided.")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	m, err := a.loadManifestLocked()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "READ_FAILED", "Unable to read demos.")
		return
	}
	for i := range m.Demos {
		if m.Demos[i].Slug == slug {
			if input.Title != nil {
				title := strings.TrimSpace(*input.Title)
				baseSlug := slugify(title)
				if baseSlug == "" {
					writeError(w, http.StatusUnprocessableEntity, "INVALID_TITLE", "Demo title is required.")
					return
				}
				nextTitle, nextSlug := nextDemoNameAndSlugExcept(title, baseSlug, m.Demos, slug)
				if nextSlug != m.Demos[i].Slug {
					oldDir := filepath.Join(a.demosDir, m.Demos[i].Slug)
					nextDir := filepath.Join(a.demosDir, nextSlug)
					if err := os.Rename(oldDir, nextDir); err != nil {
						writeError(w, http.StatusInternalServerError, "RENAME_FAILED", "Unable to rename demo files.")
						return
					}
				}
				m.Demos[i].Title = nextTitle
				m.Demos[i].Slug = nextSlug
				m.Demos[i].Address = a.demoAddress(nextSlug)
			}
			if input.Disabled != nil {
				m.Demos[i].Disabled = *input.Disabled
			}
			m.Demos[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			if err := a.saveManifestLocked(m); err != nil {
				writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to save demo list.")
				return
			}
			writeJSON(w, http.StatusOK, m.Demos[i])
			return
		}
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "Demo not found.")
}

func (a *app) handleDeleteDemo(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	a.mu.Lock()
	defer a.mu.Unlock()

	m, err := a.loadManifestLocked()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "READ_FAILED", "Unable to read demos.")
		return
	}

	next := m.Demos[:0]
	found := false
	for _, item := range m.Demos {
		if item.Slug == slug {
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Demo not found.")
		return
	}
	m.Demos = next
	if err := os.RemoveAll(filepath.Join(a.demosDir, slug)); err != nil {
		writeError(w, http.StatusInternalServerError, "DELETE_FAILED", "Unable to delete demo files.")
		return
	}
	if err := a.saveManifestLocked(m); err != nil {
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to save demo list.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) handleServeDemo(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/demo/")
	slug, subPath, ok := strings.Cut(rest, "/")
	if !ok {
		slug = rest
		subPath = ""
	}
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	item, ok := a.findDemo(slug)
	if !ok || item.Disabled {
		http.NotFound(w, r)
		return
	}

	cleanSubPath := path.Clean("/" + subPath)
	if cleanSubPath == "/" {
		cleanSubPath = "/index.html"
	}
	target := filepath.Join(a.demosDir, slug, filepath.FromSlash(strings.TrimPrefix(cleanSubPath, "/")))
	if !isWithin(filepath.Join(a.demosDir, slug), target) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, target)
}

func (a *app) handleStaticFallback(w http.ResponseWriter, r *http.Request) {
	cleanPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if cleanPath == "" {
		http.ServeFile(w, r, filepath.Join(a.staticRoot, "index.html"))
		return
	}
	target := filepath.Join(a.staticRoot, filepath.FromSlash(cleanPath))
	if !isWithin(a.staticRoot, target) {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		http.ServeFile(w, r, target)
		return
	}
	http.ServeFile(w, r, filepath.Join(a.staticRoot, "index.html"))
}

func (a *app) ensureManifest() error {
	if _, err := os.Stat(a.manifestPath); errors.Is(err, os.ErrNotExist) {
		return a.saveManifestLocked(manifest{Demos: []demoItem{}})
	}
	return nil
}

func (a *app) normalizeDemoAddresses() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	m, err := a.loadManifestLocked()
	if err != nil {
		return err
	}
	changed := false
	for i := range m.Demos {
		address := a.demoAddress(m.Demos[i].Slug)
		if m.Demos[i].Address != address {
			m.Demos[i].Address = address
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return a.saveManifestLocked(m)
}

func (a *app) demoAddress(slug string) string {
	return a.publicOrigin + "/demo/" + slug
}

func (a *app) pruneStoredMarkdownTemplateTitles() error {
	m, err := a.loadManifest()
	if err != nil {
		return err
	}
	for _, item := range m.Demos {
		if item.Kind != "markdown" {
			continue
		}
		pagePath := filepath.Join(a.demosDir, item.Slug, "index.html")
		data, err := os.ReadFile(pagePath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		oldPage := string(data)
		templateTitle := "      </div>\n      <h1>" + html.EscapeString(item.Title) + "</h1>\n"
		nextPage := strings.Replace(oldPage, templateTitle, "      </div>\n", 1)
		if !strings.Contains(nextPage, `href="/favicon.svg`) {
			nextPage = strings.Replace(nextPage, "  <title>", `  <link rel="icon" type="image/svg+xml" href="/favicon.svg?v=20260528">
  <title>`, 1)
		}
		if nextPage != oldPage {
			if err := os.WriteFile(pagePath, []byte(nextPage), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *app) loadManifest() (manifest, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loadManifestLocked()
}

func (a *app) loadManifestLocked() (manifest, error) {
	var m manifest
	data, err := os.ReadFile(a.manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return manifest{Demos: []demoItem{}}, nil
	}
	if err != nil {
		return m, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return manifest{Demos: []demoItem{}}, nil
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.Demos == nil {
		m.Demos = []demoItem{}
	}
	return m, nil
}

func (a *app) saveManifestLocked(m manifest) error {
	if err := os.MkdirAll(filepath.Dir(a.manifestPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	temp := a.manifestPath + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, a.manifestPath)
}

func (a *app) findDemo(slug string) (demoItem, bool) {
	m, err := a.loadManifest()
	if err != nil {
		return demoItem{}, false
	}
	for _, item := range m.Demos {
		if item.Slug == slug {
			return item, true
		}
	}
	return demoItem{}, false
}

func (a *app) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.isAuthenticated(r) {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Login required.")
			return
		}
		next(w, r)
	}
}

func (a *app) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(cookieName)
	return err == nil && hmac.Equal([]byte(cookie.Value), []byte(a.sessionToken()))
}

func (a *app) sessionToken() string {
	mac := hmac.New(sha256.New, a.sessionSecret)
	mac.Write([]byte("go-sites-demo-admin"))
	return hex.EncodeToString(mac.Sum(nil))
}

func materializeUpload(file multipart.File, header *multipart.FileHeader, targetDir, title string) (string, error) {
	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".html", ".htm":
		data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes))
		if err != nil {
			return "", err
		}
		return "html", os.WriteFile(filepath.Join(targetDir, "index.html"), data, 0o644)
	case ".md", ".markdown":
		data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes))
		if err != nil {
			return "", err
		}
		page := renderMarkdownPage(title, string(data))
		return "markdown", os.WriteFile(filepath.Join(targetDir, "index.html"), []byte(page), 0o644)
	case ".zip":
		data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes))
		if err != nil {
			return "", err
		}
		return "zip", extractZipDemo(data, targetDir)
	default:
		return "", fmt.Errorf("only .html, .md, and .zip uploads are supported")
	}
}

func extractZipDemo(data []byte, targetDir string) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("invalid zip file")
	}
	prefix := zipSingleRootPrefix(reader.File)
	hasIndex := false
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.FileInfo().Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("zip files cannot contain symbolic links")
		}

		name := strings.TrimPrefix(filepath.ToSlash(file.Name), prefix)
		name = strings.TrimPrefix(name, "/")
		if name == "" {
			continue
		}
		if !isSafeArchivePath(name) {
			return fmt.Errorf("zip contains unsafe path: %s", file.Name)
		}
		if !isAllowedStaticFile(name) {
			return fmt.Errorf("zip contains unsupported file type: %s", name)
		}
		if strings.EqualFold(name, "index.html") {
			hasIndex = true
		}

		target := filepath.Join(targetDir, filepath.FromSlash(name))
		if !isWithin(targetDir, target) {
			return fmt.Errorf("zip contains unsafe path: %s", file.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			_ = src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, io.LimitReader(src, maxUploadBytes))
		closeErr := errors.Join(src.Close(), dst.Close())
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
	}
	if !hasIndex {
		return fmt.Errorf("zip must contain index.html")
	}
	return nil
}

func zipSingleRootPrefix(files []*zip.File) string {
	root := ""
	for _, file := range files {
		name := strings.Trim(filepath.ToSlash(file.Name), "/")
		if name == "" {
			continue
		}
		part := strings.SplitN(name, "/", 2)[0]
		if root == "" {
			root = part
			continue
		}
		if root != part {
			return ""
		}
	}
	if root == "" {
		return ""
	}
	return root + "/"
}

func isSafeArchivePath(name string) bool {
	clean := path.Clean("/" + name)
	return clean != "/" && !strings.Contains(clean, "/../") && !strings.HasPrefix(clean, "/..") && !strings.Contains(name, "\\")
}

func isAllowedStaticFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html", ".htm", ".css", ".js", ".mjs", ".json", ".txt", ".md", ".map",
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico",
		".woff", ".woff2", ".ttf", ".otf", ".wasm", ".mp3", ".mp4", ".wav":
		return true
	default:
		return false
	}
}

func renderMarkdownPage(title, source string) string {
	body := renderMarkdown(source)
	return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + html.EscapeString(title) + `</title>
  <link rel="icon" type="image/svg+xml" href="/favicon.svg?v=20260528">
  <style>
    :root { color-scheme: light; --ink: #27312b; --soft: #6f766c; --paper: #fbfaf4; --wash: #eff0e8; --line: #d7cfc2; --clay: #a55f49; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; background: linear-gradient(180deg, var(--paper), var(--wash)); color: var(--ink); font-family: "Avenir Next", "PingFang SC", "Microsoft YaHei", "Segoe UI", sans-serif; line-height: 1.72; }
    main { width: min(860px, calc(100vw - 2rem)); margin: 0 auto; padding: 3rem 0 4rem; }
    .md-brand { display: flex; align-items: center; gap: 0.875rem; margin-bottom: 1.35rem; color: var(--soft); }
    .md-brand-seal { position: relative; width: 3rem; height: 3rem; border: 1px solid rgba(117, 107, 89, 0.34); border-radius: 55% 45% 58% 42%; background: radial-gradient(circle at 42% 38%, rgba(255, 253, 248, 0.4) 0 0.3rem, transparent 0.38rem), radial-gradient(circle at 55% 58%, rgba(117, 107, 89, 0.78), rgba(117, 107, 89, 0.4) 64%, transparent 70%), rgba(235, 216, 207, 0.82); box-shadow: 0 0 0 7px rgba(235, 216, 207, 0.55), 0 12px 25px -19px rgba(94, 69, 52, 0.55); flex-shrink: 0; }
    .md-brand-seal::after { content: ""; position: absolute; inset: 0.3rem; border: 1px solid rgba(80, 72, 62, 0.24); border-radius: 42% 58% 50% 50%; transform: rotate(-18deg); }
    .md-brand-text { font-size: 0.8rem; letter-spacing: 0.08em; }
    article { padding: 2rem; border: 1px solid var(--line); border-radius: 18px 24px 17px 21px; background: rgba(255, 255, 251, 0.76); box-shadow: 0 18px 42px -34px rgba(48, 55, 49, 0.45); }
    h1, h2, h3 { line-height: 1.25; color: var(--ink); }
    h1 { margin-top: 0; font-size: clamp(1.9rem, 5vw, 3.1rem); }
    h2 { margin-top: 2rem; padding-top: 0.5rem; border-top: 1px solid var(--line); }
    a { color: var(--clay); }
    code { padding: 0.12rem 0.35rem; border-radius: 6px; background: var(--wash); }
    pre { overflow-x: auto; padding: 1rem; border-radius: 14px; background: #283129; color: #f8f5ef; }
    blockquote { margin: 1rem 0; padding: 0.4rem 1rem; border-left: 3px solid var(--clay); color: var(--soft); background: rgba(239, 240, 232, 0.62); }
  </style>
</head>
<body>
  <main>
    <article>
      <div class="md-brand" aria-hidden="true">
        <span class="md-brand-seal"></span>
        <span class="md-brand-text">灵感书架</span>
      </div>
` + body + `
    </article>
  </main>
</body>
</html>`
}

func renderMarkdown(source string) string {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	var b strings.Builder
	listType := ""
	inCode := false

	closeList := func() {
		if listType != "" {
			b.WriteString("</" + listType + ">\n")
			listType = ""
		}
	}

	openList := func(kind string) {
		if listType == kind {
			return
		}
		closeList()
		b.WriteString("<" + kind + ">\n")
		listType = kind
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				b.WriteString("</code></pre>\n")
				inCode = false
			} else {
				closeList()
				b.WriteString("<pre><code>")
				inCode = true
			}
			continue
		}
		if inCode {
			b.WriteString(html.EscapeString(line))
			b.WriteByte('\n')
			continue
		}
		if trimmed == "" {
			closeList()
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			closeList()
			b.WriteString("<h3>" + inlineMarkdown(trimmed[4:]) + "</h3>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			closeList()
			b.WriteString("<h2>" + inlineMarkdown(trimmed[3:]) + "</h2>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			closeList()
			b.WriteString("<h1>" + inlineMarkdown(trimmed[2:]) + "</h1>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "> ") {
			closeList()
			b.WriteString("<blockquote>" + inlineMarkdown(trimmed[2:]) + "</blockquote>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			openList("ul")
			b.WriteString("<li>" + inlineMarkdown(trimmed[2:]) + "</li>\n")
			continue
		}
		if itemText, ok := orderedListItem(trimmed); ok {
			openList("ol")
			b.WriteString("<li>" + inlineMarkdown(itemText) + "</li>\n")
			continue
		}
		closeList()
		b.WriteString("<p>" + inlineMarkdown(trimmed) + "</p>\n")
	}
	closeList()
	if inCode {
		b.WriteString("</code></pre>\n")
	}
	return b.String()
}

func markdownTitleFromContent(source string) string {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	for _, line := range lines {
		title := cleanMarkdownTitleLine(strings.TrimSpace(line))
		if title == "" {
			continue
		}
		if before, ok := beforeTitlePunctuation(title); ok {
			title = before
		}
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		return firstRunes(title, 10)
	}
	return ""
}

func cleanMarkdownTitleLine(line string) string {
	for strings.HasPrefix(line, ">") {
		line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
	}
	line = strings.TrimLeft(line, "#")
	line = strings.TrimSpace(line)

	for {
		switch {
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ "):
			line = strings.TrimSpace(line[2:])
		default:
			if itemText, ok := orderedListItem(line); ok {
				line = itemText
				continue
			}
			line = strings.TrimSpace(strings.Trim(line, "*_`~"))
			line = strings.TrimSpace(stripTitleMarkdown(line))
			return line
		}
	}
}

func stripTitleMarkdown(text string) string {
	replacer := strings.NewReplacer("**", "", "__", "", "`", "")
	text = replacer.Replace(text)

	var b strings.Builder
	for {
		start := strings.Index(text, "[")
		if start < 0 {
			b.WriteString(text)
			break
		}
		endText := strings.Index(text[start:], "](")
		if endText < 0 {
			b.WriteString(text)
			break
		}
		endText += start
		endURL := strings.Index(text[endText+2:], ")")
		if endURL < 0 {
			b.WriteString(text)
			break
		}
		endURL += endText + 2
		b.WriteString(text[:start])
		b.WriteString(text[start+1 : endText])
		text = text[endURL+1:]
	}
	return b.String()
}

func beforeTitlePunctuation(text string) (string, bool) {
	for index, r := range text {
		if strings.ContainsRune("，。！？；：,.!?;:、（）()【】[]《》<>", r) {
			before := strings.TrimSpace(text[:index])
			if before != "" {
				return before, true
			}
		}
	}
	return text, false
}

func firstRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func inlineMarkdown(text string) string {
	escaped := html.EscapeString(text)
	escaped = renderStrong(escaped, "**")
	escaped = renderStrong(escaped, "__")
	var b strings.Builder
	for {
		start := strings.Index(escaped, "[")
		if start < 0 {
			b.WriteString(escaped)
			break
		}
		endText := strings.Index(escaped[start:], "](")
		if endText < 0 {
			b.WriteString(escaped)
			break
		}
		endText += start
		endURL := strings.Index(escaped[endText+2:], ")")
		if endURL < 0 {
			b.WriteString(escaped)
			break
		}
		endURL += endText + 2
		label := escaped[start+1 : endText]
		url := escaped[endText+2 : endURL]
		b.WriteString(escaped[:start])
		if safeLink(url) {
			b.WriteString(`<a href="` + url + `" target="_blank" rel="noopener noreferrer">` + label + `</a>`)
		} else {
			b.WriteString(label)
		}
		escaped = escaped[endURL+1:]
	}
	return b.String()
}

func renderStrong(text, marker string) string {
	var b strings.Builder
	for {
		start := strings.Index(text, marker)
		if start < 0 {
			b.WriteString(text)
			break
		}
		end := strings.Index(text[start+len(marker):], marker)
		if end < 0 {
			b.WriteString(text)
			break
		}
		end += start + len(marker)
		content := text[start+len(marker) : end]
		if content == "" {
			b.WriteString(text[:end+len(marker)])
			text = text[end+len(marker):]
			continue
		}
		b.WriteString(text[:start])
		b.WriteString("<strong>" + content + "</strong>")
		text = text[end+len(marker):]
	}
	return b.String()
}

func orderedListItem(text string) (string, bool) {
	dot := strings.Index(text, ".")
	if dot <= 0 || dot+1 >= len(text) {
		return "", false
	}
	for _, r := range text[:dot] {
		if !unicode.IsDigit(r) {
			return "", false
		}
	}
	if text[dot+1] != ' ' && text[dot+1] != '\t' {
		return "", false
	}
	return strings.TrimSpace(text[dot+2:]), true
}

func safeLink(url string) bool {
	lower := strings.ToLower(url)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "#")
}

func slugify(input string) string {
	input = strings.TrimSpace(strings.TrimSuffix(input, filepath.Ext(input)))
	var b strings.Builder
	lastDash := false
	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if unicode.IsSpace(r) || r == '-' || r == '_' {
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func nextDemoNameAndSlug(title, baseSlug string, demos []demoItem) (string, string) {
	return nextDemoNameAndSlugExcept(title, baseSlug, demos, "")
}

func nextDemoNameAndSlugExcept(title, baseSlug string, demos []demoItem, exceptSlug string) (string, string) {
	used := map[string]bool{}
	for _, item := range demos {
		if item.Slug == exceptSlug {
			continue
		}
		used[item.Slug] = true
	}
	if !used[baseSlug] {
		return title, baseSlug
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s%d", baseSlug, i)
		if !used[candidate] {
			return fmt.Sprintf("%s%d", title, i), candidate
		}
	}
}

func isWithin(root, target string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var payload apiError
	payload.Error.Code = code
	payload.Error.Message = message
	writeJSON(w, status, payload)
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
