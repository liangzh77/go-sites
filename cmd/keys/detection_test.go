package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestDetectionRequestFormatsAndAuth(t *testing.T) {
	tests := []struct {
		name       string
		protocol   string
		authMode   string
		pathSuffix string
		assertBody func(*testing.T, map[string]any)
		assertAuth func(*testing.T, http.Header)
	}{
		{
			name: "openai chat", protocol: protocolOpenAIChat, authMode: authBearer,
			pathSuffix: "/chat/completions",
			assertBody: func(t *testing.T, body map[string]any) {
				if body["max_tokens"] != float64(8) {
					t.Fatalf("max_tokens = %#v", body["max_tokens"])
				}
				if body["model"] != "test-model" {
					t.Fatalf("model = %#v", body["model"])
				}
			},
			assertAuth: func(t *testing.T, header http.Header) {
				if header.Get("Authorization") != "Bearer test-secret" {
					t.Fatalf("Authorization = %q", header.Get("Authorization"))
				}
			},
		},
		{
			name: "openai responses", protocol: protocolOpenAIResponses, authMode: authBearer,
			pathSuffix: "/responses",
			assertBody: func(t *testing.T, body map[string]any) {
				if body["max_output_tokens"] != float64(8) || body["input"] == "" {
					t.Fatalf("body = %#v", body)
				}
			},
			assertAuth: func(t *testing.T, header http.Header) {
				if header.Get("Authorization") == "" {
					t.Fatal("missing bearer auth")
				}
			},
		},
		{
			name: "anthropic", protocol: protocolAnthropic, authMode: authAnthropic,
			pathSuffix: "/messages",
			assertBody: func(t *testing.T, body map[string]any) {
				if body["max_tokens"] != float64(8) {
					t.Fatalf("body = %#v", body)
				}
			},
			assertAuth: func(t *testing.T, header http.Header) {
				if header.Get("x-api-key") != "test-secret" {
					t.Fatal("missing x-api-key")
				}
				if header.Get("anthropic-version") != "2023-06-01" {
					t.Fatal("missing anthropic-version")
				}
			},
		},
		{
			name: "gemini", protocol: protocolGemini, authMode: authGemini,
			pathSuffix: "/models/test-model:generateContent",
			assertBody: func(t *testing.T, body map[string]any) {
				config, ok := body["generationConfig"].(map[string]any)
				if !ok || config["maxOutputTokens"] != float64(8) {
					t.Fatalf("generationConfig = %#v", body["generationConfig"])
				}
			},
			assertAuth: func(t *testing.T, header http.Header) {
				if header.Get("x-goog-api-key") != "test-secret" {
					t.Fatal("missing Gemini API key header")
				}
			},
		},
		{
			name: "minimax", protocol: protocolMiniMax, authMode: authBearer,
			pathSuffix: "/chatcompletion_v2",
			assertBody: func(t *testing.T, body map[string]any) {
				if body["max_tokens"] != float64(8) {
					t.Fatalf("body = %#v", body)
				}
			},
			assertAuth: func(t *testing.T, header http.Header) {
				if header.Get("Authorization") == "" {
					t.Fatal("missing bearer auth")
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var seenPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seenPath = r.URL.Path
				test.assertAuth(t, r.Header)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				test.assertBody(t, body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			d := newDetector()
			d.client = server.Client()
			target := DetectionTarget{
				Key: APIKey{ID: "k_test", Alias: "test"},
				Provider: Provider{
					ID: "p_test", PresetID: "custom", Protocol: test.protocol,
					BaseURL: server.URL, AuthMode: test.authMode,
				},
				Model: ModelConfig{
					ModelID: "test-model", TestParams: json.RawMessage(`{}`),
				},
				Secret: "test-secret", Fingerprint: "fingerprint",
				StartedAt: time.Now().UTC(),
			}
			result := d.testTarget(context.Background(), target)
			if result.Status != statusSuccess {
				t.Fatalf("status = %s, detail = %s", result.Status, result.Detail)
			}
			if seenPath != test.pathSuffix {
				t.Fatalf("request path = %q, want %q", seenPath, test.pathSuffix)
			}
		})
	}
}

func TestDetectionErrorClassification(t *testing.T) {
	tests := []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusUnauthorized, `{"error":{"message":"bad key"}}`, statusAuthFailed},
		{http.StatusPaymentRequired, `{"error":{"message":"no credit"}}`, statusQuota},
		{http.StatusTooManyRequests, `{"error":{"message":"rate limit"}}`, statusRateLimited},
		{http.StatusTooManyRequests, `{"error":{"message":"quota exhausted"}}`, statusQuota},
		{http.StatusBadRequest, `{"error":{"message":"temperature unsupported"}}`, statusParameters},
		{http.StatusBadRequest, `{"error":{"message":"model not found"}}`, statusModelDenied},
		{http.StatusBadRequest, `{"error":{"code":"insufficient_quota","message":"billing"}}`, statusQuota},
		{http.StatusBadRequest, `{"error":{"message":"invalid API key"}}`, statusAuthFailed},
		{http.StatusBadGateway, `{"error":{"message":"upstream down"}}`, statusService},
		{http.StatusTeapot, `not-json`, statusUnknown},
	}
	for _, test := range tests {
		got, detail := classifyDetectionResponse(test.status, []byte(test.body), "super-secret-token")
		if got != test.want {
			t.Errorf("status %d body %s: got %s (%s), want %s", test.status, test.body, got, detail, test.want)
		}
		if strings.Contains(detail, "super-secret-token") {
			t.Fatalf("detail leaked secret: %s", detail)
		}
	}
}

func TestPublicIPRejectsPrivateAndReservedRanges(t *testing.T) {
	tests := map[string]bool{
		"1.1.1.1":            true,
		"2606:4700::1111":    true,
		"127.0.0.1":          false,
		"10.0.0.1":           false,
		"169.254.169.254":    false,
		"100.64.0.1":         false,
		"192.168.1.2":        false,
		"::1":                false,
		"fe80::1":            false,
		"64:ff9b::a9fe:a9fe": false,
	}
	for value, want := range tests {
		addr := netip.MustParseAddr(value)
		if got := isPublicIP(addr.AsSlice()); got != want {
			t.Errorf("isPublicIP(%s) = %v, want %v", value, got, want)
		}
	}
}

func TestModelListParserHandlesOpenAIAndGemini(t *testing.T) {
	openAI := parseModelList([]byte(`{"data":[{"id":"b"},{"id":"a"},{"id":"a"}]}`))
	if strings.Join(openAI, ",") != "a,b" {
		t.Fatalf("OpenAI models = %v", openAI)
	}
	gemini := parseModelList([]byte(`{"models":[{"name":"models/gemini-test"}]}`))
	if len(gemini) != 1 || gemini[0] != "gemini-test" {
		t.Fatalf("Gemini models = %v", gemini)
	}
}

func TestVolcengineModelParserKeepsChatModelsAndMetadata(t *testing.T) {
	payload := []byte(`{
		"data": [
			{
				"id": "doubao-chat-260101",
				"name": "doubao-chat",
				"context_window": 262144,
				"max_output_tokens": 131072,
				"api_supported": {"chat/completions": true, "responses": true},
				"effort": {"default_level": "high", "supported_levels": ["low", "high", "max"]},
				"features": {"thinking": {"types": ["enabled", "disabled"]}, "tools": {"function_calling": true}}
			},
			{
				"id": "doubao-image",
				"name": "doubao-image",
				"api_supported": {"images/generations": true}
			},
			{
				"id": "qwen-chat",
				"name": "qwen-chat",
				"token_limits": {"context_window": "131072", "max_output_token_length": 32768},
				"api_supported": {"chat/completions": true},
				"features": {"thinking": {"types": []}}
			}
		]
	}`)
	models := parseVolcengineModels(payload)
	if len(models) != 2 {
		t.Fatalf("Volcengine chat models = %+v", models)
	}
	if models[0].ID != "doubao-chat-260101" ||
		models[0].ContextWindow == nil || *models[0].ContextWindow != 262144 ||
		models[0].MaxOutput == nil || *models[0].MaxOutput != 131072 ||
		models[0].SupportsThinking == nil || !*models[0].SupportsThinking ||
		!strings.Contains(models[0].ThinkingLevel, "low high max") {
		t.Fatalf("Volcengine metadata = %+v", models[0])
	}
	if models[1].ContextWindow == nil || *models[1].ContextWindow != 131072 ||
		models[1].MaxOutput == nil || *models[1].MaxOutput != 32768 {
		t.Fatalf("Volcengine string limits = %+v", models[1])
	}
}

func TestOpenCodeGoSendsSessionHeaderForDetectionAndDiscovery(t *testing.T) {
	var paths []string
	var sessions []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		if request.URL.Hostname() == "opencode.ai" {
			sessions = append(sessions, request.Header.Get("x-opencode-session"))
		}
		body := `{"choices":[]}`
		if request.URL.Hostname() == "models.dev" {
			body = `{"opencode-go":{"models":{"deepseek-v4.1-flash":{"id":"deepseek-v4.1-flash","name":"DeepSeek V4.1 Flash","reasoning":true,"reasoning_options":[{"type":"effort","values":"low high max"}],"limit":{"context":1000000,"output":384000}},"flex-model":{"id":"flex-model","name":"Flexible Model","reasoning":"true","reasoning_options":[{"type":"effort","values":["low","high"]}],"limit":{"context":"123000","output":45000}}}}}`
		} else if strings.HasSuffix(request.URL.Path, "/models") {
			body = `{"data":[{"id":"deepseek-v4.1-flash"},{"id":"flex-model"}]}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
	d := newDetector()
	d.client = client
	provider := Provider{
		ID: "p_open_code", PresetID: "opencode-go", Protocol: protocolOpenAIChat,
		BaseURL: "https://opencode.ai/inference/go/openai/v1", AuthMode: authBearer,
	}
	target := DetectionTarget{
		Key:      APIKey{ID: "k_open_code"},
		Provider: provider,
		Model:    ModelConfig{ModelID: "deepseek-v4.1-flash", TestParams: json.RawMessage(`{}`)},
		Secret:   "oc_test", Fingerprint: "fp", StartedAt: time.Now().UTC(),
	}
	result := d.testTarget(context.Background(), target)
	if result.Status != statusSuccess {
		t.Fatalf("detection status = %s detail=%s", result.Status, result.Detail)
	}
	discovery, err := d.discoverModels(context.Background(), provider, "oc_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 || len(sessions) != 2 {
		t.Fatalf("paths=%v sessions=%v", paths, sessions)
	}
	for _, session := range sessions {
		if !strings.HasPrefix(session, "ses_") {
			t.Fatalf("OpenCode session = %q", session)
		}
	}
	if paths[0] != "/inference/go/openai/v1/chat/completions" ||
		paths[1] != "/inference/go/openai/v1/models" {
		t.Fatalf("OpenCode paths = %v", paths)
	}
	if len(discovery.Models) != 2 ||
		discovery.Models[0].ContextWindow == nil || *discovery.Models[0].ContextWindow != 1000000 ||
		discovery.Models[0].MaxOutput == nil || *discovery.Models[0].MaxOutput != 384000 {
		t.Fatalf("enriched discovery = %+v", discovery.Models)
	}
	if discovery.Models[1].ContextWindow == nil || *discovery.Models[1].ContextWindow != 123000 ||
		discovery.Models[1].MaxOutput == nil || *discovery.Models[1].MaxOutput != 45000 ||
		!strings.Contains(discovery.Models[1].ThinkingLevel, "low high") {
		t.Fatalf("flexible model discovery = %+v", discovery.Models[1])
	}
}
