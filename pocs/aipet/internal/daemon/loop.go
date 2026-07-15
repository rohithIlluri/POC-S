package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// Serve runs the daemon loop until ctx is cancelled, re-collecting every
// collect_interval_min. It writes a PID file so the TUI can tell whether the
// companion is alive and so a second daemon refuses to start.
//
// The event store and collector scan state are opened once and reused across
// cycles: the store's dedupe index and event cache make each subsequent cycle
// incremental (only new log lines are parsed), instead of re-reading the whole
// event log every interval.
func Serve(ctx context.Context, cfg config.Config) error {
	if err := acquireLock(); err != nil {
		return err
	}
	defer releaseLock()

	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	scan := loadScanState()

	// Collection is cheap and local (no network, no model calls), so a short
	// interval keeps the pet fresh without meaningful cost.
	collectEvery := time.Duration(cfg.CollectIntervalMin) * time.Minute
	if collectEvery <= 0 {
		collectEvery = 2 * time.Minute
	}

	if snap, err := runCycle(cfg, st, scan, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "aipet: initial cycle: %v\n", err)
	} else {
		heartbeat(snap)
	}

	t := time.NewTicker(collectEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			// Fresh time.Now() per tick keeps the pet's calendar-day logic
			// correct across midnight in a long-lived daemon process.
			if snap, err := runCycle(cfg, st, scan, time.Now()); err != nil {
				fmt.Fprintf(os.Stderr, "aipet: cycle: %v\n", err)
			} else {
				heartbeat(snap)
			}
		}
	}
}

// heartbeat prints one line per collection cycle so the foreground daemon
// visibly does something — without it the process prints a single startup
// line and then sits silent for hours, indistinguishable from hung.
func heartbeat(snap *Snapshot) {
	fmt.Println(HeartbeatLine(snap))
}

// HeartbeatLine formats the one-line collection summary shared by the
// foreground daemon loop and `aipet collect` (non-quiet mode) — the two
// places a human ever watches a collect cycle happen, kept textually
// identical so the experience reads the same whichever path produced it.
func HeartbeatLine(snap *Snapshot) string {
	line := fmt.Sprintf("%s  +%d events", snap.UpdatedAt.Format("15:04:05"), snap.NewEvents)
	switch {
	case snap.PetError != "":
		line += " · pet: " + snap.PetError
	case snap.Pet.IsEgg():
		n := snap.Pet.EggSessionCount
		if n > sim.HatchSessionThreshold {
			n = sim.HatchSessionThreshold
		}
		line += fmt.Sprintf(" · egg %d/%d sessions", n, sim.HatchSessionThreshold)
	default:
		name := snap.Pet.SpeciesID
		if sp, ok := species.ByID(snap.Pet.SpeciesID); ok {
			name = sp.Name
		}
		line += fmt.Sprintf(" · %s lv %d · %s", name, snap.Pet.Level, snap.Pet.Mood)
	}
	return line
}

func pidPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "daemon.pid"), nil
}

func acquireLock() error {
	p, err := pidPath()
	if err != nil {
		return err
	}
	if err := acquirePidLock(p); err != nil {
		var held *pidLockHeldError
		if errors.As(err, &held) {
			return fmt.Errorf("daemon already running (pid %d)", held.pid)
		}
		return err
	}
	return nil
}

func releaseLock() {
	if p, err := pidPath(); err == nil {
		os.Remove(p)
	}
}

// Running reports the daemon PID if one is alive.
func Running() (int, bool) {
	p, err := pidPath()
	if err != nil {
		return 0, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil || !processAlive(pid) {
		return 0, false
	}
	return pid, true
}

// pidLockHeldError reports that a pidfile lock is held by another live
// process. Both the daemon's single-instance lock and collect's single-flight
// lock share this shape so callers can extract the blocking PID uniformly.
type pidLockHeldError struct{ pid int }

func (e *pidLockHeldError) Error() string { return fmt.Sprintf("lock held by pid %d", e.pid) }

// acquirePidLock is the create-exclusive + PID + liveness-probe pattern used
// by every lock file in this package (the daemon's daemon.pid and collect's
// collect.lock): write our own PID if the file is absent or its recorded PID
// is no longer alive (stale lock, safely reclaimed); otherwise report who
// holds it. Centralized here so the two lock implementations never drift.
//
// The fast path uses O_CREATE|O_EXCL, which is atomic even between
// goroutines in the same process (unlike a plain ReadFile-then-WriteFile
// check, which two goroutines can both pass before either writes) — that
// matters because concurrent Stop hooks can, in practice, race inside a
// single `aipet collect` invocation's test harness even though real hook
// invocations are separate OS processes.
func acquirePidLock(path string) error {
	self := []byte(strconv.Itoa(os.Getpid()))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, writeErr := f.Write(self)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	if !os.IsExist(err) {
		return err
	}

	// The file already exists: it's either a live lock or stale debris from
	// a process that died without cleaning up. Reclaim only the latter.
	b, readErr := os.ReadFile(path)
	if readErr == nil {
		if pid, convErr := strconv.Atoi(string(b)); convErr == nil && processAlive(pid) {
			return &pidLockHeldError{pid: pid}
		}
	}
	// Stale (or unreadable/corrupt, which is stale-enough): remove and retry
	// once. A second concurrent reclaimer losing this retry will see the
	// winner's fresh EXCL and correctly report the lock as held.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return &pidLockHeldError{pid: 0} // another reclaimer won the retry; pid unknown
		}
		return err
	}
	_, writeErr := f.Write(self)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// releasePidLock removes a lock file acquired via acquirePidLock. Best-effort:
// a failed remove just leaves a stale-but-reclaimable lock behind.
func releasePidLock(path string) {
	os.Remove(path)
}
