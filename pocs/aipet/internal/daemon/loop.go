package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
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

	if _, err := runCycle(cfg, st, scan, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "aipet: initial cycle: %v\n", err)
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
			if _, err := runCycle(cfg, st, scan, time.Now()); err != nil {
				fmt.Fprintf(os.Stderr, "aipet: cycle: %v\n", err)
			}
		}
	}
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
	if b, err := os.ReadFile(p); err == nil {
		if pid, err := strconv.Atoi(string(b)); err == nil && processAlive(pid) {
			return fmt.Errorf("daemon already running (pid %d)", pid)
		}
	}
	return os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())), 0o600)
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
