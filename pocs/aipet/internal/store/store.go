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

// maxLineBytes bounds a single JSONL line. It must match (or exceed) the buffer
// the collectors use, otherwise Open could fail to index a long line that a
// collector can still read — producing a dedupe miss and double-counted spend.
const maxLineBytes = 16 * 1024 * 1024

func newScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	return sc
}

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

// Store is the append-only event log with an in-memory dedupe index. The
// events themselves are kept in memory too: Open already has to parse the
// whole file to index keys, so retaining the events makes every later All()
// free instead of a second full parse from disk. Only this process appends
// (the daemon holds a PID lock), so memory and disk cannot drift.
type Store struct {
	mu     sync.Mutex
	path   string
	f      *os.File
	seen   map[string]struct{}
	events []Event
	names  map[string]string // string intern pool for repeated fields
}

// intern returns a canonical instance of v so the thousands of events sharing
// a model, project, session, or source name share one string allocation
// instead of one per JSON-decoded line. Caller must hold s.mu.
func (s *Store) intern(v string) string {
	if v == "" {
		return ""
	}
	if c, ok := s.names[v]; ok {
		return c
	}
	s.names[v] = v
	return v
}

// compact rewrites an event's repeated fields through the intern pool. The
// unique Key is left alone. Caller must hold s.mu.
func (s *Store) compact(e Event) Event {
	e.Source = s.intern(e.Source)
	e.Session = s.intern(e.Session)
	e.Project = s.intern(e.Project)
	e.Model = s.intern(e.Model)
	return e
}

// Open loads (or creates) the store at path, indexing existing keys for dedupe
// and caching the parsed events for All.
func Open(path string) (*Store, error) {
	s := &Store{path: path, seen: make(map[string]struct{}), names: make(map[string]string)}
	if f, err := os.Open(path); err == nil {
		sc := newScanner(f)
		for sc.Scan() {
			var e Event
			if json.Unmarshal(sc.Bytes(), &e) == nil && e.Key != "" {
				if _, dup := s.seen[e.Key]; dup {
					continue
				}
				s.seen[e.Key] = struct{}{}
				s.events = append(s.events, s.compact(e))
			}
		}
		closeErr := f.Close()
		// A scan error here means the dedupe index is incomplete, which risks
		// double-counting. Surface it rather than silently continuing.
		if err := sc.Err(); err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
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
	s.events = append(s.events, s.compact(e))
	return true, nil
}

// Close flushes and closes the underlying file.
func (s *Store) Close() error {
	if s.f == nil {
		return nil
	}
	return s.f.Close()
}

// All returns every event, sorted by timestamp ascending. Events are served
// from the in-memory cache built at Open and maintained by Append — no disk
// read and no copy. The returned slice aliases the store's cache: callers must
// treat it as read-only and must not hold it across a later Append. Both
// consumers (stats aggregation, the daemon cycle) only iterate.
func (s *Store) All() ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sort.Slice(s.events, func(i, j int) bool { return s.events[i].Timestamp.Before(s.events[j].Timestamp) })
	return s.events, nil
}
