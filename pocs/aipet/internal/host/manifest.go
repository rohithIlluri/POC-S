// Package host installs and removes aipet's integration into the coding
// hosts it lives inside — Claude Code and Codex. Every write this package
// makes to a file it does not own (settings.json) is a merge, never an
// overwrite: existing content is parsed, our piece is added, and the result
// is written back atomically, with a timestamped backup taken first. What
// was written is recorded in a manifest so `--remove` can reverse it exactly.
package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
)

// ManifestVersion is bumped if the manifest's shape ever changes in a way
// that requires migration; Load rejects a future version rather than
// misinterpreting it.
const ManifestVersion = 1

// EntryKind distinguishes the three shapes of thing setup can write, since
// each is reversed differently: a whole file is deleted, a JSON key is
// restored to its prior value (or removed if it had none), a hook entry is
// spliced back out of an array.
type EntryKind string

const (
	KindFile      EntryKind = "file"
	KindJSONKey   EntryKind = "json-key"
	KindHookEntry EntryKind = "hook-entry"
)

// Entry records one thing setup wrote, with enough detail for --remove to
// undo exactly that change and nothing else — critical for JSONKey/HookEntry
// entries that live inside a file setup doesn't own (settings.json), where
// "undo" must never touch a sibling key or a foreign hook another tool added
// later.
type Entry struct {
	Host string    `json:"host"` // "claude" | "codex"
	File string    `json:"file"` // absolute path touched
	Kind EntryKind `json:"kind"`

	// JSONPath is a dotted path into the file's JSON for json-key and
	// hook-entry kinds (e.g. "statusLine", "hooks.Stop"). Empty for Kind
	// File.
	JSONPath string `json:"json_path,omitempty"`

	// Backup is the path under ~/.aipet/backup/<ts>/ that held this file's
	// pre-write contents, empty if the file did not exist before this write
	// (so --remove should delete rather than restore).
	Backup string `json:"backup,omitempty"`
}

// Manifest is ~/.aipet/setup.json: the single source of truth for "is aipet
// installed, and what exactly did it touch" (R6). Both the install wizard's
// idempotence check and `--remove` read this before touching anything else.
type Manifest struct {
	Version     int       `json:"version"`
	InstalledAt time.Time `json:"installed_at"`
	Entries     []Entry   `json:"entries"`
}

// ManifestPath returns ~/.aipet/setup.json.
func ManifestPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "setup.json"), nil
}

// LoadManifest reads the manifest, or (nil, false, nil) if none exists yet
// — the "not installed" state bare `aipet` and `aipet setup`'s idempotence
// check both key off of.
func LoadManifest() (*Manifest, bool, error) {
	p, err := ManifestPath()
	if err != nil {
		return nil, false, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, false, err
	}
	return &m, true, nil
}

// Save writes the manifest atomically (tmp + rename), matching every other
// on-disk write in this codebase (daemon snapshot, pet save, dex save).
func (m *Manifest) Save() error {
	p, err := ManifestPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// HasEntry reports whether the manifest already records a write to (file,
// jsonPath) — the primary idempotence check (R6): "manifest hit" skips
// before setup does any belt-and-braces string scanning.
func (m *Manifest) HasEntry(file, jsonPath string) bool {
	if m == nil {
		return false
	}
	for _, e := range m.Entries {
		if e.File == file && e.JSONPath == jsonPath {
			return true
		}
	}
	return false
}

// backupDir returns ~/.aipet/backup/<unix-ts>, creating it. Every install
// run gets its own timestamped directory so re-running setup after a manual
// edit never overwrites an earlier backup.
func backupDir(now time.Time) (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(d, "backup", formatUnixTS(now))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func formatUnixTS(t time.Time) string {
	// A plain decimal Unix timestamp: sortable, filesystem-safe on every
	// platform (no colons, unlike RFC3339), and unambiguous across the
	// backup directories a user might accumulate over many setup runs.
	return t.UTC().Format("20060102T150405")
}

// backupFile copies an existing file's bytes into this run's backup
// directory before it is modified, returning the backup path (or "" if the
// file did not exist yet — nothing to back up, and Entry.Backup being empty
// is exactly how --remove knows to delete rather than restore).
func backupFile(path string, now time.Time) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	dir, err := backupDir(now)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filepath.Base(path))
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		return "", err
	}
	return dst, nil
}
