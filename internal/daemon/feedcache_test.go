package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/enterprise/aipet/internal/config"
	"github.com/enterprise/aipet/internal/feed"
)

// TestFeedCacheHonorsPollInterval is the regression test for the bug where the
// daemon loop fetched the enterprise feed on every 2-minute collection tick,
// ignoring poll_interval_min entirely.
func TestFeedCacheHonorsPollInterval(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "feed.json")

	write := func(tipID string) {
		b, _ := json.Marshal(feed.Manifest{Version: 1, Tips: []feed.Tip{{ID: tipID}}})
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("v1")

	cfg := config.Default()
	cfg.FeedURL = p
	cfg.PollIntervalMin = 60

	fc := &FeedCache{}
	m1, err := fc.get(cfg)
	if err != nil || len(m1.Tips) != 1 || m1.Tips[0].ID != "v1" {
		t.Fatalf("first fetch: %v %+v", err, m1)
	}

	// Change the feed on disk; a fresh cache must NOT see it yet.
	write("v2")
	m2, err := fc.get(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Tips[0].ID != "v1" {
		t.Errorf("cache should serve v1 within the poll interval, got %s", m2.Tips[0].ID)
	}

	// Expire the cache manually; now the new content must load.
	fc.fetched = time.Now().Add(-2 * time.Hour)
	m3, err := fc.get(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if m3.Tips[0].ID != "v2" {
		t.Errorf("expired cache should refetch v2, got %s", m3.Tips[0].ID)
	}
}

// TestNilFeedCacheAlwaysFetches covers the one-shot status/update path.
func TestNilFeedCacheAlwaysFetches(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "feed.json")
	b, _ := json.Marshal(feed.Manifest{Version: 1, Tips: []feed.Tip{{ID: "fresh"}}})
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.FeedURL = p

	var fc *FeedCache // nil receiver
	m, err := fc.get(cfg)
	if err != nil || m.Tips[0].ID != "fresh" {
		t.Fatalf("nil cache should fetch directly: %v %+v", err, m)
	}
}

// TestFeedCacheCachesErrors: a failing feed is also memoized, so a broken URL
// doesn't get hammered every collection tick.
func TestFeedCacheCachesErrors(t *testing.T) {
	isolateHome(t)
	cfg := config.Default()
	cfg.FeedURL = "/does/not/exist.json"
	cfg.PollIntervalMin = 60

	fc := &FeedCache{}
	if _, err := fc.get(cfg); err == nil {
		t.Fatal("expected an error for a missing feed")
	}
	first := fc.fetched
	if _, err := fc.get(cfg); err == nil {
		t.Fatal("cached error should still be an error")
	}
	if !fc.fetched.Equal(first) {
		t.Error("error result should be served from cache, not refetched")
	}
}
