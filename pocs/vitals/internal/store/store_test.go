package store

import (
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestAppendLoadSortsByTime(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	for _, e := range []Entry{
		{ID: "b", Time: t0.Add(time.Hour), Metric: "mood", Value: 4},
		{ID: "a", Time: t0, Metric: "sleep", Value: 7.5, Note: "ok"},
	} {
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	got, skipped, err := s.Load()
	if err != nil || skipped != 0 {
		t.Fatalf("Load: err=%v skipped=%d", err, skipped)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("unexpected order: %+v", got)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	s, _ := Open(t.TempDir())
	got, skipped, err := s.Load()
	if err != nil || skipped != 0 || len(got) != 0 {
		t.Fatalf("got %v %d %v", got, skipped, err)
	}
}

func TestLoadSkipsCorruptLines(t *testing.T) {
	s, _ := Open(t.TempDir())
	_ = s.Append(Entry{ID: "a", Time: time.Now(), Metric: "sleep", Value: 7})
	f, _ := os.OpenFile(s.Path(), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("{not json\n{\"id\":\"\",\"metric\":\"x\"}\n")
	f.Close()
	got, skipped, err := s.Load()
	if err != nil || len(got) != 1 || skipped != 2 {
		t.Fatalf("got %d entries, %d skipped, err %v", len(got), skipped, err)
	}
}

func TestDelete(t *testing.T) {
	s, _ := Open(t.TempDir())
	now := time.Now()
	_ = s.Append(Entry{ID: "a", Time: now, Metric: "sleep", Value: 7})
	_ = s.Append(Entry{ID: "b", Time: now, Metric: "mood", Value: 3})

	removed, err := s.Delete("a")
	if err != nil || removed.Metric != "sleep" {
		t.Fatalf("Delete: %+v %v", removed, err)
	}
	got, _, _ := s.Load()
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("after delete: %+v", got)
	}
	if _, err := s.Delete("zzz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestLastWrittenIgnoresTimestamps(t *testing.T) {
	s, _ := Open(t.TempDir())
	if _, err := s.LastWritten(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty log: %v", err)
	}
	now := time.Now()
	_ = s.Append(Entry{ID: "new", Time: now, Metric: "sleep", Value: 7})
	_ = s.Append(Entry{ID: "backdated", Time: now.AddDate(0, 0, -3), Metric: "sleep", Value: 6})
	e, err := s.LastWritten()
	if err != nil || e.ID != "backdated" {
		t.Fatalf("LastWritten = %+v, %v", e, err)
	}
}

func TestFilePermissionsArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions only")
	}
	s, _ := Open(t.TempDir())
	_ = s.Append(Entry{ID: "a", Time: time.Now(), Metric: "sleep", Value: 7})
	_ = s.Append(Entry{ID: "b", Time: time.Now(), Metric: "sleep", Value: 7})
	_, _ = s.Delete("a") // rewrite path must keep 0600 too
	fi, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("log perms = %o, want 600", perm)
	}
}

func TestNewIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID()
		if len(id) != 12 || seen[id] {
			t.Fatalf("bad or duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestUpsertIsIdempotentAndUpdatesInPlace(t *testing.T) {
	s, _ := Open(t.TempDir())
	t0 := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	_ = s.Append(Entry{ID: "manual", Time: t0, Metric: "mood", Value: 4})

	batch := []Entry{
		{ID: "whoop:strain:1", Time: t0, Metric: "strain", Value: 8.2, Source: "whoop"},
		{ID: "whoop:recovery:1", Time: t0, Metric: "recovery", Value: 64, Source: "whoop"},
	}
	if a, u, err := s.Upsert(batch); err != nil || a != 2 || u != 0 {
		t.Fatalf("first upsert: added=%d updated=%d err=%v", a, u, err)
	}
	if a, u, err := s.Upsert(batch); err != nil || a != 0 || u != 0 {
		t.Fatalf("repeat upsert should be a no-op: added=%d updated=%d err=%v", a, u, err)
	}
	batch[0].Value = 14.6 // the day's strain kept climbing after the first sync
	if a, u, err := s.Upsert(batch); err != nil || a != 0 || u != 1 {
		t.Fatalf("rescored upsert: added=%d updated=%d err=%v", a, u, err)
	}
	got, skipped, _ := s.Load()
	if len(got) != 3 || skipped != 0 {
		t.Fatalf("entries = %+v", got)
	}
	for _, e := range got {
		if e.ID == "whoop:strain:1" && e.Value != 14.6 {
			t.Errorf("strain not updated: %v", e.Value)
		}
	}
	if last, _ := s.LastWritten(); last.ID != "whoop:recovery:1" {
		t.Errorf("in-place update must not reorder the log; last = %s", last.ID)
	}
}
