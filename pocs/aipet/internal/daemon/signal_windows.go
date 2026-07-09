//go:build windows

package daemon

import "os"

// processAlive reports whether a process with the given pid exists. Windows has
// no signal 0; instead os.FindProcess calls OpenProcess, which fails for a pid
// that is not running. A successfully opened handle is treated as "alive",
// which is what the daemon's single-instance lock needs — a stale pidfile whose
// process has exited cannot be opened, so the lock is correctly reclaimed.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = proc.Release()
	return true
}
