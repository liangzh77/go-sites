package main

import (
	"encoding/json"
	"time"
)

const (
	protocolOpenAIChat      = "openai_chat"
	protocolOpenAIResponses = "openai_responses"
	protocolAnthropic       = "anthropic"
	protocolGemini          = "gemini"
	protocolMiniMax         = "minimax"

	authBearer    = "bearer"
	authAnthropic = "anthropic"
	authGemini    = "gemini"
)

const (
	statusUntested       = "untested"
	statusSuccess        = "success"
	statusAuthFailed     = "auth_failed"
	statusQuota          = "quota"
	statusRateLimited    = "rate_limited"
	statusModelDenied    = "model_permission"
	statusParameters     = "parameters"
	statusNetwork        = "network"
	statusService        = "service"
	statusUnknown        = "unknown"
	statusTesting        = "testing"
	detectionBodyLimit   = 64 << 10
	detectionTimeout     = 25 * time.Second
	maxSecretBytes       = 16 << 10
	maxTestParametersLen = 16 << 10
)

var protocolLabels = map[string]string{
	protocolOpenAIChat:      "OpenAI Chat Completions",
	protocolOpenAIResponses: "OpenAI Responses",
	protocolAnthropic:       "Anthropic Messages",
	protocolGemini:          "Google Gemini",
	protocolMiniMax:         "MiniMax ChatCompletion v2",
}

var authLabels = map[string]string{
	authBearer:    "Authorization: Bearer",
	authAnthropic: "x-api-key + anthropic-version",
	authGemini:    "x-goog-api-key",
}

type ProviderPreset struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Category         string `json:"category"`
	Protocol         string `json:"protocol"`
	BaseURL          string `json:"baseUrl"`
	AuthMode         string `json:"authMode"`
	DiscoverModels   bool   `json:"discoverModels"`
	AutoImportModels bool   `json:"autoImportModels"`
	Documentation    string `json:"documentation"`
	AddressRequired  bool   `json:"addressRequired,omitempty"`
}

type Provider struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	PresetID  string        `json:"presetId"`
	Category  string        `json:"category"`
	Protocol  string        `json:"protocol"`
	BaseURL   string        `json:"baseUrl"`
	AuthMode  string        `json:"authMode"`
	Note      string        `json:"note"`
	Enabled   bool          `json:"enabled"`
	KeyCount  int           `json:"keyCount"`
	Keys      []APIKey      `json:"keys"`
	Models    []ModelConfig `json:"models"`
	CreatedAt string        `json:"createdAt"`
	UpdatedAt string        `json:"updatedAt"`
	ConfigRev int64         `json:"-"`
}

type APIKey struct {
	ID                string           `json:"id"`
	ProviderID        string           `json:"providerId"`
	Alias             string           `json:"alias"`
	MaskedKey         string           `json:"maskedKey"`
	Enabled           bool             `json:"enabled"`
	TestModel         string           `json:"testModel"`
	PurchaseDate      string           `json:"purchaseDate,omitempty"`
	ExpiresOn         string           `json:"expiresOn,omitempty"`
	ReminderDays      int              `json:"reminderDays"`
	Note              string           `json:"note"`
	Status            string           `json:"status"`
	StatusDetail      string           `json:"statusDetail,omitempty"`
	TestedModel       string           `json:"testedModel,omitempty"`
	TestedAt          string           `json:"testedAt,omitempty"`
	DurationMS        int64            `json:"durationMs,omitempty"`
	HTTPStatus        int              `json:"httpStatus,omitempty"`
	StatusStale       bool             `json:"statusStale"`
	Quota             *OpenCodeGoQuota `json:"quota,omitempty"`
	QuotaFetchedAt    string           `json:"quotaFetchedAt,omitempty"`
	QuotaError        string           `json:"quotaError,omitempty"`
	StatusFingerprint string           `json:"-"`
	StatusStartedAt   string           `json:"-"`
	CreatedAt         string           `json:"createdAt"`
	UpdatedAt         string           `json:"updatedAt"`
	ConfigRev         int64            `json:"-"`
	ExpiryDays        *int             `json:"expiryDays,omitempty"`
}

type OpenCodeGoQuota struct {
	Rolling   QuotaMeter `json:"rolling"`
	Weekly    QuotaMeter `json:"weekly"`
	Monthly   QuotaMeter `json:"monthly"`
	FetchedAt string     `json:"fetchedAt"`
}

type QuotaMeter struct {
	UsedUSD      float64 `json:"usedUsd"`
	LimitUSD     float64 `json:"limitUsd"`
	RemainingUSD float64 `json:"remainingUsd"`
	Percent      float64 `json:"percent"`
	ResetsAt     string  `json:"resetsAt"`
	Limited      bool    `json:"limited"`
}

type ModelConfig struct {
	ID               string          `json:"id"`
	ProviderID       string          `json:"providerId"`
	ModelID          string          `json:"modelId"`
	ContextWindow    *int64          `json:"contextWindow,omitempty"`
	MaxOutput        *int64          `json:"maxOutput,omitempty"`
	SupportsThinking *bool           `json:"supportsThinking,omitempty"`
	ThinkingLevel    string          `json:"thinkingLevel,omitempty"`
	ThinkingBudget   *int64          `json:"thinkingBudget,omitempty"`
	TestParams       json.RawMessage `json:"testParams"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	DefaultTest      bool            `json:"defaultTest"`
	Source           string          `json:"source"`
	UpdatedAt        string          `json:"updatedAt"`
}

type DiscoveredModel struct {
	ID               string          `json:"id"`
	Name             string          `json:"name,omitempty"`
	Description      string          `json:"description,omitempty"`
	ContextWindow    *int64          `json:"contextWindow,omitempty"`
	MaxOutput        *int64          `json:"maxOutput,omitempty"`
	SupportsThinking *bool           `json:"supportsThinking,omitempty"`
	ThinkingLevel    string          `json:"thinkingLevel,omitempty"`
	ThinkingBudget   *int64          `json:"thinkingBudget,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	Source           string          `json:"source"`
}

type ModelImportResult struct {
	Discovered int           `json:"discovered"`
	Created    int           `json:"created"`
	Updated    int           `json:"updated"`
	Models     []ModelConfig `json:"models"`
}

type ExportKey struct {
	KeyID           string
	ProviderID      string
	ProviderName    string
	PresetID        string
	Protocol        string
	AuthMode        string
	BaseURL         string
	KeyAlias        string
	Model           string
	Secret          string
	ProviderEnabled bool
	KeyEnabled      bool
}

type Detection struct {
	ID             string           `json:"id"`
	KeyID          string           `json:"keyId"`
	ProviderID     string           `json:"providerId"`
	ProviderName   string           `json:"providerName"`
	KeyAlias       string           `json:"keyAlias"`
	ModelID        string           `json:"modelId"`
	Protocol       string           `json:"protocol"`
	BaseURL        string           `json:"baseUrl"`
	Status         string           `json:"status"`
	Detail         string           `json:"detail,omitempty"`
	HTTPStatus     int              `json:"httpStatus,omitempty"`
	DurationMS     int64            `json:"durationMs"`
	Fingerprint    string           `json:"fingerprint"`
	StartedAt      string           `json:"startedAt"`
	FinishedAt     string           `json:"finishedAt"`
	Stale          bool             `json:"stale"`
	ConfigRev      int64            `json:"-"`
	Quota          *OpenCodeGoQuota `json:"quota,omitempty"`
	QuotaFetchedAt string           `json:"quotaFetchedAt,omitempty"`
	QuotaError     string           `json:"quotaError,omitempty"`
}

type DetectionTarget struct {
	Key         APIKey
	Provider    Provider
	Model       ModelConfig
	Secret      string
	Fingerprint string
	ConfigRev   int64
	StartedAt   time.Time
}

type ProviderInput struct {
	Name     string `json:"name"`
	PresetID string `json:"presetId"`
	Category string `json:"category"`
	Protocol string `json:"protocol"`
	BaseURL  string `json:"baseUrl"`
	AuthMode string `json:"authMode"`
	Note     string `json:"note"`
	Enabled  *bool  `json:"enabled"`
}

type APIKeyInput struct {
	Alias        string  `json:"alias"`
	Secret       *string `json:"secret"`
	Enabled      *bool   `json:"enabled"`
	TestModel    string  `json:"testModel"`
	PurchaseDate string  `json:"purchaseDate"`
	ExpiresOn    string  `json:"expiresOn"`
	ReminderDays int     `json:"reminderDays"`
	Note         string  `json:"note"`
}

type ModelInput struct {
	ModelID          string          `json:"modelId"`
	ContextWindow    *int64          `json:"contextWindow"`
	MaxOutput        *int64          `json:"maxOutput"`
	SupportsThinking *bool           `json:"supportsThinking"`
	ThinkingLevel    string          `json:"thinkingLevel"`
	ThinkingBudget   *int64          `json:"thinkingBudget"`
	TestParams       json.RawMessage `json:"testParams"`
	DefaultTest      bool            `json:"defaultTest"`
	Source           string          `json:"source"`
}

type AppOptions struct {
	Protocols []ProtocolOption `json:"protocols"`
	AuthModes []ProtocolOption `json:"authModes"`
	Presets   []ProviderPreset `json:"presets"`
}

type ProtocolOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type OptionsResponse struct {
	Options AppOptions        `json:"options"`
	Status  map[string]string `json:"statusLabels"`
}

type apiErrorEnvelope struct {
	Error apiErrorBody `json:"error"`
}

type apiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
