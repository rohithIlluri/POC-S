package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestLockSingleInstance: a live PID in the lock file must block a second
// daemon; releasing must unblock it.
func TestLockSingleInstance(t *testing.T) {
	isolateHome(t)

	if err := acquireLock(); err != nil {
		t.Fatalf("first acquire should succeed: %v", err)
	}
	// The lock file now holds our own (alive) PID, so a second acquire fails.
	if err := acquireLock(); err == nil {
		t.Fatal("second acquire against a live pid should fail")
	}
	releaseLock()
	if err := acquireLock(); err != nil {
		t.Fatalf("acquire after release should succeed: %v", err)
	}
	releaseLock()
}

// TestLockStalePidIsReclaimed: a lock file left behind by a dead process must
// not brick the daemon forever.
func TestLockStalePidIsReclaimed(t *testing.T) {
	home := isolateHome(t)
	dir := filepath.Join(home, ".aipet")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// PID 1 is init/launchd — os.FindProcess succeeds but Signal(0) from an
	// unprivileged test gets EPERM... which processAlive treats as dead-enough
	// only on error. Use an implausibly high PID instead to guarantee "dead".
	stale := 99999999
	if err := os.WriteFile(filepath.Join(dir, "daemon.pid"), []byte(strconv.Itoa(stale)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := acquireLock(); err != nil {
		t.Fatalf("stale pid should be reclaimed: %v", err)
	}
	releaseLock()
}

// TestRunningReflectsLockState covers the API the TUI uses for its status dot.
func TestRunningReflectsLockState(t *testing.T) {
	isolateHome(t)
	if _, up := Running(); up {
		t.Error("no daemon should be reported before acquire")
	}
	if err := acquireLock(); err != nil {
		t.Fatal(err)
	}
	pid, up := Running()
	if !up || pid != os.Getpid() {
		t.Errorf("Running() = %d,%v; want own pid %d", pid, up, os.Getpid())
	}
	releaseLock()
	if _, up := Running(); up {
		t.Error("daemon should not be reported after release")
	}
}
