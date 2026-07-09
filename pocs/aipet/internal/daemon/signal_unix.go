//go:build unix

package daemon

import (
	"os"
	"syscall"
)

// processAlive reports whether a process with the given pid exists. On Unix,
// signal 0 probes liveness without delivering a real signal.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
