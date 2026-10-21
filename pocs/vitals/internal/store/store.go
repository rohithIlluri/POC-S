// Package store keeps the health log as an append-only JSONL file in the
// user's vitals home. One line per entry; no external database.
package store

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// FileName is the log file inside the vitals home.
const FileName = "log.jsonl"

// Entry is one logged measurement or event.
type Entry struct {
	ID     string    `json:"id"`
	Time   time.Time `json:"time"`
	Metric string    `json:"metric"`
	Value  float64   `json:"value"`
	Label  string    `json:"label,omitempty"`
	Note   string    `json:"note,omitempty"`
}

// ErrNotFound is returned by Delete when no entry has the given ID.
var ErrNotFound = errors.New("entry not found")

// Store is a handle on one log file.
type Store struct {
	path string
}

// Open prepares dir (created 0700 — health data is private) and returns a
// store backed by dir/log.jsonl. The file itself is created on first write.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	return &Store{path: filepath.Join(dir, FileName)}, nil
}

// Path is the log file location.
func (s *Store) Path() string { return s.path }

// NewID returns a short random identifier for an entry.
func NewID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b[:])
}

// Append writes one entry to the end of the log.
func (s *Store) Append(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Load reads every entry, sorted by time. Lines that fail to parse are
// skipped and counted rather than aborting the whole read, so one bad edit
// never locks a user out of their history.
func (s *Store) Load() (entries []Entry, skipped int, err error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Entry
		if json.Unmarshal(line, &e) != nil || e.ID == "" || e.Metric == "" || e.Time.IsZero() {
			skipped++
			continue
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, 0, err
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Time.Before(entries[j].Time) })
	return entries, skipped, nil
}

// LastWritten returns the entry on the final valid line of the log — the one
// appended most recently, regardless of its timestamp.
func (s *Store) LastWritten() (Entry, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		var e Entry
		if json.Unmarshal(lines[i], &e) == nil && e.ID != "" {
			return e, nil
		}
	}
	return Entry{}, ErrNotFound
}

// Delete removes the entry with the given ID by atomically rewriting the
// log. Unparseable lines are preserved verbatim.
func (s *Store) Delete(id string) (Entry, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	var (
		out     bytes.Buffer
		removed Entry
		found   bool
	)
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e Entry
		if !found && json.Unmarshal(line, &e) == nil && e.ID == id {
			removed, found = e, true
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	if !found {
		return Entry{}, ErrNotFound
	}
	return removed, writeAtomic(s.path, out.Bytes())
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".log-*.tmp")
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
