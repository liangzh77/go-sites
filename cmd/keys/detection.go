package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type hostResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type detector struct {
	client   *http.Client
	resolver hostResolver
	sem      chan struct{}
}

var rawSecretPattern = regexp.MustCompile(`[A-Za-z0-9_\-]{24,}`)

func newDetector() *detector {
	resolver := net.DefaultResolver
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	transport.DialContext = safeDialContext(dialer, resolver)
	client := &http.Client{
		Transport: transport,
		Timeout:   detectionTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("provider redirects are blocked to protect credentials")
		},
	}
	return &detector{
		client:   client,
		resolver: resolver,
		sem:      make(chan struct{}, 4),
	}
}

func safeDialContext(dialer *net.Dialer, resolver hostResolver) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid provider address: %w", err)
		}
		host = strings.TrimSuffix(strings.TrimSpace(host), ".")
		if host == "" || strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".local") {
			return nil, errors.New("provider host is not public")
		}

		ips := make([]net.IP, 0)
		if parsed := net.ParseIP(host); parsed != nil {
			ips = append(ips, parsed)
		} else {
			answers, lookupErr := resolver.LookupIPAddr(ctx, host)
			if lookupErr != nil {
				return nil, fmt.Errorf("resolve provider host: %w", lookupErr)
			}
			for _, answer := range answers {
				ips = append(ips, answer.IP)
			}
		}
		if len(ips) == 0 {
			return nil, errors.New("provider host resolved to no addresses")
		}
		sort.SliceStable(ips, func(i, j int) bool {
			left, right := ips[i].To4(), ips[j].To4()
			return left != nil && right == nil
		})
		for _, ip := range ips {
			if !isPublicIP(ip) {
				// Reject the complete DNS answer if any address is private. This
				// prevents a split answer from making the later dial fall back to
				// an internal address.
				return nil, errors.New("provider host resolves to a non-public address")
			}
		}

		var lastErr error
		for _, ip := range ips {
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
}

var blockedIPPrefixes = func() []netip.Prefix {
	values := []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"::/96",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
		"2001:db8::/32",
		"2002::/16",
		"64:ff9b::/96",
	}
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}()

func isPublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() ||
		addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() {
		return false
	}
	for _, prefix := range blockedIPPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return addr.IsGlobalUnicast()
}

func (d *detector) testKey(keyID string, store *keyStore) Detection {
	select {
	case d.sem <- struct{}{}:
		defer func() { <-d.sem }()
	case <-time.After(detectionTimeout + time.Second):
		return Detection{
			KeyID: keyID, Status: statusNetwork, Detail: "检测队列等待超时",
			StartedAt: nowString(), FinishedAt: nowString(),
		}
	}
	target, err := store.detectionTarget(keyID)
	if err != nil {
		return Detection{
			KeyID: keyID, Status: statusUnknown, Detail: safeDetail(err.Error(), ""),
			StartedAt: nowString(), FinishedAt: nowString(),
		}
	}
	result := d.testTarget(context.Background(), target)
	if result.KeyID == "" {
		result.KeyID = target.Key.ID
	}
	result.ProviderID = target.Provider.ID
	result.ProviderName = target.Provider.Name
	result.KeyAlias = target.Key.Alias
	result.ModelID = target.Model.ModelID
	result.Protocol = target.Provider.Protocol
	result.BaseURL = target.Provider.BaseURL
	result.Fingerprint = target.Fingerprint
	result.StartedAt = target.StartedAt.UTC().Format(time.RFC3339Nano)
	result.ConfigRev = target.ConfigRev
	if result.FinishedAt == "" {
		result.FinishedAt = nowString()
	}
	saved, saveErr := store.saveDetection(result)
	if saveErr != nil {
		result.Detail = safeDetail("保存检测结果失败", target.Secret)
		return result
	}
	return saved
}

func (d *detector) testTarget(ctx context.Context, target DetectionTarget) Detection {
	started := time.Now()
	result := Detection{
		KeyID: target.Key.ID, ProviderID: target.Provider.ID, ProviderName: target.Provider.Name,
		KeyAlias: target.Key.Alias, ModelID: target.Model.ModelID,
		Protocol: target.Provider.Protocol, BaseURL: target.Provider.BaseURL,
		Fingerprint: target.Fingerprint, StartedAt: target.StartedAt.UTC().Format(time.RFC3339Nano),
	}
	endpoint, body, err := buildDetectionRequest(target)
	if err != nil {
		result.Status = statusParameters
		result.Detail = safeDetail(err.Error(), target.Secret)
		result.DurationMS = time.Since(started).Milliseconds()
		result.FinishedAt = nowString()
		return result
	}
	requestCtx, cancel := context.WithTimeout(ctx, detectionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		result.Status = statusNetwork
		result.Detail = "无法构造检测请求"
		result.DurationMS = time.Since(started).Milliseconds()
		result.FinishedAt = nowString()
		return result
	}
	applyAuthHeaders(req.Header, target.Provider.AuthMode, target.Secret)
	applyProviderSessionHeaders(req.Header, target.Provider)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "go-sites-keys/1.0")

	resp, err := d.client.Do(req)
	if err != nil {
		result.Status = statusNetwork
		result.Detail = safeDetail(classifyNetworkError(err), target.Secret)
		result.DurationMS = time.Since(started).Milliseconds()
		result.FinishedAt = nowString()
		return result
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, detectionBodyLimit+1))
	if readErr != nil {
		result.Status = statusNetwork
		result.Detail = "读取厂商响应失败"
		result.HTTPStatus = resp.StatusCode
		result.DurationMS = time.Since(started).Milliseconds()
		result.FinishedAt = nowString()
		return result
	}
	if len(data) > detectionBodyLimit {
		data = data[:detectionBodyLimit]
	}
	result.HTTPStatus = resp.StatusCode
	result.Status, result.Detail = classifyDetectionResponse(resp.StatusCode, data, target.Secret)
	result.DurationMS = time.Since(started).Milliseconds()
	result.FinishedAt = nowString()
	return result
}

func buildDetectionRequest(target DetectionTarget) (string, []byte, error) {
	base := strings.TrimRight(target.Provider.BaseURL, "/")
	if base == "" {
		return "", nil, errors.New("Provider 地址为空")
	}
	payload := make(map[string]any)
	if len(target.Model.TestParams) > 0 {
		if err := json.Unmarshal(target.Model.TestParams, &payload); err != nil {
			return "", nil, errors.New("测试参数不是有效 JSON 对象")
		}
	}
	var endpoint string
	switch target.Provider.Protocol {
	case protocolOpenAIChat:
		endpoint = base + "/chat/completions"
		payload["model"] = target.Model.ModelID
		payload["messages"] = []map[string]string{{"role": "user", "content": "Reply with OK."}}
		delete(payload, "max_output_tokens")
		if target.Provider.PresetID == "openai" {
			delete(payload, "max_tokens")
			payload["max_completion_tokens"] = 8
		} else {
			delete(payload, "max_completion_tokens")
			payload["max_tokens"] = 8
		}
	case protocolOpenAIResponses:
		endpoint = base + "/responses"
		payload["model"] = target.Model.ModelID
		payload["input"] = "Reply with OK."
		delete(payload, "max_tokens")
		delete(payload, "max_completion_tokens")
		payload["max_output_tokens"] = 8
	case protocolAnthropic:
		endpoint = base + "/messages"
		payload["model"] = target.Model.ModelID
		payload["messages"] = []map[string]string{{"role": "user", "content": "Reply with OK."}}
		payload["max_tokens"] = 8
	case protocolGemini:
		endpoint = base + "/models/" + url.PathEscape(target.Model.ModelID) + ":generateContent"
		payload["contents"] = []map[string]any{{
			"parts": []map[string]string{{"text": "Reply with OK."}},
		}}
		generationConfig := map[string]any{}
		if raw, ok := payload["generationConfig"]; ok {
			existing, ok := raw.(map[string]any)
			if !ok {
				return "", nil, errors.New("generationConfig 必须是 JSON 对象")
			}
			generationConfig = existing
		}
		generationConfig["maxOutputTokens"] = 8
		payload["generationConfig"] = generationConfig
	case protocolMiniMax:
		endpoint = base + "/chatcompletion_v2"
		payload["model"] = target.Model.ModelID
		payload["messages"] = []map[string]string{{"role": "user", "content": "Reply with OK."}}
		delete(payload, "max_completion_tokens")
		payload["max_tokens"] = 8
	default:
		return "", nil, errors.New("不支持的接口协议")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, err
	}
	return endpoint, body, nil
}

func applyAuthHeaders(header http.Header, authMode, secret string) {
	switch authMode {
	case authBearer:
		header.Set("Authorization", "Bearer "+secret)
	case authAnthropic:
		header.Set("x-api-key", secret)
		header.Set("anthropic-version", "2023-06-01")
	case authGemini:
		header.Set("x-goog-api-key", secret)
	}
}

func applyProviderSessionHeaders(header http.Header, provider Provider) {
	if isOpenCodeGoProvider(provider) {
		// OpenCode Go groups requests by conversation. A detection is one
		// isolated conversation, so it receives its own stable session id.
		header.Set("x-opencode-session", newOpenCodeSessionID())
	}
}

func isOpenCodeGoProvider(provider Provider) bool {
	parsed, err := url.Parse(provider.BaseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "opencode.ai") &&
		strings.HasPrefix(strings.ToLower(parsed.Path), "/inference/go/")
}

func newOpenCodeSessionID() string {
	id, err := newID("ses")
	if err == nil {
		return id
	}
	// Session ids group requests but are not credentials. The timestamp
	// fallback keeps detection available even if the entropy source fails.
	return fmt.Sprintf("ses_%d", time.Now().UnixNano())
}

func classifyDetectionResponse(statusCode int, body []byte, secret string) (string, string) {
	code, detail := extractProviderError(body, secret)
	if statusCode >= 200 && statusCode < 300 {
		if code != "" {
			return statusUnknown, "厂商返回 2xx，但响应包含错误"
		}
		return statusSuccess, "调用成功"
	}
	joined := strings.ToLower(code + " " + detail)
	if statusCode >= 400 && containsAny(joined, []string{
		"quota", "credit", "balance", "insufficient", "余额", "额度", "欠费",
	}) {
		return statusQuota, nonEmpty(detail, fmt.Sprintf("HTTP %d：余额或额度不足", statusCode))
	}
	switch {
	case statusCode == http.StatusUnauthorized:
		return statusAuthFailed, nonEmpty(detail, fmt.Sprintf("HTTP %d：认证失败", statusCode))
	case statusCode == http.StatusPaymentRequired:
		return statusQuota, nonEmpty(detail, fmt.Sprintf("HTTP %d：余额或额度不足", statusCode))
	case statusCode == http.StatusForbidden:
		if containsAny(joined, []string{"model", "模型", "deployment", "endpoint"}) {
			return statusModelDenied, nonEmpty(detail, fmt.Sprintf("HTTP %d：模型不存在或无权限", statusCode))
		}
		return statusAuthFailed, nonEmpty(detail, fmt.Sprintf("HTTP %d：权限被拒绝", statusCode))
	case statusCode == http.StatusTooManyRequests:
		return statusRateLimited, nonEmpty(detail, fmt.Sprintf("HTTP %d：请求受限", statusCode))
	case statusCode == http.StatusNotFound:
		if containsAny(joined, []string{"model", "模型", "deployment"}) {
			return statusModelDenied, nonEmpty(detail, fmt.Sprintf("HTTP %d：模型不存在或无权限", statusCode))
		}
		return statusService, nonEmpty(detail, fmt.Sprintf("HTTP %d：接口地址不存在或服务异常", statusCode))
	case statusCode == http.StatusBadRequest || statusCode == http.StatusUnprocessableEntity:
		if containsAny(joined, []string{"model not found", "model does not", "no access", "permission", "无权限", "模型不存在"}) {
			return statusModelDenied, nonEmpty(detail, fmt.Sprintf("HTTP %d：模型不存在或无权限", statusCode))
		}
		if containsAny(joined, []string{"api key", "api_key", "authentication", "unauthorized", "invalid key", "认证失败"}) {
			return statusAuthFailed, nonEmpty(detail, fmt.Sprintf("HTTP %d：认证失败", statusCode))
		}
		return statusParameters, nonEmpty(detail, fmt.Sprintf("HTTP %d：请求参数不兼容", statusCode))
	case statusCode == http.StatusRequestTimeout || statusCode >= 500:
		return statusService, nonEmpty(detail, fmt.Sprintf("HTTP %d：厂商服务异常", statusCode))
	default:
		return statusUnknown, nonEmpty(detail, fmt.Sprintf("HTTP %d：无法可靠分类", statusCode))
	}
}

func extractProviderError(body []byte, secret string) (string, string) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return "", ""
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return "", safeDetail(truncateText(string(body), 300), secret)
	}
	code, detail := "", ""
	if raw, ok := root["error"]; ok {
		switch value := raw.(type) {
		case string:
			detail = value
		case map[string]any:
			code = firstString(value, "code", "type", "status")
			detail = firstString(value, "message", "msg", "detail")
			if detail == "" {
				detail = code
			}
		}
	}
	if detail == "" {
		detail = firstString(root, "message", "msg", "detail", "error_description")
	}
	if code == "" {
		code = firstString(root, "code", "type", "status")
	}
	return safeDetail(code, secret), safeDetail(truncateText(detail, 300), secret)
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func classifyNetworkError(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "redirect"):
		return "厂商地址返回重定向，已阻止以保护凭据"
	case strings.Contains(message, "non-public"), strings.Contains(message, "not public"):
		return "地址解析到非公网 IP，已阻止检测"
	case strings.Contains(message, "no such host"), strings.Contains(message, "resolve"):
		return "Provider 域名解析失败"
	case strings.Contains(message, "timeout"), strings.Contains(message, "deadline"):
		return "Provider 请求超时"
	case strings.Contains(message, "certificate"), strings.Contains(message, "tls"):
		return "Provider TLS 证书校验失败"
	default:
		return "Provider 网络连接失败"
	}
}

func safeDetail(value, secret string) string {
	value = strings.TrimSpace(value)
	if secret != "" {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	value = rawSecretPattern.ReplaceAllString(value, "[REDACTED]")
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "�")
	}
	return truncateText(value, 300)
}

func truncateText(value string, max int) string {
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return value
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func nonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

type discoveryResult struct {
	Models    []string `json:"models"`
	Truncated bool     `json:"truncated"`
}

func (d *detector) discoverModels(ctx context.Context, provider Provider, secret string) (discoveryResult, error) {
	select {
	case d.sem <- struct{}{}:
		defer func() { <-d.sem }()
	case <-ctx.Done():
		return discoveryResult{}, ctx.Err()
	}
	preset, _ := presetByID(provider.PresetID)
	if !preset.DiscoverModels {
		return discoveryResult{}, errors.New("该 Provider 预设不支持模型列表接口，请手动添加")
	}
	endpoint := strings.TrimRight(provider.BaseURL, "/") + "/models"
	reqCtx, cancel := context.WithTimeout(ctx, detectionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return discoveryResult{}, err
	}
	applyAuthHeaders(req.Header, provider.AuthMode, secret)
	applyProviderSessionHeaders(req.Header, provider)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "go-sites-keys/1.0")
	resp, err := d.client.Do(req)
	if err != nil {
		return discoveryResult{}, errors.New(classifyNetworkError(err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return discoveryResult{}, errors.New("读取模型列表失败")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, detail := extractProviderError(data, secret)
		if detail == "" {
			detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return discoveryResult{}, errors.New("获取模型列表失败：" + detail)
	}
	models := parseModelList(data)
	if len(models) == 0 {
		return discoveryResult{}, errors.New("响应中没有可识别的模型 ID")
	}
	result := discoveryResult{Models: models}
	if len(result.Models) > 500 {
		result.Models = result.Models[:500]
		result.Truncated = true
	}
	return result, nil
}

func parseModelList(data []byte) []string {
	var root struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &root) != nil {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0)
	add := func(id string) {
		id = strings.TrimSpace(strings.TrimPrefix(id, "models/"))
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, item := range root.Data {
		add(item.ID)
	}
	for _, item := range root.Models {
		add(item.Name)
	}
	sort.Strings(out)
	return out
}

// batchDetect limits concurrent network calls while preserving a stable result
// order. A single batch cannot grow beyond a practical personal-use size.
func (d *detector) batchDetect(keyIDs []string, store *keyStore) []Detection {
	if len(keyIDs) > 64 {
		keyIDs = keyIDs[:64]
	}
	results := make([]Detection, len(keyIDs))
	var wg sync.WaitGroup
	workers := 3
	if len(keyIDs) < workers {
		workers = len(keyIDs)
	}
	jobs := make(chan int)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index] = d.testKey(keyIDs[index], store)
			}
		}()
	}
	for index := range keyIDs {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return results
}
