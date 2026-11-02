package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- the day boundary -------------------------------------------------------

func TestQuotaDayUsesShanghaiNotUTC(t *testing.T) {
	// 16:30 UTC on the 2nd is 00:30 on the 3rd in Shanghai: a fill made then
	// belongs to the 3rd, and that is what keeps the reset at local midnight.
	utc := time.Date(2026, 11, 2, 16, 30, 0, 0, time.UTC)
	if got := quotaDay(utc); got != "2026-11-03" {
		t.Fatalf("quotaDay(%s) = %s, want 2026-11-03", utc, got)
	}
	// One second before local midnight is still the old day.
	before := time.Date(2026, 11, 2, 15, 59, 59, 0, time.UTC)
	if got := quotaDay(before); got != "2026-11-02" {
		t.Fatalf("quotaDay(%s) = %s, want 2026-11-02", before, got)
	}
}

func TestNextResetIsLocalMidnight(t *testing.T) {
	// 20:00 Shanghai on the 2nd resets at 00:00 on the 3rd, i.e. 16:00 UTC.
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	reset := nextReset(now)
	if reset.UTC().Format(time.RFC3339) != "2026-11-02T16:00:00Z" {
		t.Fatalf("nextReset = %s", reset.UTC().Format(time.RFC3339))
	}
	if reset.Sub(now) != 4*time.Hour {
		t.Fatalf("nextReset is %s away, want 4h", reset.Sub(now))
	}
}

// --- the counter ------------------------------------------------------------

func newQuotaTestService(t *testing.T, limit int, ai *aiClient) *service {
	t.Helper()
	svc := newTestService(t, ai)
	svc.dailyLimit = limit
	return svc
}

func postFill(t *testing.T, svc *service, text string) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf(`{"template":"enrollment","text":%q}`, text))
	req := httptest.NewRequest(http.MethodPost, defaultBasePath+"/api/fill", body)
	req.Header.Set("Content-Type", "application/json")
	return do(t, svc, req)
}

func TestFirstHundredFillsAreAllowedAndThe101stIsRefused(t *testing.T) {
	// A model endpoint that answers instantly: this test is about the counter,
	// not the network.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"fields\":{\"f04\":\"张伟\"}}"}}]}`))
	}))
	defer srv.Close()
	ai := &aiClient{apiKey: "k", endpoint: srv.URL, model: "m", http: http.DefaultClient}
	svc := newQuotaTestService(t, 100, ai)

	for i := 1; i <= 100; i++ {
		rec := postFill(t, svc, "姓名：张伟")
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d: %s", i, rec.Code, rec.Body.String())
		}
	}

	rec := postFill(t, svc, "姓名：张伟")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("call 101: status = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	out := decodeBody(t, rec)
	if out["code"] != "QUOTA_EXCEEDED" {
		t.Fatalf("code = %v", out["code"])
	}
	msg, _ := out["error"].(string)
	if !strings.Contains(msg, "今日 AI 填表次数已用完") || !strings.Contains(msg, "100/100") {
		t.Fatalf("message should say the budget is spent in full: %q", msg)
	}
	if !strings.Contains(msg, "明天 00:00 重置") {
		t.Fatalf("message should say when it resets: %q", msg)
	}
	if remaining, ok := out["remaining"].(float64); ok && remaining != 0 {
		t.Fatalf("remaining = %v, want 0", remaining)
	}
}

func TestQuotaSurvivesARestart(t *testing.T) {
	// The counter is in SQLite, not in memory: a new service on the same
	// database must see the same used count.
	dir := t.TempDir()
	path := filepath.Join(dir, "quota.db")

	st, err := openStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	day := "2026-11-02"
	if ok, _, err := st.reserveQuota(day, 5); err != nil || !ok {
		t.Fatalf("reserve = %t, %v", ok, err)
	}
	if ok, used, err := st.reserveQuota(day, 5); err != nil || !ok || used != 2 {
		t.Fatalf("second reserve = %t used=%d err=%v", ok, used, err)
	}
	st.close()

	st2, err := openStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.close()
	used, err := st2.quotaUsed(day)
	if err != nil {
		t.Fatalf("quotaUsed: %v", err)
	}
	if used != 2 {
		t.Fatalf("used after restart = %d, want 2", used)
	}
}

func TestQuotaResetsOnTheNextDay(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.close()

	// Spend the whole budget on the 2nd.
	for i := 0; i < 3; i++ {
		if ok, _, err := st.reserveQuota("2026-11-02", 3); err != nil || !ok {
			t.Fatalf("reserve %d = %t, %v", i, ok, err)
		}
	}
	if ok, used, _ := st.reserveQuota("2026-11-02", 3); ok || used != 3 {
		t.Fatalf("the 4th on the same day should fail, got ok=%t used=%d", ok, used)
	}

	// The next day has its own row and a fresh budget.
	if ok, used, err := st.reserveQuota("2026-11-03", 3); err != nil || !ok || used != 1 {
		t.Fatalf("next day = %t used=%d err=%v", ok, used, err)
	}
	if got, _ := st.quotaUsed("2026-11-02"); got != 3 {
		t.Fatalf("yesterday's row changed: %d", got)
	}
}

func TestQuotaNeverExceedsTheLimitUnderConcurrency(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.close()

	const limit = 10
	const callers = 40
	var granted atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ok, _, err := st.reserveQuota("2026-11-02", limit); err != nil {
				t.Errorf("reserve: %v", err)
			} else if ok {
				granted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := granted.Load(); got != limit {
		t.Fatalf("%d callers were granted, want exactly %d", got, limit)
	}
	if used, _ := st.quotaUsed("2026-11-02"); used != limit {
		t.Fatalf("stored used = %d, want %d", used, limit)
	}
}

func TestReleaseQuotaGivesTheCallBack(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.close()

	if ok, used, _ := st.reserveQuota("2026-11-02", 3); !ok || used != 1 {
		t.Fatalf("reserve = %t used=%d", ok, used)
	}
	st.releaseQuota("2026-11-02")
	if used, _ := st.quotaUsed("2026-11-02"); used != 0 {
		t.Fatalf("after release used = %d, want 0", used)
	}
	// Releasing below zero would hand out free calls.
	st.releaseQuota("2026-11-02")
	if used, _ := st.quotaUsed("2026-11-02"); used != 0 {
		t.Fatalf("a second release went below zero: %d", used)
	}
}

// A failed model call is not the user's fault, so it must not consume a slot.
func TestFailedModelCallDoesNotSpendQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream is down"}}`))
	}))
	defer srv.Close()
	ai := &aiClient{apiKey: "k", endpoint: srv.URL, model: "m", http: http.DefaultClient}
	svc := newQuotaTestService(t, 3, ai)

	rec := postFill(t, svc, "姓名：张伟")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	used, err := svc.st.quotaUsed(quotaDay(time.Now()))
	if err != nil {
		t.Fatalf("quotaUsed: %v", err)
	}
	if used != 0 {
		t.Fatalf("a failed call spent %d slots, want 0", used)
	}
}

func TestQuotaIsZeroMeansUnlimited(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.close()

	for i := 0; i < 5; i++ {
		if ok, _, err := st.reserveQuota("2026-11-02", 0); err != nil || !ok {
			t.Fatalf("unlimited reserve %d = %t, %v", i, ok, err)
		}
	}
	q, err := st.quota(time.Now(), 0)
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if !q.Unlimited {
		t.Fatalf("limit 0 should report unlimited: %+v", q)
	}
}

func TestDailyLimitFromEnv(t *testing.T) {
	cases := map[string]int{
		"":    defaultDailyLimit,
		"25":  25,
		"0":   0,
		"-3":  defaultDailyLimit,
		"abc": defaultDailyLimit,
		" 7 ": 7,
	}
	for in, want := range cases {
		t.Setenv(dailyLimitEnv, in)
		if got := dailyLimitFromEnv(); got != want {
			t.Errorf("dailyLimitFromEnv(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestStateReportsRemainingQuota(t *testing.T) {
	svc := newQuotaTestService(t, 100, nil)
	if _, _, err := svc.st.reserveQuota(quotaDay(time.Now()), 100); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	rec := do(t, svc, httptest.NewRequest(http.MethodGet, defaultBasePath+"/api/state", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out struct {
		Quota quotaState `json:"quota"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Quota.Limit != 100 || out.Quota.Used != 1 || out.Quota.Remaining != 99 {
		t.Fatalf("quota = %+v", out.Quota)
	}
}
