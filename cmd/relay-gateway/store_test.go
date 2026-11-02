package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *store {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.close() })
	return st
}

func TestStoreRoundTripKeepsMeta(t *testing.T) {
	st := newTestStore(t)
	now := time.Now().UTC()

	created, err := st.enqueue(task{
		ID: "round-trip", Text: "跑一下", Meta: json.RawMessage(`{"model":"deepseek","cwd":"D:/w"}`), CreatedAt: now,
	}, maxQueueDepth)
	if err != nil || !created {
		t.Fatalf("enqueue: created=%v err=%v", created, err)
	}

	got, err := st.claim(time.Now(), time.Minute)
	if err != nil || got == nil {
		t.Fatalf("claim: %v %v", got, err)
	}
	if got.ID != "round-trip" || got.Text != "跑一下" {
		t.Fatalf("unexpected task %+v", got)
	}
	var meta map[string]string
	if err := json.Unmarshal(got.Meta, &meta); err != nil {
		t.Fatalf("meta did not survive: %v", err)
	}
	if meta["model"] != "deepseek" || meta["cwd"] != "D:/w" {
		t.Fatalf("meta = %v", meta)
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d", got.Attempts)
	}
}

func TestStoreEnqueueIsIdempotent(t *testing.T) {
	st := newTestStore(t)
	for i := 0; i < 3; i++ {
		created, err := st.enqueue(task{ID: "dup", Text: "x", CreatedAt: time.Now()}, maxQueueDepth)
		if err != nil {
			t.Fatal(err)
		}
		if created != (i == 0) {
			t.Fatalf("call %d: created=%v", i, created)
		}
	}
	if n, _ := st.count(); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}

func TestStoreRefusesDuplicateOfAnsweredTask(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.enqueue(task{ID: "answered", Text: "x", CreatedAt: time.Now()}, maxQueueDepth); err != nil {
		t.Fatal(err)
	}
	if _, err := st.claim(time.Now(), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := st.ack(result{ID: "answered", Status: "done", Summary: "好了", UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	created, err := st.enqueue(task{ID: "answered", Text: "again", CreatedAt: time.Now()}, maxQueueDepth)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("an already-answered id was queued again")
	}
}

func TestStoreQueueDepthLimit(t *testing.T) {
	st := newTestStore(t)
	for i := 0; i < 3; i++ {
		created, err := st.enqueue(task{ID: "q" + string(rune('a'+i)), Text: "x", CreatedAt: time.Now()}, 3)
		if err != nil || !created {
			t.Fatalf("enqueue %d: %v %v", i, created, err)
		}
	}
	if _, err := st.enqueue(task{ID: "overflow", Text: "x", CreatedAt: time.Now()}, 3); !errors.Is(err, errQueueFull) {
		t.Fatalf("got %v, want errQueueFull", err)
	}
}

func TestStoreLeaseExpiry(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.enqueue(task{ID: "leased", Text: "x", CreatedAt: time.Now()}, maxQueueDepth); err != nil {
		t.Fatal(err)
	}
	first, err := st.claim(time.Now(), 50*time.Millisecond)
	if err != nil || first == nil {
		t.Fatalf("first claim: %v %v", first, err)
	}
	if again, _ := st.claim(time.Now(), 50*time.Millisecond); again != nil {
		t.Fatal("task was handed out twice while its lease held")
	}
	time.Sleep(80 * time.Millisecond)
	third, err := st.claim(time.Now(), 50*time.Millisecond)
	if err != nil || third == nil {
		t.Fatalf("after lease: %v %v", third, err)
	}
	if third.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", third.Attempts)
	}
}

func TestStoreConcurrentClaimsStayExclusive(t *testing.T) {
	st := newTestStore(t)
	const n = 20
	for i := 0; i < n; i++ {
		if _, err := st.enqueue(task{
			ID:        "c" + string(rune('A'+i)),
			Text:      "x",
			CreatedAt: time.Now().Add(time.Duration(i) * time.Millisecond),
		}, maxQueueDepth); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]int{}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := st.claim(time.Now(), time.Minute)
			if err != nil || got == nil {
				return
			}
			mu.Lock()
			seen[got.ID]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	for id, times := range seen {
		if times > 1 {
			t.Fatalf("task %s handed out %d times", id, times)
		}
	}
	if len(seen) != n {
		t.Fatalf("claimed %d of %d tasks", len(seen), n)
	}
}

func TestStoreReadsAreConcurrentWithWrites(t *testing.T) {
	st := newTestStore(t)
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writers enqueue and ack while readers hammer health-style counts.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			select {
			case <-stop:
				return
			default:
			}
			st.enqueue(task{ID: "w" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Text: "x", CreatedAt: time.Now()}, maxQueueDepth)
		}
	}()

	var readWG sync.WaitGroup
	for r := 0; r < 4; r++ {
		readWG.Add(1)
		go func() {
			defer readWG.Done()
			for i := 0; i < 40; i++ {
				if _, err := st.count(); err != nil {
					t.Errorf("count: %v", err)
					return
				}
				if _, err := st.queue(); err != nil {
					t.Errorf("queue: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	readWG.Wait()
}

func TestStoreResultSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relay.db")

	st, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ack(result{ID: "durable", Status: "blocked", Summary: "等你确认", UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	st.close()

	again, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.close()
	got, err := again.result("durable")
	if err != nil || got == nil {
		t.Fatalf("result after reopen: %v %v", got, err)
	}
	if got.Status != "blocked" || got.Summary != "等你确认" {
		t.Fatalf("unexpected result %+v", got)
	}
}

func TestStoreUsesWAL(t *testing.T) {
	st := newTestStore(t)
	var mode string
	if err := st.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}
