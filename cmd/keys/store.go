package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const keySchemaSQL = `
CREATE TABLE IF NOT EXISTS providers (
	id             TEXT PRIMARY KEY,
	name           TEXT NOT NULL,
	preset_id      TEXT NOT NULL,
	category       TEXT NOT NULL,
	protocol       TEXT NOT NULL,
	base_url       TEXT NOT NULL,
	auth_mode      TEXT NOT NULL,
	note           TEXT NOT NULL DEFAULT '',
	enabled        INTEGER NOT NULL DEFAULT 1,
	config_rev     INTEGER NOT NULL DEFAULT 1,
	created_at     TEXT NOT NULL,
	updated_at     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS api_keys (
	id                   TEXT PRIMARY KEY,
	provider_id          TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
	alias                TEXT NOT NULL,
	ciphertext           TEXT NOT NULL,
	masked_key           TEXT NOT NULL,
	enabled              INTEGER NOT NULL DEFAULT 1,
	test_model           TEXT NOT NULL DEFAULT '',
	purchase_date        TEXT NOT NULL DEFAULT '',
	expires_on           TEXT NOT NULL DEFAULT '',
	reminder_days        INTEGER NOT NULL DEFAULT 30,
	note                 TEXT NOT NULL DEFAULT '',
	status               TEXT NOT NULL DEFAULT 'untested',
	status_detail        TEXT NOT NULL DEFAULT '',
	tested_model         TEXT NOT NULL DEFAULT '',
	tested_at            TEXT NOT NULL DEFAULT '',
	duration_ms          INTEGER NOT NULL DEFAULT 0,
	http_status          INTEGER NOT NULL DEFAULT 0,
	status_stale         INTEGER NOT NULL DEFAULT 1,
	status_fingerprint   TEXT NOT NULL DEFAULT '',
	status_started_at    TEXT NOT NULL DEFAULT '',
	config_rev           INTEGER NOT NULL DEFAULT 1,
	created_at           TEXT NOT NULL,
	updated_at           TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_api_keys_provider ON api_keys(provider_id, created_at);

CREATE TABLE IF NOT EXISTS models (
	id                 TEXT PRIMARY KEY,
	provider_id        TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
	model_id           TEXT NOT NULL,
	context_window     INTEGER,
	max_output         INTEGER,
	supports_thinking  INTEGER,
	thinking_level     TEXT NOT NULL DEFAULT '',
	thinking_budget    INTEGER,
	test_params        TEXT NOT NULL DEFAULT '{}',
	metadata_json      TEXT NOT NULL DEFAULT '{}',
	default_test       INTEGER NOT NULL DEFAULT 0,
	source             TEXT NOT NULL DEFAULT 'manual',
	created_at         TEXT NOT NULL,
	updated_at         TEXT NOT NULL,
	UNIQUE(provider_id, model_id)
);
CREATE INDEX IF NOT EXISTS idx_models_provider ON models(provider_id, created_at);

CREATE TABLE IF NOT EXISTS detections (
	id             TEXT PRIMARY KEY,
	key_id         TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
	provider_id    TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
	model_id       TEXT NOT NULL,
	protocol       TEXT NOT NULL,
	base_url       TEXT NOT NULL,
	status         TEXT NOT NULL,
	detail         TEXT NOT NULL DEFAULT '',
	http_status    INTEGER NOT NULL DEFAULT 0,
	duration_ms    INTEGER NOT NULL DEFAULT 0,
	fingerprint    TEXT NOT NULL,
	stale          INTEGER NOT NULL DEFAULT 0,
	started_at     TEXT NOT NULL,
	finished_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_detections_key ON detections(key_id, started_at DESC);
`

type keyStore struct {
	db      *sql.DB
	cipher  *secretCipher
	writeMu sync.Mutex
}

func openKeyStore(path string, cipher *secretCipher) (*keyStore, error) {
	if cipher == nil {
		return nil, errors.New("encryption cipher is required")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create data directory: %w", err)
		}
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(time.Hour)
	if _, err := db.Exec(keySchemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	if err := ensureColumn(db, "models", "metadata_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate model metadata: %w", err)
	}
	return &keyStore{db: db, cipher: cipher}, nil
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue any
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

func (s *keyStore) close() error {
	return s.db.Close()
}

func (s *keyStore) listProviders() ([]Provider, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.name, p.preset_id, p.category, p.protocol, p.base_url,
		       p.auth_mode, p.note, p.enabled, p.created_at, p.updated_at, p.config_rev,
		       (SELECT COUNT(*) FROM api_keys k WHERE k.provider_id = p.id)
		  FROM providers p
		 ORDER BY p.created_at, p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	providers := make([]Provider, 0)
	byID := make(map[string]int)
	for rows.Next() {
		var p Provider
		var enabled int
		if err := rows.Scan(
			&p.ID, &p.Name, &p.PresetID, &p.Category, &p.Protocol, &p.BaseURL,
			&p.AuthMode, &p.Note, &enabled, &p.CreatedAt, &p.UpdatedAt, &p.ConfigRev,
			&p.KeyCount,
		); err != nil {
			return nil, err
		}
		p.Enabled = enabled != 0
		p.Keys = []APIKey{}
		p.Models = []ModelConfig{}
		byID[p.ID] = len(providers)
		providers = append(providers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	keyRows, err := s.db.Query(`
		SELECT id, provider_id, alias, masked_key, enabled, test_model, purchase_date,
		       expires_on, reminder_days, note, status, status_detail, tested_model,
		       tested_at, duration_ms, http_status, status_stale, status_fingerprint,
		       status_started_at, created_at, updated_at, config_rev
		  FROM api_keys
		 ORDER BY created_at, alias`)
	if err != nil {
		return nil, err
	}
	defer keyRows.Close()
	for keyRows.Next() {
		k, err := scanAPIKey(keyRows)
		if err != nil {
			return nil, err
		}
		if index, ok := byID[k.ProviderID]; ok {
			providers[index].Keys = append(providers[index].Keys, k)
		}
	}
	if err := keyRows.Err(); err != nil {
		return nil, err
	}

	modelRows, err := s.db.Query(`
		SELECT id, provider_id, model_id, context_window, max_output, supports_thinking,
		       thinking_level, thinking_budget, test_params, metadata_json,
		       default_test, source, updated_at
		  FROM models
		 ORDER BY default_test DESC, model_id`)
	if err != nil {
		return nil, err
	}
	defer modelRows.Close()
	for modelRows.Next() {
		m, err := scanModel(modelRows)
		if err != nil {
			return nil, err
		}
		if index, ok := byID[m.ProviderID]; ok {
			providers[index].Models = append(providers[index].Models, m)
		}
	}
	if err := modelRows.Err(); err != nil {
		return nil, err
	}
	return providers, nil
}

func scanAPIKey(scanner interface{ Scan(...any) error }) (APIKey, error) {
	var (
		k         APIKey
		enabled   int
		stale     int
		reminders int
	)
	err := scanner.Scan(
		&k.ID, &k.ProviderID, &k.Alias, &k.MaskedKey, &enabled, &k.TestModel,
		&k.PurchaseDate, &k.ExpiresOn, &reminders, &k.Note, &k.Status, &k.StatusDetail,
		&k.TestedModel, &k.TestedAt, &k.DurationMS, &k.HTTPStatus, &stale,
		&k.StatusFingerprint, &k.StatusStartedAt, &k.CreatedAt, &k.UpdatedAt, &k.ConfigRev,
	)
	if err != nil {
		return k, err
	}
	k.Enabled = enabled != 0
	k.StatusStale = stale != 0
	k.ReminderDays = reminders
	k.ExpiryDays = expiryDays(k.ExpiresOn)
	return k, nil
}

func scanModel(scanner interface{ Scan(...any) error }) (ModelConfig, error) {
	var (
		m                ModelConfig
		contextWindow    sql.NullInt64
		maxOutput        sql.NullInt64
		supportsThinking sql.NullInt64
		thinkingBudget   sql.NullInt64
		defaultTest      int
		testParams       string
		metadata         string
	)
	err := scanner.Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &contextWindow, &maxOutput, &supportsThinking,
		&m.ThinkingLevel, &thinkingBudget, &testParams, &metadata,
		&defaultTest, &m.Source, &m.UpdatedAt,
	)
	if err != nil {
		return m, err
	}
	if contextWindow.Valid {
		m.ContextWindow = &contextWindow.Int64
	}
	if maxOutput.Valid {
		m.MaxOutput = &maxOutput.Int64
	}
	if supportsThinking.Valid {
		value := supportsThinking.Int64 != 0
		m.SupportsThinking = &value
	}
	if thinkingBudget.Valid {
		m.ThinkingBudget = &thinkingBudget.Int64
	}
	m.DefaultTest = defaultTest != 0
	if json.Valid([]byte(testParams)) {
		m.TestParams = json.RawMessage(testParams)
	} else {
		m.TestParams = json.RawMessage(`{}`)
	}
	if json.Valid([]byte(metadata)) && strings.TrimSpace(metadata) != "" {
		m.Metadata = json.RawMessage(metadata)
	} else {
		m.Metadata = json.RawMessage(`{}`)
	}
	return m, nil
}

func expiryDays(value string) *int {
	if value == "" {
		return nil
	}
	target, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}
	now := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	// Parse dates as UTC civil dates and compare against the current Shanghai
	// civil date, also expressed as UTC midnight. Mixing an instant-relative
	// location with parsed UTC midnight would shift every date by eight hours.
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	days := int(target.Sub(today).Hours() / 24)
	return &days
}

func (s *keyStore) createProvider(input ProviderInput) (Provider, error) {
	normalized, err := normalizeProviderInput(input)
	if err != nil {
		return Provider{}, err
	}
	id, err := newID("p")
	if err != nil {
		return Provider{}, err
	}
	now := nowString()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.db.Exec(`
		INSERT INTO providers
			(id, name, preset_id, category, protocol, base_url, auth_mode, note, enabled,
			 config_rev, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		id, normalized.Name, normalized.PresetID, normalized.Category, normalized.Protocol,
		normalized.BaseURL, normalized.AuthMode, normalized.Note, boolInt(*normalized.Enabled),
		now, now,
	)
	if err != nil {
		return Provider{}, err
	}
	return Provider{
		ID: id, Name: normalized.Name, PresetID: normalized.PresetID,
		Category: normalized.Category, Protocol: normalized.Protocol, BaseURL: normalized.BaseURL,
		AuthMode: normalized.AuthMode, Note: normalized.Note, Enabled: *normalized.Enabled,
		Keys: []APIKey{}, Models: []ModelConfig{}, CreatedAt: now, UpdatedAt: now, ConfigRev: 1,
	}, nil
}

func (s *keyStore) updateProvider(id string, input ProviderInput) (Provider, error) {
	normalized, err := normalizeProviderInput(input)
	if err != nil {
		return Provider{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Provider{}, err
	}
	defer tx.Rollback()
	now := nowString()
	result, err := tx.Exec(`
		UPDATE providers
		   SET name = ?, preset_id = ?, category = ?, protocol = ?, base_url = ?,
		       auth_mode = ?, note = ?, enabled = ?, config_rev = config_rev + 1,
		       updated_at = ?
		 WHERE id = ?`,
		normalized.Name, normalized.PresetID, normalized.Category, normalized.Protocol,
		normalized.BaseURL, normalized.AuthMode, normalized.Note,
		boolInt(*normalized.Enabled), now, id,
	)
	if err != nil {
		return Provider{}, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return Provider{}, errNotFound
	}
	if _, err := tx.Exec(`
		UPDATE api_keys
		   SET config_rev = config_rev + 1, status_stale = 1, updated_at = ?
		 WHERE provider_id = ?`, now, id); err != nil {
		return Provider{}, err
	}
	if err := tx.Commit(); err != nil {
		return Provider{}, err
	}
	return s.providerByID(id)
}

func (s *keyStore) providerByID(id string) (Provider, error) {
	providers, err := s.listProviders()
	if err != nil {
		return Provider{}, err
	}
	for _, provider := range providers {
		if provider.ID == id {
			return provider, nil
		}
	}
	return Provider{}, errNotFound
}

func (s *keyStore) deleteProvider(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	result, err := s.db.Exec(`DELETE FROM providers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

func (s *keyStore) createAPIKey(providerID string, input APIKeyInput) (APIKey, error) {
	normalized, err := normalizeKeyInput(input, true)
	if err != nil {
		return APIKey{}, err
	}
	id, err := newID("k")
	if err != nil {
		return APIKey{}, err
	}
	ciphertext, err := s.cipher.seal(id, *normalized.Secret)
	if err != nil {
		return APIKey{}, err
	}
	now := nowString()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return APIKey{}, err
	}
	defer tx.Rollback()
	if err := requireProvider(tx, providerID); err != nil {
		return APIKey{}, err
	}
	testModel := normalized.TestModel
	if testModel == "" {
		_ = tx.QueryRow(`
			SELECT model_id FROM models
			 WHERE provider_id = ?
			   AND default_test = 1
			 LIMIT 1`, providerID).Scan(&testModel)
	}
	if err := requireModel(tx, providerID, testModel); err != nil {
		return APIKey{}, err
	}
	_, err = tx.Exec(`
		INSERT INTO api_keys
			(id, provider_id, alias, ciphertext, masked_key, enabled, test_model,
			 purchase_date, expires_on, reminder_days, note, status, status_stale,
			 config_rev, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'untested', 1, 1, ?, ?)`,
		id, providerID, normalized.Alias, ciphertext, maskSecret(*normalized.Secret),
		boolInt(*normalized.Enabled), testModel, normalized.PurchaseDate, normalized.ExpiresOn,
		normalized.ReminderDays, normalized.Note, now, now,
	)
	if err != nil {
		return APIKey{}, err
	}
	if err := tx.Commit(); err != nil {
		return APIKey{}, err
	}
	k := APIKey{
		ID: id, ProviderID: providerID, Alias: normalized.Alias,
		MaskedKey: maskSecret(*normalized.Secret), Enabled: *normalized.Enabled,
		TestModel: testModel, PurchaseDate: normalized.PurchaseDate,
		ExpiresOn: normalized.ExpiresOn, ReminderDays: normalized.ReminderDays,
		Note: normalized.Note, Status: statusUntested, StatusStale: true,
		CreatedAt: now, UpdatedAt: now, ConfigRev: 1,
	}
	k.ExpiryDays = expiryDays(k.ExpiresOn)
	return k, nil
}

func (s *keyStore) updateAPIKey(id string, input APIKeyInput) (APIKey, error) {
	normalized, err := normalizeKeyInput(input, false)
	if err != nil {
		return APIKey{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return APIKey{}, err
	}
	defer tx.Rollback()

	var providerID string
	if err := tx.QueryRow(`SELECT provider_id FROM api_keys WHERE id = ?`, id).Scan(&providerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return APIKey{}, errNotFound
		}
		return APIKey{}, err
	}
	if err := requireModel(tx, providerID, normalized.TestModel); err != nil {
		return APIKey{}, err
	}
	now := nowString()
	if normalized.Secret == nil {
		_, err = tx.Exec(`
			UPDATE api_keys
			   SET alias = ?, enabled = ?, test_model = ?, purchase_date = ?, expires_on = ?,
			       reminder_days = ?, note = ?, status_stale = 1,
			       config_rev = config_rev + 1, updated_at = ?
			 WHERE id = ?`,
			normalized.Alias, boolInt(*normalized.Enabled), normalized.TestModel,
			normalized.PurchaseDate, normalized.ExpiresOn, normalized.ReminderDays,
			normalized.Note, now, id,
		)
	} else {
		ciphertext, sealErr := s.cipher.seal(id, *normalized.Secret)
		if sealErr != nil {
			return APIKey{}, sealErr
		}
		_, err = tx.Exec(`
			UPDATE api_keys
			   SET alias = ?, ciphertext = ?, masked_key = ?, enabled = ?, test_model = ?,
			       purchase_date = ?, expires_on = ?, reminder_days = ?, note = ?,
			       status_stale = 1, config_rev = config_rev + 1, updated_at = ?
			 WHERE id = ?`,
			normalized.Alias, ciphertext, maskSecret(*normalized.Secret),
			boolInt(*normalized.Enabled), normalized.TestModel, normalized.PurchaseDate,
			normalized.ExpiresOn, normalized.ReminderDays, normalized.Note, now, id,
		)
	}
	if err != nil {
		return APIKey{}, err
	}
	if err := tx.Commit(); err != nil {
		return APIKey{}, err
	}
	return s.apiKeyByID(id)
}

func (s *keyStore) apiKeyByID(id string) (APIKey, error) {
	row := s.db.QueryRow(`
		SELECT id, provider_id, alias, masked_key, enabled, test_model, purchase_date,
		       expires_on, reminder_days, note, status, status_detail, tested_model,
		       tested_at, duration_ms, http_status, status_stale, status_fingerprint,
		       status_started_at, created_at, updated_at, config_rev
		  FROM api_keys WHERE id = ?`, id)
	k, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, errNotFound
	}
	return k, err
}

func (s *keyStore) deleteAPIKey(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	result, err := s.db.Exec(`DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

func (s *keyStore) createModel(providerID string, input ModelInput) (ModelConfig, error) {
	normalized, err := normalizeModelInput(input)
	if err != nil {
		return ModelConfig{}, err
	}
	id, err := newID("m")
	if err != nil {
		return ModelConfig{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return ModelConfig{}, err
	}
	defer tx.Rollback()
	if err := requireProvider(tx, providerID); err != nil {
		return ModelConfig{}, err
	}
	now := nowString()
	if normalized.DefaultTest {
		if _, err := tx.Exec(`UPDATE models SET default_test = 0 WHERE provider_id = ?`, providerID); err != nil {
			return ModelConfig{}, err
		}
	}
	_, err = tx.Exec(`
		INSERT INTO models
			(id, provider_id, model_id, context_window, max_output, supports_thinking,
			 thinking_level, thinking_budget, test_params, default_test, source,
			 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, providerID, normalized.ModelID, nullableInt(normalized.ContextWindow),
		nullableInt(normalized.MaxOutput), nullableBool(normalized.SupportsThinking),
		normalized.ThinkingLevel, nullableInt(normalized.ThinkingBudget),
		string(normalized.TestParams), boolInt(normalized.DefaultTest), normalized.Source,
		now, now,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ModelConfig{}, errConflict
		}
		return ModelConfig{}, err
	}
	if err := invalidateProviderConfiguration(tx, providerID, now); err != nil {
		return ModelConfig{}, err
	}
	if err := tx.Commit(); err != nil {
		return ModelConfig{}, err
	}
	return s.modelByID(id)
}

func (s *keyStore) updateModel(id string, input ModelInput) (ModelConfig, error) {
	normalized, err := normalizeModelInput(input)
	if err != nil {
		return ModelConfig{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return ModelConfig{}, err
	}
	defer tx.Rollback()
	var providerID, oldModelID string
	if err := tx.QueryRow(`SELECT provider_id, model_id FROM models WHERE id = ?`, id).
		Scan(&providerID, &oldModelID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ModelConfig{}, errNotFound
		}
		return ModelConfig{}, err
	}
	now := nowString()
	if normalized.DefaultTest {
		if _, err := tx.Exec(`UPDATE models SET default_test = 0 WHERE provider_id = ? AND id <> ?`, providerID, id); err != nil {
			return ModelConfig{}, err
		}
	}
	_, err = tx.Exec(`
		UPDATE models
		   SET model_id = ?, context_window = ?, max_output = ?, supports_thinking = ?,
		       thinking_level = ?, thinking_budget = ?, test_params = ?, default_test = ?,
		       source = ?, updated_at = ?
		 WHERE id = ?`,
		normalized.ModelID, nullableInt(normalized.ContextWindow),
		nullableInt(normalized.MaxOutput), nullableBool(normalized.SupportsThinking),
		normalized.ThinkingLevel, nullableInt(normalized.ThinkingBudget),
		string(normalized.TestParams), boolInt(normalized.DefaultTest), normalized.Source,
		now, id,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ModelConfig{}, errConflict
		}
		return ModelConfig{}, err
	}
	if oldModelID != normalized.ModelID {
		if _, err := tx.Exec(
			`UPDATE api_keys SET test_model = ? WHERE provider_id = ? AND test_model = ?`,
			normalized.ModelID, providerID, oldModelID,
		); err != nil {
			return ModelConfig{}, err
		}
	}
	if err := invalidateProviderConfiguration(tx, providerID, now); err != nil {
		return ModelConfig{}, err
	}
	if err := tx.Commit(); err != nil {
		return ModelConfig{}, err
	}
	return s.modelByID(id)
}

func (s *keyStore) modelByID(id string) (ModelConfig, error) {
	row := s.db.QueryRow(`
		SELECT id, provider_id, model_id, context_window, max_output, supports_thinking,
		       thinking_level, thinking_budget, test_params, metadata_json,
		       default_test, source, updated_at
		  FROM models WHERE id = ?`, id)
	m, err := scanModel(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ModelConfig{}, errNotFound
	}
	return m, err
}

func (s *keyStore) deleteModel(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var providerID, modelID string
	if err := tx.QueryRow(`SELECT provider_id, model_id FROM models WHERE id = ?`, id).
		Scan(&providerID, &modelID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		return err
	}
	var references int
	if err := tx.QueryRow(`
		SELECT COUNT(*) FROM api_keys WHERE provider_id = ? AND test_model = ?`,
		providerID, modelID).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return errConflict
	}
	if _, err := tx.Exec(`DELETE FROM models WHERE id = ?`, id); err != nil {
		return err
	}
	if err := invalidateProviderConfiguration(tx, providerID, nowString()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *keyStore) importDiscoveredModels(providerID string, discovered []DiscoveredModel) (ModelImportResult, error) {
	if len(discovered) == 0 {
		return ModelImportResult{}, invalid("models", "没有可导入的模型")
	}
	if len(discovered) > 200 {
		return ModelImportResult{}, invalid("models", "一次最多导入 200 个模型")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return ModelImportResult{}, err
	}
	defer tx.Rollback()
	if err := requireProvider(tx, providerID); err != nil {
		return ModelImportResult{}, err
	}
	var providerPresetID string
	if err := tx.QueryRow(`SELECT preset_id FROM providers WHERE id = ?`, providerID).Scan(&providerPresetID); err != nil {
		return ModelImportResult{}, err
	}

	seen := make(map[string]bool, len(discovered))
	unique := make([]DiscoveredModel, 0, len(discovered))
	for _, item := range discovered {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		unique = append(unique, item)
	}
	if len(unique) == 0 {
		return ModelImportResult{}, invalid("models", "没有有效模型 ID")
	}

	var existingDefault string
	err = tx.QueryRow(`
		SELECT model_id FROM models
		 WHERE provider_id = ? AND default_test = 1
		 LIMIT 1`, providerID).Scan(&existingDefault)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ModelImportResult{}, err
	}
	preferredDefault := existingDefault
	if preferredDefault == "" && providerPresetID == "opencode-go" {
		for _, item := range unique {
			if item.ID == "deepseek-v4.1-flash" {
				preferredDefault = item.ID
				break
			}
		}
	}

	now := nowString()
	result := ModelImportResult{Discovered: len(unique)}
	configurationChanged := false
	for _, item := range unique {
		incomingSource := strings.TrimSpace(item.Source)
		if incomingSource == "" {
			incomingSource = "models.dev"
		}
		input := ModelInput{
			ModelID:          item.ID,
			ContextWindow:    item.ContextWindow,
			MaxOutput:        item.MaxOutput,
			SupportsThinking: item.SupportsThinking,
			ThinkingLevel:    item.ThinkingLevel,
			ThinkingBudget:   item.ThinkingBudget,
			TestParams:       json.RawMessage(`{}`),
			Source:           incomingSource,
		}
		normalized, err := normalizeModelInput(input)
		if err != nil {
			return ModelImportResult{}, err
		}
		metadata := item.Metadata
		if len(metadata) == 0 || !json.Valid(metadata) {
			metadata = json.RawMessage(`{}`)
		}
		if len(metadata) > 64<<10 {
			return ModelImportResult{}, invalid("metadata", "模型元数据过大")
		}

		var (
			existingID       string
			contextWindow    sql.NullInt64
			maxOutput        sql.NullInt64
			supportsThinking sql.NullInt64
			thinkingLevel    string
			thinkingBudget   sql.NullInt64
			existingMetadata string
			defaultTest      int
			existingSource   string
		)
		err = tx.QueryRow(`
			SELECT id, context_window, max_output, supports_thinking, thinking_level,
			       thinking_budget, metadata_json, default_test, source
			  FROM models WHERE provider_id = ? AND model_id = ?`,
			providerID, normalized.ModelID,
		).Scan(
			&existingID, &contextWindow, &maxOutput, &supportsThinking,
			&thinkingLevel, &thinkingBudget, &existingMetadata, &defaultTest, &existingSource,
		)
		if errors.Is(err, sql.ErrNoRows) {
			id, err := newID("m")
			if err != nil {
				return ModelImportResult{}, err
			}
			isDefault := preferredDefault != "" && normalized.ModelID == preferredDefault
			if isDefault {
				if _, err := tx.Exec(`UPDATE models SET default_test = 0 WHERE provider_id = ?`, providerID); err != nil {
					return ModelImportResult{}, err
				}
			}
			_, err = tx.Exec(`
				INSERT INTO models
					(id, provider_id, model_id, context_window, max_output, supports_thinking,
					 thinking_level, thinking_budget, test_params, metadata_json, default_test,
					 source, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, '{}', ?, ?, ?, ?, ?)`,
				id, providerID, normalized.ModelID, nullableInt(normalized.ContextWindow),
				nullableInt(normalized.MaxOutput), nullableBool(normalized.SupportsThinking),
				normalized.ThinkingLevel, nullableInt(normalized.ThinkingBudget),
				string(metadata), boolInt(isDefault), normalized.Source, now, now,
			)
			if err != nil {
				return ModelImportResult{}, err
			}
			result.Created++
			configurationChanged = true
			continue
		}
		if err != nil {
			return ModelImportResult{}, err
		}

		changed := false
		nextContext := contextWindow
		if !nextContext.Valid && normalized.ContextWindow != nil {
			nextContext = sql.NullInt64{Int64: *normalized.ContextWindow, Valid: true}
			changed = true
		}
		nextOutput := maxOutput
		if !nextOutput.Valid && normalized.MaxOutput != nil {
			nextOutput = sql.NullInt64{Int64: *normalized.MaxOutput, Valid: true}
			changed = true
		}
		nextThinking := supportsThinking
		if !nextThinking.Valid && normalized.SupportsThinking != nil {
			nextThinking = sql.NullInt64{Int64: int64(boolInt(*normalized.SupportsThinking)), Valid: true}
			changed = true
		}
		nextThinkingLevel := thinkingLevel
		if strings.TrimSpace(nextThinkingLevel) == "" && normalized.ThinkingLevel != "" {
			nextThinkingLevel = normalized.ThinkingLevel
			changed = true
		}
		nextThinkingBudget := thinkingBudget
		if !nextThinkingBudget.Valid && normalized.ThinkingBudget != nil {
			nextThinkingBudget = sql.NullInt64{Int64: *normalized.ThinkingBudget, Valid: true}
			changed = true
		}
		nextMetadata := existingMetadata
		if string(metadata) != "{}" && nextMetadata != string(metadata) {
			nextMetadata = string(metadata)
			changed = true
		}
		nextSource := existingSource
		if changed {
			switch {
			case existingSource == "" || existingSource == "manual":
				nextSource = "manual+models.dev"
			case existingSource == "provider":
				nextSource = normalized.Source
			}
		}
		nextDefault := defaultTest != 0
		if !nextDefault && existingDefault == "" && preferredDefault != "" && normalized.ModelID == preferredDefault {
			nextDefault = true
			changed = true
		}
		if nextMetadata != existingMetadata || nextSource != existingSource || nextDefault != (defaultTest != 0) {
			changed = true
		}
		if !changed {
			continue
		}
		_, err = tx.Exec(`
			UPDATE models
			   SET context_window = ?, max_output = ?, supports_thinking = ?,
			       thinking_level = ?, thinking_budget = ?, metadata_json = ?,
			       default_test = ?, source = ?, updated_at = ?
			 WHERE id = ?`,
			nextContext, nextOutput, nextThinking, nextThinkingLevel, nextThinkingBudget,
			nextMetadata, boolInt(nextDefault), nextSource, now, existingID,
		)
		if err != nil {
			return ModelImportResult{}, err
		}
		result.Updated++
		configurationChanged = true
	}
	if configurationChanged {
		if err := invalidateProviderConfiguration(tx, providerID, now); err != nil {
			return ModelImportResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ModelImportResult{}, err
	}
	result.Models = make([]ModelConfig, 0, len(unique))
	providerProvider, err := s.providerByID(providerID)
	if err != nil {
		return ModelImportResult{}, err
	}
	result.Models = providerProvider.Models
	return result, nil
}

func (s *keyStore) detectionTarget(keyID string) (DetectionTarget, error) {
	var (
		target      DetectionTarget
		enabled     int
		providerOn  int
		stale       int
		purchase    string
		expires     string
		reminders   int
		keyStatus   string
		keyDetail   string
		testedModel string
		testedAt    string
		duration    int64
		httpStatus  int
		fingerprint string
		statusStart string
		keyCreated  string
		keyUpdated  string
		keyRevision int64
		ciphertext  string
	)
	err := s.db.QueryRow(`
		SELECT k.id, k.provider_id, k.alias, k.masked_key, k.enabled, k.test_model,
		       k.purchase_date, k.expires_on, k.reminder_days, k.note, k.status,
		       k.status_detail, k.tested_model, k.tested_at, k.duration_ms, k.http_status,
		       k.status_stale, k.status_fingerprint, k.status_started_at, k.created_at,
		       k.updated_at, k.config_rev, k.ciphertext,
		       p.id, p.name, p.preset_id, p.category, p.protocol, p.base_url, p.auth_mode,
		       p.note, p.enabled, p.created_at, p.updated_at, p.config_rev
		  FROM api_keys k JOIN providers p ON p.id = k.provider_id
		 WHERE k.id = ?`, keyID,
	).Scan(
		&target.Key.ID, &target.Key.ProviderID, &target.Key.Alias, &target.Key.MaskedKey,
		&enabled, &target.Key.TestModel, &purchase, &expires, &reminders,
		&target.Key.Note, &keyStatus, &keyDetail, &testedModel, &testedAt, &duration,
		&httpStatus, &stale, &fingerprint, &statusStart, &keyCreated, &keyUpdated,
		&keyRevision, &ciphertext,
		&target.Provider.ID, &target.Provider.Name, &target.Provider.PresetID,
		&target.Provider.Category, &target.Provider.Protocol, &target.Provider.BaseURL,
		&target.Provider.AuthMode, &target.Provider.Note, &providerOn,
		&target.Provider.CreatedAt, &target.Provider.UpdatedAt, &target.Provider.ConfigRev,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DetectionTarget{}, errNotFound
	}
	if err != nil {
		return DetectionTarget{}, err
	}
	target.Key.Enabled = enabled != 0
	target.Key.PurchaseDate = purchase
	target.Key.ExpiresOn = expires
	target.Key.ReminderDays = reminders
	target.Key.Status = keyStatus
	target.Key.StatusDetail = keyDetail
	target.Key.TestedModel = testedModel
	target.Key.TestedAt = testedAt
	target.Key.DurationMS = duration
	target.Key.HTTPStatus = httpStatus
	target.Key.StatusStale = stale != 0
	target.Key.StatusFingerprint = fingerprint
	target.Key.StatusStartedAt = statusStart
	target.Key.CreatedAt = keyCreated
	target.Key.UpdatedAt = keyUpdated
	target.Key.ConfigRev = keyRevision
	target.Provider.Enabled = providerOn != 0

	if !target.Key.Enabled {
		return DetectionTarget{}, invalid("key", "Key 已停用")
	}
	if !target.Provider.Enabled {
		return DetectionTarget{}, invalid("provider", "Provider 已停用")
	}
	if target.Key.TestModel == "" {
		return DetectionTarget{}, invalid("testModel", "请先为 Key 选择测试模型")
	}
	modelRow := s.db.QueryRow(`
		SELECT id, provider_id, model_id, context_window, max_output, supports_thinking,
		       thinking_level, thinking_budget, test_params, metadata_json,
		       default_test, source, updated_at
		  FROM models WHERE provider_id = ? AND model_id = ?`,
		target.Provider.ID, target.Key.TestModel)
	model, err := scanModel(modelRow)
	if errors.Is(err, sql.ErrNoRows) {
		return DetectionTarget{}, invalid("testModel", "测试模型配置不存在")
	}
	if err != nil {
		return DetectionTarget{}, err
	}
	target.Model = model
	target.Secret, err = s.cipher.open(target.Key.ID, ciphertext)
	if err != nil {
		return DetectionTarget{}, err
	}
	target.ConfigRev = keyRevision
	target.StartedAt = time.Now().UTC()
	target.Fingerprint = detectionFingerprint(target)
	return target, nil
}

func detectionFingerprint(target DetectionTarget) string {
	payload, _ := json.Marshal(map[string]any{
		"providerRevision": target.Provider.ConfigRev,
		"keyRevision":      target.Key.ConfigRev,
		"baseUrl":          target.Provider.BaseURL,
		"protocol":         target.Provider.Protocol,
		"authMode":         target.Provider.AuthMode,
		"model":            target.Model.ModelID,
		"contextWindow":    target.Model.ContextWindow,
		"maxOutput":        target.Model.MaxOutput,
		"supportsThinking": target.Model.SupportsThinking,
		"thinkingLevel":    target.Model.ThinkingLevel,
		"thinkingBudget":   target.Model.ThinkingBudget,
		"testParams":       target.Model.TestParams,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (s *keyStore) saveDetection(detection Detection) (Detection, error) {
	id, err := newID("d")
	if err != nil {
		return Detection{}, err
	}
	detection.ID = id
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Detection{}, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		UPDATE api_keys
		   SET status = ?, status_detail = ?, tested_model = ?, tested_at = ?,
		       duration_ms = ?, http_status = ?, status_stale = 0,
		       status_fingerprint = ?, status_started_at = ?, updated_at = ?
		 WHERE id = ? AND config_rev = ?
		   AND (status_started_at = '' OR status_started_at <= ?)`,
		detection.Status, detection.Detail, detection.ModelID, detection.FinishedAt,
		detection.DurationMS, detection.HTTPStatus, detection.Fingerprint,
		detection.StartedAt, detection.FinishedAt, detection.KeyID, detection.ConfigRev,
		detection.StartedAt,
	)
	if err != nil {
		return Detection{}, err
	}
	updated, _ := result.RowsAffected()
	detection.Stale = updated == 0
	_, err = tx.Exec(`
		INSERT INTO detections
			(id, key_id, provider_id, model_id, protocol, base_url, status, detail,
			 http_status, duration_ms, fingerprint, stale, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		detection.ID, detection.KeyID, detection.ProviderID, detection.ModelID,
		detection.Protocol, detection.BaseURL, detection.Status, detection.Detail,
		detection.HTTPStatus, detection.DurationMS, detection.Fingerprint,
		boolInt(detection.Stale), detection.StartedAt, detection.FinishedAt,
	)
	if err != nil {
		return Detection{}, err
	}
	_, _ = tx.Exec(`
		DELETE FROM detections
		 WHERE key_id = ?
		   AND id NOT IN (
		       SELECT id FROM detections WHERE key_id = ? ORDER BY started_at DESC LIMIT 200
		   )`, detection.KeyID, detection.KeyID)
	if err := tx.Commit(); err != nil {
		return Detection{}, err
	}
	return detection, nil
}

func (s *keyStore) listDetections(keyID string, limit int) ([]Detection, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT d.id, d.key_id, d.provider_id, p.name, k.alias, d.model_id,
		       d.protocol, d.base_url, d.status, d.detail, d.http_status, d.duration_ms,
		       d.fingerprint, d.stale, d.started_at, d.finished_at
		  FROM detections d
		  JOIN providers p ON p.id = d.provider_id
		  JOIN api_keys k ON k.id = d.key_id
		 WHERE d.key_id = ?
		 ORDER BY d.started_at DESC
		 LIMIT ?`, keyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Detection, 0)
	for rows.Next() {
		var d Detection
		var stale int
		if err := rows.Scan(
			&d.ID, &d.KeyID, &d.ProviderID, &d.ProviderName, &d.KeyAlias, &d.ModelID,
			&d.Protocol, &d.BaseURL, &d.Status, &d.Detail, &d.HTTPStatus, &d.DurationMS,
			&d.Fingerprint, &stale, &d.StartedAt, &d.FinishedAt,
		); err != nil {
			return nil, err
		}
		d.Stale = stale != 0
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *keyStore) revealSecret(keyID string) (string, error) {
	var ciphertext string
	err := s.db.QueryRow(`SELECT ciphertext FROM api_keys WHERE id = ?`, keyID).Scan(&ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	if err != nil {
		return "", err
	}
	return s.cipher.open(keyID, ciphertext)
}

func (s *keyStore) exportKeys(keyIDs []string) ([]ExportKey, error) {
	seen := make(map[string]bool, len(keyIDs))
	ids := make([]string, 0, len(keyIDs))
	for _, id := range keyIDs {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, invalid("keyIds", "至少选择一个 Key")
	}
	if len(ids) > 256 {
		return nil, invalid("keyIds", "一次最多导出 256 个 Key")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.Query(`
		SELECT k.id, k.provider_id, p.name, p.preset_id, p.protocol, p.auth_mode,
		       p.base_url, k.alias, k.test_model, k.ciphertext, p.enabled, k.enabled
		  FROM api_keys k JOIN providers p ON p.id = k.provider_id
		 WHERE k.id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]ExportKey, 0, len(ids))
	for rows.Next() {
		var (
			entry      ExportKey
			ciphertext string
			providerOn int
			keyOn      int
		)
		if err := rows.Scan(
			&entry.KeyID, &entry.ProviderID, &entry.ProviderName, &entry.PresetID,
			&entry.Protocol, &entry.AuthMode, &entry.BaseURL, &entry.KeyAlias,
			&entry.Model, &ciphertext, &providerOn, &keyOn,
		); err != nil {
			return nil, err
		}
		entry.ProviderEnabled = providerOn != 0
		entry.KeyEnabled = keyOn != 0
		entry.Secret, err = s.cipher.open(entry.KeyID, ciphertext)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(entries) != len(ids) {
		return nil, errNotFound
	}
	sortExportKeys(entries)
	return entries, nil
}

func (s *keyStore) providerCredential(keyID string) (Provider, string, error) {
	var (
		provider   Provider
		enabled    int
		keyEnabled int
		ciphertext string
	)
	err := s.db.QueryRow(`
		SELECT p.id, p.name, p.preset_id, p.category, p.protocol, p.base_url,
		       p.auth_mode, p.note, p.enabled, p.created_at, p.updated_at, p.config_rev,
		       k.enabled, k.ciphertext
		  FROM api_keys k JOIN providers p ON p.id = k.provider_id
		 WHERE k.id = ?`, keyID,
	).Scan(
		&provider.ID, &provider.Name, &provider.PresetID, &provider.Category,
		&provider.Protocol, &provider.BaseURL, &provider.AuthMode, &provider.Note,
		&enabled, &provider.CreatedAt, &provider.UpdatedAt, &provider.ConfigRev,
		&keyEnabled, &ciphertext,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Provider{}, "", errNotFound
	}
	if err != nil {
		return Provider{}, "", err
	}
	provider.Enabled = enabled != 0
	if !provider.Enabled {
		return Provider{}, "", invalid("provider", "Provider 已停用")
	}
	if keyEnabled == 0 {
		return Provider{}, "", invalid("key", "Key 已停用")
	}
	secret, err := s.cipher.open(keyID, ciphertext)
	return provider, secret, err
}

func (s *keyStore) detectableKeyIDs(providerID string, enabledOnly bool) ([]string, error) {
	query := `
		SELECT k.id
		  FROM api_keys k JOIN providers p ON p.id = k.provider_id
		 WHERE k.test_model <> '' AND p.enabled = 1`
	args := make([]any, 0, 1)
	if providerID != "" {
		query += " AND k.provider_id = ?"
		args = append(args, providerID)
	}
	if enabledOnly {
		query += " AND k.enabled = 1"
	}
	query += " ORDER BY p.created_at, k.created_at"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *keyStore) backup(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return errors.New("backup path is required")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("backup path already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	_, err := s.db.Exec(`VACUUM INTO ?`, path)
	return err
}

func requireProvider(tx *sql.Tx, providerID string) error {
	var exists int
	err := tx.QueryRow(`SELECT 1 FROM providers WHERE id = ?`, providerID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	return err
}

func requireModel(tx *sql.Tx, providerID, modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		// A Key may be saved before its first model is configured. Detection is
		// blocked later with a concrete validation error.
		return nil
	}
	var exists int
	err := tx.QueryRow(`SELECT 1 FROM models WHERE provider_id = ? AND model_id = ?`,
		providerID, modelID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return invalid("testModel", "测试模型配置不存在")
	}
	return err
}

func invalidateProviderConfiguration(tx *sql.Tx, providerID, now string) error {
	_, err := tx.Exec(`
		UPDATE api_keys
		   SET config_rev = config_rev + 1, status_stale = 1, updated_at = ?
		 WHERE provider_id = ?`, now, providerID)
	return err
}

func nullableInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	return boolInt(*value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func newID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(value), nil
}
