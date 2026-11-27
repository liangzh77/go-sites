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
func (a *app) infer(ctx context.Context, preset string, s selection, msg []chatMessage, maxTokens int) (string, error) {
	if !strings.Contains(strings.ToLower(s.Model), "deepseek") || len(s.Model) > 200 || s.Key == "" {
		return "", errors.New("请选择已配置的 DeepSeek 模型")
	}
	var allowed bool
	err := a.store.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM api_keys k JOIN providers p ON p.id=k.provider_id WHERE k.id=? AND k.enabled=1 AND p.enabled=1 AND p.preset_id=? AND (k.test_model=? OR EXISTS(SELECT 1 FROM models WHERE provider_id=p.id AND model_id=?)))`, s.Key, preset, s.Model, s.Model).Scan(&allowed)
	if err != nil || !allowed {
		return "", errors.New("Key 或模型当前不可用")
	}
	p, key, err := a.store.providerCredential(s.Key)
	if err != nil || p.PresetID != preset || p.AuthMode != authBearer {
		return "", errors.New("凭据不可用")
	}
	base := strings.TrimRight(p.BaseURL, "/")
	if !(preset == "ollama" && base == "https://ollama.com/v1" || preset == "deepseek" && (base == "https://api.deepseek.com" || base == "https://api.deepseek.com/v1")) {
		return "", errors.New("入口不匹配")
	}
	body := map[string]any{"model": s.Model, "messages": msg, "max_tokens": maxTokens}
	if preset == "deepseek" {
		body["thinking"] = map[string]string{"type": "disabled"}
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", errors.New("入口无效")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.detector.client.Do(req)
	if err != nil {
		return "", errors.New("供应商超时或连接失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("供应商 HTTP %d", resp.StatusCode)
	}
	var result struct {
		Choices []struct{ Message struct{ Content string } }
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&result) != nil || len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", errors.New("供应商未返回有效内容")
	}
	return result.Choices[0].Message.Content, nil
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
	if decodeJSON(w, r, &in, 96<<10) != nil || len(in.Messages) == 0 || len(in.Messages) > 16 {
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
	content, e := a.infer(r.Context(), preset, s, in.Messages, 2048)
	if e != nil {
		http.Error(w, e.Error(), 502)
		return
	}
	writeJSON(w, 200, map[string]any{"choices": []any{map[string]any{"message": chatMessage{"assistant", content}}}})
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
	srv := http.Server{Handler: mux, ReadTimeout: 8 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192}
	go func() {
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("local inference listener failed")
		}
	}()
	return nil
}
