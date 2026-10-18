package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// A bounded test inbox, not an attribution or billing service. Callback callers
// are untrusted; do not store their IP, headers, cookies or query parameters.
const monitorLimit = 256

var monitorRun = regexp.MustCompile(`^chuangliang-[0-9]{8}-[a-z0-9]{6,24}$`)

type monitorEvent struct {
	ReceivedAt time.Time `json:"receivedAt"`
	Run        string    `json:"run"`
	Event      string    `json:"event"`
}

func (a *app) loadMonitorEvents() ([]monitorEvent, error) {
	file := filepath.Join(a.dataDir, "monitor-events.json")
	info, err := os.Stat(file)
	if errors.Is(err, os.ErrNotExist) {
		return []monitorEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Size() > 256<<10 {
		return nil, fmt.Errorf("monitor inbox too large")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var events []monitorEvent
	if err = json.Unmarshal(data, &events); err != nil {
		return nil, err
	}
	if len(events) > monitorLimit {
		return nil, fmt.Errorf("monitor inbox exceeds limit")
	}
	return events, nil
}

func recentMonitorEvents(events []monitorEvent, now time.Time) []monitorEvent {
	recent := make([]monitorEvent, 0, len(events))
	for _, event := range events {
		if event.ReceivedAt.After(now.Add(-7 * 24 * time.Hour)) {
			recent = append(recent, event)
		}
	}
	return recent
}

func (a *app) handleMonitorCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	run, event := r.PathValue("run"), r.PathValue("event")
	if !monitorRun.MatchString(run) || (event != "impression" && event != "click" && event != "video-start" && event != "video-finish") {
		http.NotFound(w, r)
		return
	}
	if len(r.URL.RawQuery) > 8192 {
		http.Error(w, "query too long", http.StatusRequestURITooLong)
		return
	}
	// HEAD only checks reachability and does not count as an event.
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	a.monitorMu.Lock()
	defer a.monitorMu.Unlock()
	now := time.Now().UTC()
	if now.Sub(a.monitorWindow) >= time.Second {
		a.monitorWindow, a.monitorCount = now, 0
	}
	if a.monitorCount >= 20 {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "test inbox rate limit", http.StatusTooManyRequests)
		return
	}
	a.monitorCount++
	events, err := a.loadMonitorEvents()
	if err != nil {
		writeError(w, 503, "MONITOR_STORE_UNAVAILABLE", "Test inbox unavailable.")
		return
	}
	events = append(recentMonitorEvents(events, now), monitorEvent{ReceivedAt: now, Run: run, Event: event})
	if len(events) > monitorLimit {
		events = events[len(events)-monitorLimit:]
	}
	data, err := json.Marshal(events)
	if err == nil {
		err = os.MkdirAll(a.dataDir, 0700)
	}
	file := filepath.Join(a.dataDir, "monitor-events.json")
	if err == nil {
		err = os.WriteFile(file+".tmp", data, 0600)
	}
	if err == nil {
		err = os.Rename(file+".tmp", file)
	}
	if err != nil {
		writeError(w, 503, "MONITOR_STORE_UNAVAILABLE", "Test inbox unavailable.")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (a *app) handleMonitorEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	run := r.URL.Query().Get("run")
	if run != "" && !monitorRun.MatchString(run) {
		writeError(w, 422, "INVALID_RUN", "Invalid test run.")
		return
	}
	a.monitorMu.Lock()
	defer a.monitorMu.Unlock()
	events, err := a.loadMonitorEvents()
	if err != nil {
		writeError(w, 503, "MONITOR_STORE_UNAVAILABLE", "Test inbox unavailable.")
		return
	}
	result := []monitorEvent{}
	for _, event := range recentMonitorEvents(events, time.Now().UTC()) {
		if run == "" || event.Run == run {
			result = append(result, event)
		}
	}
	writeJSON(w, 200, map[string]any{"events": result, "limit": monitorLimit, "retentionDays": 7, "source": "unverified-http-callback"})
}
