package daemon

import (
	"os"
	"path/filepath"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
)

// collectDebounce is the minimum gap between two collect cycles, measured
// against snapshot.json's mtime. Claude Code's Stop hook fires once per
// completed turn — in a rapid back-and-forth, or with several sessions
// active at once, that can mean multiple hook invocations within seconds.
// Session-log parsing is cheap but not free; debouncing keeps a burst of
// hook calls from thrashing the store.
const collectDebounce = 30 * time.Second

// collectLockPath returns ~/.aipet/collect.lock, the single-flight lock for
// CollectOnce. Separate from the daemon's own daemon.pid: `aipet daemon` and
// hook-driven `aipet collect` are independent callers of the same underlying
// cycle and must not block each other just because one happens to be running
// — they only need to avoid two collects racing each other.
func collectLockPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "collect.lock"), nil
}

// CollectOnce runs at most one incremental collect cycle and returns the
// resulting snapshot. It is the single freshness owner for hook- and
// CLI-driven collection (R5 in the host-integration plan): callers that only
// want to read (card, statusline) go through here or through ReadSnapshot,
// never around it.
//
// Two guards keep concurrent/rapid hook invocations cheap:
//   - debounce: unless force, a cycle that finished within collectDebounce is
//     considered fresh enough and the last snapshot is returned unchanged.
//   - single-flight lock: if another CollectOnce is already running (a live
//     PID holds collect.lock), this call skips rather than queuing — a
//     skipped collect is invisible to the user (the pet just updates on the
//     next hook), whereas a queue of blocked hook processes would stall
//     Claude Code turns.
//
// ran reports whether this call actually executed a cycle, so callers that
// print a heartbeat only on real work (not on skips) can tell the difference.
func CollectOnce(cfg config.Config, force bool, now time.Time) (snap *Snapshot, ran bool, err error) {
	if !force {
		if p, err := SnapshotPath(); err == nil {
			if fi, statErr := os.Stat(p); statErr == nil && now.Sub(fi.ModTime()) < collectDebounce {
				s, readErr := ReadSnapshot()
				if readErr == nil {
					return s, false, nil
				}
				// A debounce-eligible snapshot that fails to parse falls
				// through to a real collect rather than surfacing a read
				// error for what should be an invisible skip.
			}
		}
	}

	lockPath, err := collectLockPath()
	if err != nil {
		return nil, false, err
	}
	if lockErr := acquirePidLock(lockPath); lockErr != nil {
		// Another collect is in flight (or crashed mid-cycle and left a live
		// PID, which is a live process by definition, not stale) — skip
		// rather than wait, matching the "never queue" guarantee hooks need.
		s, readErr := ReadSnapshot()
		if readErr != nil {
			return nil, false, nil //nolint:nilerr // skip is not a failure the caller should surface
		}
		return s, false, nil
	}
	defer releasePidLock(lockPath)

	snap, err = RunCycleAt(cfg, now)
	return snap, true, err
}
