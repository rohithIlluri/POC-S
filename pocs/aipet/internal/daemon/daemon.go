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
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// Snapshot is the daemon's published state, read by the TUI and `status`.
type Snapshot struct {
	UpdatedAt     time.Time            `json:"updated_at"`
	Stats         store.Stats          `json:"stats"`
	Suggestions   []advisor.Suggestion `json:"suggestions"`
	Board         leaderboard.Board    `json:"board"`
	Pet           sim.Pet              `json:"pet"`
	Dex           save.DexState        `json:"dex"`
	Sources       map[string]bool      `json:"sources"` // detected tool dirs
	NewEvents     int                  `json:"new_events"`
	CollectErrors []string             `json:"collect_errors,omitempty"` // non-fatal per-source errors
	PetError      string               `json:"pet_error,omitempty"`      // non-fatal: pet tick failed, rest of the cycle still published
}

// Run executes one collect+advise cycle and writes a snapshot. Everything is
// local: session logs in, suggestions out, no network anywhere. One-shot
// callers (status, TUI launch) pay the store load each time; the daemon loop
// uses runCycle with a store it keeps open instead.
func Run(cfg config.Config) (*Snapshot, error) {
	return RunCycleAt(cfg, time.Now())
}

// RunCycleAt is Run with an explicit "now", so tests can drive multi-day pet
// growth and catch-up deterministically instead of depending on wall-clock
// time. Production code should call Run; RunCycleAt exists for tests and for
// any future replay/backfill tooling.
func RunCycleAt(cfg config.Config, now time.Time) (*Snapshot, error) {
	dbPath, err := config.DBPath()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	return runCycle(cfg, st, loadScanState(), now)
}

// runCycle collects into an already-open store, advances the pet, and
// publishes a snapshot. The scan state lets unchanged session-log files be
// skipped entirely; it is saved best-effort after the cycle (a lost save only
// means re-scanning). "now" flows through to everything time-dependent
// (leaderboard windows, the pet's calendar-day tick) so the daemon loop and
// deterministic tests share one code path.
func runCycle(cfg config.Config, st *store.Store, scan *collector.ScanState, now time.Time) (*Snapshot, error) {
	prices := pricing.Default()
	snap := &Snapshot{Sources: map[string]bool{}, UpdatedAt: now}

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
	snap.Board = leaderboard.Compute(events, now)

	pet, dex, err := runPetTick(events, cfg, now)
	if err != nil {
		// The pet is gameplay, not the companion's core coaching function —
		// a tick failure must never block spend advice or the leaderboard.
		snap.PetError = err.Error()
	} else {
		snap.Pet = pet
		snap.Dex = dex
	}

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
