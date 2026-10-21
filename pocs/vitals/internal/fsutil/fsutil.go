// Package fsutil holds the one file-writing primitive every vitals file
// shares: atomic, private (0600) replacement.
package fsutil

import (
	"os"
	"path/filepath"
)

// WriteAtomic replaces path with data via a temp file + rename, so a crash
// never leaves a half-written file, and the result is readable only by the
// owner — every file vitals writes holds personal health data or tokens.
func WriteAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
