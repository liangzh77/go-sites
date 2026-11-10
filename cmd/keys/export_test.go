package main

import (
	"strings"
	"testing"
)

func TestBuildExportTextProducesUniqueEnvPrefixes(t *testing.T) {
	text := buildExportText([]ExportKey{
		{
			KeyID: "k_1", ProviderID: "p_1", ProviderName: "火山方舟",
			PresetID: "volcengine", Protocol: protocolOpenAIChat, AuthMode: authBearer,
			BaseURL: "https://ark.example.com/api/v3", KeyAlias: "主账号",
			Model: "doubao-pro", Secret: "secret-one",
			ProviderEnabled: true, KeyEnabled: true,
		},
		{
			KeyID: "k_2", ProviderID: "p_1", ProviderName: "火山方舟",
			PresetID: "volcengine", Protocol: protocolOpenAIChat, AuthMode: authBearer,
			BaseURL: "https://ark.example.com/api/v3", KeyAlias: "backup",
			Model: "doubao-lite", Secret: "secret-two",
			ProviderEnabled: true, KeyEnabled: true,
		},
	})
	for _, want := range []string{
		"VOLCENGINE_K_1_BASE_URL=https://ark.example.com/api/v3",
		"VOLCENGINE_K_1_API_KEY=secret-one",
		"VOLCENGINE_BACKUP_API_KEY=secret-two",
		"VOLCENGINE_BACKUP_MODEL=doubao-lite",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("export text missing %q:\n%s", want, text)
		}
	}
}

func TestBuildExportTextQuotesUnsafeValues(t *testing.T) {
	text := buildExportText([]ExportKey{{
		KeyID: "k_1", ProviderID: "p_1", ProviderName: "自定义",
		PresetID: "custom", Protocol: protocolOpenAIChat, AuthMode: authBearer,
		BaseURL: "https://example.com/v1", KeyAlias: "main",
		Model: "model", Secret: "contains space#and-hash",
		ProviderEnabled: true, KeyEnabled: true,
	}})
	if !strings.Contains(text, `CUSTOM_API_KEY="contains space#and-hash"`) {
		t.Fatalf("unsafe value was not quoted:\n%s", text)
	}
}
