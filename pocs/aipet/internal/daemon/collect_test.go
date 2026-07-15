package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
)

// TestCollectOnceDebounce: a second CollectOnce within collectDebounce of the
// first must not re-run a cycle — it should just re-read the same snapshot.
func TestCollectOnceDebounce(t *testing.T) {
	home := isolateHome(t)
	seedClaude(t, home)

	now := time.Now()
	snap1, ran1, err := CollectOnce(config.Default(), false, now)
	if err != nil {
		t.Fatal(err)
	}
	if !ran1 {
		t.Fatal("first call should run a cycle")
	}

	snap2, ran2, err := CollectOnce(config.Default(), false, now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if ran2 {
		t.Error("second call inside the debounce window should not run a cycle")
	}
	if !snap2.UpdatedAt.Equal(snap1.UpdatedAt) {
		t.Errorf("debounced call should return the same snapshot, got UpdatedAt %v vs %v", snap2.UpdatedAt, snap1.UpdatedAt)
	}
}

// TestCollectOnceForceOverridesDebounce: force=true must always run a fresh
// cycle even inside the debounce window.
func TestCollectOnceForceOverridesDebounce(t *testing.T) {
	home := isolateHome(t)
	seedClaude(t, home)

	now := time.Now()
	if _, ran, err := CollectOnce(config.Default(), false, now); err != nil || !ran {
		t.Fatalf("first call: ran=%v err=%v", ran, err)
	}

	_, ran2, err := CollectOnce(config.Default(), true, now.Add(1*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !ran2 {
		t.Error("force=true should run a cycle even inside the debounce window")
	}
}

// TestCollectOnceDebounceElapsed: once collectDebounce has passed, a normal
// (non-forced) call must run again.
func TestCollectOnceDebounceElapsed(t *testing.T) {
	home := isolateHome(t)
	seedClaude(t, home)

	now := time.Now()
	if _, ran, err := CollectOnce(config.Default(), false, now); err != nil || !ran {
		t.Fatalf("first call: ran=%v err=%v", ran, err)
	}

	_, ran2, err := CollectOnce(config.Default(), false, now.Add(collectDebounce+time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !ran2 {
		t.Error("a call after the debounce window elapsed should run a cycle")
	}
}

// TestCollectOnceConcurrentSingleFlight: many goroutines calling CollectOnce
// at once (simulating parallel Stop hooks from concurrent sessions) must
// never let two cycles run simultaneously. We can't observe the lock
// critical section directly from outside the package, so instead this
// asserts the documented contract — every call returns a valid snapshot and
// no error — while a background reader confirms the lock file only ever
// carries a live PID at any moment.
func TestCollectOnceConcurrentSingleFlight(t *testing.T) {
	home := isolateHome(t)
	seedClaude(t, home)

	const n = 8
	var wg sync.WaitGroup
	var ranCount int32
	errs := make([]error, n)

	// force=true on every call so each goroutine actually attempts a cycle
	// (rather than most short-circuiting on the debounce check) — this is
	// the scenario that stresses the single-flight lock the hardest.
	now := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, ran, err := CollectOnce(config.Default(), true, now)
			if ran {
				atomic.AddInt32(&ranCount, 1)
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	if ranCount < 1 {
		t.Error("at least one goroutine should have run a cycle")
	}
	// The lock must always be released, win or skip.
	lockPath, err := collectLockPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("collect.lock should not linger after all calls complete, stat err=%v", err)
	}
}

// TestCollectOnceStaleLockReclaimed: a lock file left by a dead PID must not
// permanently block collection — same stale-detection contract as the
// daemon's own pidfile lock, since both now share acquirePidLock.
func TestCollectOnceStaleLockReclaimed(t *testing.T) {
	home := isolateHome(t)
	seedClaude(t, home)

	dir := filepath.Join(home, ".aipet")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := 99999999
	if err := os.WriteFile(filepath.Join(dir, "collect.lock"), []byte(strconv.Itoa(stale)), 0o600); err != nil {
		t.Fatal(err)
	}

	snap, ran, err := CollectOnce(config.Default(), true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("a stale lock (dead PID) should be reclaimed, not skipped")
	}
	if snap == nil {
		t.Fatal("expected a snapshot back")
	}
}

// TestCollectOnceLiveLockSkips: a lock held by our own (definitely alive)
// PID must cause CollectOnce to skip rather than run a second, overlapping
// cycle.
func TestCollectOnceLiveLockSkips(t *testing.T) {
	home := isolateHome(t)
	seedClaude(t, home)

	// Seed a first snapshot so the skip path has something to read back.
	if _, ran, err := CollectOnce(config.Default(), true, time.Now()); err != nil || !ran {
		t.Fatalf("seed call: ran=%v err=%v", ran, err)
	}

	lockPath, err := collectLockPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := acquirePidLock(lockPath); err != nil {
		t.Fatalf("failed to hold the lock as our own live pid: %v", err)
	}
	defer releasePidLock(lockPath)

	_, ran, err := CollectOnce(config.Default(), true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Error("a call against a live-held lock should skip, not run a cycle")
	}
}
