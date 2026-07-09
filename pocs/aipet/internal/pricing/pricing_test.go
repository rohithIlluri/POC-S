package pricing

import (
	"math"
	"testing"
)

func TestLookupSubstringAndLongestMatch(t *testing.T) {
	tbl := Default()
	if _, ok := tbl.Lookup("claude-opus-4-8"); !ok {
		t.Fatal("expected opus match for dated model id")
	}
	// gpt-5 must win over a shorter accidental match.
	r, ok := tbl.Lookup("gpt-5-codex-preview")
	if !ok {
		t.Fatal("expected a match for gpt-5 model id")
	}
	if r.Input != 1.25 {
		t.Errorf("expected gpt-5 input rate 1.25, got %v", r.Input)
	}
}

func TestCostKnownAndUnknown(t *testing.T) {
	tbl := Default()
	got := tbl.Cost("claude-opus-4-8", Usage{Input: 1_000_000, Output: 1_000_000})
	want := 15.0 + 75.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("opus cost = %v, want %v", got, want)
	}
	if c := tbl.Cost("totally-unknown-model", Usage{Input: 1_000_000}); c != 0 {
		t.Errorf("unknown model should cost 0, got %v", c)
	}
}
