package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testMonitorPath = "/api/demo/monitor/oceanengine/chuangliang-20261019-test001/click"

func monitorRequest(a *app, method, path string, auth bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if auth {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: a.sessionToken()})
	}
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	return w
}

func TestMonitorPersistsAndProtectsReceipts(t *testing.T) {
	a := &app{dataDir: t.TempDir(), sessionSecret: []byte("test-secret")}
	if w := monitorRequest(a, "GET", testMonitorPath+"?ip=private&token=secret", false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	b := &app{dataDir: a.dataDir, sessionSecret: a.sessionSecret}
	if w := monitorRequest(b, "GET", "/api/demo/monitor/events", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := monitorRequest(b, "GET", "/api/demo/monitor/events?run=chuangliang-20261019-test001", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"event":"click"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(filepath.Join(a.dataDir, "monitor-events.json"))
	for _, private := range []string{"private", "secret", "192.0.2.1"} {
		if strings.Contains(string(data), private) {
			t.Fatal("sensitive data persisted")
		}
	}
	if w := monitorRequest(b, "GET", "/api/demo/monitor/events?run=chuangliang-20261019-other00", true); strings.Contains(w.Body.String(), `"event":"click"`) {
		t.Fatal("run filter failed")
	}
}

func TestMonitorRejectsInvalidRequestsAndHeadDoesNotRecord(t *testing.T) {
	a := &app{dataDir: t.TempDir()}
	for _, test := range []struct {
		method, path string
		code         int
	}{
		{"HEAD", testMonitorPath, 200}, {"POST", testMonitorPath, 405},
		{"GET", strings.Replace(testMonitorPath, "test001", "BAD", 1), 404},
		{"GET", strings.Replace(testMonitorPath, "click", "unknown", 1), 404},
		{"GET", testMonitorPath + "?x=" + strings.Repeat("a", 8193), 414},
	} {
		if w := monitorRequest(a, test.method, test.path, false); w.Code != test.code {
			t.Fatalf("%s: %d", test.method, w.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(a.dataDir, "monitor-events.json")); !os.IsNotExist(err) {
		t.Fatal("invalid request persisted")
	}
}

func TestMonitorBoundsRetentionAndRate(t *testing.T) {
	a := &app{dataDir: t.TempDir()}
	events := []monitorEvent{{ReceivedAt: time.Now().Add(-8 * 24 * time.Hour), Run: "expired", Event: "click"}}
	for i := 1; i < monitorLimit; i++ {
		events = append(events, monitorEvent{ReceivedAt: time.Now(), Run: "old", Event: "click"})
	}
	data, _ := json.Marshal(events)
	if err := os.WriteFile(filepath.Join(a.dataDir, "monitor-events.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if w := monitorRequest(a, "GET", testMonitorPath, false); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if w := monitorRequest(a, "GET", testMonitorPath, false); w.Code != 429 {
		t.Fatal(w.Code)
	}
	stored, err := a.loadMonitorEvents()
	if err != nil || len(stored) != monitorLimit {
		t.Fatal(len(stored), err)
	}
	for _, e := range stored {
		if e.Run == "expired" {
			t.Fatal("expired event retained")
		}
	}
}

func TestMonitorCorruptionDoesNotReportSuccessOrOverwrite(t *testing.T) {
	a := &app{dataDir: t.TempDir()}
	file := filepath.Join(a.dataDir, "monitor-events.json")
	if err := os.WriteFile(file, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if w := monitorRequest(a, "GET", testMonitorPath, false); w.Code != 503 {
		t.Fatal(w.Code)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "broken" {
		t.Fatal("corrupt store overwritten")
	}
}

func TestMonitorConcurrentCallbacksRemainReadable(t *testing.T) {
	a := &app{dataDir: t.TempDir()}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := monitorRequest(a, "GET", testMonitorPath, false); w.Code != 200 {
				t.Error(w.Code)
			}
		}()
	}
	wg.Wait()
	events, err := a.loadMonitorEvents()
	if err != nil || len(events) != 12 {
		t.Fatal(len(events), err)
	}
}
