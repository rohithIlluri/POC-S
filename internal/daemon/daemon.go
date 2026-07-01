// Package daemon runs the background loop: it periodically collects usage from
// local session logs, refreshes the enterprise feed, recomputes suggestions, and
// writes a snapshot the TUI reads. It holds a PID lock so only one instance runs.
//
// IPC is deliberately a plain JSON snapshot file rather than a socket: it's
// crash-safe, inspectable, and the TUI can render even when the daemon is down.
package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/enterprise/aipet/internal/advisor"
	"github.com/enterprise/aipet/internal/collector"
	"github.com/enterprise/aipet/internal/config"
	"github.com/enterprise/aipet/internal/feed"
	"github.com/enterprise/aipet/internal/pricing"
	"github.com/enterprise/aipet/internal/store"
)

// Snapshot is the daemon's published state, read by the TUI and `status`.
type Snapshot struct {
	UpdatedAt       time.Time            `json:"updated_at"`
	Stats           store.Stats          `json:"stats"`
	Suggestions     []advisor.Suggestion `json:"suggestions"`
	Tips            []feed.Tip           `json:"tips"`
	FeedOK          bool                 `json:"feed_ok"`
	FeedError       string               `json:"feed_error,omitempty"`
	UpdateAvailable bool                 `json:"update_available"`
	UpdateInfo      *feed.UpdateInfo     `json:"update_info,omitempty"`
	Sources         map[string]bool      `json:"sources"` // detected tool dirs
	NewEvents       int                  `json:"new_events"`
	CollectErrors   []string             `json:"collect_errors,omitempty"` // non-fatal per-source errors
}

// FeedCache memoizes the last feed fetch so the daemon loop — which collects
// usage every couple of minutes — only hits the enterprise feed at the
// configured poll_interval_min, not on every collection tick.
type FeedCache struct {
	manifest *feed.Manifest
	err      error
	fetched  time.Time
}

// get returns a cached manifest while it is fresh, refetching otherwise.
// A nil receiver always fetches (the one-shot `status`/`update` path).
func (fc *FeedCache) get(cfg config.Config) (*feed.Manifest, error) {
	if fc == nil {
		return loadFeed(cfg)
	}
	ttl := time.Duration(cfg.PollIntervalMin) * time.Minute
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	if !fc.fetched.IsZero() && time.Since(fc.fetched) < ttl {
		return fc.manifest, fc.err
	}
	fc.manifest, fc.err = loadFeed(cfg)
	fc.fetched = time.Now()
	return fc.manifest, fc.err
}

// Run executes one collect+advise+feed cycle and writes a snapshot. One-shot
// callers (`status`, `update`) always fetch the feed fresh.
func Run(cfg config.Config) (*Snapshot, error) {
	return RunCycle(cfg, nil)
}

// RunCycle is Run with an optional feed cache, used by the daemon loop to honor
// the feed poll interval across frequent collection ticks.
func RunCycle(cfg config.Config, fc *FeedCache) (*Snapshot, error) {
	prices := pricing.Default()

	// Refresh the feed first so pricing overrides apply to this cycle.
	snap := &Snapshot{Sources: map[string]bool{}, UpdatedAt: time.Now()}
	if m, err := fc.get(cfg); err != nil {
		snap.FeedError = err.Error()
	} else {
		snap.FeedOK = true
		snap.Tips = m.Tips
		if len(m.Pricing) > 0 {
			prices.Override(m.Pricing)
		}
		if up, info := m.UpdateAvailable(); up {
			snap.UpdateAvailable = true
			snap.UpdateInfo = info
		}
	}

	dbPath, err := config.DBPath()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer st.Close()

	// Collect from whichever tools are present; absence is not an error, but a
	// real failure (permissions, corrupt tree) is recorded without aborting the
	// cycle so the other source and the feed still refresh.
	if dir, ok := config.ClaudeProjectsDir(); ok {
		snap.Sources["claude-code"] = true
		n, err := collector.CollectClaude(dir, st, prices)
		snap.NewEvents += n
		if err != nil {
			snap.CollectErrors = append(snap.CollectErrors, "claude-code: "+err.Error())
		}
	}
	if dir, ok := config.CodexSessionsDir(); ok {
		snap.Sources["codex"] = true
		n, err := collector.CollectCodex(dir, st, prices)
		snap.NewEvents += n
		if err != nil {
			snap.CollectErrors = append(snap.CollectErrors, "codex: "+err.Error())
		}
	}

	events, err := st.All()
	if err != nil {
		return nil, err
	}
	snap.Stats = store.Aggregate(events)
	snap.Suggestions = advisor.Run(advisor.Inputs{
		Stats:          snap.Stats,
		Events:         events,
		DailyBudgetUSD: cfg.DailyBudgetUSD,
	}, advisor.DefaultRules())

	// Append feed tips as info-level suggestions so the pet surfaces market news.
	for _, t := range snap.Tips {
		snap.Suggestions = append(snap.Suggestions, advisor.Suggestion{
			ID: "feed-" + t.ID, Severity: advisor.Info, Source: "feed",
			Title: t.Title, Detail: t.Body,
		})
	}

	if err := writeSnapshot(snap); err != nil {
		return snap, err
	}
	return snap, nil
}

func loadFeed(cfg config.Config) (*feed.Manifest, error) {
	url := cfg.FeedURL
	if url == "" {
		// Fall back to the bundled sample shipped next to the binary or in CWD.
		if p, ok := findSampleFeed(); ok {
			url = p
		} else {
			return nil, fmt.Errorf("no feed configured and no local sample found")
		}
	}
	c, err := feed.NewClient(url, cfg.FeedPublicKey)
	if err != nil {
		return nil, err
	}
	return c.Fetch()
}

// findSampleFeed looks for feed/sample-feed.json next to the executable or under
// the current directory, so the POC works out of the box.
func findSampleFeed() (string, bool) {
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "feed", "sample-feed.json"),
			filepath.Join(filepath.Dir(exe), "..", "feed", "sample-feed.json"),
		)
	}
	candidates = append(candidates, filepath.Join("feed", "sample-feed.json"))
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, true
		}
	}
	return "", false
}

// SnapshotPath is where the daemon publishes its state.
func SnapshotPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "snapshot.json"), nil
}

func writeSnapshot(s *Snapshot) error {
	p, err := SnapshotPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p) // atomic publish
}

// ReadSnapshot loads the last published snapshot for the TUI/status.
func ReadSnapshot() (*Snapshot, error) {
	p, err := SnapshotPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
