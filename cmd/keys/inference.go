package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const brokerSocket = "/run/go-sites-inference.sock"

type selection struct {
	Key   string `json:"key"`
	Model string `json:"model"`
}
type grantPair struct {
	Day   selection `json:"day"`
	Night selection `json:"night"`
}
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (s *keyStore) grant() (g grantPair, err error) {
	err = s.db.QueryRow(`SELECT day_key,day_model,night_key,night_model FROM inference_grant WHERE singleton=1`).Scan(&g.Day.Key, &g.Day.Model, &g.Night.Key, &g.Night.Model)
	return
}
func (a *app) grantStatus(w http.ResponseWriter, r *http.Request, _ string) {
	g, e := a.store.grant()
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		writeServerError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": e == nil, "routes": g})
}
func (a *app) updateGrant(w http.ResponseWriter, r *http.Request, _ string) {
	var in struct {
		Password string    `json:"password"`
		Action   string    `json:"action"`
		Routes   grantPair `json:"routes"`
	}
	if decodeJSON(w, r, &in, 12<<10) != nil {
		writeError(w, 400, "BAD_INPUT", "授权格式无效")
		return
	}
	if !a.auth.allowAttempt() {
		writeError(w, 429, "LOCKED", "请稍后再试")
		return
	}
	if !a.auth.verifyPassword(in.Password) {
		a.auth.recordFailure()
		writeError(w, 403, "REAUTH", "管理密码不正确")
		return
	}
	a.auth.recordSuccess()
	var err error
	switch in.Action {
	case "revoke":
		_, err = a.store.db.Exec(`DELETE FROM inference_grant`)
	case "enable":
		for _, entry := range []struct {
			preset string
			choice selection
		}{{"ollama", in.Routes.Day}, {"deepseek", in.Routes.Night}} {
			_, e := a.infer(r.Context(), entry.preset, entry.choice, []chatMessage{{"user", "Return just the word ready."}}, 256)
			if e != nil {
				writeError(w, 422, "CHECK_FAILED", entry.preset+" 模型验证失败："+e.Error()+"；未更改授权")
				return
			}
		}
		_, err = a.store.db.Exec(`INSERT OR REPLACE INTO inference_grant VALUES(1,?,?,?,?)`, in.Routes.Day.Key, in.Routes.Day.Model, in.Routes.Night.Key, in.Routes.Night.Model)
	default:
		writeError(w, 400, "ACTION", "未知操作")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"enabled": in.Action == "enable"})
}

// Full-essay feedback is one request, with a bounded larger answer budget. Other
// callers keep their short budgets. Never let the caller choose a model, host or key.
const fullReviewTokens = 65_536

func inferencePayload(preset, model string, msg []chatMessage, maxTokens int) map[string]any {
	body := map[string]any{"model": model, "messages": msg, "max_tokens": maxTokens}
	if preset == "deepseek" {
		body["thinking"] = map[string]string{"type": "disabled"}
	} else if preset == "ollama" && maxTokens == fullReviewTokens {
		body["reasoning_effort"] = "low"
	}
	return body
}

// Report provider counts, not estimates. Some upstreams omit the reasoning split.
func usageCount(n *int) string {
	if n == nil {
		return "unreported"
	}
	return strconv.Itoa(*n)
}

type inferenceResult struct {
	Content      string
	FinishReason string
}

func inferTransportError(err error) error {
	if os.IsTimeout(err) || errors.Is(err, context.DeadlineExceeded) {
		return errors.New("供应商超时")
	}
	return errors.New("供应商连接失败")
}
func (a *app) infer(ctx context.Context, preset string, s selection, msg []chatMessage, maxTokens int) (inferenceResult, error) {
	if !strings.Contains(strings.ToLower(s.Model), "deepseek") || len(s.Model) > 200 || s.Key == "" {
		return inferenceResult{}, errors.New("请选择已配置的 DeepSeek 模型")
	}
	var allowed bool
	err := a.store.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM api_keys k JOIN providers p ON p.id=k.provider_id WHERE k.id=? AND k.enabled=1 AND p.enabled=1 AND p.preset_id=? AND (k.test_model=? OR EXISTS(SELECT 1 FROM models WHERE provider_id=p.id AND model_id=?)))`, s.Key, preset, s.Model, s.Model).Scan(&allowed)
	if err != nil || !allowed {
		return inferenceResult{}, errors.New("Key 或模型当前不可用")
	}
	p, key, err := a.store.providerCredential(s.Key)
	if err != nil || p.PresetID != preset || p.AuthMode != authBearer {
		return inferenceResult{}, errors.New("凭据不可用")
	}
	base := strings.TrimRight(p.BaseURL, "/")
	if !(preset == "ollama" && base == "https://ollama.com/v1" || preset == "deepseek" && (base == "https://api.deepseek.com" || base == "https://api.deepseek.com/v1")) {
		return inferenceResult{}, errors.New("入口不匹配")
	}
	raw, _ := json.Marshal(inferencePayload(preset, s.Model, msg, maxTokens))
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return inferenceResult{}, errors.New("入口无效")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	client := a.detector.client
	if maxTokens == fullReviewTokens {
		client = a.detector.reviewClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return inferenceResult{}, inferTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return inferenceResult{}, fmt.Errorf("供应商 HTTP %d", resp.StatusCode)
	}
	var result struct {
		Usage struct {
			Completion *int `json:"completion_tokens"`
			Details    struct {
				Reasoning *int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
		Choices []struct {
			Message      struct{ Content string }
			FinishReason string `json:"finish_reason"`
		}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		if os.IsTimeout(err) {
			return inferenceResult{}, errors.New("供应商超时")
		}
		return inferenceResult{}, errors.New("供应商未返回有效内容")
	}
	if maxTokens == fullReviewTokens {
		log.Printf("writing_review_usage limit=%d completion_tokens=%s reasoning_tokens=%s", maxTokens, usageCount(result.Usage.Completion), usageCount(result.Usage.Details.Reasoning))
	}
	if len(result.Choices) > 0 && result.Choices[0].FinishReason == "length" && strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return inferenceResult{}, errors.New("生成达到长度上限但没有返回正文")
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return inferenceResult{}, errors.New("供应商未返回有效内容")
	}
	return inferenceResult{Content: result.Choices[0].Message.Content, FinishReason: result.Choices[0].FinishReason}, nil
}
func (a *app) localChat(w http.ResponseWriter, r *http.Request) {
	g, e := a.store.grant()
	if errors.Is(e, sql.ErrNoRows) {
		http.Error(w, "模型调用尚未授权", 428)
		return
	}
	if e != nil {
		http.Error(w, "授权状态不可用", 503)
		return
	}
	var in struct {
		Messages []chatMessage `json:"messages"`
	}
	tokens, bodyLimit := 2048, int64(96<<10)
	if r.Header.Get("X-Inference-Profile") == "writing-review" {
		tokens, bodyLimit = fullReviewTokens, 1<<20
	}
	if decodeJSON(w, r, &in, bodyLimit) != nil || len(in.Messages) == 0 || len(in.Messages) > 16 {
		http.Error(w, "invalid messages", 400)
		return
	}
	for _, m := range in.Messages {
		if (m.Role != "system" && m.Role != "user" && m.Role != "assistant") || m.Content == "" {
			http.Error(w, "invalid message", 400)
			return
		}
	}
	select {
	case a.detector.sem <- struct{}{}:
		defer func() { <-a.detector.sem }()
	default:
		http.Error(w, "busy", 429)
		return
	}
	h := time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Hour()
	preset, s := "ollama", g.Day
	if h < 2 || h >= 20 {
		preset, s = "deepseek", g.Night
	}
	content, e := a.infer(r.Context(), preset, s, in.Messages, tokens)
	if e != nil {
		http.Error(w, e.Error(), 502)
		return
	}
	writeJSON(w, 200, map[string]any{"choices": []any{map[string]any{"message": chatMessage{"assistant", content.Content}, "finish_reason": content.FinishReason}}})
}

// Only systemd supplies this Unix socket. Its group controls which service units may connect.
// The socket's mux contains no admin or secret export routes; the website's mux has no chat route.
func (a *app) startLocalInference() error {
	if os.Getenv("KEYS_INFERENCE_SOCKET") != "1" {
		return nil
	}
	if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) || os.Getenv("LISTEN_FDS") != "1" {
		return errors.New("missing systemd inference socket")
	}
	f := os.NewFile(3, "inference")
	if f == nil {
		return errors.New("no socket fd")
	}
	l, e := net.FileListener(f)
	f.Close()
	if e != nil {
		return e
	}
	if l.Addr().Network() != "unix" || l.Addr().String() != brokerSocket {
		l.Close()
		return errors.New("unexpected inference socket")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", a.localChat)
	srv := http.Server{Handler: mux, ReadTimeout: 8 * time.Second, WriteTimeout: 125 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192}
	go func() {
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("local inference listener failed")
		}
	}()
	return nil
}
