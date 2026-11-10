package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var embeddedWeb embed.FS

type app struct {
	store        *keyStore
	detector     *detector
	auth         *authManager
	publicOrigin string
	web          fs.FS
}

type authedHandler func(http.ResponseWriter, *http.Request, string)

func newApp(store *keyStore, detector *detector, auth *authManager, publicOrigin string) (*app, error) {
	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		return nil, err
	}
	return &app{
		store: store, detector: detector, auth: auth,
		publicOrigin: strings.TrimRight(publicOrigin, "/"), web: web,
	}, nil
}

func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/keys/", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /keys/{$}", a.serveIndex)
	mux.HandleFunc("GET /keys/assets/", a.serveAsset)
	mux.HandleFunc("GET /keys/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": keysVersion})
	})

	mux.HandleFunc("GET /keys/api/session", a.handleSessionStatus)
	mux.HandleFunc("POST /keys/api/session", a.handleLogin)
	mux.HandleFunc("POST /keys/api/logout", a.withMutation(a.handleLogout))

	mux.HandleFunc("GET /keys/api/bootstrap", a.withSession(a.handleBootstrap))
	mux.HandleFunc("GET /keys/api/providers", a.withSession(a.handleListProviders))
	mux.HandleFunc("POST /keys/api/providers", a.withMutation(a.handleCreateProvider))
	mux.HandleFunc("PUT /keys/api/providers/{id}", a.withMutation(a.handleUpdateProvider))
	mux.HandleFunc("DELETE /keys/api/providers/{id}", a.withMutation(a.handleDeleteProvider))
	mux.HandleFunc("POST /keys/api/providers/{id}/keys", a.withMutation(a.handleCreateKey))
	mux.HandleFunc("PUT /keys/api/keys/{id}", a.withMutation(a.handleUpdateKey))
	mux.HandleFunc("DELETE /keys/api/keys/{id}", a.withMutation(a.handleDeleteKey))
	mux.HandleFunc("POST /keys/api/providers/{id}/models", a.withMutation(a.handleCreateModel))
	mux.HandleFunc("PUT /keys/api/models/{id}", a.withMutation(a.handleUpdateModel))
	mux.HandleFunc("DELETE /keys/api/models/{id}", a.withMutation(a.handleDeleteModel))
	mux.HandleFunc("POST /keys/api/keys/{id}/test", a.withMutation(a.handleTestKey))
	mux.HandleFunc("POST /keys/api/providers/{id}/test", a.withMutation(a.handleTestProvider))
	mux.HandleFunc("POST /keys/api/providers/{id}/discover-models", a.withMutation(a.handleImportProviderModels))
	mux.HandleFunc("POST /keys/api/test-enabled", a.withMutation(a.handleTestEnabled))
	mux.HandleFunc("POST /keys/api/keys/{id}/reveal", a.withMutation(a.handleRevealKey))
	mux.HandleFunc("POST /keys/api/keys/{id}/discover-models", a.withMutation(a.handleDiscoverModels))
	mux.HandleFunc("GET /keys/api/keys/{id}/detections", a.withSession(a.handleListDetections))

	mux.HandleFunc("/keys/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "接口不存在")
	})
	return secureKeyHeaders(mux)
}

func secureKeyHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; "+
				"connect-src 'self'; font-src 'none'; object-src 'none'; frame-ancestors 'none'; "+
				"base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func (a *app) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/keys/" {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(a.web, "index.html")
	if err != nil {
		http.Error(w, "page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *app) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/keys/assets/")
	name = path.Clean("/" + name)
	name = strings.TrimPrefix(name, "/")
	if name == "" || name == "." || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(a.web, "assets/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *app) withSession(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := a.auth.sessionFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
			return
		}
		next(w, r, token)
	}
}

func (a *app) withMutation(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := a.auth.sessionFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
			return
		}
		if !a.auth.validOrigin(r) {
			writeError(w, http.StatusForbidden, "INVALID_ORIGIN", "请求来源无效")
			return
		}
		if !a.auth.verifyCSRF(token, r.Header.Get("X-CSRF-Token")) {
			writeError(w, http.StatusForbidden, "INVALID_CSRF", "CSRF 校验失败，请刷新页面后重试")
			return
		}
		next(w, r, token)
	}
}

func (a *app) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	token, ok := a.auth.sessionFromRequest(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "authenticated": true, "csrfToken": a.auth.csrfToken(token),
	})
}

func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !a.auth.validOrigin(r) {
		writeError(w, http.StatusForbidden, "INVALID_ORIGIN", "请求来源无效")
		return
	}
	if !a.auth.allowAttempt() {
		writeError(w, http.StatusTooManyRequests, "LOGIN_RATE_LIMITED", "尝试次数过多，请一分钟后再试")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	if !a.auth.verifyPassword(input.Password) {
		a.auth.recordFailure()
		writeError(w, http.StatusUnauthorized, "INVALID_PASSWORD", "密码错误")
		return
	}
	a.auth.recordSuccess()
	token, err := a.auth.newSessionToken(timeNow())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SESSION_FAILED", "无法创建登录会话")
		return
	}
	a.auth.setSessionCookie(w, token, int(sessionLifetime.Seconds()))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "authenticated": true, "csrfToken": a.auth.csrfToken(token),
	})
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request, _ string) {
	a.auth.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleBootstrap(w http.ResponseWriter, _ *http.Request, _ string) {
	response := optionsResponse()
	providers, err := a.store.listProviders()
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"providers":    providers,
		"options":      response.Options,
		"statusLabels": response.Status,
	})
}

func (a *app) handleListProviders(w http.ResponseWriter, _ *http.Request, _ string) {
	providers, err := a.store.listProviders()
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
}

func (a *app) handleCreateProvider(w http.ResponseWriter, r *http.Request, _ string) {
	var input ProviderInput
	if err := decodeJSON(w, r, &input, 32<<10); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	provider, err := a.store.createProvider(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"provider": provider})
}

func (a *app) handleUpdateProvider(w http.ResponseWriter, r *http.Request, _ string) {
	var input ProviderInput
	if err := decodeJSON(w, r, &input, 32<<10); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	provider, err := a.store.updateProvider(r.PathValue("id"), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": provider})
}

func (a *app) handleDeleteProvider(w http.ResponseWriter, r *http.Request, _ string) {
	if err := a.store.deleteProvider(r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleCreateKey(w http.ResponseWriter, r *http.Request, _ string) {
	var input APIKeyInput
	if err := decodeJSON(w, r, &input, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	key, err := a.store.createAPIKey(r.PathValue("id"), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"key": key})
}

func (a *app) handleUpdateKey(w http.ResponseWriter, r *http.Request, _ string) {
	var input APIKeyInput
	if err := decodeJSON(w, r, &input, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	key, err := a.store.updateAPIKey(r.PathValue("id"), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key})
}

func (a *app) handleDeleteKey(w http.ResponseWriter, r *http.Request, _ string) {
	if err := a.store.deleteAPIKey(r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleCreateModel(w http.ResponseWriter, r *http.Request, _ string) {
	var input ModelInput
	if err := decodeJSON(w, r, &input, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	model, err := a.store.createModel(r.PathValue("id"), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"model": model})
}

func (a *app) handleUpdateModel(w http.ResponseWriter, r *http.Request, _ string) {
	var input ModelInput
	if err := decodeJSON(w, r, &input, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	model, err := a.store.updateModel(r.PathValue("id"), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model": model})
}

func (a *app) handleDeleteModel(w http.ResponseWriter, r *http.Request, _ string) {
	if err := a.store.deleteModel(r.PathValue("id")); err != nil {
		if errors.Is(err, errConflict) {
			writeError(w, http.StatusConflict, "MODEL_IN_USE", "该模型仍被 Key 用作测试模型，请先修改 Key")
			return
		}
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleTestKey(w http.ResponseWriter, r *http.Request, _ string) {
	result := a.detector.testKey(r.PathValue("id"), a.store)
	writeJSON(w, http.StatusOK, map[string]any{"detection": result})
}

func (a *app) handleTestProvider(w http.ResponseWriter, r *http.Request, _ string) {
	a.handleBatchTest(w, r, r.PathValue("id"))
}

func (a *app) handleTestEnabled(w http.ResponseWriter, r *http.Request, _ string) {
	a.handleBatchTest(w, r, "")
}

func (a *app) handleBatchTest(w http.ResponseWriter, r *http.Request, providerID string) {
	var input struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(w, r, &input, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	if !input.Confirm {
		writeError(w, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "批量检测需要明确确认")
		return
	}
	ids, err := a.store.detectableKeyIDs(providerID, true)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if len(ids) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "NO_TESTABLE_KEYS", "没有启用且已配置测试模型的 Key")
		return
	}
	if len(ids) > 64 {
		writeError(w, http.StatusUnprocessableEntity, "BATCH_TOO_LARGE", "一次最多检测 64 个 Key")
		return
	}
	results := a.detector.batchDetect(ids, a.store)
	writeJSON(w, http.StatusOK, map[string]any{"count": len(results), "detections": results})
}

func (a *app) handleRevealKey(w http.ResponseWriter, r *http.Request, _ string) {
	if !a.auth.allowAttempt() {
		writeError(w, http.StatusTooManyRequests, "LOGIN_RATE_LIMITED", "重新验证次数过多，请一分钟后再试")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	if !a.auth.verifyPassword(input.Password) {
		a.auth.recordFailure()
		writeError(w, http.StatusUnauthorized, "REAUTH_FAILED", "密码错误")
		return
	}
	a.auth.recordSuccess()
	secret, err := a.store.revealSecret(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"secret": secret})
}

func (a *app) handleDiscoverModels(w http.ResponseWriter, r *http.Request, _ string) {
	provider, secret, err := a.store.providerCredential(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), detectionTimeout)
	defer cancel()
	result, err := a.detector.discoverModels(ctx, provider, secret)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "MODEL_DISCOVERY_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *app) handleImportProviderModels(w http.ResponseWriter, r *http.Request, _ string) {
	providerID := r.PathValue("id")
	provider, err := a.store.providerByID(providerID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var (
		discovered discoveryResult
		sourceErr  error
	)
	var keyID string
	for _, key := range provider.Keys {
		if key.Enabled {
			keyID = key.ID
			break
		}
	}
	if keyID != "" {
		_, secret, err := a.store.providerCredential(keyID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		discovered, sourceErr = a.detector.discoverModels(r.Context(), provider, secret)
	} else {
		discovered, sourceErr = a.detector.discoverProviderCatalog(r.Context(), provider)
	}
	if sourceErr != nil {
		writeError(w, http.StatusUnprocessableEntity, "MODEL_DISCOVERY_FAILED", sourceErr.Error())
		return
	}
	result, err := a.store.importDiscoveredModels(providerID, discovered.Models)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"discovered":     result.Discovered,
		"created":        result.Created,
		"updated":        result.Updated,
		"models":         result.Models,
		"metadataSource": discovered.MetadataSource,
	})
}

func (a *app) handleListDetections(w http.ResponseWriter, r *http.Request, _ string) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	detections, err := a.store.listDetections(r.PathValue("id"), limit)
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"detections": detections})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("请求体不能为空")
		}
		return errors.New("请求 JSON 无效")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("请求体只能包含一个 JSON 对象")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiErrorEnvelope{Error: apiErrorBody{Code: code, Message: message}})
}

func writeStoreError(w http.ResponseWriter, err error) {
	var validation *validationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", validation.Error())
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "记录不存在")
	case errors.Is(err, errConflict):
		writeError(w, http.StatusConflict, "CONFLICT", "记录发生冲突")
	default:
		writeServerError(w, err)
	}
}

func writeServerError(w http.ResponseWriter, err error) {
	log.Printf("keys service error: %v", err)
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "服务暂时不可用")
}

func timeNow() time.Time {
	return time.Now()
}
