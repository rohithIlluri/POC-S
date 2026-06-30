// Package store persists usage events locally. To keep the binary dependency-free
// and the data fully inspectable (no opaque DB file leaving the machine), events
// are appended to a JSONL log under ~/.aipet and aggregated in memory on load.
//
// A turn is identified by a stable key (source + session + turn uuid) so the
// collector can re-scan session files idempotently without double-counting.
package store

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

// Event is one model turn observed from a coding tool's session log.
type Event struct {
	Key        string    `json:"key"`    // dedupe key: source|session|uuid
	Source     string    `json:"source"` // "claude-code" | "codex"
	Session    string    `json:"session"`
	Project    string    `json:"project"` // cwd or repo path
	Model      string    `json:"model"`
	Timestamp  time.Time `json:"ts"`
	Input      int64     `json:"in"`
	Output     int64     `json:"out"`
	CacheWrite int64     `json:"cw"`
	CacheRead  int64     `json:"cr"`
	CostUSD    float64   `json:"cost"`
}

// Store is the append-only event log with an in-memory dedupe index.
type Store struct {
	mu   sync.Mutex
	path string
	f    *os.File
	seen map[string]struct{}
}

// Open loads (or creates) the store at path, indexing existing keys for dedupe.
func Open(path string) (*Store, error) {
	s := &Store{path: path, seen: make(map[string]struct{})}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
		for sc.Scan() {
			var e Event
			if json.Unmarshal(sc.Bytes(), &e) == nil && e.Key != "" {
				s.seen[e.Key] = struct{}{}
			}
		}
		f.Close()
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	s.f = f
	return s, nil
}

// Has reports whether an event key has already been recorded.
func (s *Store) Has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[key]
	return ok
}

// Append records a new event, skipping duplicates. Returns true if written.
func (s *Store) Append(e Event) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[e.Key]; ok {
		return false, nil
	}
	b, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	if _, err := s.f.Write(append(b, '\n')); err != nil {
		return false, err
	}
	s.seen[e.Key] = struct{}{}
	return true, nil
}

// Close flushes and closes the underlying file.
func (s *Store) Close() error {
	if s.f == nil {
		return nil
	}
	return s.f.Close()
}

// All reads every event back from disk, sorted by timestamp ascending.
func (s *Store) All() ([]Event, error) {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Key != "" {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out, sc.Err()
}
