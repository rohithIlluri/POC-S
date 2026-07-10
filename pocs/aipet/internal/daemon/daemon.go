// Package daemon runs the background loop: it periodically collects usage from
// local session logs, recomputes suggestions, and writes a snapshot the TUI
// reads. It holds a PID lock so only one instance runs.
//
// IPC is deliberately a plain JSON snapshot file rather than a socket: it's
// crash-safe, inspectable, and the TUI can render even when the daemon is down.
package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/advisor"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/collector"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/leaderboard"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/pricing"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// Snapshot is the daemon's published state, read by the TUI and `status`.
type Snapshot struct {
	UpdatedAt     time.Time            `json:"updated_at"`
	Stats         store.Stats          `json:"stats"`
	Suggestions   []advisor.Suggestion `json:"suggestions"`
	Board         leaderboard.Board    `json:"board"`
	Sources       map[string]bool      `json:"sources"` // detected tool dirs
	NewEvents     int                  `json:"new_events"`
	CollectErrors []string             `json:"collect_errors,omitempty"` // non-fatal per-source errors
}

// Run executes one collect+advise cycle and writes a snapshot. Everything is
// local: session logs in, suggestions out, no network anywhere. One-shot
// callers (status, TUI launch) pay the store load each time; the daemon loop
// uses runCycle with a store it keeps open instead.
func Run(cfg config.Config) (*Snapshot, error) {
	dbPath, err := config.DBPath()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	return runCycle(cfg, st, loadScanState())
}

// runCycle collects into an already-open store and publishes a snapshot.
// The scan state lets unchanged session-log files be skipped entirely; it is
// saved best-effort after the cycle (a lost save only means re-scanning).
func runCycle(cfg config.Config, st *store.Store, scan *collector.ScanState) (*Snapshot, error) {
	prices := pricing.Default()
	snap := &Snapshot{Sources: map[string]bool{}, UpdatedAt: time.Now()}

	// Collect from whichever tools are present; absence is not an error, but a
	// real failure (permissions, corrupt tree) is recorded without aborting the
	// cycle so the other source still refreshes.
	if dir, ok := config.ClaudeProjectsDir(); ok {
		snap.Sources["claude-code"] = true
		n, err := collector.CollectClaude(dir, st, prices, scan)
		snap.NewEvents += n
		if err != nil {
			snap.CollectErrors = append(snap.CollectErrors, "claude-code: "+err.Error())
		}
	}
	if dir, ok := config.CodexSessionsDir(); ok {
		snap.Sources["codex"] = true
		n, err := collector.CollectCodex(dir, st, prices, scan)
		snap.NewEvents += n
		if err != nil {
			snap.CollectErrors = append(snap.CollectErrors, "codex: "+err.Error())
		}
	}
	if err := scan.Save(); err != nil {
		snap.CollectErrors = append(snap.CollectErrors, "scan-state: "+err.Error())
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
	snap.Board = leaderboard.Compute(events, time.Now())

	if err := writeSnapshot(snap); err != nil {
		return snap, err
	}
	return snap, nil
}

// loadScanState loads the collector scan fingerprints from ~/.aipet. A load
// failure degrades to a full scan, never to an error.
func loadScanState() *collector.ScanState {
	d, err := config.Dir()
	if err != nil {
		return collector.LoadScanState("")
	}
	return collector.LoadScanState(filepath.Join(d, "scanstate.json"))
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
