package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const validMathJSON = `{"summary":"可识别两题，无评分依据，不给总分。","rows":[{"question":"1","studentAnswer":"-7","verdict":"正确","feedback":"(-3.5)×2=-7，正确。"},{"question":"2","studentAnswer":"看不清","verdict":"待确认","feedback":"请上传更清晰的符号位置。"}],"notice":"请老师复核。"}`

func mathImage(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func mathRequest(t *testing.T, s *service, images [][]byte, consent, text, origin string) *httptest.ResponseRecorder {
	return mathRequestWithPrompt(t, s, images, consent, text, origin, "")
}
func mathRequestWithPrompt(t *testing.T, s *service, images [][]byte, consent, text, origin, prompt string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	form := multipart.NewWriter(&b)
	_ = form.WriteField("consent", consent)
	_ = form.WriteField("text", text)
	if prompt != "" {
		_ = form.WriteField("prompt", prompt)
	}
	for _, data := range images {
		f, err := form.CreateFormFile("image", "math.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write(data)
	}
	_ = form.Close()
	r := httptest.NewRequest("POST", mathPrefix, &b)
	r.Header.Set("Content-Type", form.FormDataContentType())
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	return w
}
func TestMathReviewUsesExistingModelAndSharedQuota(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			Messages  []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != "test-model" || payload.MaxTokens != 16384 {
			t.Error(payload.Model, payload.MaxTokens)
		}
		if !bytes.Contains(raw, []byte(mathPrompt)) || !bytes.Contains(raw, []byte("data:image/png;base64,")) || bytes.Contains(raw, []byte("小学语文")) {
			t.Error("math prompt or image not isolated")
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth/session")
		}
		completion(w, validMathJSON)
	})
	if err := s.claim(s.day()); err != nil {
		t.Fatal(err)
	} // Existing essay usage must remain counted.
	w := mathRequest(t, s, [][]byte{mathImage(t)}, "yes", "", "https://liangz77.cn")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Result    mathResult `json:"result"`
		Remaining int        `json:"remaining"`
		Scope     string     `json:"quotaScope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Result.Rows) != 2 || out.Result.Rows[1].Verdict != "待确认" || out.Remaining != 48 || out.Scope != "essay-and-math" {
		t.Fatal(out)
	}
	if n, _ := s.used(); n != 2 {
		t.Fatal(n)
	}
	for _, p := range []string{prefix + "/status", mathPrefix + "/status"} {
		status := httptest.NewRecorder()
		s.routes().ServeHTTP(status, httptest.NewRequest("GET", p, nil))
		if status.Code != 200 || !strings.Contains(status.Body.String(), `"remaining":48`) {
			t.Fatal(status.Body.String())
		}
	}
}
func TestMathInputFailuresDoNotSpendQuota(t *testing.T) {
	img := mathImage(t)
	cases := []struct {
		name                  string
		images                [][]byte
		consent, text, origin string
		code                  int
	}{
		{"missing image", nil, "yes", "", "", 400},
		{"multiple images", [][]byte{img, img}, "yes", "", "", 400}, {"spoofed mime", [][]byte{[]byte("not an image")}, "yes", "", "", 400},
		{"too large", [][]byte{make([]byte, maxImage+1)}, "yes", "", "", 400}, {"unexpected text", [][]byte{img}, "yes", "extra text", "", 400}, {"prompt too long", [][]byte{img}, "yes", "", "", 400},
		{"foreign origin", [][]byte{img}, "yes", "", "https://evil.invalid", 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected model call") })
			prompt := ""
			if tc.name == "prompt too long" {
				prompt = strings.Repeat("字", 2001)
			}
			w := mathRequestWithPrompt(t, s, tc.images, tc.consent, tc.text, tc.origin, prompt)
			if w.Code != tc.code {
				t.Fatal(w.Code, w.Body.String())
			}
			if n, _ := s.used(); n != 0 {
				t.Fatal(n)
			}
		})
	}
}
func TestMathCustomPromptAndNoConsentRequired(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !bytes.Contains(raw, []byte("作为初二数学老师，只批改方程题")) {
			t.Error("custom prompt not forwarded")
		}
		if !bytes.Contains(raw, []byte("rows")) || !bytes.Contains(raw, []byte("待确认")) {
			t.Error("fixed output guardrails missing")
		}
		completion(w, validMathJSON)
	})
	if w := mathRequestWithPrompt(t, s, [][]byte{mathImage(t)}, "", "", "", "作为初二数学老师，只批改方程题"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	s2 := setup(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !bytes.Contains(raw, []byte(mathPrompt)) {
			t.Error("default prompt not applied")
		}
		completion(w, validMathJSON)
	})
	if w := mathRequest(t, s2, [][]byte{mathImage(t)}, "", "", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestMathParsing(t *testing.T) {
	for _, raw := range []string{validMathJSON, "```json\n" + validMathJSON + "\n```", `{"summary":"无法辨认试卷，请重新拍照。","rows":[],"notice":"图片过于模糊。"}`} {
		if _, err := parseMathResult(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"not json", `{"summary":"missing rows"}`, `{"summary":"null rows","rows":null}`, `{"summary":"x","rows":[{"question":"1","verdict":"100分","feedback":"x"}]}`, `{"summary":"","rows":[]}`, strings.Replace(validMathJSON, "待确认", "已通过", 1), strings.Replace(validMathJSON, "请老师复核。", strings.Repeat("字", 1001), 1)} {
		if _, err := parseMathResult(raw); err == nil {
			t.Fatal("accepted invalid output", raw[:min(80, len(raw))])
		}
	}
	var huge mathResult
	huge.Summary = "too many"
	for i := 0; i < 61; i++ {
		huge.Rows = append(huge.Rows, mathRow{Question: "1", Verdict: "正确", Feedback: "x"})
	}
	raw, _ := json.Marshal(huge)
	if _, err := parseMathResult(string(raw)); err == nil {
		t.Fatal("unbounded rows")
	}
	// Unknown model-provided fields are not forwarded as authoritative scores or HTML.
	result, err := parseMathResult(strings.Replace(validMathJSON, `"notice":`, `"totalScore":100,"html":"<script>x</script>","notice":`, 1))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if bytes.Contains(encoded, []byte("totalScore")) || bytes.Contains(encoded, []byte("<script>")) {
		t.Fatal(string(encoded))
	}
}
func TestMathFailureRefundAndUnsupportedImage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		expected int
	}{
		{"malformed JSON", 200, "not valid output", 502}, {"no vision", 400, "image not supported: secret-key", 422}, {"model busy", 429, "secret-key", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := setup(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.status == 200 {
					completion(w, tc.body)
				} else {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				}
			})
			w := mathRequest(t, s, [][]byte{mathImage(t)}, "yes", "", "")
			if w.Code != tc.expected || strings.Contains(w.Body.String(), "secret-key") {
				t.Fatal(w.Code, w.Body.String())
			}
			if n, _ := s.used(); n != 0 {
				t.Fatal(n)
			}
		})
	}
}
func TestMathAndEssayShareAdmissionLimits(t *testing.T) {
	s := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected model call") })
	s.daily = 1
	_ = s.claim(s.day())
	w := mathRequest(t, s, [][]byte{mathImage(t)}, "yes", "", "")
	if w.Code != 429 || !strings.Contains(w.Body.String(), "DAILY_LIMIT") {
		t.Fatal(w.Code, w.Body.String())
	}
	s.refund(s.day())
	s.slots <- struct{}{}
	s.slots <- struct{}{}
	w = mathRequest(t, s, [][]byte{mathImage(t)}, "yes", "", "")
	if w.Code != 429 || !strings.Contains(w.Body.String(), "BUSY") {
		t.Fatal(w.Code, w.Body.String())
	}
	if n, _ := s.used(); n != 0 {
		t.Fatal(n)
	}
}
