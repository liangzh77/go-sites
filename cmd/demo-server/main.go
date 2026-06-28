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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	rendererhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	cookieName                 = "go_sites_demo_session"
	sessionMaxAgeSec           = 60 * 60 * 24 * 7
	maxUploadBytes             = 25 << 20
	markdownDemoFaviconVersion = "20260618-bulb-logo"
	demoThemeMarker            = `id="go-sites-demo-theme"`
)

var markdownHrefAttributePattern = regexp.MustCompile(`href="([^"]*)"`)

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
	if err := a.refreshStoredMarkdownDemoPages(); err != nil {
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
		HTML:      renderMarkdown(input.Content),
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
	sourceName := strings.TrimSpace(r.FormValue("sourceName"))
	folderFiles, err := demoFolderUploadFiles(r.MultipartForm)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_UPLOAD", err.Error())
		return
	}

	var file multipart.File
	var header *multipart.FileHeader
	if len(folderFiles) == 0 {
		file, header, err = r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "FILE_REQUIRED", "A demo file is required.")
			return
		}
		defer file.Close()
		if title == "" {
			title = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
		}
	} else if title == "" {
		title = demoFolderUploadTitle(sourceName, folderFiles)
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

	var kind string
	if len(folderFiles) > 0 {
		kind, err = materializeFolderUpload(folderFiles, tempDir, title)
	} else {
		kind, err = materializeUpload(file, header, tempDir, title)
	}
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
	demoRoot := filepath.Join(a.demosDir, slug)
	target := filepath.Join(demoRoot, filepath.FromSlash(strings.TrimPrefix(cleanSubPath, "/")))
	if !isWithin(demoRoot, target) {
		http.NotFound(w, r)
		return
	}
	a.serveDemoFile(w, r, item, demoRoot, target, slug)
}

func (a *app) serveDemoFile(w http.ResponseWriter, r *http.Request, item demoItem, demoRoot, target, slug string) {
	if !isDemoHTMLFile(target) || (!strings.EqualFold(item.Kind, "markdown") && !strings.EqualFold(item.Kind, "markdown-folder")) {
		http.ServeFile(w, r, target)
		return
	}

	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "READ_FAILED", "Unable to read demo page.")
		return
	}

	page := injectDemoThemeHTML(string(data))
	if item.Kind == "markdown-folder" && strings.Contains(string(data), demoThemeMarker) {
		page = injectMarkdownFolderTreeHTML(page, demoRoot, target, slug)
	}
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, info.Name(), info.ModTime(), strings.NewReader(page))
}

func isDemoHTMLFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html", ".htm":
		return true
	default:
		return false
	}
}

func injectDemoThemeHTML(page string) string {
	if strings.Contains(page, demoThemeMarker) {
		return page
	}

	injection := demoThemeHeadHTML()
	lowerPage := strings.ToLower(page)
	if index := strings.Index(lowerPage, "</head>"); index >= 0 {
		return page[:index] + injection + page[index:]
	}
	if index := strings.Index(lowerPage, "<body"); index >= 0 {
		return page[:index] + injection + page[index:]
	}
	return injection + page
}

const markdownFolderTreeMarker = `data-md-folder-tree`

type markdownFolderTreeFile struct {
	Path  string
	Label string
}

type markdownFolderTreeNode struct {
	Folders map[string]*markdownFolderTreeNode
	Files   []markdownFolderTreeFile
}

func injectMarkdownFolderTreeHTML(page, demoRoot, currentTarget, slug string) string {
	if strings.Contains(page, markdownFolderTreeMarker) {
		return page
	}

	files, err := markdownFolderTreeFiles(demoRoot)
	if err != nil || len(files) == 0 {
		return page
	}
	currentRel, err := filepath.Rel(demoRoot, currentTarget)
	if err != nil {
		return page
	}
	currentRel = filepath.ToSlash(currentRel)
	treeHTML := renderMarkdownFolderTree(files, currentRel, slug)
	page = injectBeforeClosingTag(page, "</style>", markdownFolderTreeCSS())
	page = injectAfterOpeningBody(page, treeHTML)
	page = injectBeforeClosingTag(page, "</body>", markdownFolderTreeScript())
	return page
}

func markdownFolderTreeFiles(demoRoot string) ([]markdownFolderTreeFile, error) {
	files := []markdownFolderTreeFile{}
	err := filepath.WalkDir(demoRoot, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if filePath == demoRoot {
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !isDemoHTMLFile(filePath) {
			return nil
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		if !strings.Contains(string(data), demoThemeMarker) {
			return nil
		}
		rel, err := filepath.Rel(demoRoot, filePath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		files = append(files, markdownFolderTreeFile{
			Path:  rel,
			Label: markdownFolderTreeLabel(rel),
		})
		return nil
	})
	sort.Slice(files, func(i, j int) bool {
		return markdownFolderTreeSortKey(files[i].Path) < markdownFolderTreeSortKey(files[j].Path)
	})
	return files, err
}

func markdownFolderTreeLabel(relPath string) string {
	name := path.Base(relPath)
	ext := path.Ext(name)
	name = strings.TrimSuffix(name, ext)
	if name == "" {
		return "Untitled"
	}
	return name
}

func markdownFolderTreeSortKey(relPath string) string {
	lower := strings.ToLower(relPath)
	if lower == "readme.html" || lower == "index.html" {
		return " " + lower
	}
	return lower
}

func renderMarkdownFolderTree(files []markdownFolderTreeFile, currentRel, slug string) string {
	root := &markdownFolderTreeNode{Folders: map[string]*markdownFolderTreeNode{}}
	for _, file := range files {
		addMarkdownFolderTreeFile(root, file)
	}

	var b strings.Builder
	b.WriteString(`<button class="md-folder-tree-toggle" type="button" aria-expanded="false" aria-controls="mdFolderTree" data-md-folder-tree-toggle>目录</button>`)
	b.WriteString(`<div class="md-folder-tree-backdrop" data-md-folder-tree-backdrop></div>`)
	b.WriteString(`<aside id="mdFolderTree" class="md-folder-tree" data-md-folder-tree>`)
	b.WriteString(`<div class="md-folder-tree-head">文档目录</div>`)
	b.WriteString(`<nav aria-label="文档目录"><ul class="md-folder-tree-list">`)
	renderMarkdownFolderTreeNode(&b, root, currentRel, slug)
	b.WriteString(`</ul></nav></aside>`)
	return b.String()
}

func addMarkdownFolderTreeFile(root *markdownFolderTreeNode, file markdownFolderTreeFile) {
	parts := strings.Split(file.Path, "/")
	node := root
	for _, part := range parts[:len(parts)-1] {
		if node.Folders == nil {
			node.Folders = map[string]*markdownFolderTreeNode{}
		}
		child := node.Folders[part]
		if child == nil {
			child = &markdownFolderTreeNode{Folders: map[string]*markdownFolderTreeNode{}}
			node.Folders[part] = child
		}
		node = child
	}
	node.Files = append(node.Files, file)
}

func renderMarkdownFolderTreeNode(b *strings.Builder, node *markdownFolderTreeNode, currentRel, slug string) {
	folderNames := make([]string, 0, len(node.Folders))
	for name := range node.Folders {
		folderNames = append(folderNames, name)
	}
	sort.Strings(folderNames)
	for _, name := range folderNames {
		child := node.Folders[name]
		b.WriteString(`<li class="md-folder-tree-folder"><details open><summary>`)
		b.WriteString(html.EscapeString(name))
		b.WriteString(`</summary><ul>`)
		renderMarkdownFolderTreeNode(b, child, currentRel, slug)
		b.WriteString(`</ul></details></li>`)
	}

	sort.Slice(node.Files, func(i, j int) bool {
		return markdownFolderTreeSortKey(node.Files[i].Path) < markdownFolderTreeSortKey(node.Files[j].Path)
	})
	for _, file := range node.Files {
		active := strings.EqualFold(file.Path, currentRel)
		b.WriteString(`<li class="md-folder-tree-file"><a href="/demo/`)
		b.WriteString(escapeRelativeURLPath(slug))
		b.WriteString(`/`)
		b.WriteString(escapeRelativeURLPath(file.Path))
		b.WriteString(`"`)
		if active {
			b.WriteString(` aria-current="page"`)
		}
		b.WriteString(`>`)
		b.WriteString(html.EscapeString(file.Label))
		b.WriteString(`</a></li>`)
	}
}

func injectBeforeClosingTag(page, tag, value string) string {
	index := strings.LastIndex(strings.ToLower(page), strings.ToLower(tag))
	if index < 0 {
		return page + value
	}
	return page[:index] + value + page[index:]
}

func injectAfterOpeningBody(page, value string) string {
	lower := strings.ToLower(page)
	index := strings.Index(lower, "<body")
	if index < 0 {
		return value + page
	}
	closeIndex := strings.Index(page[index:], ">")
	if closeIndex < 0 {
		return value + page
	}
	closeIndex += index
	bodyTag := page[index : closeIndex+1]
	if !strings.Contains(strings.ToLower(bodyTag), "md-folder-page") {
		if strings.Contains(strings.ToLower(bodyTag), "class=") {
			bodyTag = strings.Replace(bodyTag, `class="`, `class="md-folder-page `, 1)
		} else {
			bodyTag = strings.TrimSuffix(bodyTag, ">") + ` class="md-folder-page">`
		}
	}
	return page[:index] + bodyTag + value + page[closeIndex+1:]
}

func markdownFolderTreeCSS() string {
	return `

    body.md-folder-page {
      display: grid;
      grid-template-columns: minmax(230px, 300px) minmax(0, 1fr);
      align-items: start;
    }

    body.md-folder-page > main {
      grid-column: 2;
      width: min(920px, calc(100vw - 3rem));
    }

    .md-folder-tree {
      grid-column: 1;
      position: sticky;
      top: 0;
      height: 100vh;
      overflow: auto;
      padding: 1rem 0.85rem 1.5rem;
      border-right: 1px solid var(--go-site-border);
      background: var(--go-site-surface);
      color: var(--go-site-text);
    }

    .md-folder-tree-head {
      margin: 0.25rem 0.45rem 0.75rem;
      color: var(--go-site-muted);
      font-size: 0.82rem;
      font-weight: 700;
    }

    .md-folder-tree ul {
      list-style: none;
      margin: 0;
      padding: 0;
    }

    .md-folder-tree li {
      margin: 0;
      padding: 0;
    }

    .md-folder-tree details {
      margin: 0.15rem 0;
    }

    .md-folder-tree summary {
      min-height: 2rem;
      display: flex;
      align-items: center;
      padding: 0 0.45rem;
      border-radius: 8px;
      color: var(--go-site-muted);
      cursor: pointer;
      font-size: 0.84rem;
      font-weight: 700;
    }

    .md-folder-tree summary:hover,
    .md-folder-tree a:hover {
      background: var(--go-site-surface-hover);
      color: var(--go-site-primary-hover);
    }

    .md-folder-tree details > ul {
      padding-left: 0.65rem;
      border-left: 1px solid var(--go-site-border-soft);
      margin-left: 0.45rem;
    }

    .md-folder-tree a {
      min-height: 2rem;
      display: flex;
      align-items: center;
      padding: 0.2rem 0.45rem;
      border-radius: 8px;
      color: var(--go-site-text);
      font-size: 0.84rem;
      line-height: 1.3;
      text-decoration: none;
      overflow-wrap: anywhere;
    }

    .md-folder-tree a[aria-current="page"] {
      background: var(--go-site-focus);
      color: var(--go-site-primary);
      font-weight: 700;
    }

    .md-folder-tree-toggle,
    .md-folder-tree-backdrop {
      display: none;
    }

    @media (max-width: 900px) {
      body.md-folder-page {
        display: block;
      }

      body.md-folder-page > main {
        width: min(900px, calc(100vw - 1rem));
        padding-top: 4.4rem;
      }

      .md-folder-tree-toggle {
        position: fixed;
        top: 0.75rem;
        left: 0.75rem;
        z-index: 41;
        min-width: 3.6rem;
        height: 2.25rem;
        display: inline-flex;
        align-items: center;
        justify-content: center;
        padding: 0 0.85rem;
        border: 1px solid var(--go-site-border);
        border-radius: 8px;
        background: var(--go-site-surface);
        color: var(--go-site-text);
        font-size: 0.88rem;
        box-shadow: 0 2px 8px rgba(28, 30, 33, 0.12);
      }

      .md-folder-tree {
        position: fixed;
        inset: 0 auto 0 0;
        z-index: 40;
        width: min(84vw, 20rem);
        height: 100vh;
        transform: translateX(-100%);
        transition: transform 180ms ease;
        box-shadow: 8px 0 24px rgba(28, 30, 33, 0.18);
      }

      body.md-tree-open .md-folder-tree {
        transform: translateX(0);
      }

      .md-folder-tree-backdrop {
        position: fixed;
        inset: 0;
        z-index: 39;
        background: rgba(28, 30, 33, 0.28);
      }

      body.md-tree-open .md-folder-tree-backdrop {
        display: block;
      }
    }
`
}

func markdownFolderTreeScript() string {
	return `
  <script data-md-folder-tree-script>
    (() => {
      const body = document.body;
      const toggle = document.querySelector('[data-md-folder-tree-toggle]');
      const tree = document.querySelector('[data-md-folder-tree]');
      const backdrop = document.querySelector('[data-md-folder-tree-backdrop]');
      const isNarrow = () => window.matchMedia('(max-width: 900px)').matches;
      const setOpen = (open) => {
        body.classList.toggle('md-tree-open', open);
        toggle?.setAttribute('aria-expanded', String(open));
      };
      toggle?.addEventListener('click', () => setOpen(!body.classList.contains('md-tree-open')));
      backdrop?.addEventListener('click', () => setOpen(false));
      tree?.addEventListener('click', (event) => {
        const link = event.target.closest('a');
        if (link && isNarrow()) setOpen(false);
      });
      document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') setOpen(false);
      });
      window.addEventListener('resize', () => {
        if (!isNarrow()) setOpen(false);
      });
    })();
  </script>
`
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

func (a *app) refreshStoredMarkdownDemoPages() error {
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
		body, ok := extractStoredMarkdownDemoBody(oldPage)
		if !ok {
			templateTitle := "      </div>\n      <h1>" + html.EscapeString(item.Title) + "</h1>\n"
			nextPage := strings.Replace(oldPage, templateTitle, "      </div>\n", 1)
			if !strings.Contains(nextPage, `href="/favicon.svg`) {
				nextPage = strings.Replace(nextPage, "  <title>", `  <link rel="icon" type="image/svg+xml" href="/favicon.svg?v=`+markdownDemoFaviconVersion+`">
  <title>`, 1)
			}
			if nextPage != oldPage {
				if err := os.WriteFile(pagePath, []byte(nextPage), 0o644); err != nil {
					return err
				}
			}
			continue
		}
		nextPage := renderMarkdownPageHTML(item.Title, body)
		if nextPage != oldPage {
			if err := os.WriteFile(pagePath, []byte(nextPage), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractStoredMarkdownDemoBody(page string) (string, bool) {
	brandStart := strings.Index(page, `<div class="md-brand"`)
	if brandStart < 0 {
		return "", false
	}
	brandEndRel := strings.Index(page[brandStart:], "      </div>\n")
	if brandEndRel < 0 {
		return "", false
	}
	bodyStart := brandStart + brandEndRel + len("      </div>\n")
	articleEnd := strings.LastIndex(page, "\n    </article>")
	if articleEnd < bodyStart {
		return "", false
	}
	return page[bodyStart:articleEnd], true
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

type demoUploadFile struct {
	path   string
	header *multipart.FileHeader
}

func demoFolderUploadFiles(form *multipart.Form) ([]demoUploadFile, error) {
	if form == nil {
		return nil, nil
	}
	headers := form.File["files"]
	if len(headers) == 0 {
		return nil, nil
	}
	paths := form.Value["paths"]
	if len(paths) != len(headers) {
		return nil, fmt.Errorf("folder upload paths do not match files")
	}

	files := make([]demoUploadFile, 0, len(headers))
	for index, header := range headers {
		if header == nil {
			continue
		}
		relPath := strings.TrimSpace(paths[index])
		if relPath == "" {
			relPath = strings.TrimSpace(header.Filename)
		}
		if relPath == "" {
			continue
		}
		files = append(files, demoUploadFile{path: relPath, header: header})
	}
	return files, nil
}

func demoFolderUploadTitle(sourceName string, files []demoUploadFile) string {
	sourceName = strings.TrimSpace(filepath.Base(filepath.ToSlash(sourceName)))
	if sourceName != "" && sourceName != "." && sourceName != "/" {
		return strings.TrimSuffix(sourceName, filepath.Ext(sourceName))
	}

	prefix := uploadSingleRootPrefix(files)
	if prefix != "" {
		root := strings.TrimSuffix(prefix, "/")
		if root != "" {
			return root
		}
	}
	if len(files) > 0 {
		name := filepath.Base(filepath.ToSlash(files[0].path))
		return strings.TrimSuffix(name, filepath.Ext(name))
	}
	return "文件夹演示"
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

func materializeFolderUpload(files []demoUploadFile, targetDir, title string) (string, error) {
	entries, err := normalizedDemoUploadFiles(files)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("folder does not contain supported files")
	}

	hasIndex := false
	hasHTML := false
	hasMarkdown := false
	htmlEntries := []string{}
	for _, entry := range entries {
		ext := strings.ToLower(path.Ext(entry.path))
		switch ext {
		case ".html", ".htm":
			hasHTML = true
			htmlEntries = append(htmlEntries, entry.path)
			if entry.path == "index.html" {
				hasIndex = true
			}
		case ".md", ".markdown":
			hasMarkdown = true
		}
	}

	if hasHTML {
		if err := copyDemoUploadFiles(entries, targetDir); err != nil {
			return "", err
		}
		if !hasIndex {
			if len(htmlEntries) != 1 {
				return "", fmt.Errorf("folder must contain index.html or exactly one HTML file")
			}
			if err := writeDemoEntryRedirect(targetDir, htmlEntries[0]); err != nil {
				return "", err
			}
		}
		return "folder", nil
	}

	if hasMarkdown {
		return "markdown-folder", materializeMarkdownFolder(entries, targetDir, title)
	}

	return "", fmt.Errorf("folder must contain index.html, exactly one HTML file, or Markdown files")
}

func normalizedDemoUploadFiles(files []demoUploadFile) ([]demoUploadFile, error) {
	prefix := uploadSingleRootPrefix(files)
	entries := make([]demoUploadFile, 0, len(files))
	for _, file := range files {
		if file.header == nil {
			continue
		}
		rawName := strings.TrimSpace(file.path)
		if strings.Contains(rawName, "\\") {
			return nil, fmt.Errorf("folder contains unsafe path: %s", file.path)
		}
		name := filepath.ToSlash(rawName)
		name = strings.TrimPrefix(name, prefix)
		name = strings.TrimPrefix(name, "/")
		if name == "" || isIgnoredArchivePath(name) {
			continue
		}
		if !isSafeArchivePath(name) {
			return nil, fmt.Errorf("folder contains unsafe path: %s", file.path)
		}
		if !isAllowedStaticFile(name) {
			return nil, fmt.Errorf("folder contains unsupported file type: %s", name)
		}
		entries = append(entries, demoUploadFile{path: name, header: file.header})
	}
	return entries, nil
}

func uploadSingleRootPrefix(files []demoUploadFile) string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		name := strings.Trim(filepath.ToSlash(strings.TrimSpace(file.path)), "/")
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return singleRootPrefix(names)
}

func singleRootPrefix(names []string) string {
	root := ""
	for _, name := range names {
		name = strings.Trim(name, "/")
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

func copyDemoUploadFiles(files []demoUploadFile, targetDir string) error {
	for _, file := range files {
		target := filepath.Join(targetDir, filepath.FromSlash(file.path))
		if !isWithin(targetDir, target) {
			return fmt.Errorf("folder contains unsafe path: %s", file.path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := file.header.Open()
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
	return nil
}

func materializeMarkdownFolder(entries []demoUploadFile, targetDir, title string) error {
	pages := map[string]string{}
	contents := map[string]string{}
	assets := []demoUploadFile{}
	for _, entry := range entries {
		ext := strings.ToLower(path.Ext(entry.path))
		switch ext {
		case ".md", ".markdown":
			htmlPath := markdownFolderHTMLPath(entry.path)
			if existing := findCaseInsensitiveKeyByValue(pages, htmlPath); existing != "" {
				return fmt.Errorf("markdown files produce duplicate page path: %s and %s", existing, entry.path)
			}
			content, err := readUploadFileString(entry.header)
			if err != nil {
				return err
			}
			pages[entry.path] = htmlPath
			contents[entry.path] = content
		default:
			assets = append(assets, entry)
		}
	}
	if len(pages) == 0 {
		return fmt.Errorf("folder does not contain Markdown files")
	}
	if err := copyDemoUploadFiles(assets, targetDir); err != nil {
		return err
	}

	mdPaths := make([]string, 0, len(pages))
	for mdPath := range pages {
		mdPaths = append(mdPaths, mdPath)
	}
	sort.Strings(mdPaths)
	for _, mdPath := range mdPaths {
		content := contents[mdPath]
		pageTitle := markdownTitleFromContent(content)
		if pageTitle == "" {
			pageTitle = strings.TrimSuffix(path.Base(mdPath), path.Ext(mdPath))
		}
		if pageTitle == "" {
			pageTitle = title
		}
		body := rewriteMarkdownFolderLinks(renderMarkdown(content), mdPath, pages)
		target := filepath.Join(targetDir, filepath.FromSlash(pages[mdPath]))
		if !isWithin(targetDir, target) {
			return fmt.Errorf("folder contains unsafe path: %s", pages[mdPath])
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(renderMarkdownPageHTML(pageTitle, body)), 0o644); err != nil {
			return err
		}
	}

	entryPath := markdownFolderEntryPath(pages)
	if entryHTML := pages[entryPath]; entryHTML != "" && entryHTML != "index.html" {
		return writeDemoEntryRedirect(targetDir, entryHTML)
	}
	return nil
}

func readUploadFileString(header *multipart.FileHeader) (string, error) {
	file, err := header.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func markdownFolderHTMLPath(mdPath string) string {
	ext := path.Ext(mdPath)
	return strings.TrimSuffix(mdPath, ext) + ".html"
}

func markdownFolderEntryPath(pages map[string]string) string {
	for _, want := range []string{"index.md", "index.markdown", "README.md", "README.markdown"} {
		if path := findCaseInsensitiveKey(pages, want); path != "" {
			return path
		}
	}

	paths := make([]string, 0, len(pages))
	for path := range pages {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func findCaseInsensitiveKey(values map[string]string, want string) string {
	for key := range values {
		if strings.EqualFold(key, want) {
			return key
		}
	}
	return ""
}

func findCaseInsensitiveKeyByValue(values map[string]string, want string) string {
	for key, value := range values {
		if strings.EqualFold(value, want) {
			return key
		}
	}
	return ""
}

func rewriteMarkdownFolderLinks(rendered, currentPath string, pages map[string]string) string {
	return markdownHrefAttributePattern.ReplaceAllStringFunc(rendered, func(attr string) string {
		match := markdownHrefAttributePattern.FindStringSubmatch(attr)
		if len(match) != 2 {
			return attr
		}
		if href, ok := markdownFolderHref(currentPath, match[1], pages); ok {
			return `href="` + html.EscapeString(href) + `"`
		}
		return attr
	})
}

func markdownFolderHref(currentPath, rawHref string, pages map[string]string) (string, bool) {
	href := html.UnescapeString(strings.TrimSpace(rawHref))
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "//") {
		return "", false
	}
	if parsed, err := url.Parse(href); err == nil && parsed.Scheme != "" {
		return "", false
	}

	pathPart, suffix := splitURLSuffix(href)
	if pathPart == "" {
		return "", false
	}
	if decoded, err := url.PathUnescape(pathPart); err == nil {
		pathPart = decoded
	}
	switch strings.ToLower(path.Ext(pathPart)) {
	case ".md", ".markdown":
	default:
		return "", false
	}

	targetPath := resolveMarkdownFolderPath(currentPath, pathPart)
	targetHTML, ok := pages[targetPath]
	if !ok {
		targetPath = findCaseInsensitiveKey(pages, targetPath)
		targetHTML, ok = pages[targetPath]
	}
	if !ok {
		return "", false
	}

	currentHTML := pages[currentPath]
	fromDir := path.Dir(currentHTML)
	if fromDir == "." {
		fromDir = ""
	}
	return relativeDemoURLPath(fromDir, targetHTML) + suffix, true
}

func splitURLSuffix(value string) (string, string) {
	index := strings.IndexAny(value, "?#")
	if index < 0 {
		return value, ""
	}
	return value[:index], value[index:]
}

func resolveMarkdownFolderPath(currentPath, targetPath string) string {
	var resolved string
	if strings.HasPrefix(targetPath, "/") {
		resolved = path.Clean(targetPath)
	} else {
		base := path.Dir(currentPath)
		if base == "." {
			base = ""
		}
		resolved = path.Clean(path.Join(base, targetPath))
	}
	if resolved == "." || resolved == "/" {
		return ""
	}
	return strings.TrimPrefix(resolved, "/")
}

func relativeDemoURLPath(fromDir, targetPath string) string {
	base := "."
	if fromDir != "" {
		base = filepath.FromSlash(fromDir)
	}
	rel, err := filepath.Rel(base, filepath.FromSlash(targetPath))
	if err != nil {
		rel = filepath.FromSlash(targetPath)
	}
	return escapeRelativeURLPath(filepath.ToSlash(rel))
}

func extractZipDemo(data []byte, targetDir string) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("invalid zip file")
	}
	entries, err := zipDemoEntries(reader.File)
	if err != nil {
		return err
	}
	prefix := zipSingleRootPrefix(entries)
	hasIndex := false
	htmlEntries := []string{}
	for _, entry := range entries {
		file := entry.file
		if file.FileInfo().IsDir() {
			continue
		}
		if file.FileInfo().Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("zip files cannot contain symbolic links")
		}

		name := strings.TrimPrefix(entry.name, prefix)
		name = strings.TrimPrefix(name, "/")
		if name == "" {
			continue
		}
		if isIgnoredArchivePath(name) {
			continue
		}
		if !isSafeArchivePath(name) {
			return fmt.Errorf("zip contains unsafe path: %s", file.Name)
		}
		if !isAllowedStaticFile(name) {
			return fmt.Errorf("zip contains unsupported file type: %s", name)
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".html" || ext == ".htm" {
			htmlEntries = append(htmlEntries, name)
		}
		if name == "index.html" {
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
		if len(htmlEntries) != 1 {
			return fmt.Errorf("zip must contain index.html or exactly one HTML file")
		}
		if err := writeDemoEntryRedirect(targetDir, htmlEntries[0]); err != nil {
			return err
		}
	}
	return nil
}

type zipDemoEntry struct {
	file *zip.File
	name string
}

func zipDemoEntries(files []*zip.File) ([]zipDemoEntry, error) {
	entries := make([]zipDemoEntry, 0, len(files))
	for _, file := range files {
		name, err := zipEntryName(file)
		if err != nil {
			return nil, err
		}
		entries = append(entries, zipDemoEntry{file: file, name: name})
	}
	return entries, nil
}

func zipEntryName(file *zip.File) (string, error) {
	name := file.Name
	if file.NonUTF8 && hasNonASCIIByte(name) {
		decoded, err := simplifiedchinese.GB18030.NewDecoder().String(name)
		if err != nil {
			return "", fmt.Errorf("zip contains filename that cannot be decoded: %s", file.Name)
		}
		name = decoded
	}
	return filepath.ToSlash(name), nil
}

func hasNonASCIIByte(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] >= 0x80 {
			return true
		}
	}
	return false
}

func zipSingleRootPrefix(entries []zipDemoEntry) string {
	root := ""
	for _, entry := range entries {
		name := strings.Trim(entry.name, "/")
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

func writeDemoEntryRedirect(targetDir, entryPath string) error {
	targetURL := "./" + escapeRelativeURLPath(entryPath)
	page := `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta http-equiv="refresh" content="0; url=` + html.EscapeString(targetURL) + `">
  <title>Opening demo</title>
</head>
<body>
  <script>window.location.replace(` + strconv.Quote(targetURL) + `);</script>
  <a href="` + html.EscapeString(targetURL) + `">Open demo</a>
</body>
</html>
`
	return os.WriteFile(filepath.Join(targetDir, "index.html"), []byte(page), 0o644)
}

func escapeRelativeURLPath(value string) string {
	segments := strings.Split(filepath.ToSlash(value), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

func isIgnoredArchivePath(name string) bool {
	for _, segment := range strings.Split(strings.Trim(filepath.ToSlash(name), "/"), "/") {
		if segment == "__MACOSX" || segment == ".DS_Store" {
			return true
		}
	}
	return false
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
	return renderMarkdownPageHTML(title, renderMarkdown(source))
}

func demoThemeHeadHTML() string {
	return `
  <link rel="icon" type="image/svg+xml" href="/favicon.svg?v=` + markdownDemoFaviconVersion + `">
  <style ` + demoThemeMarker + `>
` + demoThemeCSS() + `
  </style>
`
}

func demoThemeCSS() string {
	return `
    :root {
      color-scheme: light;
      --go-site-primary: #0866FF;
      --go-site-primary-hover: #075CE5;
      --go-site-bg: #F0F2F5;
      --go-site-surface: #FFFFFF;
      --go-site-surface-soft: #F5F6F7;
      --go-site-surface-hover: #F2F3F5;
      --go-site-border: #DADDE1;
      --go-site-border-soft: #E4E6EB;
      --go-site-text: #1C1E21;
      --go-site-muted: #65676B;
      --go-site-focus: rgba(8, 102, 255, 0.16);
    }

    html {
      background: var(--go-site-bg);
    }

    body {
      background: var(--go-site-bg) !important;
      color: var(--go-site-text);
      font-family: "Avenir Next", "PingFang SC", "Microsoft YaHei", "Segoe UI", Arial, sans-serif;
    }

    a {
      color: var(--go-site-primary);
    }

    a:hover {
      color: var(--go-site-primary-hover);
    }

    button,
    input,
    select,
    textarea {
      font-family: inherit;
    }

    button,
    [role="button"],
    input[type="button"],
    input[type="submit"],
    input[type="reset"],
    .btn,
    .button {
      border-radius: 8px;
    }

    button,
    input[type="button"],
    input[type="submit"],
    input[type="reset"],
    .btn-primary,
    .button-primary {
      border-color: var(--go-site-primary);
      background-color: var(--go-site-primary);
      color: #FFFFFF;
      box-shadow: none;
    }

    button:hover,
    input[type="button"]:hover,
    input[type="submit"]:hover,
    input[type="reset"]:hover,
    .btn-primary:hover,
    .button-primary:hover {
      background-color: var(--go-site-primary-hover);
      border-color: var(--go-site-primary-hover);
    }

    input:not([type]),
    input[type="email"],
    input[type="number"],
    input[type="password"],
    input[type="search"],
    input[type="tel"],
    input[type="text"],
    input[type="url"],
    select,
    textarea {
      border-color: #CED0D4;
      background-color: var(--go-site-surface);
      color: var(--go-site-text);
    }

    input:focus,
    select:focus,
    textarea:focus,
    button:focus-visible,
    [role="button"]:focus-visible {
      border-color: var(--go-site-primary);
      box-shadow: 0 0 0 3px var(--go-site-focus);
      outline: none;
    }

    table {
      background: var(--go-site-surface);
      border-color: var(--go-site-border);
    }

    th {
      background: var(--go-site-surface-soft);
      color: var(--go-site-muted);
    }

    td,
    th {
      border-color: var(--go-site-border-soft);
    }

    tr:hover td {
      background: var(--go-site-surface-hover);
    }

    ::selection {
      background: rgba(8, 102, 255, 0.18);
      color: var(--go-site-text);
    }
`
}

func markdownDemoCSS() string {
	return `
    body {
      margin: 0;
      min-height: 100vh;
      line-height: 1.72;
    }

    main {
      width: min(900px, calc(100vw - 2rem));
      margin: 0 auto;
      padding: 2rem 0 4rem;
    }

    .md-brand {
      display: flex;
      align-items: center;
      gap: 0.875rem;
      margin-bottom: 1rem;
      color: var(--go-site-muted);
    }

    .md-brand-seal {
      width: 2.75rem;
      height: 2.75rem;
      flex-shrink: 0;
      background: transparent url("/favicon.svg?v=` + markdownDemoFaviconVersion + `") center / contain no-repeat;
    }

    .md-brand-text {
      color: var(--go-site-text);
      font-size: 1rem;
      font-weight: 700;
      letter-spacing: 0;
    }
    article {
      padding: 2rem;
      border: 1px solid var(--go-site-border);
      border-radius: 8px;
      background: var(--go-site-surface);
      box-shadow: 0 1px 2px rgba(28, 30, 33, 0.1);
    }

    h1,
    h2,
    h3 {
      color: var(--go-site-text);
      line-height: 1.25;
    }

    h1 {
      margin-top: 0;
      font-size: clamp(1.9rem, 5vw, 3rem);
    }

    h2 {
      margin-top: 2rem;
      padding-top: 0.5rem;
      border-top: 1px solid var(--go-site-border-soft);
    }

    code {
      padding: 0.12rem 0.35rem;
      border-radius: 6px;
      background: var(--go-site-bg);
    }

    pre {
      overflow-x: auto;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      padding: 0.9rem 1rem;
      border: 1px solid var(--go-site-border);
      border-left: 3px solid var(--go-site-primary);
      border-radius: 8px;
      background: var(--go-site-surface-soft);
      color: var(--go-site-text);
      font-family: "Avenir Next", "PingFang SC", "Microsoft YaHei", "Segoe UI", Arial, sans-serif;
      line-height: 1.72;
    }

    pre code {
      display: block;
      padding: 0;
      border-radius: 0;
      background: transparent;
      color: inherit;
      font: inherit;
      white-space: inherit;
    }

    blockquote {
      margin: 1rem 0;
      padding: 0.4rem 1rem;
      border-left: 3px solid var(--go-site-primary);
      color: var(--go-site-muted);
      background: var(--go-site-bg);
    }

    @media (max-width: 640px) {
      main {
        width: min(100vw - 1rem, 900px);
        padding: 1rem 0 2rem;
      }

      article {
        padding: 1.1rem;
      }
    }
`
}

func renderMarkdownPageHTML(title, body string) string {
	return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + html.EscapeString(title) + `</title>
  <link rel="icon" type="image/svg+xml" href="/favicon.svg?v=` + markdownDemoFaviconVersion + `">
  <style ` + demoThemeMarker + `>
` + demoThemeCSS() + markdownDemoCSS() + `
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
