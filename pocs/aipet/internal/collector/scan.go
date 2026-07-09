package collector

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// maxLineBytes bounds a single session-log line. It is kept in lockstep with
// store.maxLineBytes: if a collector could read a line the store cannot index,
// dedupe would miss it and spend would be double-counted.
const maxLineBytes = 16 * 1024 * 1024

// newLineScanner returns a bufio.Scanner sized for large JSONL transcript lines.
func newLineScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	return sc
}

// fileErrors accumulates per-file collection failures during a directory walk.
// A single unreadable or corrupt session file should not abort collection of the
// rest, but the failures must still surface so the daemon can report them.
type fileErrors struct {
	msgs []string
}

func (e *fileErrors) add(path string, err error) {
	if err != nil {
		// The path (and any filename inside err) is untrusted and reaches the
		// terminal unescaped via `status`/TUI, so strip control characters that
		// could smuggle terminal escape sequences.
		e.msgs = append(e.msgs, sanitizeField(fmt.Sprintf("%s: %v", path, err)))
	}
}

func (e *fileErrors) err() error {
	if len(e.msgs) == 0 {
		return nil
	}
	return fmt.Errorf("%d file(s) failed: %s", len(e.msgs), strings.Join(e.msgs, "; "))
}
