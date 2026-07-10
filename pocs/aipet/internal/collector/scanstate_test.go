package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/pricing"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

const claudeTurn = `{"type":"assistant","uuid":"%s","sessionId":"s1","cwd":"/home/u/proj","timestamp":"2026-07-01T10:00:00Z","message":{"model":"claude-sonnet-5","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
`

func writeClaudeSession(t *testing.T, path, uuid string) {
	t.Helper()
	if err := os.WriteFile(path, fmt.Appendf(nil, claudeTurn, uuid), 0o600); err != nil {
		t.Fatal(err)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestScanStateSkipsUnchangedFile proves the skip really happens: the file
// content is swapped for a line with a NEW uuid but the same size and mtime.
// Dedupe alone would collect it; only the fingerprint skip explains n == 0.
func TestScanStateSkipsUnchangedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeClaudeSession(t, path, "uuid-aaaa-0001")

	st := openStore(t)
	scan := LoadScanState(filepath.Join(t.TempDir(), "scanstate.json"))
	if n, err := CollectClaude(dir, st, pricing.Default(), scan); err != nil || n != 1 {
		t.Fatalf("first collect: n=%d err=%v, want 1", n, err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Same byte length (uuid swaps 1<->2), restored mtime => fingerprint match.
	writeClaudeSession(t, path, "uuid-aaaa-0002")
	if err := os.Chtimes(path, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if n, _ := CollectClaude(dir, st, pricing.Default(), scan); n != 0 {
		t.Fatalf("unchanged fingerprint was re-scanned: n=%d, want 0 (skipped)", n)
	}

	// Bump mtime: the file must be re-scanned and the new uuid collected.
	future := fi.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if n, _ := CollectClaude(dir, st, pricing.Default(), scan); n != 1 {
		t.Fatalf("changed file not re-scanned: n=%d, want 1", n)
	}
}

// TestScanStateGrowingFile is the common real case: a session log grows.
func TestScanStateGrowingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeClaudeSession(t, path, "uuid-bbbb-0001")

	st := openStore(t)
	scan := LoadScanState(filepath.Join(t.TempDir(), "scanstate.json"))
	if n, _ := CollectClaude(dir, st, pricing.Default(), scan); n != 1 {
		t.Fatal("first collect failed")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(fmt.Sprintf(claudeTurn, "uuid-bbbb-0002")); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if n, _ := CollectClaude(dir, st, pricing.Default(), scan); n != 1 {
		t.Fatal("appended turn not collected after file grew")
	}
}

// TestScanStateRoundtrip verifies fingerprints survive a save/load cycle, so
// a one-shot `aipet status` also benefits across invocations.
func TestScanStateRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeClaudeSession(t, path, "uuid-cccc-0001")

	statePath := filepath.Join(t.TempDir(), "scanstate.json")
	st := openStore(t)
	scan := LoadScanState(statePath)
	if n, _ := CollectClaude(dir, st, pricing.Default(), scan); n != 1 {
		t.Fatal("first collect failed")
	}
	if err := scan.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded := LoadScanState(statePath)
	fi, _ := os.Stat(path)
	if !reloaded.unchanged(path, fi) {
		t.Fatal("fingerprint did not survive save/load")
	}
}

// TestNilScanStateNeverSkips: a nil state must behave exactly like the old
// full-scan code path.
func TestNilScanStateNeverSkips(t *testing.T) {
	dir := t.TempDir()
	writeClaudeSession(t, filepath.Join(dir, "sess.jsonl"), "uuid-dddd-0001")
	st := openStore(t)
	if n, err := CollectClaude(dir, st, pricing.Default(), nil); err != nil || n != 1 {
		t.Fatalf("nil scan collect: n=%d err=%v, want 1", n, err)
	}
	var s *ScanState
	if err := s.Save(); err != nil {
		t.Fatalf("nil Save must be a no-op, got %v", err)
	}
}
