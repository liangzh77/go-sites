package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestHTTPAuthenticationCSRFAndPageHeaders(t *testing.T) {
	store := testStore(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	sessionSecret := []byte(strings.Repeat("s", 32))
	auth, err := newAuthManager(hash, sessionSecret, "http://example.test", false)
	if err != nil {
		t.Fatal(err)
	}
	application, err := newApp(store, newDetector(), auth, "http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	application.detector.client = &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body := `{"choices":[]}`
			if strings.HasSuffix(request.URL.Path, "/models") {
				body = `{"data":[{"id":"discovered-model"}]}`
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    request,
			}, nil
		}),
	}
	server := httptest.NewServer(application.handler())
	defer server.Close()

	pageResponse, err := http.Get(server.URL + "/keys/")
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(pageResponse.Body)
	pageResponse.Body.Close()
	if pageResponse.StatusCode != http.StatusOK || !bytes.Contains(pageBody, []byte("API 密钥管理")) {
		t.Fatalf("page status=%d body=%s", pageResponse.StatusCode, pageBody)
	}
	if !strings.Contains(pageResponse.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("missing page CSP: %q", pageResponse.Header.Get("Content-Security-Policy"))
	}

	sessionStatus, err := http.Get(server.URL + "/keys/api/session")
	if err != nil {
		t.Fatal(err)
	}
	sessionPayload, _ := io.ReadAll(sessionStatus.Body)
	sessionStatus.Body.Close()
	if sessionStatus.StatusCode != http.StatusOK ||
		!bytes.Contains(sessionPayload, []byte(`"authenticated":false`)) {
		t.Fatalf("anonymous session status=%d body=%s", sessionStatus.StatusCode, sessionPayload)
	}

	unauthorized, err := http.Get(server.URL + "/keys/api/providers")
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized providers status = %d", unauthorized.StatusCode)
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, server.URL+"/keys/api/session", map[string]any{
		"password": "correct-password",
	}, "")
	if login.status != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.status, login.body)
	}
	var loginPayload struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login.body, &loginPayload); err != nil || loginPayload.CSRFToken == "" {
		t.Fatalf("login payload = %s, %v", login.body, err)
	}

	withoutCSRF := requestJSON(t, client, http.MethodPost, server.URL+"/keys/api/providers", map[string]any{
		"name": "Blocked", "presetId": "custom", "protocol": protocolOpenAIChat,
		"baseUrl": "https://api.example.com/v1", "authMode": authBearer,
	}, "")
	if withoutCSRF.status != http.StatusForbidden {
		t.Fatalf("provider without CSRF status = %d body=%s", withoutCSRF.status, withoutCSRF.body)
	}

	created := requestJSON(t, client, http.MethodPost, server.URL+"/keys/api/providers", map[string]any{
		"name": "Example", "presetId": "custom", "category": "测试",
		"protocol": protocolOpenAIChat, "baseUrl": "https://api.example.com/v1",
		"authMode": authBearer, "enabled": true,
	}, loginPayload.CSRFToken)
	if created.status != http.StatusCreated {
		t.Fatalf("provider create status = %d body=%s", created.status, created.body)
	}
	if strings.Contains(string(created.body), "password") || strings.Contains(string(created.body), "correct-password") {
		t.Fatalf("provider response leaked auth data: %s", created.body)
	}
	var providerPayload struct {
		Provider Provider `json:"provider"`
	}
	if err := json.Unmarshal(created.body, &providerPayload); err != nil {
		t.Fatal(err)
	}

	modelResponse := requestJSON(t, client, http.MethodPost,
		server.URL+"/keys/api/providers/"+providerPayload.Provider.ID+"/models", map[string]any{
			"modelId": "test-model", "contextWindow": 128000, "maxOutput": 8192,
			"supportsThinking": true, "thinkingLevel": "high", "thinkingBudget": nil,
			"testParams": map[string]any{"temperature": 0}, "defaultTest": true,
			"source": "manual",
		}, loginPayload.CSRFToken)
	if modelResponse.status != http.StatusCreated {
		t.Fatalf("model create status = %d body=%s", modelResponse.status, modelResponse.body)
	}
	var modelPayload struct {
		Model ModelConfig `json:"model"`
	}
	if err := json.Unmarshal(modelResponse.body, &modelPayload); err != nil {
		t.Fatal(err)
	}

	keyResponse := requestJSON(t, client, http.MethodPost,
		server.URL+"/keys/api/providers/"+providerPayload.Provider.ID+"/keys", map[string]any{
			"alias": "Primary", "secret": "sk-http-test-secret-value",
			"enabled": true, "testModel": "test-model", "purchaseDate": "2026-11-01",
			"expiresOn": "2027-11-01", "reminderDays": 30, "note": "integration",
		}, loginPayload.CSRFToken)
	if keyResponse.status != http.StatusCreated {
		t.Fatalf("key create status = %d body=%s", keyResponse.status, keyResponse.body)
	}
	var keyPayload struct {
		Key APIKey `json:"key"`
	}
	if err := json.Unmarshal(keyResponse.body, &keyPayload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(keyResponse.body), "sk-http-test-secret-value") {
		t.Fatalf("key create response leaked plaintext: %s", keyResponse.body)
	}

	discoveryResponse := requestJSON(t, client, http.MethodPost,
		server.URL+"/keys/api/keys/"+keyPayload.Key.ID+"/discover-models", map[string]any{},
		loginPayload.CSRFToken)
	if discoveryResponse.status != http.StatusOK ||
		!bytes.Contains(discoveryResponse.body, []byte(`"discovered-model"`)) {
		t.Fatalf("discovery status = %d body=%s", discoveryResponse.status, discoveryResponse.body)
	}
	testResponse := requestJSON(t, client, http.MethodPost,
		server.URL+"/keys/api/keys/"+keyPayload.Key.ID+"/test", map[string]any{},
		loginPayload.CSRFToken)
	if testResponse.status != http.StatusOK ||
		!bytes.Contains(testResponse.body, []byte(`"status":"success"`)) {
		t.Fatalf("single test status = %d body=%s", testResponse.status, testResponse.body)
	}
	batchResponse := requestJSON(t, client, http.MethodPost,
		server.URL+"/keys/api/providers/"+providerPayload.Provider.ID+"/test",
		map[string]any{"confirm": true}, loginPayload.CSRFToken)
	if batchResponse.status != http.StatusOK ||
		!bytes.Contains(batchResponse.body, []byte(`"count":1`)) {
		t.Fatalf("batch test status = %d body=%s", batchResponse.status, batchResponse.body)
	}

	providersResponse := requestJSON(t, client, http.MethodGet,
		server.URL+"/keys/api/providers", nil, "")
	if providersResponse.status != http.StatusOK ||
		strings.Contains(string(providersResponse.body), "sk-http-test-secret-value") {
		t.Fatalf("providers response status=%d body=%s", providersResponse.status, providersResponse.body)
	}

	wrongReveal := requestJSON(t, client, http.MethodPost,
		server.URL+"/keys/api/keys/"+keyPayload.Key.ID+"/reveal", map[string]any{
			"password": "wrong-password",
		}, loginPayload.CSRFToken)
	if wrongReveal.status != http.StatusUnauthorized {
		t.Fatalf("wrong reveal status = %d body=%s", wrongReveal.status, wrongReveal.body)
	}
	reveal := requestJSON(t, client, http.MethodPost,
		server.URL+"/keys/api/keys/"+keyPayload.Key.ID+"/reveal", map[string]any{
			"password": "correct-password",
		}, loginPayload.CSRFToken)
	if reveal.status != http.StatusOK ||
		!bytes.Contains(reveal.body, []byte("sk-http-test-secret-value")) {
		t.Fatalf("reveal status = %d body=%s", reveal.status, reveal.body)
	}
	if reveal.header.Get("Cache-Control") != "no-store, max-age=0" {
		t.Fatalf("reveal Cache-Control = %q", reveal.header.Get("Cache-Control"))
	}

	history := requestJSON(t, client, http.MethodGet,
		server.URL+"/keys/api/keys/"+keyPayload.Key.ID+"/detections", nil, "")
	if history.status != http.StatusOK {
		t.Fatalf("history status = %d body=%s", history.status, history.body)
	}

	modelInUse := requestJSON(t, client, http.MethodDelete,
		server.URL+"/keys/api/models/"+modelPayload.Model.ID, map[string]any{}, loginPayload.CSRFToken)
	if modelInUse.status != http.StatusConflict {
		t.Fatalf("delete referenced model status = %d body=%s", modelInUse.status, modelInUse.body)
	}
	deleteKey := requestJSON(t, client, http.MethodDelete,
		server.URL+"/keys/api/keys/"+keyPayload.Key.ID, map[string]any{}, loginPayload.CSRFToken)
	if deleteKey.status != http.StatusOK {
		t.Fatalf("delete key status = %d body=%s", deleteKey.status, deleteKey.body)
	}
	deleteModel := requestJSON(t, client, http.MethodDelete,
		server.URL+"/keys/api/models/"+modelPayload.Model.ID, map[string]any{}, loginPayload.CSRFToken)
	if deleteModel.status != http.StatusOK {
		t.Fatalf("delete model status = %d body=%s", deleteModel.status, deleteModel.body)
	}
	deleteProvider := requestJSON(t, client, http.MethodDelete,
		server.URL+"/keys/api/providers/"+providerPayload.Provider.ID, map[string]any{}, loginPayload.CSRFToken)
	if deleteProvider.status != http.StatusOK {
		t.Fatalf("delete provider status = %d body=%s", deleteProvider.status, deleteProvider.body)
	}

	logout := requestJSON(t, client, http.MethodPost, server.URL+"/keys/api/logout",
		map[string]any{}, loginPayload.CSRFToken)
	if logout.status != http.StatusOK {
		t.Fatalf("logout status = %d body=%s", logout.status, logout.body)
	}
	afterLogout := requestJSON(t, client, http.MethodGet, server.URL+"/keys/api/providers", nil, "")
	if afterLogout.status != http.StatusUnauthorized {
		t.Fatalf("providers after logout status = %d body=%s", afterLogout.status, afterLogout.body)
	}
}

func TestAuthTokenRejectsTamperingAndExpiredSessions(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	auth, err := newAuthManager(hash, []byte(strings.Repeat("x", 32)), "https://example.test", true)
	if err != nil {
		t.Fatal(err)
	}
	now := timeNow()
	token, err := auth.newSessionToken(now)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.verifySessionToken(token, now) {
		t.Fatal("fresh token did not verify")
	}
	tampered := token[:len(token)-1] + "A"
	if auth.verifySessionToken(tampered, now) {
		t.Fatal("tampered token verified")
	}
	if auth.verifySessionToken(token, now.Add(sessionLifetime+time.Minute)) {
		t.Fatal("expired token verified")
	}
	if !auth.verifyCSRF(token, auth.csrfToken(token)) {
		t.Fatal("matching CSRF token did not verify")
	}
	if auth.verifyCSRF(token, "wrong") {
		t.Fatal("wrong CSRF token verified")
	}
}

func TestMasterKeyLoaderAcceptsFileValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")
	value := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEYS_MASTER_KEY", "")
	t.Setenv("KEYS_MASTER_KEY_FILE", path)
	got, err := secretFromEnv("KEYS_MASTER_KEY", "KEYS_MASTER_KEY_FILE")
	if err != nil || got != value {
		t.Fatalf("secretFromEnv = %q, %v", got, err)
	}
}

type jsonResponse struct {
	status int
	body   []byte
	header http.Header
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func requestJSON(t *testing.T, client *http.Client, method, target string, payload any, csrf string) jsonResponse {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return jsonResponse{status: response.StatusCode, body: responseBody, header: response.Header.Clone()}
}
