//go:build unix

package daemon

import "syscall"

// syscallZero is signal 0, used to probe process liveness without delivering a
// real signal. Defined per-platform so a future Windows build can stub it.
var syscallZero = syscall.Signal(0)
