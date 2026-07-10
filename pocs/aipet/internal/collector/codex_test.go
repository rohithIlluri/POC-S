package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/pricing"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

func codexStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func writeSession(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Top-level usage with the primary field names.
const codexTopLevel = `{"type":"turn","timestamp":"2026-06-30T10:00:00Z","model":"gpt-5","usage":{"input_tokens":1000,"output_tokens":200}}
`

// Usage nested in payload, with the alternate prompt/completion naming and a
// cached-token field — exercises every alias path in normalize().
const codexNested = `{"type":"event","ts":"2026-06-30T11:00:00Z","payload":{"model":"gpt-5","usage":{"prompt_tokens":500,"completion_tokens":100,"cached_input_tokens":300}}}
`

// Lines the parser must skip: no usage, malformed JSON, zero usage.
const codexNoise = `{"type":"meta","note":"no usage here"}
this is not json at all
{"type":"turn","usage":{"input_tokens":0,"output_tokens":0}}
`

func TestCollectCodexTopLevelUsage(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "sess1.jsonl", codexTopLevel)
	st := codexStore(t)

	n, err := CollectCodex(dir, st, pricing.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 event, got %d", n)
	}
	events, _ := st.All()
	e := events[0]
	if e.Model != "gpt-5" || e.Input != 1000 || e.Output != 200 {
		t.Errorf("bad normalization: %+v", e)
	}
	if e.CostUSD <= 0 {
		t.Errorf("gpt-5 turn should have positive cost, got %v", e.CostUSD)
	}
}

func TestCollectCodexNestedAliasUsage(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "sess2.jsonl", codexNested)
	st := codexStore(t)

	n, err := CollectCodex(dir, st, pricing.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 event from nested payload, got %d", n)
	}
	events, _ := st.All()
	e := events[0]
	if e.Input != 500 || e.Output != 100 || e.CacheRead != 300 {
		t.Errorf("alias fields not normalized: %+v", e)
	}
}

func TestCollectCodexSkipsNoise(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "sess3.jsonl", codexNoise)
	st := codexStore(t)

	n, err := CollectCodex(dir, st, pricing.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("noise lines should produce 0 events, got %d", n)
	}
}

// TestCollectCodexIdempotent re-scans the same file and expects no new events —
// the content-hash key must be deterministic.
func TestCollectCodexIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "sess4.jsonl", codexTopLevel+codexNested)
	st := codexStore(t)

	prices := pricing.Default()
	if n, _ := CollectCodex(dir, st, prices, nil); n != 2 {
		t.Fatalf("first scan: expected 2, got %d", n)
	}
	if n, _ := CollectCodex(dir, st, prices, nil); n != 0 {
		t.Fatalf("re-scan must add 0 events, got %d", n)
	}
}

// TestCollectCodexUnknownModelFallback: a usage line with no model anywhere
// falls back to the generic "codex" rate rather than being dropped.
func TestCollectCodexUnknownModelFallback(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "sess5.jsonl",
		`{"type":"turn","timestamp":"2026-06-30T12:00:00Z","usage":{"input_tokens":100,"output_tokens":10}}`+"\n")
	st := codexStore(t)

	if n, _ := CollectCodex(dir, st, pricing.Default(), nil); n != 1 {
		t.Fatal("expected fallback event")
	}
	events, _ := st.All()
	if events[0].Model != "codex" {
		t.Errorf("expected model fallback to \"codex\", got %q", events[0].Model)
	}
	if events[0].CostUSD <= 0 {
		t.Errorf("fallback model should still be priced, got %v", events[0].CostUSD)
	}
}

// TestCollectorsShareBufferBound guards the store/collector line-size contract
// (a line only one side can handle would break dedupe).
func TestCollectorsShareBufferBound(t *testing.T) {
	if maxLineBytes != 16*1024*1024 {
		t.Errorf("collector maxLineBytes changed (%d); keep in lockstep with store", maxLineBytes)
	}
}
