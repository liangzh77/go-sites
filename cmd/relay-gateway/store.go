package main

import (
	"database/sql"
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

// store keeps the queue and the results in one SQLite database.
//
// WAL is the point of the choice: readers (health, queue inspection, result
// lookup) never block the writer, and a single writer keeps the hot path
// serialised without a global mutex over every request. The whole relay is one
// box with one local worker, so SQLite is a better fit than a server.
type store struct {
	db      *sql.DB
	writeMu sync.Mutex // one writer at a time; readers stay concurrent
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS tasks (
	id           TEXT PRIMARY KEY,
	body         TEXT NOT NULL,
	meta         TEXT,
	created_at   TEXT NOT NULL,
	delivered_at TEXT,
	attempts     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tasks_claimable ON tasks (delivered_at, created_at);

CREATE TABLE IF NOT EXISTS results (
	id         TEXT PRIMARY KEY,
	status     TEXT NOT NULL,
	summary    TEXT NOT NULL,
	detail     TEXT,
	updated_at TEXT NOT NULL
);
`

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
	// WAL gives us concurrent readers next to the single writer; a handful of
	// connections is plenty and keeps the file handle count small.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(time.Hour)

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	return &store{db: db}, nil
}

func (s *store) close() error { return s.db.Close() }

// enqueue returns created=false when the id is already queued or already
// answered, which makes a retried POST harmless.
func (s *store) enqueue(t task, maxDepth int) (created bool, err error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = ?`, t.ID).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM results WHERE id = ?`, t.ID).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		return false, err
	}
	if n >= maxDepth {
		return false, errQueueFull
	}

	meta := ""
	if len(t.Meta) > 0 {
		meta = string(t.Meta)
	}
	if _, err := tx.Exec(
		`INSERT INTO tasks (id, body, meta, created_at, attempts) VALUES (?, ?, ?, ?, 0)`,
		t.ID, t.Text, meta, formatTime(t.CreatedAt),
	); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

var errQueueFull = errors.New("queue is full")

// claim hands out the oldest task that is new or whose lease has expired.
func (s *store) claim(now time.Time, lease time.Duration) (*task, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	cutoff := formatTime(now.Add(-lease))
	row := tx.QueryRow(
		`SELECT id, body, COALESCE(meta, ''), created_at, attempts
		   FROM tasks
		  WHERE delivered_at IS NULL OR delivered_at < ?
		  ORDER BY created_at, id
		  LIMIT 1`, cutoff)

	var (
		t         task
		meta      string
		createdAt string
	)
	if err := row.Scan(&t.ID, &t.Text, &meta, &createdAt, &t.Attempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if meta != "" {
		t.Meta = json.RawMessage(meta)
	}
	if ts, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		t.CreatedAt = ts
	}

	t.DeliveredAt = now.UTC()
	t.Attempts++
	if _, err := tx.Exec(`UPDATE tasks SET delivered_at = ?, attempts = ? WHERE id = ?`,
		formatTime(t.DeliveredAt), t.Attempts, t.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &t, nil
}

// ack stores the outcome and drops the queued task in one transaction. A
// repeated ack simply rewrites the same result.
func (s *store) ack(r result) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	detail := ""
	if len(r.Detail) > 0 {
		detail = string(r.Detail)
	}
	if _, err := tx.Exec(
		`INSERT INTO results (id, status, summary, detail, updated_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET status = excluded.status,
		                               summary = excluded.summary,
		                               detail = excluded.detail,
		                               updated_at = excluded.updated_at`,
		r.ID, r.Status, r.Summary, detail, formatTime(r.UpdatedAt),
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tasks WHERE id = ?`, r.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *store) result(id string) (*result, error) {
	var (
		r       result
		detail  string
		updated string
	)
	err := s.db.QueryRow(
		`SELECT id, status, summary, COALESCE(detail, ''), updated_at FROM results WHERE id = ?`, id,
	).Scan(&r.ID, &r.Status, &r.Summary, &detail, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if detail != "" {
		r.Detail = json.RawMessage(detail)
	}
	if ts, err := time.Parse(time.RFC3339Nano, updated); err == nil {
		r.UpdatedAt = ts
	}
	return &r, nil
}

type queuedTask struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	DeliveredAt time.Time `json:"deliveredAt,omitempty"`
	Attempts    int       `json:"attempts"`
}

func (s *store) queue() ([]queuedTask, error) {
	rows, err := s.db.Query(
		`SELECT id, created_at, COALESCE(delivered_at, ''), attempts
		   FROM tasks ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]queuedTask, 0)
	for rows.Next() {
		var (
			q         queuedTask
			createdAt string
			delivered string
		)
		if err := rows.Scan(&q.ID, &createdAt, &delivered, &q.Attempts); err != nil {
			return nil, err
		}
		if ts, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			q.CreatedAt = ts
		}
		if delivered != "" {
			if ts, err := time.Parse(time.RFC3339Nano, delivered); err == nil {
				q.DeliveredAt = ts
			}
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *store) count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n)
	return n, err
}

// importLegacyQueue moves tasks from the earlier file-backed queue into the
// database once, so an upgrade never strands work. Files that were already
// answered are ignored; the moved files are renamed so a restart is a no-op.
func (s *store) importLegacyQueue(queueDir string) (int, error) {
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	moved := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		full := filepath.Join(queueDir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		var t task
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}
		if t.CreatedAt.IsZero() {
			t.CreatedAt = time.Now().UTC()
		}
		if _, err := s.enqueue(t, maxQueueDepth); err != nil && !errors.Is(err, errQueueFull) {
			return moved, err
		}
		_ = os.Rename(full, full+".imported")
		moved++
	}
	return moved, nil
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func (s *store) stats() string {
	n, err := s.count()
	if err != nil {
		return "unreadable"
	}
	return fmt.Sprintf("%d", n)
}
