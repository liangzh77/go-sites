package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// store keeps templates, fill sessions and their answers in one SQLite file.
//
// Same shape as the relay gateway's store: WAL so reads never block the writer,
// and one mutex around writes because the write volume here is a couple of rows
// per user action, not a hot path.
type store struct {
	db *sql.DB
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS templates (
	id           TEXT PRIMARY KEY,
	slug         TEXT NOT NULL UNIQUE,
	name         TEXT NOT NULL,
	source_file  TEXT NOT NULL,
	html         TEXT NOT NULL,
	fields_json  TEXT NOT NULL,
	position     INTEGER NOT NULL DEFAULT 0,
	created_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	id           TEXT PRIMARY KEY,
	template_id  TEXT NOT NULL REFERENCES templates(id),
	input_kind   TEXT NOT NULL,
	input_name   TEXT NOT NULL,
	input_text   TEXT NOT NULL,
	status       TEXT NOT NULL,
	values_json  TEXT NOT NULL,
	warnings     TEXT NOT NULL DEFAULT '',
	model        TEXT NOT NULL DEFAULT '',
	error        TEXT NOT NULL DEFAULT '',
	created_at   TEXT NOT NULL,
	updated_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_created ON sessions (created_at DESC);
`

const (
	statusPending = "pending"
	statusFilled  = "filled"
	statusFailed  = "failed"
	// statusManual marks a session the user filled in themselves, with no model
	// call; the export path treats it the same as a filled one.
	statusManual = "manual"
	statusNoKey  = "no_key"
)

func openStore(path string) (*store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
	}
	dsn := "file:" + path +
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
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	// A database created before a column existed keeps its old shape, and
	// CREATE TABLE IF NOT EXISTS will not add it. Add what is missing; any
	// error here means the column is already there.
	for _, stmt := range []string{
		`ALTER TABLE templates ADD COLUMN position INTEGER NOT NULL DEFAULT 0`,
	} {
		_, _ = db.Exec(stmt)
	}
	return &store{db: db}, nil
}

func (s *store) close() error { return s.db.Close() }

// templateRecord is a template as stored, with its parsed fields.
type templateRecord struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Source    string    `json:"sourceFile"`
	HTML      string    `json:"html,omitempty"`
	Fields    []field   `json:"fields"`
	CreatedAt time.Time `json:"createdAt"`
}

// saveTemplate upserts by slug, so restarting the service with an updated
// template file refreshes the stored copy instead of duplicating it.
func (s *store) saveTemplate(t *parsedTemplate, slug, source string, position int) (templateRecord, error) {
	fieldsJSON, err := json.Marshal(t.Fields)
	if err != nil {
		return templateRecord{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`
		INSERT INTO templates (id, slug, name, source_file, html, fields_json, position, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(slug) DO UPDATE SET
			name = excluded.name,
			source_file = excluded.source_file,
			html = excluded.html,
			position = excluded.position,
			fields_json = excluded.fields_json`,
		slug, slug, t.Name, source, t.HTML, string(fieldsJSON), position, now); err != nil {
		return templateRecord{}, fmt.Errorf("save template %s: %w", slug, err)
	}
	return s.template(slug)
}

func (s *store) template(slug string) (templateRecord, error) {
	var rec templateRecord
	var fieldsJSON, createdAt string
	err := s.db.QueryRow(`
		SELECT id, slug, name, source_file, html, fields_json, created_at
		FROM templates WHERE slug = ?`, slug).
		Scan(&rec.ID, &rec.Slug, &rec.Name, &rec.Source, &rec.HTML, &fieldsJSON, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return templateRecord{}, errNotFound
	}
	if err != nil {
		return templateRecord{}, err
	}
	if err := json.Unmarshal([]byte(fieldsJSON), &rec.Fields); err != nil {
		return templateRecord{}, fmt.Errorf("decode template fields: %w", err)
	}
	rec.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return rec, nil
}

func (s *store) templates() ([]templateRecord, error) {
	rows, err := s.db.Query(`SELECT slug FROM templates ORDER BY position, slug`)
	if err != nil {
		return nil, err
	}
	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			rows.Close()
			return nil, err
		}
		slugs = append(slugs, slug)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]templateRecord, 0, len(slugs))
	for _, slug := range slugs {
		rec, err := s.template(slug)
		if err != nil {
			return nil, err
		}
		rec.HTML = "" // the list view never needs the document
		out = append(out, rec)
	}
	return out, nil
}

// fillSession is one "fill this template from this material" attempt.
type fillSession struct {
	ID         string            `json:"id"`
	TemplateID string            `json:"templateId"`
	Template   string            `json:"template"`
	InputKind  string            `json:"inputKind"`
	InputName  string            `json:"inputName"`
	Status     string            `json:"status"`
	Values     map[string]string `json:"values"`
	Warnings   []string          `json:"warnings"`
	Model      string            `json:"model"`
	Error      string            `json:"error,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

func (s *store) createSession(tpl templateRecord, kind, name, text string) (fillSession, error) {
	sess := fillSession{
		ID:         newID("s"),
		TemplateID: tpl.ID,
		Template:   tpl.Slug,
		InputKind:  kind,
		InputName:  name,
		Status:     statusPending,
		Values:     map[string]string{},
		Warnings:   []string{},
	}
	now := time.Now().UTC()
	sess.CreatedAt, sess.UpdatedAt = now, now

	// The material is stored for the audit trail, but a fill only needs it
	// during the request that created it; keeping the prompt-sized text lets a
	// later retry work without the user re-uploading.
	if _, err := s.db.Exec(`
		INSERT INTO sessions (id, template_id, input_kind, input_name, input_text, status,
			values_json, warnings, model, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, '{}', '', '', '', ?, ?)`,
		sess.ID, sess.TemplateID, kind, name, text, sess.Status,
		now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		return fillSession{}, err
	}
	return sess, nil
}

func (s *store) sessionInput(id string) (string, error) {
	var text string
	err := s.db.QueryRow(`SELECT input_text FROM sessions WHERE id = ?`, id).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	return text, err
}

func (s *store) finishSession(id string, values map[string]string, warnings []string, model, status, errText string) error {
	valuesJSON, err := json.Marshal(values)
	if err != nil {
		return err
	}
	warningsJSON, err := json.Marshal(warnings)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		UPDATE sessions
		SET status = ?, values_json = ?, warnings = ?, model = ?, error = ?, updated_at = ?
		WHERE id = ?`,
		status, string(valuesJSON), string(warningsJSON), model, errText,
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *store) saveSession(id string, values map[string]string) (fillSession, error) {
	valuesJSON, err := json.Marshal(values)
	if err != nil {
		return fillSession{}, err
	}
	res, err := s.db.Exec(`UPDATE sessions SET values_json = ?, updated_at = ? WHERE id = ?`,
		string(valuesJSON), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fillSession{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fillSession{}, errNotFound
	}
	return s.session(id)
}

func (s *store) session(id string) (fillSession, error) {
	var sess fillSession
	var valuesJSON, warningsJSON, createdAt, updatedAt string
	var tplName string
	err := s.db.QueryRow(`
		SELECT s.id, s.template_id, t.slug, s.input_kind, s.input_name, s.status,
			s.values_json, s.warnings, s.model, s.error, s.created_at, s.updated_at
		FROM sessions s JOIN templates t ON t.id = s.template_id
		WHERE s.id = ?`, id).
		Scan(&sess.ID, &sess.TemplateID, &tplName, &sess.InputKind, &sess.InputName,
			&sess.Status, &valuesJSON, &warningsJSON, &sess.Model, &sess.Error,
			&createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fillSession{}, errNotFound
	}
	if err != nil {
		return fillSession{}, err
	}
	sess.Template = tplName
	if err := json.Unmarshal([]byte(valuesJSON), &sess.Values); err != nil {
		return fillSession{}, fmt.Errorf("decode session values: %w", err)
	}
	if sess.Values == nil {
		sess.Values = map[string]string{}
	}
	if err := json.Unmarshal([]byte(warningsJSON), &sess.Warnings); err != nil {
		sess.Warnings = []string{}
	}
	if sess.Warnings == nil {
		sess.Warnings = []string{}
	}
	sess.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	sess.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return sess, nil
}

// createManualSession opens an empty session for a form the user fills in by
// hand. Without it a page opened without an API key could be edited and printed
// but never exported, because export reads values back from a stored session.
func (s *store) createManualSession(tpl templateRecord, values map[string]string) (fillSession, error) {
	sess, err := s.createSession(tpl, "manual", "手动填写", "")
	if err != nil {
		return fillSession{}, err
	}
	if len(values) > 0 {
		if _, err := s.saveSession(sess.ID, values); err != nil {
			return fillSession{}, err
		}
	}
	if err := s.finishSession(sess.ID, values, nil, "", statusManual, ""); err != nil {
		return fillSession{}, err
	}
	return s.session(sess.ID)
}

// recentSessions powers the "最近填写" list on the demo page.
func (s *store) recentSessions(limit int) ([]fillSession, error) {
	rows, err := s.db.Query(`
		SELECT id FROM sessions ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]fillSession, 0, len(ids))
	for _, id := range ids {
		sess, err := s.session(id)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, nil
}

var errNotFound = errors.New("not found")
