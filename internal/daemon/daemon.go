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
	UpdatedAt       time.Time             `json:"updated_at"`
	Stats           store.Stats           `json:"stats"`
	Suggestions     []advisor.Suggestion  `json:"suggestions"`
	Tips            []feed.Tip            `json:"tips"`
	FeedOK          bool                  `json:"feed_ok"`
	FeedError       string                `json:"feed_error,omitempty"`
	UpdateAvailable bool                  `json:"update_available"`
	UpdateInfo      *feed.UpdateInfo      `json:"update_info,omitempty"`
	Sources         map[string]bool       `json:"sources"` // detected tool dirs
	NewEvents       int                   `json:"new_events"`
}

// Run executes one collect+advise+feed cycle and writes a snapshot. The daemon's
// main loop calls this on an interval; `status` calls it once on demand.
func Run(cfg config.Config) (*Snapshot, error) {
	prices := pricing.Default()

	// Refresh the feed first so pricing overrides apply to this cycle.
	snap := &Snapshot{Sources: map[string]bool{}, UpdatedAt: time.Now()}
	if m, err := loadFeed(cfg); err != nil {
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

	// Collect from whichever tools are present; absence is not an error.
	if dir, ok := config.ClaudeProjectsDir(); ok {
		snap.Sources["claude-code"] = true
		n, _ := collector.CollectClaude(dir, st, prices)
		snap.NewEvents += n
	}
	if dir, ok := config.CodexSessionsDir(); ok {
		snap.Sources["codex"] = true
		n, _ := collector.CollectCodex(dir, st, prices)
		snap.NewEvents += n
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
