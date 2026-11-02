package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testTemplate(t *testing.T) templateRecord {
	t.Helper()
	tpl := loadTemplate(t, "enrollment")
	return templateRecord{
		ID: "t1", Slug: "enrollment", Name: tpl.Name, Fields: tpl.Fields,
	}
}

func TestParseFillJSONKeepsOnlyRealAnswers(t *testing.T) {
	tpl := testTemplate(t)

	// What a model actually returns: good ids, a hallucinated id, an empty
	// string, the word "未知", and a null.
	content := `{"fields":{
		"f01":"张三",
		"f04":"张三",
		"f99":"编造的值",
		"f05":"未知",
		"f06":null,
		"f07":"",
		"f14":"河北省石家庄市长安区中山东路1号"
	}}`
	res, err := parseFillJSON(tpl, content, "deepseek-chat")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Values) != 3 {
		t.Fatalf("values = %v, want 3", res.Values)
	}
	if res.Values["f14"] != "河北省石家庄市长安区中山东路1号" {
		t.Fatalf("address = %q", res.Values["f14"])
	}
	if _, ok := res.Values["f99"]; ok {
		t.Fatal("a field that is not in the template was kept")
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "f99") {
		t.Fatalf("warnings = %v, want a note about the unknown id", res.Warnings)
	}
}

func TestParseFillJSONAcceptsABareObject(t *testing.T) {
	tpl := testTemplate(t)
	res, err := parseFillJSON(tpl, `{"f01":"张三"}`, "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Values["f01"] != "张三" {
		t.Fatalf("values = %v", res.Values)
	}
}

func TestParseFillJSONStripsCodeFences(t *testing.T) {
	tpl := testTemplate(t)
	res, err := parseFillJSON(tpl, "```json\n{\"f01\":\"张三\"}\n```", "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Values["f01"] != "张三" {
		t.Fatalf("values = %v", res.Values)
	}
}

func TestParseFillJSONRejectsGarbage(t *testing.T) {
	tpl := testTemplate(t)
	if _, err := parseFillJSON(tpl, "抱歉，我无法完成这个请求。", ""); err == nil {
		t.Fatal("expected an error for a non-JSON answer")
	} else if !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("error = %v, want it to mention JSON", err)
	}
}

func TestUserPromptCarriesEverythingTheModelNeeds(t *testing.T) {
	tpl := testTemplate(t)
	prompt := userPrompt(tpl, "姓名：张三，男，2019年3月出生")

	for _, want := range []string{
		"学生入学登记表",
		"f01", "登记姓名",
		"f19", "家庭成员1姓名",
		"家庭成员1手机号码2",
		"姓名：张三",
		"<<<", ">>>",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	// Sections are what tell 姓名 in the header from 姓名 in a family row.
	if !strings.Contains(prompt, "分区=家庭成员基本情况") {
		t.Error("prompt does not carry the section")
	}
}

func TestUserPromptMarksChoiceFields(t *testing.T) {
	rec := templateRecord{Name: "采集表", Fields: []field{
		{ID: "f01", Label: "是否独子", Kind: kindChoice, Options: []string{"是", "否"}},
	}}
	prompt := userPrompt(rec, "资料")
	if !strings.Contains(prompt, "类型=单选") || !strings.Contains(prompt, "是/否") {
		t.Fatalf("prompt = %s", prompt)
	}
}

func TestSystemPromptStatesTheNoFabricationRule(t *testing.T) {
	for _, want := range []string{"绝不允许编造", "按语义匹配", "留空", "按资料里的顺序逐行对应"} {
		if !strings.Contains(systemPrompt, want) {
			t.Errorf("system prompt is missing %q", want)
		}
	}
}

func TestFillWithoutKeyReturnsSentinel(t *testing.T) {
	c := &aiClient{}
	if c.configured() {
		t.Fatal("an empty key should read as not configured")
	}
	if _, err := c.fill(context.Background(), testTemplate(t), "x"); err != errNoAPIKey {
		t.Fatalf("err = %v", err)
	}
}

func TestFillRejectsAnEmptyMaterialWithoutCallingTheModel(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	c := &aiClient{apiKey: "k", endpoint: srv.URL, model: "m", http: http.DefaultClient}
	res, err := c.fill(context.Background(), testTemplate(t), "   \n  ")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(res.Values) != 0 {
		t.Fatalf("values = %v", res.Values)
	}
	if called {
		t.Fatal("an empty material should not cost a model call")
	}
}

func TestFillSendsTheJSONModeRequest(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": `{"f01":"张三"}`}}},
		})
	}))
	defer srv.Close()

	c := &aiClient{apiKey: "secret", endpoint: srv.URL, model: "deepseek-chat", http: http.DefaultClient}
	res, err := c.fill(context.Background(), testTemplate(t), "姓名：张三")
	if err != nil {
		t.Fatalf("fill: %v", err)
	}
	if res.Values["f01"] != "张三" || res.Model != "deepseek-chat" {
		t.Fatalf("res = %+v", res)
	}
	if body["model"] != "deepseek-chat" {
		t.Errorf("model = %v", body["model"])
	}
	// JSON mode plus temperature 0: this is extraction, not writing.
	rf, _ := body["response_format"].(map[string]any)
	if rf["type"] != "json_object" {
		t.Errorf("response_format = %v", body["response_format"])
	}
	if body["temperature"] != float64(0) {
		t.Errorf("temperature = %v", body["temperature"])
	}
}

func TestFillSurfacesAnUpstreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"Insufficient Balance"}}`))
	}))
	defer srv.Close()
	c := &aiClient{apiKey: "k", endpoint: srv.URL, model: "m", http: http.DefaultClient}
	_, err := c.fill(context.Background(), testTemplate(t), "姓名：张三")
	if err == nil || !strings.Contains(err.Error(), "Insufficient Balance") {
		t.Fatalf("err = %v", err)
	}
}

func TestFillSurfacesANonJSONAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "我无法完成"}}},
		})
	}))
	defer srv.Close()
	c := &aiClient{apiKey: "k", endpoint: srv.URL, model: "m", http: http.DefaultClient}
	if _, err := c.fill(context.Background(), testTemplate(t), "姓名：张三"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestNewAIClientFromEnvDefaults(t *testing.T) {
	t.Setenv(deepseekKeyEnv, "  ")
	t.Setenv(deepseekBaseEnv, "")
	t.Setenv(deepseekModelEnv, "")
	c := newAIClientFromEnv()
	if c.configured() {
		t.Fatal("a whitespace key should not count as configured")
	}
	if c.endpoint != defaultDeepseekBase+"/chat/completions" || c.model != defaultModel {
		t.Fatalf("endpoint = %q, model = %q", c.endpoint, c.model)
	}
}

func TestNewAIClientFromEnvOverrides(t *testing.T) {
	t.Setenv(deepseekKeyEnv, "k")
	t.Setenv(deepseekBaseEnv, "https://example.test/v1")
	t.Setenv(deepseekModelEnv, "deepseek-reasoner")
	c := newAIClientFromEnv()
	if !c.configured() {
		t.Fatal("should be configured")
	}
	// A base is a base: the path is appended, so the same env value works for
	// providers that do not share DeepSeek's URL.
	if c.endpoint != "https://example.test/v1/chat/completions" || c.model != "deepseek-reasoner" {
		t.Fatalf("endpoint = %q, model = %q", c.endpoint, c.model)
	}
}

func TestCompletionsURLJoinsWithoutDuplicating(t *testing.T) {
	cases := map[string]string{
		"https://api.deepseek.com":                   "https://api.deepseek.com/chat/completions",
		"https://api.deepseek.com/":                  "https://api.deepseek.com/chat/completions",
		"https://api.deepseek.com/v1":                "https://api.deepseek.com/v1/chat/completions",
		"https://opencode.ai/inference/go/openai/v1": "https://opencode.ai/inference/go/openai/v1/chat/completions",
		// A full URL pasted into the base slot must not double the path.
		"https://api.deepseek.com/chat/completions": "https://api.deepseek.com/chat/completions",
		"": defaultDeepseekBase + "/chat/completions",
	}
	for in, want := range cases {
		if got := completionsURL(in); got != want {
			t.Errorf("completionsURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOpencodeBaseNeedsASessionHeader(t *testing.T) {
	t.Setenv(deepseekKeyEnv, "k")
	t.Setenv(deepseekBaseEnv, "https://opencode.ai/inference/go/openai/v1")
	if !newAIClientFromEnv().needsSession {
		t.Fatal("an OpenCode base must send x-opencode-session")
	}
	t.Setenv(deepseekBaseEnv, "https://api.deepseek.com/v1")
	if newAIClientFromEnv().needsSession {
		t.Fatal("the official API does not want that header")
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("张三李四", 2); got != "张三…" {
		t.Fatalf("got %q", got)
	}
	if got := truncateRunes("张三", 5); got != "张三" {
		t.Fatalf("got %q", got)
	}
}
