package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *keyStore {
	t.Helper()
	store, err := openKeyStore(filepath.Join(t.TempDir(), "keys.db"), testCipher(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	return store
}

func seedKey(t *testing.T, store *keyStore) (Provider, ModelConfig, APIKey) {
	t.Helper()
	enabled := true
	provider, err := store.createProvider(ProviderInput{
		Name: "Mock Provider", PresetID: "custom", Category: "测试",
		Protocol: protocolOpenAIChat, BaseURL: "https://api.example.com/v1",
		AuthMode: authBearer, Enabled: &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := store.createModel(provider.ID, ModelInput{
		ModelID: "mock-model", DefaultTest: true, Source: "manual",
		TestParams: json.RawMessage(`{"temperature":0}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	secret := "sk-test-super-secret-value"
	key, err := store.createAPIKey(provider.ID, APIKeyInput{
		Alias: "Primary", Secret: &secret, Enabled: &enabled,
		TestModel: model.ModelID, ReminderDays: 30,
		ExpiresOn: time.Now().AddDate(0, 1, 0).Format("2006-01-02"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider, model, key
}

func TestStorePersistsEncryptedSecretsAndBackup(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "keys.db")
	cipher := testCipher(t)
	store, err := openKeyStore(path, cipher)
	if err != nil {
		t.Fatal(err)
	}
	provider, model, key := seedKey(t, store)
	if provider.ID == "" || model.ID == "" || key.ID == "" {
		t.Fatal("seed records have empty IDs")
	}

	var ciphertext string
	if err := store.db.QueryRow(`SELECT ciphertext FROM api_keys WHERE id = ?`, key.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, "sk-test-super-secret-value") || !strings.HasPrefix(ciphertext, "v1:") {
		t.Fatalf("stored ciphertext = %q", ciphertext)
	}
	providers, err := store.listProviders()
	if err != nil || len(providers) != 1 || len(providers[0].Keys) != 1 {
		t.Fatalf("listProviders = %+v, %v", providers, err)
	}
	if got := providers[0].Keys[0].MaskedKey; strings.Contains(got, "super-secret") {
		t.Fatalf("list response leaked secret: %q", got)
	}
	if secret, err := store.revealSecret(key.ID); err != nil || secret != "sk-test-super-secret-value" {
		t.Fatalf("revealSecret = %q, %v", secret, err)
	}

	backup := filepath.Join(root, "backup.db")
	if err := store.backup(backup); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(backup); err != nil || info.Size() == 0 {
		t.Fatalf("backup stat = %v, %v", info, err)
	}
	restored, err := openKeyStore(backup, cipher)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	if secret, err := restored.revealSecret(key.ID); err != nil || secret != "sk-test-super-secret-value" {
		t.Fatalf("secret from restored backup = %q, %v", secret, err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := openKeyStore(path, cipher)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	if secret, err := reopened.revealSecret(key.ID); err != nil || secret != "sk-test-super-secret-value" {
		t.Fatalf("secret after restart = %q, %v", secret, err)
	}
}

func TestConfigurationChangeRejectsOlderDetectionResult(t *testing.T) {
	store := testStore(t)
	provider, model, key := seedKey(t, store)
	target := mustDetectionTarget(t, store, key.ID)

	updated, err := store.updateModel(model.ID, ModelInput{
		ModelID: model.ModelID, ContextWindow: int64Ptr(128000),
		DefaultTest: true, Source: "manual", TestParams: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = provider
	_ = updated

	old := detectionFromTarget(target, statusSuccess)
	saved, err := store.saveDetection(old)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Stale {
		t.Fatal("old detection result was not marked stale")
	}
	current, err := store.apiKeyByID(key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != statusUntested || !current.StatusStale {
		t.Fatalf("configuration was overwritten: %+v", current)
	}

	newTarget := mustDetectionTarget(t, store, key.ID)
	fresh := detectionFromTarget(newTarget, statusSuccess)
	saved, err = store.saveDetection(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Stale {
		t.Fatal("fresh detection result was marked stale")
	}
	current, err = store.apiKeyByID(key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != statusSuccess || current.StatusStale {
		t.Fatalf("fresh status not applied: %+v", current)
	}
}

func TestDeleteModelRefusesAKeyReference(t *testing.T) {
	store := testStore(t)
	_, model, _ := seedKey(t, store)
	if err := store.deleteModel(model.ID); !errors.Is(err, errConflict) {
		t.Fatalf("deleteModel error = %v, want conflict", err)
	}
}

func TestExpiryDaysUsesShanghaiCivilDate(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	tomorrow := time.Now().In(location).AddDate(0, 0, 1).Format("2006-01-02")
	days := expiryDays(tomorrow)
	if days == nil || *days != 1 {
		t.Fatalf("expiryDays(%s) = %v, want 1", tomorrow, days)
	}
}

func TestImportDiscoveredModelsEnrichesAndIsIdempotent(t *testing.T) {
	store := testStore(t)
	provider, existing, key := seedKey(t, store)
	thinking := true
	discovered := []DiscoveredModel{
		{
			ID: existing.ModelID, Name: "Mock Official", ContextWindow: int64Ptr(1000000),
			MaxOutput: int64Ptr(128000), SupportsThinking: &thinking,
			ThinkingLevel: "low high max", Metadata: json.RawMessage(`{"name":"Mock Official","tool_call":true}`),
			Source: "models.dev",
		},
		{
			ID: "deepseek-v4.1-flash", Name: "DeepSeek V4.1 Flash",
			ContextWindow: int64Ptr(1000000), MaxOutput: int64Ptr(384000),
			SupportsThinking: &thinking, ThinkingLevel: "low high max",
			Metadata: json.RawMessage(`{"name":"DeepSeek V4.1 Flash","tool_call":true}`),
			Source:   "models.dev",
		},
	}
	result, err := store.importDiscoveredModels(provider.ID, discovered)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 || result.Updated != 1 || len(result.Models) != 2 {
		t.Fatalf("first import result = %+v", result)
	}
	enriched := modelByModelID(result.Models, existing.ModelID)
	if enriched == nil || enriched.ContextWindow == nil || *enriched.ContextWindow != 1000000 ||
		enriched.MaxOutput == nil || *enriched.MaxOutput != 128000 ||
		enriched.SupportsThinking == nil || !*enriched.SupportsThinking ||
		enriched.ThinkingLevel != "low high max" || len(enriched.Metadata) == 0 {
		t.Fatalf("enriched model = %+v", enriched)
	}
	refreshedKey, err := store.apiKeyByID(key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !refreshedKey.StatusStale {
		t.Fatal("model metadata import did not invalidate the existing detection")
	}

	second, err := store.importDiscoveredModels(provider.ID, discovered)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created != 0 || second.Updated != 0 {
		t.Fatalf("second import should be idempotent, got %+v", second)
	}
}

func modelByModelID(models []ModelConfig, modelID string) *ModelConfig {
	for index := range models {
		if models[index].ModelID == modelID {
			return &models[index]
		}
	}
	return nil
}

func mustDetectionTarget(t *testing.T, store *keyStore, keyID string) DetectionTarget {
	t.Helper()
	target, err := store.detectionTarget(keyID)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func detectionFromTarget(target DetectionTarget, status string) Detection {
	started := time.Now().UTC()
	return Detection{
		KeyID: target.Key.ID, ProviderID: target.Provider.ID,
		ProviderName: target.Provider.Name, KeyAlias: target.Key.Alias,
		ModelID: target.Model.ModelID, Protocol: target.Provider.Protocol,
		BaseURL: target.Provider.BaseURL, Status: status, Detail: "调用成功",
		HTTPStatus: 200, DurationMS: 12, Fingerprint: target.Fingerprint,
		StartedAt:  started.Format(time.RFC3339Nano),
		FinishedAt: started.Add(12 * time.Millisecond).Format(time.RFC3339Nano),
		ConfigRev:  target.ConfigRev,
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
