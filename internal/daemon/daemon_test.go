package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/enterprise/aipet/internal/config"
	"github.com/enterprise/aipet/internal/feed"
)

// isolateHome points HOME at a temp dir so daemon cycles read and write only
// test-owned state (~/.aipet, ~/.claude, ~/.codex all live under it).
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

const claudeTurn = `{"type":"assistant","uuid":"u1","sessionId":"s1","cwd":"/home/dev/webapp","timestamp":"2026-06-30T09:00:00Z","message":{"model":"claude-opus-4-8","usage":{"input_tokens":2000,"output_tokens":800}}}
`

func seedClaude(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess.jsonl"), []byte(claudeTurn), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFeed(t *testing.T, m feed.Manifest) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "feed.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestRunFullCycle drives collect → aggregate → advise → snapshot end to end.
func TestRunFullCycle(t *testing.T) {
	home := isolateHome(t)
	seedClaude(t, home)
	cfg := config.Default()
	cfg.FeedURL = writeFeed(t, feed.Manifest{
		Version: 1,
		Tips:    []feed.Tip{{ID: "t1", Title: "tip title", Body: "tip body", Category: "efficiency"}},
	})

	snap, err := Run(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Sources["claude-code"] {
		t.Error("claude-code source should be detected")
	}
	if snap.NewEvents != 1 {
		t.Errorf("expected 1 new event, got %d", snap.NewEvents)
	}
	if snap.Stats.TotalCost <= 0 {
		t.Errorf("opus turn should have positive cost, got %v", snap.Stats.TotalCost)
	}
	if !snap.FeedOK || len(snap.Tips) != 1 {
		t.Errorf("feed should load: ok=%v tips=%d err=%s", snap.FeedOK, len(snap.Tips), snap.FeedError)
	}
	// Feed tips must be appended as feed-sourced suggestions.
	var feedSugs int
	for _, s := range snap.Suggestions {
		if s.Source == "feed" {
			feedSugs++
		}
	}
	if feedSugs != 1 {
		t.Errorf("expected 1 feed suggestion, got %d", feedSugs)
	}
	if len(snap.CollectErrors) != 0 {
		t.Errorf("unexpected collect errors: %v", snap.CollectErrors)
	}

	// The snapshot must be readable back — this is the TUI's data path.
	back, err := ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if back.NewEvents != 1 || back.Stats.TotalCost != snap.Stats.TotalCost {
		t.Errorf("snapshot roundtrip mismatch: %+v vs %+v", back.Stats, snap.Stats)
	}
}

// TestRunIdempotent: a second cycle over unchanged sessions adds nothing.
func TestRunIdempotent(t *testing.T) {
	home := isolateHome(t)
	seedClaude(t, home)
	cfg := config.Default()
	cfg.FeedURL = writeFeed(t, feed.Manifest{Version: 1})

	if snap, err := Run(cfg); err != nil || snap.NewEvents != 1 {
		t.Fatalf("first run: %v / %+v", err, snap)
	}
	snap2, err := Run(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if snap2.NewEvents != 0 {
		t.Errorf("second run must add 0 events, got %d", snap2.NewEvents)
	}
	if snap2.Stats.Turns != 1 {
		t.Errorf("total turns must remain 1, got %d", snap2.Stats.Turns)
	}
}

// TestRunSurvivesFeedFailure: a broken feed must not block usage collection.
func TestRunSurvivesFeedFailure(t *testing.T) {
	home := isolateHome(t)
	seedClaude(t, home)
	cfg := config.Default()
	cfg.FeedURL = "/nonexistent/feed.json"

	snap, err := Run(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if snap.FeedOK {
		t.Error("feed should be reported unavailable")
	}
	if snap.FeedError == "" {
		t.Error("feed error should carry a reason")
	}
	if snap.NewEvents != 1 {
		t.Errorf("collection must proceed despite feed failure, got %d events", snap.NewEvents)
	}
}

// TestRunNoTools: with no coding tools installed the cycle still succeeds and
// reports empty sources — the pet's cold-start experience.
func TestRunNoTools(t *testing.T) {
	isolateHome(t)
	cfg := config.Default()
	cfg.FeedURL = writeFeed(t, feed.Manifest{Version: 1})

	snap, err := Run(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Sources) != 0 || snap.NewEvents != 0 {
		t.Errorf("expected empty cold-start snapshot, got %+v", snap)
	}
}

// TestSnapshotAtomicity: the published file must always be complete JSON, never
// a torn write (writeSnapshot goes through a tmp file + rename).
func TestSnapshotAtomicity(t *testing.T) {
	isolateHome(t)
	cfg := config.Default()
	cfg.FeedURL = writeFeed(t, feed.Manifest{Version: 1})
	if _, err := Run(cfg); err != nil {
		t.Fatal(err)
	}
	p, _ := SnapshotPath()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("snapshot on disk is not valid JSON: %v", err)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Error("tmp file should not linger after publish")
	}
}
