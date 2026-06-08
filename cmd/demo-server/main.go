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
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	rendererhtml "github.com/yuin/goldmark/renderer/html"
)

const (
	cookieName       = "go_sites_demo_session"
	sessionMaxAgeSec = 60 * 60 * 24 * 7
	maxUploadBytes   = 25 << 20
)

var appRoutePaths = []string{
	"/search",
	"/recommendations",
	"/ai-tools",
	"/dev-tools",
	"/tools",
	"/fun",
	"/works",
	"/private",
	"/demo",
	"/wiki",
}

type app struct {
	mu            sync.Mutex
	dataDir       string
	demosDir      string
	wikiRoot      string
	manifestPath  string
	staticRoot    string
	adminPassword string
	apiKey        string
	sessionSecret []byte
	publicOrigin  string
}

type demoItem struct {
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	Address   string `json:"address"`
	Disabled  bool   `json:"disabled"`
	Kind      string `json:"kind"`
	Feature   string `json:"feature"`
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

type publishDemoInput struct {
	Title    string `json:"title"`
	Slug     string `json:"slug"`
	HTML     string `json:"html"`
	Markdown string `json:"markdown"`
	Content  string `json:"content"`
	Kind     string `json:"kind"`
	Feature  string `json:"feature"`
	Disabled *bool  `json:"disabled"`
}

func main() {
	dataDir := envOr("DEMO_DATA_DIR", filepath.Join(".", "demo-data"))
	staticRoot := envOr("SITE_ROOT", ".")
	password := firstEnv("DEMO_ADMIN_PASSWORD", "GO_SITES_DEMO_PASSWORD")
	apiKey := firstEnv("DEMO_API_KEY", "GO_SITES_DEMO_API_KEY")
	secret := firstEnv("DEMO_SESSION_SECRET", "GO_SITES_DEMO_PASSWORD", "DEMO_ADMIN_PASSWORD")
	if secret == "" {
		secret = "local-development-demo-secret"
	}

	a := &app{
		dataDir:       dataDir,
		demosDir:      filepath.Join(dataDir, "demos"),
		wikiRoot:      envOr("WIKI_ROOT", filepath.Join(".", "wiki")),
		manifestPath:  filepath.Join(dataDir, "manifest.json"),
		staticRoot:    staticRoot,
		adminPassword: password,
		apiKey:        apiKey,
		sessionSecret: []byte(secret),
		publicOrigin:  strings.TrimRight(envOr("PUBLIC_ORIGIN", "https://liangz77.cn"), "/"),
	}

	if err := os.MkdirAll(a.demosDir, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(a.wikiRoot, 0o755); err != nil {
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

	mux := a.routes()
	addr := envOr("DEMO_SERVER_ADDR", ":9005")
	log.Printf("demo server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, secureHeaders(mux)))
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/demo/session", a.handleSessionStatus)
	mux.HandleFunc("POST /api/demo/session", a.handleSession)
	mux.HandleFunc("GET /api/demos", a.handleListDemos)
	mux.HandleFunc("POST /api/demos", a.requireAuth(a.handleCreateDemo))
	mux.HandleFunc("POST /api/demos/publish", a.requireAPIKey(a.handlePublishDemo))
	mux.HandleFunc("PATCH /api/demos/{slug}", a.requireAuth(a.handleUpdateDemo))
	mux.HandleFunc("DELETE /api/demos/{slug}", a.requireAuth(a.handleDeleteDemo))
	mux.HandleFunc("GET /api/wiki/files", a.requireAuth(a.handleListWikiFiles))
	mux.HandleFunc("POST /api/wiki/files", a.requireAuth(a.handleCreateWikiFile))
	mux.HandleFunc("POST /api/wiki/upload", a.requireAuth(a.handleUploadWikiFiles))
	mux.HandleFunc("GET /api/wiki/files/", a.requireAuth(a.handleGetWikiFile))
	mux.HandleFunc("PUT /api/wiki/files/", a.requireAuth(a.handleUpdateWikiFile))
	mux.HandleFunc("DELETE /api/wiki/files/", a.requireAuth(a.handleDeleteWikiFile))
	mux.HandleFunc("POST /api/wiki/folders", a.requireAuth(a.handleCreateWikiFolder))
	mux.HandleFunc("DELETE /api/wiki/folders/", a.requireAuth(a.handleDeleteWikiFolder))
	mux.HandleFunc("PATCH /api/wiki/move", a.requireAuth(a.handleMoveWikiEntry))
	for _, routePath := range appRoutePaths {
		mux.HandleFunc("GET "+routePath, a.handleStaticFallback)
	}
	mux.HandleFunc("/demo/", a.handleServeDemo)
	mux.HandleFunc("/wiki/", a.requireAuth(a.handleServeWikiAsset))
	mux.HandleFunc("/", a.handleStaticFallback)
	return mux
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
		MaxAge:   sessionMaxAgeSec,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	if !a.isAuthenticated(r) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Login required.")
		return
	}
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

type wikiFileItem struct {
	Path      string `json:"path"`
	Title     string `json:"title"`
	Content   string `json:"content,omitempty"`
	HTML      string `json:"html,omitempty"`
	UpdatedAt string `json:"updatedAt"`
	Size      int64  `json:"size"`
}

type wikiFolderItem struct {
	Path string `json:"path"`
}

func (a *app) handleListWikiFiles(w http.ResponseWriter, r *http.Request) {
	files, err := a.collectWikiFiles(false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "WIKI_READ_FAILED", "Unable to read wiki files.")
		return
	}
	folders, err := a.collectWikiFolders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "WIKI_READ_FAILED", "Unable to read wiki folders.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "folders": folders})
}

func (a *app) handleGetWikiFile(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/api/wiki/files/")
	if relPath == "" {
		writeError(w, http.StatusBadRequest, "WIKI_FILE_REQUIRED", "Wiki file path is required.")
		return
	}
	content, info, cleanPath, err := a.readWikiFile(relPath)
	if err != nil {
		status := http.StatusInternalServerError
		code := "WIKI_READ_FAILED"
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
			code = "WIKI_NOT_FOUND"
		} else if errors.Is(err, errUnsafeWikiPath) {
			status = http.StatusBadRequest
			code = "WIKI_INVALID_PATH"
		}
		writeError(w, status, code, "Unable to read wiki file.")
		return
	}
	item := wikiFileItem{
		Path:      cleanPath,
		Title:     markdownTitleFromContent(content),
		Content:   content,
		UpdatedAt: info.ModTime().UTC().Format(time.RFC3339),
		Size:      info.Size(),
	}
	if r.URL.Query().Get("render") != "false" {
		item.HTML = renderMarkdown(content)
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *app) handleCreateWikiFile(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Parent  string `json:"parent"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body.")
		return
	}
	name := strings.TrimSpace(input.Name)
	if filepath.Ext(name) == "" {
		name += ".md"
	}
	if !validWikiName(name) || !isMarkdownFile(name) {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_NAME", "Invalid wiki file name.")
		return
	}
	parentClean, parentTarget, err := a.wikiFolderTarget(input.Parent, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki folder path.")
		return
	}
	targetClean := path.Join(parentClean, name)
	if parentClean == "." || parentClean == "" {
		targetClean = name
	}
	target := filepath.Join(parentTarget, name)
	if !isWithin(filepath.Clean(a.wikiRoot), target) {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki file path.")
		return
	}
	if _, err := os.Stat(target); err == nil {
		writeError(w, http.StatusConflict, "WIKI_EXISTS", "Wiki file already exists.")
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, "WIKI_WRITE_FAILED", "Unable to create wiki file.")
		return
	}
	content := input.Content
	if strings.TrimSpace(content) == "" {
		content = "# " + strings.TrimSuffix(name, filepath.Ext(name)) + "\n"
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "WIKI_WRITE_FAILED", "Unable to create wiki file.")
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "WIKI_READ_FAILED", "Unable to inspect created wiki file.")
		return
	}
	writeJSON(w, http.StatusCreated, wikiFileItem{
		Path:      filepath.ToSlash(targetClean),
		Title:     markdownTitleFromContent(content),
		Content:   content,
		UpdatedAt: info.ModTime().UTC().Format(time.RFC3339),
		Size:      info.Size(),
	})
}

type wikiUploadItem struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func (a *app) handleUploadWikiFiles(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_UPLOAD", "Invalid wiki upload.")
		return
	}
	parent := r.FormValue("parent")
	parentClean, parentTarget, err := a.wikiFolderTarget(parent, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki folder path.")
		return
	}
	headers := r.MultipartForm.File["files"]
	if len(headers) == 0 {
		headers = r.MultipartForm.File["file"]
	}
	if len(headers) == 0 {
		writeError(w, http.StatusBadRequest, "WIKI_UPLOAD_REQUIRED", "No files were uploaded.")
		return
	}
	for _, header := range headers {
		name := strings.TrimSpace(filepath.Base(header.Filename))
		if !validWikiName(name) || !isMarkdownFile(name) {
			writeError(w, http.StatusBadRequest, "WIKI_UNSUPPORTED_UPLOAD", "Only Markdown files can be added to the wiki.")
			return
		}
	}
	uploaded := []wikiUploadItem{}
	markdownFiles := []wikiFileItem{}
	for _, header := range headers {
		name := strings.TrimSpace(filepath.Base(header.Filename))
		targetName, target, err := nextWikiUploadTarget(parentTarget, name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "WIKI_UPLOAD_FAILED", "Unable to prepare wiki upload.")
			return
		}
		if !isWithin(filepath.Clean(a.wikiRoot), target) {
			writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki upload path.")
			return
		}
		src, err := header.Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_UPLOAD", "Unable to read uploaded file.")
			return
		}
		err = writeUploadedWikiFile(src, target)
		_ = src.Close()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "WIKI_UPLOAD_FAILED", "Unable to save uploaded file.")
			return
		}
		rel := path.Join(parentClean, targetName)
		if parentClean == "." || parentClean == "" {
			rel = targetName
		}
		rel = filepath.ToSlash(rel)
		info, statErr := os.Stat(target)
		size := header.Size
		if statErr == nil {
			size = info.Size()
		}
		uploaded = append(uploaded, wikiUploadItem{Path: rel, Size: size})
		title := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
		updatedAt := ""
		fileSize := size
		if statErr == nil {
			updatedAt = info.ModTime().UTC().Format(time.RFC3339)
			fileSize = info.Size()
		}
		markdownFiles = append(markdownFiles, wikiFileItem{
			Path:      rel,
			Title:     title,
			UpdatedAt: updatedAt,
			Size:      fileSize,
		})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"uploaded": uploaded, "files": markdownFiles})
}

func (a *app) handleUpdateWikiFile(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/api/wiki/files/")
	var input struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 5<<20)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body.")
		return
	}
	cleanPath, target, err := a.wikiFileTarget(relPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki file path.")
		return
	}
	if _, err := os.Stat(target); err != nil {
		status := http.StatusInternalServerError
		code := "WIKI_WRITE_FAILED"
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
			code = "WIKI_NOT_FOUND"
		}
		writeError(w, status, code, "Unable to save wiki file.")
		return
	}
	if err := os.WriteFile(target, []byte(input.Content), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "WIKI_WRITE_FAILED", "Unable to save wiki file.")
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "WIKI_READ_FAILED", "Unable to inspect saved wiki file.")
		return
	}
	writeJSON(w, http.StatusOK, wikiFileItem{
		Path:      cleanPath,
		Title:     markdownTitleFromContent(input.Content),
		Content:   input.Content,
		UpdatedAt: info.ModTime().UTC().Format(time.RFC3339),
		Size:      info.Size(),
	})
}

func (a *app) handleDeleteWikiFile(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/api/wiki/files/")
	_, target, err := a.wikiFileTarget(relPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki file path.")
		return
	}
	info, err := os.Lstat(target)
	if err != nil {
		status := http.StatusInternalServerError
		code := "WIKI_DELETE_FAILED"
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
			code = "WIKI_NOT_FOUND"
		}
		writeError(w, status, code, "Unable to delete wiki file.")
		return
	}
	if info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki file path.")
		return
	}
	if err := os.Remove(target); err != nil {
		writeError(w, http.StatusInternalServerError, "WIKI_DELETE_FAILED", "Unable to delete wiki file.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleCreateWikiFolder(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body.")
		return
	}
	name := strings.TrimSpace(input.Name)
	if !validWikiName(name) {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_NAME", "Invalid wiki folder name.")
		return
	}
	parentClean, parentTarget, err := a.wikiFolderTarget(input.Parent, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki folder path.")
		return
	}
	targetClean := path.Join(parentClean, name)
	if parentClean == "." || parentClean == "" {
		targetClean = name
	}
	target := filepath.Join(parentTarget, name)
	if !isWithin(filepath.Clean(a.wikiRoot), target) {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki folder path.")
		return
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			writeError(w, http.StatusConflict, "WIKI_EXISTS", "Wiki folder already exists.")
			return
		}
		writeError(w, http.StatusInternalServerError, "WIKI_WRITE_FAILED", "Unable to create wiki folder.")
		return
	}
	writeJSON(w, http.StatusCreated, wikiFolderItem{Path: filepath.ToSlash(targetClean)})
}

func (a *app) handleDeleteWikiFolder(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/api/wiki/folders/")
	cleanPath, target, err := a.wikiFolderTarget(relPath, false)
	if err != nil || cleanPath == "." || cleanPath == "" {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki folder path.")
		return
	}
	info, err := os.Lstat(target)
	if err != nil {
		status := http.StatusInternalServerError
		code := "WIKI_DELETE_FAILED"
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
			code = "WIKI_NOT_FOUND"
		}
		writeError(w, status, code, "Unable to delete wiki folder.")
		return
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki folder path.")
		return
	}
	if err := os.RemoveAll(target); err != nil {
		writeError(w, http.StatusInternalServerError, "WIKI_DELETE_FAILED", "Unable to delete wiki folder.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleMoveWikiEntry(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Path         string `json:"path"`
		TargetFolder string `json:"targetFolder"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body.")
		return
	}
	sourceClean, sourceTarget, err := a.wikiFileTarget(input.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid wiki file path.")
		return
	}
	targetFolderClean, targetFolder, err := a.wikiFolderTarget(input.TargetFolder, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "WIKI_INVALID_PATH", "Invalid target folder path.")
		return
	}
	targetClean := path.Join(targetFolderClean, path.Base(sourceClean))
	if targetFolderClean == "." || targetFolderClean == "" {
		targetClean = path.Base(sourceClean)
	}
	target := filepath.Join(targetFolder, filepath.Base(sourceTarget))
	if filepath.Clean(sourceTarget) == filepath.Clean(target) {
		info, err := os.Stat(sourceTarget)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "WIKI_READ_FAILED", "Unable to read wiki file.")
			return
		}
		writeJSON(w, http.StatusOK, wikiFileItem{Path: sourceClean, Title: strings.TrimSuffix(filepath.Base(sourceClean), filepath.Ext(sourceClean)), UpdatedAt: info.ModTime().UTC().Format(time.RFC3339), Size: info.Size()})
		return
	}
	if _, err := os.Stat(target); err == nil {
		writeError(w, http.StatusConflict, "WIKI_EXISTS", "Target file already exists.")
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, "WIKI_MOVE_FAILED", "Unable to move wiki file.")
		return
	}
	if err := os.Rename(sourceTarget, target); err != nil {
		writeError(w, http.StatusInternalServerError, "WIKI_MOVE_FAILED", "Unable to move wiki file.")
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "WIKI_READ_FAILED", "Unable to read moved wiki file.")
		return
	}
	writeJSON(w, http.StatusOK, wikiFileItem{
		Path:      filepath.ToSlash(targetClean),
		Title:     strings.TrimSuffix(filepath.Base(targetClean), filepath.Ext(targetClean)),
		UpdatedAt: info.ModTime().UTC().Format(time.RFC3339),
		Size:      info.Size(),
	})
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

func (a *app) handlePublishDemo(w http.ResponseWriter, r *http.Request) {
	var input publishDemoInput
	if err := json.NewDecoder(io.LimitReader(r.Body, maxUploadBytes)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body.")
		return
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_TITLE", "Demo title is required.")
		return
	}

	kind, page, err := publishedDemoPage(title, input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CONTENT", err.Error())
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	m, err := a.loadManifestLocked()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "READ_FAILED", "Unable to read demos.")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	index := demoIndexByTitle(m.Demos, title)
	created := now
	slug := slugify(input.Slug)
	if index >= 0 {
		slug = m.Demos[index].Slug
		created = m.Demos[index].CreatedAt
	} else {
		if slug == "" {
			slug = slugify(title)
		}
		if slug == "" {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_SLUG", "Demo slug is required.")
			return
		}
		if demoSlugInUse(m.Demos, slug) {
			writeError(w, http.StatusConflict, "SLUG_EXISTS", "Demo slug is already used by another title.")
			return
		}
	}

	targetDir := filepath.Join(a.demosDir, slug)
	tempDir := filepath.Join(a.demosDir, "."+slug+".tmp-"+fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to prepare demo directory.")
		return
	}
	defer os.RemoveAll(tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, "index.html"), []byte(page), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to save demo.")
		return
	}
	if err := os.RemoveAll(targetDir); err != nil {
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to replace existing demo.")
		return
	}
	if err := os.Rename(tempDir, targetDir); err != nil {
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to publish demo.")
		return
	}

	item := demoItem{
		Title:     title,
		Slug:      slug,
		Address:   a.demoAddress(slug),
		Kind:      kind,
		Feature:   strings.TrimSpace(input.Feature),
		CreatedAt: created,
		UpdatedAt: now,
	}
	if input.Disabled != nil {
		item.Disabled = *input.Disabled
	} else if index >= 0 {
		item.Disabled = m.Demos[index].Disabled
	}

	status := http.StatusCreated
	if index >= 0 {
		m.Demos[index] = item
		status = http.StatusOK
	} else {
		m.Demos = append(m.Demos, item)
	}
	sort.Slice(m.Demos, func(i, j int) bool {
		return m.Demos[i].CreatedAt > m.Demos[j].CreatedAt
	})

	if err := a.saveManifestLocked(m); err != nil {
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", "Unable to save demo list.")
		return
	}

	writeJSON(w, status, item)
}

func (a *app) handleUpdateDemo(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var input struct {
		Disabled *bool   `json:"disabled"`
		Title    *string `json:"title"`
		Feature  *string `json:"feature"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body.")
		return
	}
	if input.Disabled == nil && input.Title == nil && input.Feature == nil {
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
			if input.Feature != nil {
				m.Demos[i].Feature = strings.TrimSpace(*input.Feature)
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
	if subPath == "" && !strings.HasSuffix(r.URL.Path, "/") {
		target := "/demo/" + slug + "/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
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

func (a *app) handleServeWikiAsset(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/wiki/")
	decoded, err := pathUnescape(relPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	clean := strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(decoded)), "/")
	if clean == "." || clean == "" || strings.Contains(clean, "..") || strings.Contains(decoded, "\\") {
		http.NotFound(w, r)
		return
	}
	root := filepath.Clean(a.wikiRoot)
	target := filepath.Join(root, filepath.FromSlash(clean))
	if !isWithin(root, target) {
		http.NotFound(w, r)
		return
	}
	info, err := os.Lstat(target)
	if err != nil || info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
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

var errUnsafeWikiPath = errors.New("unsafe wiki path")

func (a *app) collectWikiFiles(includeContent bool) ([]wikiFileItem, error) {
	root := filepath.Clean(a.wikiRoot)
	files := []wikiFileItem{}
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !isMarkdownFile(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		content := ""
		renderedHTML := ""
		title := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
		if includeContent {
			data, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			content = string(data)
			renderedHTML = renderMarkdown(content)
			if extracted := markdownTitleFromContent(content); extracted != "" {
				title = extracted
			}
		}
		files = append(files, wikiFileItem{
			Path:      rel,
			Title:     title,
			Content:   content,
			HTML:      renderedHTML,
			UpdatedAt: info.ModTime().UTC().Format(time.RFC3339),
			Size:      info.Size(),
		})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return []wikiFileItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		if strings.EqualFold(files[i].Path, "README.md") || strings.EqualFold(files[i].Path, "index.md") {
			return true
		}
		if strings.EqualFold(files[j].Path, "README.md") || strings.EqualFold(files[j].Path, "index.md") {
			return false
		}
		return strings.ToLower(files[i].Path) < strings.ToLower(files[j].Path)
	})
	return files, nil
}

func (a *app) collectWikiFolders() ([]wikiFolderItem, error) {
	root := filepath.Clean(a.wikiRoot)
	folders := []wikiFolderItem{}
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if filePath == root || !entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		folders = append(folders, wikiFolderItem{Path: filepath.ToSlash(rel)})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return []wikiFolderItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(folders, func(i, j int) bool {
		return strings.ToLower(folders[i].Path) < strings.ToLower(folders[j].Path)
	})
	return folders, nil
}

func (a *app) readWikiFile(relPath string) (string, fs.FileInfo, string, error) {
	decoded, err := pathUnescape(relPath)
	if err != nil {
		return "", nil, "", errUnsafeWikiPath
	}
	clean := strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(decoded)), "/")
	if clean == "." || clean == "" || strings.Contains(clean, "..") || strings.Contains(decoded, "\\") || !isMarkdownFile(clean) {
		return "", nil, "", errUnsafeWikiPath
	}
	root := filepath.Clean(a.wikiRoot)
	target := filepath.Join(root, filepath.FromSlash(clean))
	if !isWithin(root, target) {
		return "", nil, "", errUnsafeWikiPath
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", nil, "", err
	}
	if info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return "", nil, "", os.ErrNotExist
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", nil, "", err
	}
	return string(data), info, clean, nil
}

func (a *app) wikiFileTarget(relPath string) (string, string, error) {
	decoded, err := pathUnescape(relPath)
	if err != nil {
		return "", "", errUnsafeWikiPath
	}
	clean := strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(decoded)), "/")
	if clean == "." || clean == "" || strings.Contains(clean, "..") || strings.Contains(decoded, "\\") || !isMarkdownFile(clean) {
		return "", "", errUnsafeWikiPath
	}
	root := filepath.Clean(a.wikiRoot)
	target := filepath.Join(root, filepath.FromSlash(clean))
	if !isWithin(root, target) {
		return "", "", errUnsafeWikiPath
	}
	return clean, target, nil
}

func (a *app) wikiFolderTarget(relPath string, createIfMissing bool) (string, string, error) {
	decoded, err := pathUnescape(relPath)
	if err != nil {
		return "", "", errUnsafeWikiPath
	}
	clean := strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(decoded)), "/")
	if clean == "" {
		clean = "."
	}
	if strings.Contains(clean, "..") || strings.Contains(decoded, "\\") || clean == ".." {
		return "", "", errUnsafeWikiPath
	}
	root := filepath.Clean(a.wikiRoot)
	target := root
	if clean != "." {
		target = filepath.Join(root, filepath.FromSlash(clean))
	}
	if clean != "." && !isWithin(root, target) {
		return "", "", errUnsafeWikiPath
	}
	if createIfMissing {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return "", "", err
		}
	} else {
		info, err := os.Lstat(target)
		if err != nil {
			return "", "", err
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return "", "", errUnsafeWikiPath
		}
	}
	return clean, target, nil
}

func pathUnescape(value string) (string, error) {
	decoded, err := urlPathUnescape(value)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(decoded), nil
}

func urlPathUnescape(value string) (string, error) {
	return url.PathUnescape(value)
}

func validWikiName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return false
	}
	for _, char := range name {
		if char < 32 || char == 127 {
			return false
		}
	}
	return true
}

func nextWikiUploadTarget(parentTarget, name string) (string, string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for index := 1; index < 10000; index++ {
		candidate := name
		if index > 1 {
			candidate = fmt.Sprintf("%s-%d%s", base, index, ext)
		}
		if !validWikiName(candidate) {
			return "", "", errUnsafeWikiPath
		}
		target := filepath.Join(parentTarget, candidate)
		if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
			return candidate, target, nil
		} else if err != nil {
			return "", "", err
		}
	}
	return "", "", fmt.Errorf("too many duplicate upload names")
}

func writeUploadedWikiFile(src multipart.File, target string) error {
	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, io.LimitReader(src, maxUploadBytes))
	return err
}

func isMarkdownFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
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

func (a *app) requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.apiKey == "" {
			writeError(w, http.StatusServiceUnavailable, "API_AUTH_NOT_CONFIGURED", "Demo API key is not configured.")
			return
		}
		if !a.isAPIAuthenticated(r) {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Valid API key required.")
			return
		}
		next(w, r)
	}
}

func (a *app) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(cookieName)
	return err == nil && hmac.Equal([]byte(cookie.Value), []byte(a.sessionToken()))
}

func (a *app) isAPIAuthenticated(r *http.Request) bool {
	key := strings.TrimSpace(r.Header.Get("X-Demo-API-Key"))
	if key == "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			key = strings.TrimSpace(auth[len("bearer "):])
		}
	}
	return key != "" && hmac.Equal([]byte(key), []byte(a.apiKey))
}

func (a *app) sessionToken() string {
	mac := hmac.New(sha256.New, a.sessionSecret)
	mac.Write([]byte("go-sites-demo-admin"))
	return hex.EncodeToString(mac.Sum(nil))
}

func publishedDemoPage(title string, input publishDemoInput) (string, string, error) {
	if strings.TrimSpace(input.HTML) != "" {
		return "html", input.HTML, nil
	}

	markdown := strings.TrimSpace(input.Markdown)
	if markdown == "" && strings.EqualFold(strings.TrimSpace(input.Kind), "markdown") {
		markdown = strings.TrimSpace(input.Content)
	}
	if markdown != "" {
		return "markdown", renderMarkdownPage(title, markdown), nil
	}

	content := strings.TrimSpace(input.Content)
	if content != "" {
		return "html", input.Content, nil
	}

	return "", "", fmt.Errorf("provide html, markdown, or content")
}

func demoIndexByTitle(demos []demoItem, title string) int {
	for i, item := range demos {
		if strings.EqualFold(strings.TrimSpace(item.Title), title) {
			return i
		}
	}
	return -1
}

func demoSlugInUse(demos []demoItem, slug string) bool {
	for _, item := range demos {
		if item.Slug == slug {
			return true
		}
	}
	return false
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
    pre { overflow-x: auto; white-space: pre-wrap; overflow-wrap: anywhere; padding: 0.9rem 1rem; border: 1px solid var(--line); border-left: 3px solid #8fa5a0; border-radius: 10px; background: #f7f6ef; color: var(--ink); font-family: "Avenir Next", "PingFang SC", "Microsoft YaHei", "Segoe UI", sans-serif; line-height: 1.72; }
    pre code { display: block; padding: 0; border-radius: 0; background: transparent; color: inherit; font: inherit; white-space: inherit; }
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
	var out bytes.Buffer
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Typographer),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(rendererhtml.WithHardWraps()),
	)
	if err := md.Convert([]byte(source), &out); err != nil {
		return "<p>" + html.EscapeString(source) + "</p>\n"
	}
	return out.String()
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
