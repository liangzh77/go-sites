package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// newTestService builds a service with the real templates and a temporary
// database, and optionally a fake model endpoint.
func newTestService(t *testing.T, ai *aiClient) *service {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.close() })
	if ai == nil {
		ai = &aiClient{model: defaultModel, http: http.DefaultClient}
	}
	svc := &service{st: st, ai: ai, basePath: defaultBasePath}
	if err := svc.loadTemplates(); err != nil {
		t.Fatalf("load templates: %v", err)
	}
	return svc
}

func do(t *testing.T, svc *service, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not JSON (%d): %s", rec.Code, rec.Body.String())
	}
	return out
}

func TestIndexIsServed(t *testing.T) {
	svc := newTestService(t, nil)
	rec := do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath+"/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "School AI Demo") {
		t.Fatal("page does not name itself")
	}
	if !strings.Contains(body, "app.js") || !strings.Contains(body, "app.css") {
		t.Fatal("page does not load its assets")
	}
}

func TestAssetsAreServed(t *testing.T) {
	svc := newTestService(t, nil)
	for _, name := range []string{"app.css", "app.js"} {
		rec := do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath+"/assets/"+name, nil))
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Errorf("%s: status %d, %d bytes", name, rec.Code, rec.Body.Len())
		}
	}
}

func TestBasePathRedirectsToTrailingSlash(t *testing.T) {
	svc := newTestService(t, nil)
	rec := do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath, nil))
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != defaultBasePath+"/" {
		t.Fatalf("location = %q", loc)
	}
}

func TestStateListsBothTemplates(t *testing.T) {
	svc := newTestService(t, nil)
	rec := do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath+"/api/state", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeBody(t, rec)
	templates, _ := out["templates"].([]any)
	if len(templates) != 2 {
		t.Fatalf("templates = %d, want 2", len(templates))
	}
	ai, _ := out["ai"].(map[string]any)
	if ai["configured"] != false {
		t.Fatalf("ai = %v, want configured false without a key", ai)
	}
	if ai["keyEnv"] != deepseekKeyEnv {
		t.Fatalf("keyEnv = %v", ai["keyEnv"])
	}
}

func TestTemplateDetailCarriesBoundHTML(t *testing.T) {
	svc := newTestService(t, nil)
	rec := do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath+"/api/templates/enrollment", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeBody(t, rec)
	html, _ := out["html"].(string)
	if !strings.Contains(html, `data-field-id="f01"`) {
		t.Fatal("html has no bound field")
	}
	fields, _ := out["fields"].([]any)
	if len(fields) != 51 {
		t.Fatalf("fields = %d", len(fields))
	}
}

func TestUnknownTemplateIs404(t *testing.T) {
	svc := newTestService(t, nil)
	rec := do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath+"/api/templates/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if out := decodeBody(t, rec); out["error"] == "" || out["error"] == nil {
		t.Fatalf("no error message: %v", out)
	}
}

func TestFillWithoutKeyIsAClear503NotACrash(t *testing.T) {
	svc := newTestService(t, nil) // no API key
	body := strings.NewReader(`{"template":"enrollment","text":"张三，男"}`)
	req := httptest.NewRequest(http.MethodPost, defaultBasePath+"/api/fill", body)
	req.Header.Set("Content-Type", "application/json")
	rec := do(t, svc, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	out := decodeBody(t, rec)
	if out["code"] != "no_key" {
		t.Fatalf("code = %v", out["code"])
	}
	msg, _ := out["error"].(string)
	if !strings.Contains(msg, deepseekKeyEnv) {
		t.Fatalf("the message should name the env var: %q", msg)
	}
	if !strings.Contains(msg, "API Key") {
		t.Fatalf("the message should be in the user's words: %q", msg)
	}
}

func TestFillRejectsEmptyMaterial(t *testing.T) {
	svc := newTestService(t, &aiClient{apiKey: "test", endpoint: "http://127.0.0.1:1", model: "m", http: http.DefaultClient})
	body := strings.NewReader(`{"template":"enrollment","text":"   "}`)
	req := httptest.NewRequest(http.MethodPost, defaultBasePath+"/api/fill", body)
	rec := do(t, svc, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFillEndToEndAgainstAFakeModel(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var in struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if len(in.Messages) != 2 {
			t.Errorf("messages = %d", len(in.Messages))
		}
		prompt := in.Messages[1].Content
		for _, want := range []string{"f01", "家庭成员1姓名", "家庭成员4手机号码2", "原始资料"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("prompt is missing %q", want)
			}
		}
		// The model answers with one real field, one invented field and one
		// empty value; only the real, non-empty one should survive.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"content": `{"fields":{"f01":"张三","f99":"编造","f04":""}}`},
			}},
		})
	}))
	defer fake.Close()

	svc := newTestService(t, &aiClient{apiKey: "test-key", endpoint: fake.URL, model: "fake", http: http.DefaultClient})
	body := strings.NewReader(`{"template":"enrollment","text":"姓名：张三"}`)
	req := httptest.NewRequest(http.MethodPost, defaultBasePath+"/api/fill", body)
	rec := do(t, svc, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeBody(t, rec)
	sess, _ := out["session"].(map[string]any)
	values, _ := sess["values"].(map[string]any)
	if len(values) != 1 || values["f01"] != "张三" {
		t.Fatalf("values = %v", values)
	}
	if out["filled"] != float64(1) {
		t.Fatalf("filled = %v", out["filled"])
	}

	id, _ := sess["id"].(string)

	// The stored session can be read back and exported.
	rec = do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath+"/api/sessions/"+id, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("session status = %d", rec.Code)
	}
	rec = do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath+"/api/sessions/"+id+"/xlsx", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("content type = %q", ct)
	}
	if rec.Body.Len() < 1000 {
		t.Fatalf("xlsx is only %d bytes", rec.Body.Len())
	}
	sheet, err := readXLSX(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("export is not a readable workbook: %v", err)
	}
	var sawName bool
	for _, row := range sheet.Rows {
		if len(row) >= 3 && row[1] == "登记姓名" && row[2] == "张三" {
			sawName = true
		}
		// Blank fields stay blank in the export rather than being dropped, so
		// the downloaded sheet shows what is still missing.
		if len(row) >= 2 && row[1] == "姓名" && row[2] != "" {
			t.Errorf("an unfilled field got a value: %v", row)
		}
	}
	if !sawName {
		t.Fatalf("export does not contain the filled value: %v", sheet.Rows)
	}
}

func TestFillHandlesModelFailureWithout500(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer fake.Close()

	svc := newTestService(t, &aiClient{apiKey: "bad", endpoint: fake.URL, model: "fake", http: http.DefaultClient})
	req := httptest.NewRequest(http.MethodPost, defaultBasePath+"/api/fill",
		strings.NewReader(`{"template":"enrollment","text":"姓名：张三"}`))
	rec := do(t, svc, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	out := decodeBody(t, rec)
	msg, _ := out["error"].(string)
	if !strings.Contains(msg, "invalid api key") {
		t.Fatalf("the upstream reason should be surfaced: %q", msg)
	}
	if out["code"] != "ai_failed" {
		t.Fatalf("code = %v", out["code"])
	}
}

func TestFillAcceptsAnUploadedWorkbook(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if !strings.Contains(in.Messages[1].Content, "姓名: 张三") {
			t.Errorf("the workbook text did not reach the model: %s", in.Messages[1].Content)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"content": `{"fields":{"f04":"张三"}}`},
			}},
		})
	}))
	defer fake.Close()

	svc := newTestService(t, &aiClient{apiKey: "k", endpoint: fake.URL, model: "fake", http: http.DefaultClient})

	book, err := writeXLSX([][]string{
		{"姓名", "性别", "出生年月"},
		{"张三", "男", "2019年3月"},
	})
	if err != nil {
		t.Fatalf("build workbook: %v", err)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("template", "enrollment")
	part, err := mw.CreateFormFile("file", "学生.xlsx")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	_, _ = part.Write(book)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, defaultBasePath+"/api/fill", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := do(t, svc, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeBody(t, rec)
	sess, _ := out["session"].(map[string]any)
	values, _ := sess["values"].(map[string]any)
	if values["f04"] != "张三" {
		t.Fatalf("values = %v", values)
	}
	if sess["inputKind"] != "xlsx" || sess["inputName"] != "学生.xlsx" {
		t.Fatalf("session = %v", sess)
	}
}

func TestFillRejectsAnUnsupportedFileType(t *testing.T) {
	svc := newTestService(t, &aiClient{apiKey: "k", endpoint: "http://127.0.0.1:1", model: "m", http: http.DefaultClient})
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("template", "enrollment")
	part, _ := mw.CreateFormFile("file", "old.xls")
	_, _ = part.Write([]byte("not really excel"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, defaultBasePath+"/api/fill", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := do(t, svc, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if msg, _ := decodeBody(t, rec)["error"].(string); !strings.Contains(msg, ".xlsx") {
		t.Fatalf("the error should say what is accepted: %q", msg)
	}
}

func TestSaveSessionRejectsBadJSON(t *testing.T) {
	svc := newTestService(t, nil)
	req := httptest.NewRequest(http.MethodPatch, defaultBasePath+"/api/sessions/x", strings.NewReader("{"))
	rec := do(t, svc, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestUnknownPathUnderBaseIsTheAppShell(t *testing.T) {
	svc := newTestService(t, nil)
	// A deep link someone bookmarked should still load the page.
	rec := do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath+"/enrollment", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "School AI Demo") {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHealthReportsReadiness(t *testing.T) {
	svc := newTestService(t, nil)
	rec := do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath+"/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	out := decodeBody(t, rec)
	if out["ok"] != true || out["aiReady"] != false {
		t.Fatalf("health = %v", out)
	}
}

func TestRelativeAssetsAreServed(t *testing.T) {
	svc := newTestService(t, nil)
	// index.html links app.css relatively, so that exact path must work, not
	// only the /assets/ one.
	for _, path := range []string{"/app.css", "/app.js", "/assets/app.css"} {
		rec := do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath+path, nil))
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Errorf("%s: status %d, %d bytes", path, rec.Code, rec.Body.Len())
		}
	}
}

func TestTemplateOrderPutsTheEnrollmentFormFirst(t *testing.T) {
	svc := newTestService(t, nil)
	templates, err := svc.st.templates()
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	if len(templates) != 2 || templates[0].Slug != "enrollment" {
		t.Fatalf("order = %v", templates)
	}
}
