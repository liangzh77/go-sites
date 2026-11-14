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
	"strconv"
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
	if isOpenCodeGoProvider(target.Provider) {
		quotaCtx, cancel := context.WithTimeout(context.Background(), detectionTimeout)
		quota, quotaErr := d.fetchOpenCodeGoQuota(quotaCtx, target.Provider, target.Secret)
		cancel()
		if quotaErr != nil {
			result.QuotaError = safeDetail(quotaErr.Error(), target.Secret)
		} else {
			result.Quota = quota
			result.QuotaFetchedAt = quota.FetchedAt
			result.QuotaError = ""
		}
	}
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

func (d *detector) fetchOpenCodeGoQuota(ctx context.Context, provider Provider, secret string) (*OpenCodeGoQuota, error) {
	if !isOpenCodeGoProvider(provider) {
		return nil, errors.New("该 Provider 不是 OpenCode Go")
	}
	requestCtx, cancel := context.WithTimeout(ctx, detectionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodGet,
		"https://opencode.ai/console/api/go/status",
		nil,
	)
	if err != nil {
		return nil, err
	}
	applyAuthHeaders(req.Header, provider.AuthMode, secret)
	applyProviderSessionHeaders(req.Header, provider)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "go-sites-keys/1.0")
	response, err := d.client.Do(req)
	if err != nil {
		return nil, errors.New(classifyNetworkError(err))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, errors.New("读取 OpenCode Go 额度失败")
	}
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return nil, errors.New("OpenCode Go 额度认证失败")
	case http.StatusForbidden:
		return nil, errors.New("该 Key 没有 Usage: Read 权限")
	case http.StatusNotFound:
		return nil, errors.New("该 Key 未关联 OpenCode Go 套餐")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenCode Go 额度接口返回 HTTP %d", response.StatusCode)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("解析 OpenCode Go 额度失败")
	}
	access := jsonObject(payload["access"])
	meters := jsonObject(access["meters"])
	rolling, ok := parseQuotaMeter(meters["fiveHour"])
	if !ok {
		return nil, errors.New("OpenCode Go 额度缺少 5 小时窗口")
	}
	weekly, ok := parseQuotaMeter(meters["week"])
	if !ok {
		return nil, errors.New("OpenCode Go 额度缺少周窗口")
	}
	monthly, ok := parseQuotaMeter(meters["month"])
	if !ok {
		return nil, errors.New("OpenCode Go 额度缺少月窗口")
	}
	return &OpenCodeGoQuota{
		Rolling: rolling, Weekly: weekly, Monthly: monthly,
		FetchedAt: nowString(),
	}, nil
}

func parseQuotaMeter(raw json.RawMessage) (QuotaMeter, bool) {
	fields := jsonObject(raw)
	if fields == nil {
		return QuotaMeter{}, false
	}
	limitMicroCents := jsonInt64(fields["limitMicroCents"])
	usedMicroCents := jsonInt64(fields["usedMicroCents"])
	if limitMicroCents == nil || usedMicroCents == nil || *limitMicroCents <= 0 {
		return QuotaMeter{}, false
	}
	limitUSD := float64(*limitMicroCents) / 100_000_000
	usedUSD := float64(*usedMicroCents) / 100_000_000
	remaining := limitUSD - usedUSD
	if remaining < 0 {
		remaining = 0
	}
	percent := usedUSD / limitUSD * 100
	percent = float64(int64(percent*100+0.5)) / 100
	return QuotaMeter{
		UsedUSD: usedUSD, LimitUSD: limitUSD, RemainingUSD: remaining,
		Percent: percent, ResetsAt: jsonString(fields["resetsAt"]),
		Limited: usedUSD >= limitUSD,
	}, true
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
	Models         []DiscoveredModel `json:"models"`
	Truncated      bool              `json:"truncated"`
	MetadataSource string            `json:"metadataSource,omitempty"`
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
	modelIDs := parseModelList(data)
	if len(modelIDs) == 0 {
		return discoveryResult{}, errors.New("响应中没有可识别的模型 ID")
	}
	models := make([]DiscoveredModel, 0, len(modelIDs))
	metadataSource := "provider"
	if isVolcengineProvider(provider) {
		models = parseVolcengineModels(data)
		if len(models) == 0 {
			for _, id := range modelIDs {
				models = append(models, DiscoveredModel{ID: id, Source: "provider"})
			}
		}
		metadataSource = "volcengine.models"
	} else if isOpenCodeGoProvider(provider) {
		catalog, err := d.fetchOpenCodeCatalog(ctx)
		if err != nil {
			return discoveryResult{}, err
		}
		byID := make(map[string]DiscoveredModel, len(catalog))
		for _, model := range catalog {
			byID[model.ID] = model
		}
		for _, id := range modelIDs {
			if model, ok := byID[id]; ok {
				models = append(models, model)
			} else {
				models = append(models, DiscoveredModel{ID: id, Source: "provider"})
			}
		}
		metadataSource = "models.dev"
	} else {
		for _, id := range modelIDs {
			models = append(models, DiscoveredModel{ID: id, Source: "provider"})
		}
	}
	result := discoveryResult{Models: models, MetadataSource: metadataSource}
	if len(result.Models) > 500 {
		result.Models = result.Models[:500]
		result.Truncated = true
	}
	return result, nil
}

func (d *detector) discoverProviderCatalog(ctx context.Context, provider Provider) (discoveryResult, error) {
	if !isOpenCodeGoProvider(provider) {
		return discoveryResult{}, errors.New("该 Provider 需要先添加启用的 Key 才能获取模型")
	}
	select {
	case d.sem <- struct{}{}:
		defer func() { <-d.sem }()
	case <-ctx.Done():
		return discoveryResult{}, ctx.Err()
	}
	models, err := d.fetchOpenCodeCatalog(ctx)
	if err != nil {
		return discoveryResult{}, err
	}
	return discoveryResult{Models: models, MetadataSource: "models.dev"}, nil
}

type modelsDevProvider struct {
	Models map[string]json.RawMessage `json:"models"`
}

func (d *detector) fetchOpenCodeCatalog(ctx context.Context) ([]DiscoveredModel, error) {
	const endpoint = "https://models.dev/api.json?type=all"
	requestCtx, cancel := context.WithTimeout(ctx, detectionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "go-sites-keys/1.0")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, errors.New("获取 OpenCode 官方模型目录失败：" + classifyNetworkError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("获取 OpenCode 官方模型目录失败：HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, errors.New("读取 OpenCode 官方模型目录失败")
	}
	var catalog map[string]modelsDevProvider
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, errors.New("解析 OpenCode 官方模型目录失败")
	}
	provider, ok := catalog["opencode-go"]
	if !ok {
		return nil, errors.New("OpenCode 官方模型目录中没有 opencode-go")
	}
	models := make([]DiscoveredModel, 0, len(provider.Models))
	for catalogID, rawModel := range provider.Models {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawModel, &fields); err != nil {
			continue
		}
		modelID := jsonString(fields["id"])
		if modelID == "" {
			modelID = catalogID
		}
		if strings.EqualFold(jsonString(fields["status"]), "deprecated") || strings.TrimSpace(modelID) == "" {
			continue
		}
		limitFields := jsonObject(fields["limit"])
		contextValue := jsonInt64(limitFields["context"])
		outputValue := jsonInt64(limitFields["output"])
		var contextWindow, maxOutput *int64
		if contextValue != nil && *contextValue > 0 {
			contextWindow = contextValue
		}
		if outputValue != nil && *outputValue > 0 {
			maxOutput = outputValue
		}
		reasoning := jsonBool(fields["reasoning"])
		thinkingLevel := ""
		var thinkingBudget *int64
		parts := make([]string, 0)
		if reasoning != nil && *reasoning {
			var options []json.RawMessage
			_ = json.Unmarshal(fields["reasoning_options"], &options)
			for _, optionRaw := range options {
				option := jsonObject(optionRaw)
				switch jsonString(option["type"]) {
				case "effort":
					if values := jsonStringList(option["values"]); values != "" {
						parts = append(parts, "effort: "+values)
					}
				case "toggle":
					parts = append(parts, "toggle")
				case "budget_tokens":
					if maxValue := jsonInt64(option["max"]); maxValue != nil && *maxValue > 0 {
						thinkingBudget = maxValue
						parts = append(parts, fmt.Sprintf("budget_tokens max %d", *maxValue))
					} else {
						parts = append(parts, "budget_tokens")
					}
				}
			}
		}
		thinkingLevel = truncateText(strings.Join(parts, "; "), 80)
		models = append(models, DiscoveredModel{
			ID: modelID, Name: jsonString(fields["name"]), Description: jsonString(fields["description"]),
			ContextWindow: contextWindow, MaxOutput: maxOutput,
			SupportsThinking: reasoning, ThinkingLevel: thinkingLevel,
			ThinkingBudget: thinkingBudget, Metadata: append(json.RawMessage(nil), rawModel...),
			Source: "models.dev",
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	if len(models) == 0 {
		return nil, errors.New("OpenCode 官方模型目录为空")
	}
	return models, nil
}

func jsonObject(raw json.RawMessage) map[string]json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value map[string]json.RawMessage
	_ = json.Unmarshal(raw, &value)
	return value
}

func jsonString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return strings.TrimSpace(value)
}

func jsonBool(raw json.RawMessage) *bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return nil
		}
		parsed, err := strconv.ParseBool(strings.TrimSpace(text))
		if err != nil {
			return nil
		}
		value = parsed
	}
	return &value
}

func jsonInt64(raw json.RawMessage) *int64 {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value int64
	if json.Unmarshal(raw, &value) == nil {
		return &value
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		parsed, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func jsonStringList(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	var values []string
	if json.Unmarshal(raw, &values) == nil {
		return strings.Join(values, " ")
	}
	return ""
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

func isVolcengineProvider(provider Provider) bool {
	if provider.PresetID == "volcengine" {
		return true
	}
	parsed, err := url.Parse(provider.BaseURL)
	return err == nil && strings.EqualFold(parsed.Hostname(), "ark.cn-beijing.volces.com")
}

func parseVolcengineModels(data []byte) []DiscoveredModel {
	var root struct {
		Data []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &root) != nil {
		return nil
	}
	models := make([]DiscoveredModel, 0, len(root.Data))
	for _, rawModel := range root.Data {
		fields := jsonObject(rawModel)
		modelID := jsonString(fields["id"])
		apiSupported := jsonObject(fields["api_supported"])
		chatSupported := jsonBool(apiSupported["chat/completions"])
		if modelID == "" || chatSupported == nil || !*chatSupported {
			continue
		}
		contextWindow := jsonInt64(fields["context_window"])
		maxOutput := jsonInt64(fields["max_output_tokens"])
		tokenLimits := jsonObject(fields["token_limits"])
		if contextWindow == nil {
			contextWindow = jsonInt64(tokenLimits["context_window"])
		}
		if maxOutput == nil {
			maxOutput = jsonInt64(tokenLimits["max_output_token_length"])
		}

		effort := jsonObject(fields["effort"])
		effortLevels := jsonStringList(effort["supported_levels"])
		defaultLevel := jsonString(effort["default_level"])
		features := jsonObject(fields["features"])
		thinking := jsonObject(features["thinking"])
		thinkingTypes := jsonStringList(thinking["types"])
		supportsThinking := (*bool)(nil)
		if effortLevels != "" || thinkingTypes != "" {
			value := true
			supportsThinking = &value
		}
		levelParts := make([]string, 0, 2)
		if effortLevels != "" {
			description := effortLevels
			if defaultLevel != "" {
				description += " (default " + defaultLevel + ")"
			}
			levelParts = append(levelParts, "effort: "+description)
		}
		if thinkingTypes != "" {
			levelParts = append(levelParts, "thinking: "+thinkingTypes)
		}

		models = append(models, DiscoveredModel{
			ID:               modelID,
			Name:             jsonString(fields["name"]),
			ContextWindow:    contextWindow,
			MaxOutput:        maxOutput,
			SupportsThinking: supportsThinking,
			ThinkingLevel:    truncateText(strings.Join(levelParts, "; "), 80),
			Metadata:         append(json.RawMessage(nil), rawModel...),
			Source:           "volcengine.models",
		})
	}
	return models
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
