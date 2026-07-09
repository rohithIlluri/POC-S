package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enterprise/aipet/internal/pricing"
	"github.com/enterprise/aipet/internal/store"
)

// A minimal but realistic Claude Code transcript: one user line (no usage), one
// assistant line with usage, and a malformed line that must be skipped.
const sampleJSONL = `{"type":"user","message":{"role":"user","content":"hi"}}
{"type":"assistant","uuid":"u1","sessionId":"s1","cwd":"/home/dev/myrepo","timestamp":"2026-06-30T14:00:00Z","message":{"model":"claude-opus-4-8","usage":{"input_tokens":1000,"output_tokens":500,"cache_read_input_tokens":2000,"cache_creation_input_tokens":300}}}
{not valid json
{"type":"assistant","uuid":"u2","sessionId":"s1","cwd":"/home/dev/myrepo","timestamp":"2026-06-30T14:01:00Z","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":200,"output_tokens":100}}}
`

func TestCollectClaude(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(sampleJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	prices := pricing.Default()
	n, err := CollectClaude(dir, st, prices)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 assistant turns collected, got %d", n)
	}

	// Re-running must be idempotent (dedupe by key).
	n2, _ := CollectClaude(dir, st, prices)
	if n2 != 0 {
		t.Errorf("expected 0 new events on re-scan, got %d", n2)
	}

	events, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 stored events, got %d", len(events))
	}
	e := events[0]
	if e.Source != "claude-code" || e.Project != "myrepo" {
		t.Errorf("unexpected normalization: %+v", e)
	}
	if e.CostUSD <= 0 {
		t.Errorf("expected positive cost for opus turn, got %v", e.CostUSD)
	}
}
