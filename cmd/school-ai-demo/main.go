// Command school-ai-demo fills a school form from material the user brings.
//
// The point of the service is that it does not know any form's schema. A
// template's fillable slots are read out of its HTML (aria-label plus the text
// the form itself puts around the slot), and a language model maps the user's
// Excel rows or pasted text onto those slots by meaning. Fields the model
// cannot ground in the material stay empty: nothing is invented.
//
// Endpoints, all under SCHOOL_AI_DEMO_BASE_PATH (default /school-ai-demo):
//
//	GET   /                          the demo list and the fill screen
//	GET   /api/state                 templates, recent fills, whether AI is configured
//	GET   /api/templates/{slug}      one template with its annotated HTML and fields
//	POST  /api/fill                  material + template -> a session of filled values
//	GET   /api/sessions/{id}         one session
//	PATCH /api/sessions/{id}         save the values the user edited by hand
//	GET   /api/sessions/{id}/xlsx    download the filled form as a spreadsheet
//	GET   /health                    liveness
package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddr     = "127.0.0.1:9025"
	defaultBasePath = "/school-ai-demo"
	maxUploadBytes  = 8 << 20
	maxPasteRunes   = 60000
	maxRecentFills  = 12
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed web
var webFS embed.FS

// service holds everything the handlers need.
type service struct {
	st       *store
	ai       *aiClient
	basePath string
	now      func() time.Time
	// dailyLimit caps paid model calls per Shanghai calendar day; 0 means
	// unlimited. It is a field so tests can set it without the environment.
	dailyLimit int
}

func main() {
	basePath := strings.TrimRight(envOr("SCHOOL_AI_DEMO_BASE_PATH", defaultBasePath), "/")
	if basePath == "" {
		basePath = defaultBasePath
	}
	dataDir := envOr("SCHOOL_AI_DEMO_DATA_DIR", "./school-ai-demo-data")

	st, err := openStore(filepath.Join(dataDir, "school-ai-demo.db"))
	if err != nil {
		log.Fatalf("cannot open store: %v", err)
	}
	defer st.close()

	svc := &service{
		st:         st,
		ai:         newAIClientFromEnv(),
		basePath:   basePath,
		now:        time.Now,
		dailyLimit: dailyLimitFromEnv(),
	}
	if err := svc.loadTemplates(); err != nil {
		log.Fatalf("cannot load templates: %v", err)
	}
	if !svc.ai.configured() {
		log.Printf("warning: %s is not set, fills will return a clear error until it is", deepseekKeyEnv)
	}

	addr := envOr("SCHOOL_AI_DEMO_ADDR", defaultAddr)
	srv := &http.Server{
		Addr:              addr,
		Handler:           secureHeaders(svc.routes()),
		ReadHeaderTimeout: 10 * time.Second,
		// A fill waits on the model, so the write budget has to cover a slow
		// upstream call rather than a typical page render.
		WriteTimeout: 150 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	limitText := "unlimited"
	if svc.dailyLimit > 0 {
		limitText = strconv.Itoa(svc.dailyLimit) + "/day"
	}
	log.Printf("school-ai-demo listening on %s (base path %s, model %s, key configured %t, ai fills %s)",
		addr, basePath, svc.ai.model, svc.ai.configured(), limitText)
	log.Fatal(srv.ListenAndServe())
}

// loadTemplates parses every bundled template and upserts it into SQLite. Doing
// it on boot means a template edit ships with the binary and needs no manual
// migration step.
func (s *service) loadTemplates() error {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		return err
	}
	// The demo list shows them in this order: the enrollment form is the one
	// the request was about, so it leads.
	order := []string{"enrollment", "collection"}
	position := func(slug string) int {
		for i, name := range order {
			if name == slug {
				return i
			}
		}
		return len(order)
	}
	sort.Slice(entries, func(i, j int) bool {
		return position(strings.TrimSuffix(entries[i].Name(), ".html")) <
			position(strings.TrimSuffix(entries[j].Name(), ".html"))
	})

	loaded := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		raw, err := templateFS.ReadFile("templates/" + e.Name())
		if err != nil {
			return err
		}
		tpl, err := parseTemplateHTML(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		slug := strings.TrimSuffix(e.Name(), ".html")
		rec, err := s.st.saveTemplate(tpl, slug, e.Name(), position(slug))
		if err != nil {
			return err
		}
		log.Printf("template %-12s %d fields  %s", slug, len(rec.Fields), rec.Name)
		loaded++
	}
	if loaded == 0 {
		return errors.New("no templates found")
	}
	return nil
}

func (s *service) routes() http.Handler {
	p := s.basePath
	mux := http.NewServeMux()

	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("embedded web assets: %v", err)
	}
	mux.HandleFunc("GET "+p+"/health", func(w http.ResponseWriter, r *http.Request) {
		q, _ := s.st.quota(s.clock(), s.dailyLimit)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"aiReady":  s.ai.configured(),
			"model":    s.ai.model,
			"time":     s.clock().UTC().Format(time.RFC3339),
			"basePath": p,
			"quota":    q,
		})
	})
	mux.HandleFunc("GET "+p+"/api/state", s.handleState)
	mux.HandleFunc("GET "+p+"/api/templates/{slug}", s.handleTemplate)
	mux.HandleFunc("POST "+p+"/api/fill", s.handleFill)
	mux.HandleFunc("POST "+p+"/api/sessions", s.handleCreateSession)
	mux.HandleFunc("GET "+p+"/api/sessions/{id}", s.handleSession)
	mux.HandleFunc("PATCH "+p+"/api/sessions/{id}", s.handleSaveSession)
	mux.HandleFunc("GET "+p+"/api/sessions/{id}/xlsx", s.handleExportXLSX)

	// The page is small and its assets are linked relatively (app.css, app.js),
	// so both the relative form and the /assets/ prefix have to reach the same
	// embedded files.
	fileServer := http.StripPrefix(p+"/", http.FileServer(http.FS(webRoot)))
	mux.Handle("GET "+p+"/assets/", http.StripPrefix(p+"/assets/", http.FileServer(http.FS(webRoot))))
	mux.HandleFunc("GET "+p+"/{$}", func(w http.ResponseWriter, r *http.Request) {
		serveAsset(w, webRoot, "index.html")
	})
	mux.HandleFunc("GET "+p, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, p+"/", http.StatusMovedPermanently)
	})
	mux.Handle("GET "+p+"/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, p+"/")
		// A path with no extension is a deep link, not a file: hand it the app
		// shell so a bookmark or a refresh still loads the page.
		if name == "" || !strings.Contains(path.Base(name), ".") {
			serveAsset(w, webRoot, "index.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	}))
	return mux
}

// clock keeps the time source injectable without every handler having to
// guard against a nil func.
func (s *service) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

func serveAsset(w http.ResponseWriter, root fs.FS, name string) {
	data, err := fs.ReadFile(root, name)
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// --- handlers ---------------------------------------------------------------

type templateSummary struct {
	Slug       string  `json:"slug"`
	Name       string  `json:"name"`
	SourceFile string  `json:"sourceFile"`
	FieldCount int     `json:"fieldCount"`
	Fields     []field `json:"fields"`
}

func (s *service) handleState(w http.ResponseWriter, r *http.Request) {
	recs, err := s.st.templates()
	if err != nil {
		log.Printf("state: templates: %v", err)
		writeError(w, http.StatusInternalServerError, "读取模板失败")
		return
	}
	summaries := make([]templateSummary, 0, len(recs))
	for _, rec := range recs {
		summaries = append(summaries, templateSummary{
			Slug:       rec.Slug,
			Name:       rec.Name,
			SourceFile: rec.Source,
			FieldCount: len(rec.Fields),
			Fields:     rec.Fields,
		})
	}
	recent, err := s.st.recentSessions(maxRecentFills)
	if err != nil {
		log.Printf("state: recent: %v", err)
		recent = nil
	}
	// A quota read failure should not blank out the whole page; it only means
	// the remaining count is unknown.
	quota, err := s.st.quota(s.clock(), s.dailyLimit)
	if err != nil {
		log.Printf("state: quota: %v", err)
		quota = s.quotaView(0)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"templates": summaries,
		"recent":    recent,
		"quota":     quota,
		"ai": map[string]any{
			"configured": s.ai.configured(),
			"model":      s.ai.model,
			"keyEnv":     deepseekKeyEnv,
		},
	})
}

func (s *service) handleTemplate(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	rec, err := s.st.template(slug)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "模板不存在")
		return
	}
	if err != nil {
		log.Printf("template %s: %v", slug, err)
		writeError(w, http.StatusInternalServerError, "读取模板失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"slug":       rec.Slug,
		"name":       rec.Name,
		"sourceFile": rec.Source,
		"fields":     rec.Fields,
		"html":       rec.HTML,
	})
}

// handleFill takes the user's material (an uploaded workbook or pasted text)
// and asks the model to place it on the template's fields.
func (s *service) handleFill(w http.ResponseWriter, r *http.Request) {
	if !s.ai.configured() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"code":  "no_key",
			"error": "AI 填写还没开通：服务器上缺少 DeepSeek API Key（环境变量 " + deepseekKeyEnv + "）。请把 key 交给管理员配置后重试；你也可以先手动填写下方表格。",
		})
		return
	}

	// The same endpoint takes JSON {"text": ...} or a multipart upload, so the
	// paste box and the file picker need no separate paths.
	kind, name, material, tplSlug, errText := s.readMaterial(r)
	if errText != "" {
		writeError(w, http.StatusBadRequest, errText)
		return
	}
	tpl, err := s.st.template(tplSlug)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "模板不存在")
		return
	}
	if err != nil {
		log.Printf("fill: template %s: %v", tplSlug, err)
		writeError(w, http.StatusInternalServerError, "读取模板失败")
		return
	}
	if strings.TrimSpace(material) == "" {
		writeError(w, http.StatusBadRequest, "资料是空的：没有从上传的文件或粘贴的文字里读到内容")
		return
	}

	// The quota is claimed after the material is known to be readable, so a
	// broken upload cannot eat a slot, and before the model is called, so two
	// requests cannot both pass the check on the last remaining call.
	day := quotaDay(s.clock())
	ok, used, err := s.st.reserveQuota(day, s.dailyLimit)
	if err != nil {
		log.Printf("fill: reserve quota: %v", err)
		writeError(w, http.StatusInternalServerError, "记录今日用量失败，请稍后重试")
		return
	}
	if !ok {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"ok":        false,
			"code":      "QUOTA_EXCEEDED",
			"error":     quotaExceededMessage(s.dailyLimit),
			"quota":     s.quotaView(used),
			"remaining": 0,
		})
		return
	}
	// A call that never reached the model should not be billed against the
	// user's daily budget.
	refund := true
	defer func() {
		if refund {
			s.st.releaseQuota(day)
		}
	}()

	sess, err := s.st.createSession(tpl, kind, name, material)
	if err != nil {
		log.Printf("fill: create session: %v", err)
		writeError(w, http.StatusInternalServerError, "保存这次填写失败")
		return
	}

	res, err := s.ai.fill(r.Context(), tpl, material)
	if err != nil {
		log.Printf("fill %s: %v", sess.ID, err)
		msg := err.Error()
		if errors.Is(err, errNoAPIKey) {
			msg = "AI 填写还没开通：服务器上缺少 DeepSeek API Key"
		}
		_ = s.st.finishSession(sess.ID, map[string]string{}, nil, s.ai.model, statusFailed, msg)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":      false,
			"code":    "ai_failed",
			"error":   "AI 填写失败：" + msg,
			"session": sess.ID,
		})
		return
	}

	if err := s.st.finishSession(sess.ID, res.Values, res.Warnings, res.Model, statusFilled, ""); err != nil {
		log.Printf("fill %s: save result: %v", sess.ID, err)
		writeError(w, http.StatusInternalServerError, "保存填写结果失败")
		return
	}
	// The model answered, so this call counts against today's budget.
	refund = false
	sess, err = s.st.session(sess.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取填写结果失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"session":    sess,
		"filled":     len(sess.Values),
		"total":      len(tpl.Fields),
		"template":   tpl.Slug,
		"templateId": tpl.ID,
	})
}

// readMaterial pulls the raw text out of either upload shape.
func (s *service) readMaterial(r *http.Request) (kind, name, material, template string, errText string) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		r.Body = http.MaxBytesReader(nil, r.Body, maxUploadBytes)
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			return "", "", "", "", "上传内容太大或格式无法解析（上限 8MB）"
		}
		template = strings.TrimSpace(r.FormValue("template"))
		if template == "" {
			return "", "", "", "", "缺少 template 参数"
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			// No file part but text was pasted instead.
			if text := strings.TrimSpace(r.FormValue("text")); text != "" {
				return "text", "粘贴的文字", truncateRunes(text, maxPasteRunes), template, ""
			}
			return "", "", "", "", "没有收到文件，也没有收到文字"
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, maxUploadBytes))
		if err != nil {
			return "", "", "", "", "读取上传文件失败"
		}
		lower := strings.ToLower(header.Filename)
		switch {
		case strings.HasSuffix(lower, ".xlsx"), strings.HasSuffix(lower, ".xlsm"):
			sheet, err := readXLSX(raw)
			if err != nil {
				return "", "", "", "", "Excel 读取失败：" + err.Error()
			}
			text := sheet.text()
			if strings.TrimSpace(text) == "" {
				return "", "", "", "", "这个 Excel 里没有读到内容（第一张工作表是空的？）"
			}
			return "xlsx", header.Filename, truncateRunes(text, maxPasteRunes), template, ""
		case strings.HasSuffix(lower, ".csv"):
			return "csv", header.Filename, truncateRunes(csvToText(string(raw)), maxPasteRunes), template, ""
		case strings.HasSuffix(lower, ".txt"), strings.HasSuffix(lower, ".md"):
			return "text", header.Filename, truncateRunes(string(raw), maxPasteRunes), template, ""
		default:
			return "", "", "", "", "只支持 .xlsx / .csv / .txt 文件；旧版 .xls 请另存为 .xlsx 再上传"
		}
	}

	var in struct {
		Template string `json:"template"`
		Text     string `json:"text"`
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxUploadBytes)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return "", "", "", "", "请求格式不正确"
	}
	in.Template = strings.TrimSpace(in.Template)
	if in.Template == "" {
		return "", "", "", "", "缺少 template 参数"
	}
	if strings.TrimSpace(in.Text) == "" {
		return "", "", "", "", "请粘贴一些文字再提交"
	}
	return "text", "粘贴的文字", truncateRunes(in.Text, maxPasteRunes), in.Template, ""
}

// csvToText turns a pasted spreadsheet export into the same "第N行 表头: 值"
// shape the Excel reader produces, so the model sees one consistent format.
func csvToText(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var rows [][]string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows = append(rows, splitCSVLine(line))
		if len(rows) > maxSheetRows {
			break
		}
	}
	return sheet{Name: "csv", Rows: rows}.text()
}

func splitCSVLine(line string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			if quoted && i+1 < len(line) && line[i+1] == '"' {
				b.WriteByte('"')
				i++
				continue
			}
			quoted = !quoted
		case c == ',' && !quoted:
			out = append(out, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	out = append(out, strings.TrimSpace(b.String()))
	return out
}

// cleanValues keeps the field ids the client sent, drops blank ones, and caps
// each value so a stray paste cannot bloat the database.
// quotaView renders today's budget for the UI. used lets a failed reservation
// report the number the database actually holds.
func (s *service) quotaView(used int) quotaState {
	st := quotaState{
		Day:       quotaDay(s.clock()),
		Used:      used,
		Limit:     s.dailyLimit,
		Unlimited: s.dailyLimit <= 0,
		ResetsAt:  nextReset(s.clock()),
	}
	if s.dailyLimit > 0 {
		st.Remaining = s.dailyLimit - used
		if st.Remaining < 0 {
			st.Remaining = 0
		}
	}
	return st
}

func cleanValues(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		if v = strings.TrimSpace(v); v == "" {
			continue
		}
		out[k] = truncateRunes(v, maxValueRunes)
	}
	return out
}

// handleCreateSession opens a session for a form the user is filling in by
// hand. It needs no model, so it works before an API key is configured; the
// export endpoint then has somewhere to read the values from.
func (s *service) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Template string            `json:"template"`
		Values   map[string]string `json:"values"`
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxUploadBytes)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	slug := strings.TrimSpace(in.Template)
	if slug == "" {
		writeError(w, http.StatusBadRequest, "缺少 template 参数")
		return
	}
	tpl, err := s.st.template(slug)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "模板不存在")
		return
	}
	if err != nil {
		log.Printf("create session: template %s: %v", slug, err)
		writeError(w, http.StatusInternalServerError, "读取模板失败")
		return
	}
	values := cleanValues(in.Values)
	sess, err := s.st.createManualSession(tpl, values)
	if err != nil {
		log.Printf("create session: %v", err)
		writeError(w, http.StatusInternalServerError, "创建填写记录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session": sess, "saved": len(values)})
}

func (s *service) handleSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.st.session(r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "找不到这次填写记录")
		return
	}
	if err != nil {
		log.Printf("session: %v", err)
		writeError(w, http.StatusInternalServerError, "读取填写记录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session": sess})
}

func (s *service) handleSaveSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Values map[string]string `json:"values"`
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxUploadBytes)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	values := cleanValues(in.Values)
	sess, err := s.st.saveSession(r.PathValue("id"), values)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "找不到这次填写记录")
		return
	}
	if err != nil {
		log.Printf("save session: %v", err)
		writeError(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session": sess, "saved": len(values)})
}

// handleExportXLSX writes the filled form as one sheet: section, field, value.
// It mirrors the paper form's reading order rather than trying to rebuild the
// merged-cell layout, which is what people actually want to paste elsewhere.
func (s *service) handleExportXLSX(w http.ResponseWriter, r *http.Request) {
	sess, err := s.st.session(r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "找不到这次填写记录")
		return
	}
	if err != nil {
		log.Printf("export: %v", err)
		writeError(w, http.StatusInternalServerError, "读取填写记录失败")
		return
	}
	tpl, err := s.st.template(sess.Template)
	if err != nil {
		log.Printf("export: template %s: %v", sess.Template, err)
		writeError(w, http.StatusInternalServerError, "读取模板失败")
		return
	}

	rows := [][]string{{tpl.Name + "（AI 填写结果）"}, {"分区", "字段", "填写内容"}}
	lastSection := ""
	for _, f := range tpl.Fields {
		section := f.Section
		if section == lastSection {
			section = ""
		}
		lastSection = f.Section
		rows = append(rows, []string{section, f.Label, sess.Values[f.ID]})
	}
	data, err := writeXLSX(rows)
	if err != nil {
		log.Printf("export: build workbook: %v", err)
		writeError(w, http.StatusInternalServerError, "生成 Excel 失败")
		return
	}
	filename := fmt.Sprintf("%s-%s.xlsx", tpl.Name, sess.CreatedAt.In(shanghai()).Format("20060102-1504"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=form.xlsx; filename*=UTF-8''"+urlEscape(filename))
	_, _ = w.Write(data)
}

// --- small helpers ----------------------------------------------------------

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

func newID(prefix string) string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(buf)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
