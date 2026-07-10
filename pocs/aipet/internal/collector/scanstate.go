package collector

import (
	"encoding/json"
	"os"
)

// ScanState remembers, per session-log file, the size and mtime that were last
// fully collected, so unchanged files are skipped instead of re-read and
// re-parsed on every cycle. Skipping is purely an optimization: the store's
// key dedupe stays the source of truth, so a stale or lost scan state can only
// cause extra reading, never lost or double-counted events.
//
// The state lives in the same-user ~/.aipet directory and is trusted (it is
// ours), unlike the session logs it describes.
type ScanState struct {
	path  string
	dirty bool
	Files map[string]FileMark `json:"files"`
}

// FileMark is the fingerprint of a fully-collected file. Size plus mtime (in
// nanoseconds) is enough: appends grow the file, rewrites touch the mtime.
type FileMark struct {
	Size    int64 `json:"size"`
	MTimeNS int64 `json:"mtime_ns"`
}

// LoadScanState reads the state at path. A missing or corrupt file yields an
// empty state — every log file is then scanned, exactly like a first run.
func LoadScanState(path string) *ScanState {
	s := &ScanState{path: path, Files: map[string]FileMark{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if json.Unmarshal(b, s) != nil || s.Files == nil {
		s.Files = map[string]FileMark{}
	}
	return s
}

// unchanged reports whether the file matches its last fully-collected mark.
// A nil state never skips.
func (s *ScanState) unchanged(path string, fi os.FileInfo) bool {
	if s == nil || fi == nil {
		return false
	}
	m, ok := s.Files[path]
	return ok && m.Size == fi.Size() && m.MTimeNS == fi.ModTime().UnixNano()
}

// mark records fi as fully collected. The caller must pass the stat taken
// BEFORE the scan: if the file grows mid-scan, the pre-scan fingerprint no
// longer matches and the file is simply re-scanned next cycle.
func (s *ScanState) mark(path string, fi os.FileInfo) {
	if s == nil || fi == nil {
		return
	}
	s.Files[path] = FileMark{Size: fi.Size(), MTimeNS: fi.ModTime().UnixNano()}
	s.dirty = true
}

// Save persists the state atomically (tmp + rename). Saving is best-effort:
// on failure the next cycle just re-scans. A nil or unchanged state is a no-op.
func (s *ScanState) Save() error {
	if s == nil || !s.dirty || s.path == "" {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.dirty = false
	return nil
}
