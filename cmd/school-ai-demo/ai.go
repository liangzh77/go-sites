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
	// small cap yields an empty content string. A full 51-field form needs
	// 836-1897 tokens of content, and reasoning can outgrow that; 8192 is
	// accepted by the gateway (16384 too) and leaves headroom without giving a
	// runaway reply room to burn the whole timeout.
	maxTokens = 8192
	userAgent = "school-ai-demo/1.0"

	// closeTruncatedJSON is a last-resort repair and each candidate costs a
	// full parse; a garbage reply can offer one cut point per byte, so only the
	// last few are worth trying.
	maxJSONCutAttempts = 32
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

// errTruncated is the gateway saying it stopped at max_tokens (finish_reason
// "length"). The content is a fragment of a JSON object, so it is reported as
// its own condition: not an empty reply, and not a parse failure. The fix the
// user can actually act on is to send less material.
var errTruncated = errors.New("模型输出被长度限制截断，请把资料拆成更短的一段再试")

// errNoUsableFields is a reply that parsed but named no field this template
// has, which is a model problem worth retrying rather than an empty form.
var errNoUsableFields = errors.New("模型没有返回任何可用的字段，请重试")

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
			// "length" means the server cut the reply at max_tokens, so content
			// is a fragment even when it looks like JSON.
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &completion); err != nil {
		return fillResult{}, resp.StatusCode, fmt.Errorf("解析模型响应失败: %w", err)
	}
	if len(completion.Choices) == 0 {
		return fillResult{}, resp.StatusCode, errors.New("模型没有返回内容")
	}
	choice := completion.Choices[0]

	// Report the cutoff before touching the body, and before the empty-content
	// check: a reasoning-heavy reply can be both empty and truncated, and
	// "split your material" is then the truthful advice. The repair pass in
	// parseFillJSON is skipped on purpose: it would close the half-written
	// object, silently drop everything after the cut, and hand back a
	// partially filled form that looks like a complete answer.
	if choice.FinishReason == "length" {
		return fillResult{}, resp.StatusCode, errTruncated
	}
	if strings.TrimSpace(choice.Message.Content) == "" {
		// Reasoning-only answers come back as an empty string; say so instead
		// of reporting an empty but successful fill.
		return fillResult{}, resp.StatusCode, errEmptyContent
	}
	res, err := parseFillJSON(tpl, choice.Message.Content, c.model)
	return res, resp.StatusCode, err
}

// parseFillJSON keeps only usable answers: known ids, string values, non-empty.
//
// Reasoning models occasionally emit JSON that is nearly right: a stray brace
// inside a key ({"fields{": ...}), a trailing comma, or the object wrapped in
// prose. None of those should surface as a hard failure, so each parse attempt
// falls back to the next repair before giving up.
//
// A body cut off mid-value is repairable the same way, but callers that know
// why it was cut (finish_reason "length") should refuse this repair: the
// recovered fragment would pass for a complete answer.
func parseFillJSON(tpl templateRecord, content, model string) (fillResult, error) {
	text := strings.TrimSpace(content)
	if text == "" {
		return fillResult{}, errEmptyContent
	}

	known := make(map[string]bool, len(tpl.Fields))
	for _, f := range tpl.Fields {
		known[f.ID] = true
	}

	// Take the candidate that recognises the most fields, not merely the first
	// one json.Unmarshal accepts. A reply with a stray brace in the key
	// ({"fields{": ...}) is perfectly valid JSON whose only key is meaningless;
	// stopping there would answer "no fields found" for a form the model did
	// fill, and the user would see a blank sheet with no reason why.
	var best map[string]any
	var bestText string
	bestScore := -1
	var parseErr error
	for _, cand := range jsonCandidates(text) {
		var raw map[string]any
		if err := json.Unmarshal([]byte(cand), &raw); err != nil {
			if parseErr == nil {
				parseErr = err
			}
			continue
		}
		fields := fieldMapFrom(raw)
		score := 0
		for k := range fields {
			if known[strings.TrimSpace(k)] {
				score++
			}
		}
		if score > bestScore {
			best, bestText, bestScore = raw, cand, score
		}
	}
	if best == nil {
		return fillResult{}, fmt.Errorf("模型返回的不是有效 JSON: %w", parseErr)
	}
	if bestScore == 0 && hasKey(best, "fields") == false && hasKey(best, "values") == false {
		// A bare object that names no field of this template: not an answer.
		return fillResult{}, errNoUsableFields
	}

	var envelope struct {
		Fields   map[string]any `json:"fields"`
		Warnings []string       `json:"warnings"`
		Values   map[string]any `json:"values"`
	}
	_ = json.Unmarshal([]byte(bestText), &envelope)
	values := fieldMapFrom(best)

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
		v, ok := values[k].(string)
		if !ok {
			if values[k] == nil {
				continue
			}
			v = fmt.Sprint(values[k])
		}
		v = strings.TrimSpace(v)
		if v == "" || strings.EqualFold(v, "null") || v == "未知" || v == "不详" {
			continue
		}
		out.Values[id] = truncateRunes(v, maxValueRunes)
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

// fieldMapFrom pulls the id -> value map out of whichever envelope shape the
// model used: {"fields": {...}}, {"values": {...}}, or a bare {"f01": "..."}.
func fieldMapFrom(raw map[string]any) map[string]any {
	for _, key := range []string{"fields", "values"} {
		if inner, ok := raw[key].(map[string]any); ok {
			return inner
		}
	}
	return raw
}

// jsonCandidates returns the shapes worth trying, in order of how little they
// change the model's answer. parseFillJSON takes the first that parses.
//
// They are separate candidates rather than one string rewritten in place
// because a repair aimed at one problem can damage a reply that had only a
// different one. Replacing Chinese quotation marks is the case that bit: the
// model writes them inside warning text ("资料中的“联系电话”…"), and a blanket
// replacement turned a valid object into an unterminated string.
func jsonCandidates(text string) []string {
	out := []string{text}

	// Wrapped in a code fence, possibly with a language tag.
	fenced := text
	if i := strings.Index(fenced, "```"); i >= 0 {
		rest := fenced[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			rest = rest[:j]
		}
		fenced = strings.TrimSpace(rest)
		if fenced != text {
			out = append(out, fenced)
		}
	}

	// Prose before the first brace and after the matching one.
	trimmed := fenced
	if i := strings.IndexByte(trimmed, '{'); i > 0 {
		trimmed = trimmed[i:]
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed != "" && trimmed != fenced {
		out = append(out, trimmed)
	}
	if obj := outermostObject(trimmed); obj != "" && obj != trimmed {
		out = append(out, obj)
	}

	// The near-misses themselves.
	base := trimmed
	if base == "" {
		base = fenced
	}
	swapped := strings.NewReplacer(
		"\"fields{\":", "\"fields\":",
		"\"values{\":", "\"values\":",
		"\"warnings{\":", "\"warnings\":",
	).Replace(base)
	if swapped != base {
		out = append(out, swapped)
	}
	for _, seed := range []string{base, swapped} {
		if noCommas := dropTrailingCommas(seed); noCommas != seed {
			out = append(out, noCommas)
		}
		// Smart quotes only as a last resort, and only outside strings.
		if q := straightenQuotes(seed); q != seed {
			out = append(out, q, dropTrailingCommas(q))
		}
	}

	// Lowest priority: a body that stops mid-way. Closing it recovers the
	// fields written before the cut, and because candidates are scored by how
	// many template fields they name, a fragment can never beat an answer that
	// the model actually finished.
	if closed := closeTruncatedJSON(base); closed != "" && closed != base {
		out = append(out, closed)
	}
	return out
}

// closeTruncatedJSON finishes a JSON object that was cut off: the tail is
// dropped back to the last point where a value ended, and the brackets still
// open there are closed LIFO. It returns s unchanged when the object already
// balances, and "" when no cut point yields a non-empty JSON object.
//
// The cut almost always lands inside a string value ("f24":"1381111), and that
// half-written value is dropped rather than guessed at: a truncated phone
// number or address in a submitted form is worse than a blank field.
func closeTruncatedJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s[0] != '{' {
		return ""
	}

	// Cut points are the byte offsets just past something that can end a
	// value: a closing quote, a closing bracket, or the last character of a
	// number/true/false/null. A value cut mid-string has no closing quote, so
	// it is never a candidate and its fragment is dropped.
	var cuts []int
	depth := 0
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
				cuts = append(cuts, i+1)
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth < 0 {
				// More closers than openers is not a truncation; leave it to the
				// ordinary repairs rather than inventing a prefix.
				return ""
			}
			cuts = append(cuts, i+1)
		case ' ', '\n', '\r', '\t', ',', ':':
			// Not a value end on its own.
		default:
			cuts = append(cuts, i+1)
		}
	}
	if !inString && depth == 0 {
		return s // Already balanced, so there is nothing to close.
	}

	// Newest cut first: the shortest repair that parses wins, keeping as many
	// complete pairs as possible.
	if len(cuts) > maxJSONCutAttempts {
		cuts = cuts[len(cuts)-maxJSONCutAttempts:]
	}
	for i := len(cuts) - 1; i >= 0; i-- {
		prefix := s[:cuts[i]]
		cand := prefix + closeBrackets(prefix)
		var probe map[string]any
		if json.Unmarshal([]byte(cand), &probe) == nil && len(probe) > 0 {
			return cand
		}
	}
	return ""
}

// closeBrackets returns the closers that balance the brackets left open in
// prefix, innermost first. It returns "" when prefix is already balanced.
func closeBrackets(prefix string) string {
	var stack []byte
	inString, escaped := false, false
	for i := 0; i < len(prefix); i++ {
		c := prefix[i]
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
		case '{', '[':
			stack = append(stack, c)
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(stack) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(stack))
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '[' {
			b.WriteByte(']')
		} else {
			b.WriteByte('}')
		}
	}
	return b.String()
}

// straightenQuotes rewrites curly double quotes (U+201C, U+201D, U+201E) as
// straight ones, but only where a quote is opening or closing a JSON string.
// Curly quotes inside a value are content and stay put: the model writes
// warnings like 资料中的“联系电话”… and mangling those is what turned a valid
// answer into "invalid character" in production.
func straightenQuotes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var open rune // the quote that opened the string we are in, 0 if none
	escaped := false
	for _, r := range s {
		switch {
		case open == 0:
			switch r {
			case '"':
				open = '"'
				b.WriteByte('"')
			case '\u201c', '\u201d', '\u201e':
				open = '\u201c'
				b.WriteByte('"')
			default:
				b.WriteRune(r)
			}
		case escaped:
			escaped = false
			b.WriteRune(r)
		case r == '\\':
			escaped = true
			b.WriteRune(r)
		case open == '"' && r == '"':
			open = 0
			b.WriteByte('"')
		case open == '\u201c' && (r == '\u201d' || r == '\u201c'):
			open = 0
			b.WriteByte('"')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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
