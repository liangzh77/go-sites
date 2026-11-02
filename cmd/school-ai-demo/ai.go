package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// The form's field names are deliberately loose: the same material says
// "家庭住址" where the template says "现居地址", and a family row and a school
// row both repeat "姓名". A fixed mapping cannot survive that, so the mapping
// is a model call whose only job is to copy values out of the material.

const (
	deepseekKeyEnv       = "SCHOOL_AI_DEEPSEEK_API_KEY"
	deepseekBackupKeyEnv = "SCHOOL_AI_DEEPSEEK_API_KEY_BACKUP"
	deepseekBaseEnv      = "SCHOOL_AI_DEEPSEEK_BASE_URL"
	deepseekModelEnv     = "SCHOOL_AI_DEEPSEEK_MODEL"
	defaultDeepseekBase  = "https://api.deepseek.com/v1"
	defaultModel         = "deepseek-chat"
	aiTimeout            = 90 * time.Second
	maxMaterialRunes     = 24000
	maxValueRunes        = 1200
	// The model spends tokens on reasoning before it writes the answer, so a
	// small cap yields an empty content string. Leave room, or omit it.
	maxTokens = 4096
	userAgent = "school-ai-demo/1.0"
)

// aiClient talks to an OpenAI-compatible chat completions endpoint. The base
// URL is a base, not a full URL: "/chat/completions" is appended, so the same
// env works for api.deepseek.com and for a subscription gateway that does not
// share DeepSeek's path.
type aiClient struct {
	apiKey       string
	backupKey    string
	endpoint     string // full URL, already joined with /chat/completions
	model        string
	http         *http.Client
	needsSession bool // the OpenCode gateway rejects requests without one
}

// completionsURL joins the configured base with the chat completions path,
// tolerating a base that already ends in it or in a trailing slash.
func completionsURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = defaultDeepseekBase
	}
	base = strings.TrimSuffix(base, "/chat/completions")
	return base + "/chat/completions"
}

func newAIClientFromEnv() *aiClient {
	model := strings.TrimSpace(os.Getenv(deepseekModelEnv))
	if model == "" {
		model = defaultModel
	}
	base := strings.TrimSpace(os.Getenv(deepseekBaseEnv))
	return &aiClient{
		apiKey:       strings.TrimSpace(os.Getenv(deepseekKeyEnv)),
		backupKey:    strings.TrimSpace(os.Getenv(deepseekBackupKeyEnv)),
		endpoint:     completionsURL(base),
		model:        model,
		http:         &http.Client{Timeout: aiTimeout},
		needsSession: strings.Contains(strings.ToLower(base), "opencode"),
	}
}

// configured reports whether the service can call the model at all. The UI
// uses this to explain the state up front instead of failing at upload time.
func (c *aiClient) configured() bool { return c.apiKey != "" }

// hasBackup reports whether a second key is available for a retry.
func (c *aiClient) hasBackup() bool { return c.backupKey != "" }

var errNoAPIKey = errors.New("尚未配置 DeepSeek API Key")

// errEmptyContent is what the model returns when it never got past reasoning;
// it is worth a retry, unlike a malformed answer.
var errEmptyContent = errors.New("模型没有返回内容，请重试")

// retryableStatus marks the upstream answers that mean "this key is not
// usable right now": auth, payment, and rate limiting. Those are the ones where
// the backup key is worth trying.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusTooManyRequests:
		return true
	}
	return false
}

// fillResult is what the model produced, already filtered to real fields.
type fillResult struct {
	Values   map[string]string
	Warnings []string
	Model    string
}

// fill asks the model to map material onto the template's fields. It returns
// only ids that exist in the template, and only non-empty values: an empty or
// unknown answer leaves the slot blank rather than guessing.
//
// One attempt goes to the primary key. If the gateway answers 401/402/429 and a
// backup key is configured, the same request is retried once with it: a spent
// subscription is not something the user filling in a form can fix.
func (c *aiClient) fill(ctx context.Context, tpl templateRecord, material string) (fillResult, error) {
	if c.apiKey == "" {
		return fillResult{}, errNoAPIKey
	}
	material = truncateRunes(strings.TrimSpace(material), maxMaterialRunes)
	if material == "" {
		return fillResult{Values: map[string]string{}, Model: c.model}, nil
	}

	// One session id per fill, reused by the retry: the gateway groups a
	// conversation by it, and a retry is the same conversation.
	sessionID := newUUID()

	res, status, err := c.attempt(ctx, tpl, material, c.apiKey, sessionID)
	if err == nil {
		return res, nil
	}
	if c.hasBackup() && retryableStatus(status) {
		log.Printf("fill: primary key returned %d, retrying with the backup key", status)
		if res2, status2, err2 := c.attempt(ctx, tpl, material, c.backupKey, sessionID); err2 == nil {
			return res2, nil
		} else {
			log.Printf("fill: backup key also failed (status %d): %v", status2, err2)
		}
	}
	return fillResult{}, err
}

// attempt is one request to the model. The status is returned alongside the
// error so the caller can decide whether another key is worth trying.
func (c *aiClient) attempt(ctx context.Context, tpl templateRecord, material, apiKey, sessionID string) (fillResult, int, error) {
	body := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt(tpl, material)},
		},
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
		"stream":          false,
		"max_tokens":      maxTokens,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fillResult{}, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(raw))
	if err != nil {
		return fillResult{}, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", userAgent)
	if c.needsSession {
		// The OpenCode gateway answers 400 MissingSessionID without this.
		req.Header.Set("x-opencode-session", sessionID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fillResult{}, 0, fmt.Errorf("调用模型失败: %w", friendlyNetErr(err))
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fillResult{}, resp.StatusCode, fmt.Errorf("读取模型响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fillResult{}, resp.StatusCode,
			fmt.Errorf("模型返回 %d: %s", resp.StatusCode, deepseekErrText(payload))
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &completion); err != nil {
		return fillResult{}, resp.StatusCode, fmt.Errorf("解析模型响应失败: %w", err)
	}
	if len(completion.Choices) == 0 {
		return fillResult{}, resp.StatusCode, errors.New("模型没有返回内容")
	}
	content := completion.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		// Reasoning-only answers come back as an empty string; say so instead
		// of reporting an empty but successful fill.
		return fillResult{}, resp.StatusCode, errEmptyContent
	}
	res, err := parseFillJSON(tpl, content, c.model)
	return res, resp.StatusCode, err
}

// parseFillJSON keeps only usable answers: known ids, string values, non-empty.
//
// Reasoning models occasionally emit JSON that is nearly right: a stray brace
// inside a key ({"fields{": ...}), a trailing comma, or the object wrapped in
// prose. None of those should surface as a hard failure, so each parse attempt
// falls back to the next repair before giving up.
func parseFillJSON(tpl templateRecord, content, model string) (fillResult, error) {
	text := strings.TrimSpace(content)
	if text == "" {
		return fillResult{}, errEmptyContent
	}
	text = repairJSON(text)

	var envelope struct {
		Fields   map[string]any `json:"fields"`
		Warnings []string       `json:"warnings"`
		Values   map[string]any `json:"values"`
	}
	// Accept both {"fields": {...}} and a bare {"f01": "..."} object.
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		// Last resort: keep the outermost object and drop what trails it.
		if salvaged := outermostObject(text); salvaged != "" && json.Unmarshal([]byte(salvaged), &raw) == nil {
			text = salvaged
		} else {
			return fillResult{}, fmt.Errorf("模型返回的不是有效 JSON: %w", err)
		}
	}
	values := map[string]any{}
	switch {
	case hasKey(raw, "fields"):
		_ = json.Unmarshal([]byte(text), &envelope)
		values = envelope.Fields
	case hasKey(raw, "values"):
		_ = json.Unmarshal([]byte(text), &envelope)
		values = envelope.Values
	default:
		values = raw
	}
	if envelope.Warnings != nil {
		_ = json.Unmarshal([]byte(text), &envelope)
	}

	known := make(map[string]bool, len(tpl.Fields))
	for _, f := range tpl.Fields {
		known[f.ID] = true
	}
	out := fillResult{Values: map[string]string{}, Model: model, Warnings: []string{}}
	var unknown []string
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		id := strings.TrimSpace(k)
		if !known[id] {
			unknown = append(unknown, id)
			continue
		}
		s, ok := values[k].(string)
		if !ok {
			if values[k] == nil {
				continue
			}
			s = fmt.Sprint(values[k])
		}
		s = strings.TrimSpace(s)
		if s == "" || strings.EqualFold(s, "null") || s == "未知" || s == "不详" {
			continue
		}
		out.Values[id] = truncateRunes(s, maxValueRunes)
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		if len(unknown) > 10 {
			unknown = append(unknown[:10], "…")
		}
		out.Warnings = append(out.Warnings,
			"模型返回了模板里不存在的字段，已忽略: "+strings.Join(unknown, ", "))
	}
	for _, w := range envelope.Warnings {
		if w = strings.TrimSpace(w); w != "" {
			out.Warnings = append(out.Warnings, truncateRunes(w, 200))
		}
	}
	return out, nil
}

// repairJSON strips markdown fences and fixes the malformations a reasoning
// model actually produces, cheapest first. It never invents values: it only
// removes syntax the model added by accident.
func repairJSON(text string) string {
	t := strings.TrimSpace(text)

	// Wrapped in a code fence, possibly with a language tag and prose around it.
	if i := strings.Index(t, "```"); i >= 0 {
		rest := t[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			rest = rest[:j]
		}
		t = strings.TrimSpace(rest)
	}
	// Prose before the object.
	if i := strings.IndexByte(t, '{'); i > 0 {
		t = t[i:]
	}
	// Prose after the object.
	if j := strings.LastIndexByte(t, '}'); j >= 0 && j < len(t)-1 {
		t = t[:j+1]
	}
	t = strings.ReplaceAll(t, "\"fields{\":", "\"fields\":")
	t = strings.ReplaceAll(t, "\"values{\":", "\"values\":")
	t = strings.ReplaceAll(t, "\"warnings{\":", "\"warnings\":")
	t = strings.ReplaceAll(t, "\u201c", "\"")
	t = strings.ReplaceAll(t, "\u201d", "\"")
	return dropTrailingCommas(t)
}

// dropTrailingCommas removes a comma that sits just before a closing bracket,
// which is the other common near-miss.
func dropTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			b.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\n' || s[j] == '\r' || s[j] == '\t') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // skip the comma
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// outermostObject returns the substring from the first "{" to the matching
// "}", or "" when the braces do not balance.
func outermostObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func hasKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

const systemPrompt = `你是一个表单填写助手。你会收到一份表格的全部可填字段（每个字段有 id、名称、所属分区、以及表格里给出的说明文字 hint），
以及一份用户提供的原始资料（可能是 Excel 转成的文本，也可能是用户直接粘贴的文字）。

你的任务：只做信息搬运与语义对应，把原始资料里**确实存在**的信息填到对应字段上。

必须遵守：
1. 字段名不要求完全一致，按语义匹配。例如"现居地址"对应资料里的"家庭住址（详细）"，"称谓"对应"关系"，"学生姓名"对应"姓名"。
2. 原始资料里没有的信息，**绝不允许编造、推测、补全或套用常识**。没有就把该字段留空——直接不要在 JSON 里出现这个 id。
3. 不确定是否对得上的，也不要在 JSON 里出现。宁缺勿错。
4. 表格里有重复结构时（家庭成员 1/2/3、曾就读学校 1/2/3），按资料里的顺序逐行对应到第 1、2、3 行，不要跨行混填。
5. 单选类字段（kind 为 choice）只能填 options 里给出的选项原文之一；资料没有明确说明就留空。
6. 日期、金额、电话、身份证号等按资料原样照抄，不要改写格式，不要补零，不要加单位。
7. 值里不要带字段名，不要带解释，只给值本身。多行内容用 \n 分隔。
8. 如果资料里有多个人（例如两个孩子），只填与表格主题一致的那一个人的信息；如果你无法判断，把它留空并在 warnings 里说明。
9. 如果原始资料同时给出「按登记表读」和「按两列表格读」两种读法，说明这张表有两种可能的解读：只采用说得通的那一种，另一种是错的，直接忽略。

只输出一个 JSON 对象，格式：
{"fields": {"<字段id>": "<值>"}, "warnings": ["<可选，一两句提醒>"]}
不要输出 JSON 之外的任何文字。`

func userPrompt(tpl templateRecord, material string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "表格名称：%s\n\n可填字段（共 %d 个）：\n", tpl.Name, len(tpl.Fields))
	for _, f := range tpl.Fields {
		fmt.Fprintf(&b, "- id=%s | 名称=%s | 分区=%s", f.ID, f.Label, f.Section)
		if f.Hint != "" {
			fmt.Fprintf(&b, " | 说明=%s", f.Hint)
		}
		if f.Kind == kindChoice {
			fmt.Fprintf(&b, " | 类型=单选 | 可选值=%s", strings.Join(f.Options, "/"))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n原始资料：\n<<<\n")
	b.WriteString(material)
	b.WriteString("\n>>>\n\n请按上面规则输出 JSON。")
	return b.String()
}

func deepseekErrText(payload []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(payload, &e) == nil && e.Error.Message != "" {
		return truncateRunes(e.Error.Message, 300)
	}
	return truncateRunes(strings.TrimSpace(string(payload)), 300)
}

func friendlyNetErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "timeout"):
		return errors.New("请求超时，请稍后重试或减少资料篇幅")
	case strings.Contains(msg, "no such host"):
		return errors.New("无法解析模型服务域名，请检查服务器网络")
	default:
		return err
	}
}

func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}
