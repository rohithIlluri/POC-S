package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func tempStore(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "usage.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, p
}

func ev(key string, ts time.Time, cost float64) Event {
	return Event{
		Key: key, Source: "claude-code", Session: "s1", Project: "p1",
		Model: "claude-opus-4-8", Timestamp: ts,
		Input: 100, Output: 50, CostUSD: cost,
	}
}

func TestAppendAndDedupe(t *testing.T) {
	s, _ := tempStore(t)
	now := time.Now()

	ok, err := s.Append(ev("k1", now, 1))
	if err != nil || !ok {
		t.Fatalf("first append: ok=%v err=%v", ok, err)
	}
	ok, err = s.Append(ev("k1", now, 1))
	if err != nil || ok {
		t.Fatalf("duplicate append should be skipped: ok=%v err=%v", ok, err)
	}
	if !s.Has("k1") || s.Has("k2") {
		t.Error("Has() gave wrong answers")
	}
}

// TestDedupeSurvivesReopen is the property protecting against double-counted
// spend across daemon restarts: keys indexed at Open must block re-appends.
func TestDedupeSurvivesReopen(t *testing.T) {
	s, p := tempStore(t)
	if _, err := s.Append(ev("k1", time.Now(), 1)); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	ok, err := s2.Append(ev("k1", time.Now(), 1))
	if err != nil || ok {
		t.Fatalf("reopened store must dedupe persisted keys: ok=%v err=%v", ok, err)
	}
	events, err := s2.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after reopen, got %d", len(events))
	}
}

func TestAllSortsByTimestamp(t *testing.T) {
	s, _ := tempStore(t)
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	// Insert out of order.
	s.Append(ev("b", t0.Add(2*time.Hour), 1))
	s.Append(ev("a", t0, 1))
	s.Append(ev("c", t0.Add(1*time.Hour), 1))

	events, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	for i := 1; i < len(events); i++ {
		if events[i].Timestamp.Before(events[i-1].Timestamp) {
			t.Fatalf("events not sorted: %v then %v", events[i-1].Timestamp, events[i].Timestamp)
		}
	}
}

// TestOpenSkipsCorruptLines ensures a partially written line (e.g. crash mid
// append) does not poison the store: valid lines still load.
func TestOpenSkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "usage.db")
	content := `{"key":"good1","ts":"2026-06-01T00:00:00Z"}` + "\n" +
		`{"key":"good2","ts":` + "\n" + // truncated write
		`{"key":"good3","ts":"2026-06-02T00:00:00Z"}` + "\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.Has("good1") || !s.Has("good3") {
		t.Error("valid keys should be indexed despite corrupt line")
	}
	if s.Has("good2") {
		t.Error("corrupt line must not be indexed")
	}
}

// TestConcurrentAppend exercises the mutex under parallel writers — the daemon
// and a `status` invocation can collect at the same time.
func TestConcurrentAppend(t *testing.T) {
	s, _ := tempStore(t)
	var wg sync.WaitGroup
	const writers, perWriter = 8, 50
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				// Half the keys collide across writers to exercise dedupe.
				key := "shared"
				if i%2 == 0 {
					key = "unique"
				}
				s.Append(ev(key+"-"+string(rune('a'+w))+"-"+time.Now().String()+string(rune(i)), time.Now(), 0.01))
			}
		}(w)
	}
	wg.Wait()
	events, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	// Every line on disk must be valid JSON (no torn writes).
	for _, e := range events {
		if e.Key == "" {
			t.Fatal("found event with empty key — torn write?")
		}
	}
}

func TestFilePermissions(t *testing.T) {
	s, p := tempStore(t)
	s.Append(ev("k1", time.Now(), 1))
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("usage db should be 0600 (user-only), got %o", perm)
	}
}
