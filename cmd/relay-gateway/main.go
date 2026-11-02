// Command relay-gateway is a small store-and-forward relay between Liang's local
// machine (Paseo / local Pi) and the Grok Bot cloud assistant.
//
// Both sides only ever open outbound connections: the local side long-polls for
// work and posts results, the cloud side posts new work and reads results. No
// inbound access to the local machine is required, so a dynamic home IP is fine.
//
// Endpoints (all under RELAY_BASE_PATH, default /relay):
//
//	GET  /health              liveness + queue depth, no auth
//	POST /to-cloud            local -> cloud: forward a message to the webhook
//	GET  /to-local?wait=30    local -> claim the next queued task (long poll)
//	POST /ack                 local -> report a task's outcome
//	POST /from-cloud          cloud -> enqueue a task for the local side
//	GET  /result/{id}         cloud -> read a task's outcome
//	GET  /queue               cloud -> inspect pending tasks
//
// Authentication is a bearer token, compared in constant time. Two separate
// tokens are used so the local machine never holds the credential that can
// enqueue work, and the cloud never holds the one that can post to the webhook.
package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxBodyBytes    = 256 << 10
	maxWaitSeconds  = 60
	defaultLeaseSec = 600
	maxQueueDepth   = 500
)

// task is one unit of work travelling cloud -> local.
type task struct {
	ID          string          `json:"id"`
	Text        string          `json:"text"`
	Meta        json.RawMessage `json:"meta,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	DeliveredAt time.Time       `json:"deliveredAt,omitempty"`
	Attempts    int             `json:"attempts"`
}

// result is the local side's report for a task it claimed.
type result struct {
	ID        string          `json:"id"`
	Status    string          `json:"status"`
	Summary   string          `json:"summary"`
	Detail    json.RawMessage `json:"detail,omitempty"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,95}$`)

type gateway struct {
	dataDir  string
	localTok string
	cloudTok string
	hookURL  string
	hookTok  string
	lease    time.Duration
	basePath string
	client   *http.Client
	mu       sync.Mutex // guards queue file mutations
	now      func() time.Time
}

func main() {
	basePath := strings.TrimRight(envOr("RELAY_BASE_PATH", "/relay"), "/")
	g := &gateway{
		dataDir:  envOr("RELAY_DATA_DIR", "./relay-data"),
		localTok: os.Getenv("RELAY_LOCAL_TOKEN"),
		cloudTok: os.Getenv("RELAY_CLOUD_TOKEN"),
		hookURL:  os.Getenv("RELAY_WEBHOOK_URL"),
		hookTok:  os.Getenv("RELAY_WEBHOOK_SECRET"),
		lease:    time.Duration(envInt("RELAY_LEASE_SECONDS", defaultLeaseSec)) * time.Second,
		basePath: basePath,
		client:   &http.Client{Timeout: 20 * time.Second},
		now:      time.Now,
	}

	if g.localTok == "" || g.cloudTok == "" {
		log.Fatal("RELAY_LOCAL_TOKEN and RELAY_CLOUD_TOKEN must both be set")
	}
	if g.localTok == g.cloudTok {
		log.Fatal("RELAY_LOCAL_TOKEN and RELAY_CLOUD_TOKEN must differ")
	}
	for _, dir := range []string{g.queueDir(), g.resultDir()} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			log.Fatal(err)
		}
	}

	addr := envOr("RELAY_ADDR", "127.0.0.1:9015")
	srv := &http.Server{
		Addr:              addr,
		Handler:           secureHeaders(g.routes()),
		ReadHeaderTimeout: 10 * time.Second,
		// Long polls hold a connection open, so no global write timeout.
		IdleTimeout: 120 * time.Second,
	}
	log.Printf("relay gateway listening on %s (base path %s)", addr, basePath)
	log.Fatal(srv.ListenAndServe())
}

func (g *gateway) queueDir() string  { return filepath.Join(g.dataDir, "queue") }
func (g *gateway) resultDir() string { return filepath.Join(g.dataDir, "results") }

func (g *gateway) routes() http.Handler {
	p := g.basePath
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+p+"/health", g.handleHealth)
	mux.HandleFunc("POST "+p+"/to-cloud", g.require(g.localTok, g.handleToCloud))
	mux.HandleFunc("GET "+p+"/to-local", g.require(g.localTok, g.handleToLocal))
	mux.HandleFunc("POST "+p+"/ack", g.require(g.localTok, g.handleAck))
	mux.HandleFunc("POST "+p+"/from-cloud", g.require(g.cloudTok, g.handleFromCloud))
	mux.HandleFunc("GET "+p+"/result/", g.require(g.cloudTok, g.handleResult))
	mux.HandleFunc("GET "+p+"/queue", g.require(g.cloudTok, g.handleQueue))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return mux
}

// --- handlers ---------------------------------------------------------------

func (g *gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	tasks, err := g.loadQueue()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "queue unreadable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"queued":  len(tasks),
		"time":    g.now().UTC().Format(time.RFC3339),
		"webhook": g.hookURL != "",
	})
}

// handleToCloud forwards a local message to the Grok Bot webhook. The reply is
// deliberately coarse: the caller only needs to know whether it was accepted.
func (g *gateway) handleToCloud(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID   string          `json:"id"`
		Text string          `json:"text"`
		Meta json.RawMessage `json:"meta,omitempty"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if g.hookURL == "" || g.hookTok == "" {
		writeError(w, http.StatusServiceUnavailable, "webhook is not configured")
		return
	}
	id := in.ID
	if id == "" {
		id = newID()
	}
	if !idPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"source": "relay-gateway",
		"id":     id,
		"text":   in.Text,
		"meta":   rawOrNull(in.Meta),
	})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, g.hookURL, strings.NewReader(string(payload)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot build webhook request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.hookTok)

	resp, err := g.client.Do(req)
	if err != nil {
		log.Printf("to-cloud: webhook delivery failed: %v", err)
		writeError(w, http.StatusBadGateway, "webhook delivery failed")
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		log.Printf("to-cloud: webhook returned %d", resp.StatusCode)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("webhook returned %d", resp.StatusCode))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// handleToLocal is the long poll. It returns the oldest claimable task, or 204
// once the wait expires. A claimed task stays claimable again after the lease,
// so a local crash never strands work.
func (g *gateway) handleToLocal(w http.ResponseWriter, r *http.Request) {
	wait := 0
	if v := r.URL.Query().Get("wait"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid wait")
			return
		}
		if n > maxWaitSeconds {
			n = maxWaitSeconds
		}
		wait = n
	}

	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	for {
		t, err := g.claim()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "queue error")
			return
		}
		if t != nil {
			writeJSON(w, http.StatusOK, t)
			return
		}
		if time.Now().After(deadline) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (g *gateway) handleAck(w http.ResponseWriter, r *http.Request) {
	var in result
	if !decodeJSON(w, r, &in) {
		return
	}
	if !idPattern.MatchString(in.ID) {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	switch in.Status {
	case "done", "failed", "blocked":
	default:
		writeError(w, http.StatusBadRequest, "status must be done, failed or blocked")
		return
	}
	in.UpdatedAt = g.now().UTC()

	g.mu.Lock()
	defer g.mu.Unlock()
	if err := writeFileAtomic(g.resultPath(in.ID), in); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot store result")
		return
	}
	// Removing the queue entry makes the ack idempotent: a repeated ack just
	// rewrites the same result.
	if err := os.Remove(g.taskPath(in.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("ack: cannot remove task %s: %v", in.ID, err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": in.ID})
}

func (g *gateway) handleFromCloud(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID   string          `json:"id"`
		Text string          `json:"text"`
		Meta json.RawMessage `json:"meta,omitempty"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	id := in.ID
	if id == "" {
		id = newID()
	}
	if !idPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Idempotent: the same id never queues twice, so a retried POST from the
	// cloud cannot make the local side run the same work again.
	if _, err := os.Stat(g.taskPath(id)); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "duplicate": true})
		return
	}
	if _, err := os.Stat(g.resultPath(id)); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "duplicate": true})
		return
	}
	existing, err := g.loadQueueLocked()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "queue error")
		return
	}
	if len(existing) >= maxQueueDepth {
		writeError(w, http.StatusInsufficientStorage, "queue is full")
		return
	}
	t := task{ID: id, Text: in.Text, Meta: in.Meta, CreatedAt: g.now().UTC()}
	if err := writeFileAtomic(g.taskPath(id), t); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot queue task")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

func (g *gateway) handleResult(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, g.basePath+"/result/")
	if !idPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	data, err := os.ReadFile(g.resultPath(id))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "no result yet")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read result")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (g *gateway) handleQueue(w http.ResponseWriter, r *http.Request) {
	tasks, err := g.loadQueue()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "queue error")
		return
	}
	type brief struct {
		ID          string    `json:"id"`
		CreatedAt   time.Time `json:"createdAt"`
		DeliveredAt time.Time `json:"deliveredAt,omitempty"`
		Attempts    int       `json:"attempts"`
	}
	out := make([]brief, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, brief{t.ID, t.CreatedAt, t.DeliveredAt, t.Attempts})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

// --- queue ------------------------------------------------------------------

func (g *gateway) taskPath(id string) string   { return filepath.Join(g.queueDir(), id+".json") }
func (g *gateway) resultPath(id string) string { return filepath.Join(g.resultDir(), id+".json") }

// claim returns the oldest task that is either new or whose lease expired.
func (g *gateway) claim() (*task, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tasks, err := g.loadQueueLocked()
	if err != nil {
		return nil, err
	}
	now := g.now().UTC()
	for i := range tasks {
		t := &tasks[i]
		if !t.DeliveredAt.IsZero() && now.Sub(t.DeliveredAt) < g.lease {
			continue // still leased to another worker
		}
		t.DeliveredAt = now
		t.Attempts++
		if err := writeFileAtomic(g.taskPath(t.ID), t); err != nil {
			return nil, err
		}
		return t, nil
	}
	return nil, nil
}

func (g *gateway) loadQueue() ([]task, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.loadQueueLocked()
}

func (g *gateway) loadQueueLocked() ([]task, error) {
	entries, err := os.ReadDir(g.queueDir())
	if err != nil {
		return nil, err
	}
	tasks := make([]task, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(g.queueDir(), e.Name()))
		if err != nil {
			return nil, err
		}
		var t task
		if err := json.Unmarshal(data, &t); err != nil {
			// A corrupt entry must not block every later task.
			log.Printf("queue: skipping unreadable entry %s: %v", e.Name(), err)
			continue
		}
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].ID < tasks[j].ID
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	return tasks, nil
}

// --- helpers ----------------------------------------------------------------

func (g *gateway) require(token string, next http.HandlerFunc) http.HandlerFunc {
	want := []byte(token)
	return func(w http.ResponseWriter, r *http.Request) {
		got := []byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

func writeFileAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func rawOrNull(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func newID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("task-%d", time.Now().UnixNano())
	}
	return "task-" + time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(buf)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
