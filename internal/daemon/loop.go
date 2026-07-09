package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/enterprise/aipet/internal/config"
)

// Serve runs the daemon loop until ctx is cancelled, re-collecting every
// collect_interval_min. It writes a PID file so the TUI can tell whether the
// companion is alive and so a second daemon refuses to start.
func Serve(ctx context.Context, cfg config.Config) error {
	if err := acquireLock(); err != nil {
		return err
	}
	defer releaseLock()

	// Collection is cheap and local (no network, no model calls), so a short
	// interval keeps the pet fresh without meaningful cost.
	collectEvery := time.Duration(cfg.CollectIntervalMin) * time.Minute
	if collectEvery <= 0 {
		collectEvery = 2 * time.Minute
	}

	if _, err := Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "aipet: initial cycle: %v\n", err)
	}

	t := time.NewTicker(collectEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if _, err := Run(cfg); err != nil {
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

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, signal 0 checks for existence without affecting the process.
	return proc.Signal(syscallZero) == nil
}
