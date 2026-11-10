package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	errNotFound = errors.New("record not found")
	errConflict = errors.New("record conflict")
)

type validationError struct {
	Field   string
	Message string
}

func (e *validationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

func invalid(field, message string) error {
	return &validationError{Field: field, Message: message}
}

func normalizeProviderInput(input ProviderInput) (ProviderInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.PresetID = strings.TrimSpace(input.PresetID)
	input.Category = strings.TrimSpace(input.Category)
	input.Protocol = strings.TrimSpace(input.Protocol)
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.AuthMode = strings.TrimSpace(input.AuthMode)
	input.Note = strings.TrimSpace(input.Note)

	if input.PresetID == "" {
		input.PresetID = "custom"
	}
	if preset, ok := presetByID(input.PresetID); ok {
		if input.Category == "" {
			input.Category = preset.Category
		}
		if input.Protocol == "" {
			input.Protocol = preset.Protocol
		}
		if input.BaseURL == "" {
			input.BaseURL = strings.TrimRight(preset.BaseURL, "/")
		}
		if input.AuthMode == "" {
			input.AuthMode = preset.AuthMode
		}
	}

	if input.Name == "" {
		return input, invalid("name", "显示名称不能为空")
	}
	if err := validateLength("name", input.Name, 120); err != nil {
		return input, err
	}
	if _, ok := protocolLabels[input.Protocol]; !ok {
		return input, invalid("protocol", "不支持的接口协议")
	}
	if _, ok := authLabels[input.AuthMode]; !ok {
		return input, invalid("authMode", "不支持的认证方式")
	}
	if input.Category == "" {
		input.Category = "自定义"
	}
	if err := validateLength("category", input.Category, 80); err != nil {
		return input, err
	}
	if err := validateLength("note", input.Note, 4000); err != nil {
		return input, err
	}
	if err := validateBaseURL(input.BaseURL); err != nil {
		return input, err
	}
	if input.Enabled == nil {
		enabled := true
		input.Enabled = &enabled
	}
	return input, nil
}

func normalizeKeyInput(input APIKeyInput, creating bool) (APIKeyInput, error) {
	input.Alias = strings.TrimSpace(input.Alias)
	input.TestModel = strings.TrimSpace(input.TestModel)
	input.PurchaseDate = strings.TrimSpace(input.PurchaseDate)
	input.ExpiresOn = strings.TrimSpace(input.ExpiresOn)
	input.Note = strings.TrimSpace(input.Note)

	if input.Alias == "" {
		return input, invalid("alias", "Key 别名不能为空")
	}
	if err := validateLength("alias", input.Alias, 160); err != nil {
		return input, err
	}
	if err := validateLength("testModel", input.TestModel, 300); err != nil {
		return input, err
	}
	if err := validateLength("note", input.Note, 4000); err != nil {
		return input, err
	}
	if creating && (input.Secret == nil || strings.TrimSpace(*input.Secret) == "") {
		return input, invalid("secret", "API 密钥不能为空")
	}
	if input.Secret != nil {
		trimmed := strings.TrimSpace(*input.Secret)
		if creating && trimmed == "" {
			return input, invalid("secret", "API 密钥不能为空")
		}
		if len(trimmed) > maxSecretBytes {
			return input, invalid("secret", "API 密钥过长")
		}
		input.Secret = &trimmed
	}
	if input.ReminderDays < 0 || input.ReminderDays > 3650 {
		return input, invalid("reminderDays", "提前提醒天数应在 0 到 3650 之间")
	}
	for field, value := range map[string]string{
		"purchaseDate": input.PurchaseDate,
		"expiresOn":    input.ExpiresOn,
	} {
		if value == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return input, invalid(field, "日期格式必须为 YYYY-MM-DD")
		}
	}
	if input.Enabled == nil {
		enabled := true
		input.Enabled = &enabled
	}
	return input, nil
}

func normalizeModelInput(input ModelInput) (ModelInput, error) {
	input.ModelID = strings.TrimSpace(input.ModelID)
	input.ThinkingLevel = strings.TrimSpace(input.ThinkingLevel)
	input.Source = strings.TrimSpace(input.Source)
	if input.ModelID == "" {
		return input, invalid("modelId", "模型 ID 不能为空")
	}
	if err := validateLength("modelId", input.ModelID, 300); err != nil {
		return input, err
	}
	if err := validateLength("thinkingLevel", input.ThinkingLevel, 80); err != nil {
		return input, err
	}
	if input.Source == "" {
		input.Source = "manual"
	}
	if err := validateLength("source", input.Source, 200); err != nil {
		return input, err
	}
	if input.ContextWindow != nil && *input.ContextWindow <= 0 {
		return input, invalid("contextWindow", "上下文上限必须大于 0")
	}
	if input.MaxOutput != nil && *input.MaxOutput <= 0 {
		return input, invalid("maxOutput", "最大输出必须大于 0")
	}
	if input.ContextWindow != nil && input.MaxOutput != nil && *input.MaxOutput > *input.ContextWindow {
		return input, invalid("maxOutput", "最大输出不能大于上下文上限")
	}
	if input.ThinkingBudget != nil && *input.ThinkingBudget <= 0 {
		return input, invalid("thinkingBudget", "思考预算必须大于 0")
	}
	if input.SupportsThinking != nil && !*input.SupportsThinking &&
		(input.ThinkingLevel != "" || input.ThinkingBudget != nil) {
		return input, invalid("thinkingLevel", "模型不支持思考时不能填写思考参数")
	}
	if len(input.TestParams) == 0 {
		input.TestParams = json.RawMessage(`{}`)
	}
	if len(input.TestParams) > maxTestParametersLen {
		return input, invalid("testParams", "测试参数不能超过 16KB")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input.TestParams, &object); err != nil {
		return input, invalid("testParams", "测试参数必须是 JSON 对象")
	}
	if object == nil || bytes.Equal(bytes.TrimSpace(input.TestParams), []byte("null")) {
		return input, invalid("testParams", "测试参数必须是 JSON 对象")
	}
	for _, reserved := range []string{"model", "messages", "input", "contents", "stream"} {
		if _, exists := object[reserved]; exists {
			return input, invalid("testParams", "测试参数不能覆盖 "+reserved)
		}
	}
	return input, nil
}

func validateBaseURL(raw string) error {
	if raw == "" {
		return invalid("baseUrl", "API 地址不能为空")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return invalid("baseUrl", "API 地址格式无效")
	}
	if parsed.Scheme != "https" {
		return invalid("baseUrl", "API 地址必须使用 HTTPS")
	}
	if parsed.Hostname() == "" {
		return invalid("baseUrl", "API 地址缺少主机名")
	}
	if parsed.User != nil {
		return invalid("baseUrl", "API 地址不能包含用户名或密码")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return invalid("baseUrl", "API 地址不能包含查询参数或片段")
	}
	if parsed.Path == "/" {
		parsed.Path = ""
	}
	if parsed.Path != "" && !strings.HasPrefix(parsed.Path, "/") {
		return invalid("baseUrl", "API 地址路径无效")
	}
	if err := validateLength("baseUrl", raw, 2048); err != nil {
		return err
	}
	return nil
}

func validateLength(field, value string, max int) error {
	if !utf8.ValidString(value) {
		return invalid(field, "必须为有效 UTF-8 文本")
	}
	if utf8.RuneCountInString(value) > max {
		return invalid(field, fmt.Sprintf("不能超过 %d 个字符", max))
	}
	return nil
}
