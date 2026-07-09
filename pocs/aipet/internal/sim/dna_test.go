package sim

import (
	"encoding/json"
	"testing"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

func TestNewDNADeterministicOnSameEntropy(t *testing.T) {
	a := NewDNA([]byte("same-seed"))
	b := NewDNA([]byte("same-seed"))
	if a != b {
		t.Error("NewDNA must be a pure function of its entropy input")
	}
	c := NewDNA([]byte("different-seed"))
	if a == c {
		t.Error("different entropy should (almost certainly) yield different DNA")
	}
}

func TestRollIVsIsPureAndInRange(t *testing.T) {
	dna := NewDNA([]byte("pet-1"))
	a := RollIVs(dna)
	b := RollIVs(dna)
	if a != b {
		t.Fatal("RollIVs must be deterministic for the same DNA")
	}
	for _, v := range []int{a.Vigor, a.Focus, a.Wit, a.Grit, a.Spark} {
		if v < 0 || v > 31 {
			t.Errorf("IV %d out of range [0,31]", v)
		}
	}
}

func TestModifierRange(t *testing.T) {
	cases := map[int]int{0: -4, 8: -2, 16: 0, 24: 2, 31: 3}
	for iv, want := range cases {
		if got := Modifier(iv); got != want {
			t.Errorf("Modifier(%d) = %d, want %d", iv, got, want)
		}
	}
}

func TestLucentDenominatorPityCurve(t *testing.T) {
	cases := map[int]uint64{0: 512, 6: 512, 7: 256, 13: 256, 14: 128, 27: 128, 28: 64, 100: 64}
	for streak, want := range cases {
		if got := LucentDenominator(streak); got != want {
			t.Errorf("LucentDenominator(%d) = %d, want %d", streak, got, want)
		}
	}
}

func TestIsLucentDeterministic(t *testing.T) {
	dna := NewDNA([]byte("lucent-test"))
	a := IsLucent(dna, 0)
	b := IsLucent(dna, 0)
	if a != b {
		t.Error("IsLucent must be deterministic for the same (dna, streak)")
	}
}

func TestIsLucentRoughRate(t *testing.T) {
	// Sanity check the roll isn't wildly miscalibrated: out of 4096 distinct
	// DNAs at streak 0 (denominator 512), expect roughly 4096/512 = 8 hits.
	// Generous bounds since this is a statistical smoke test, not an exact one.
	hits := 0
	const n = 4096
	for i := 0; i < n; i++ {
		dna := NewDNA([]byte{byte(i), byte(i >> 8)})
		if IsLucent(dna, 0) {
			hits++
		}
	}
	if hits < 1 || hits > 30 {
		t.Errorf("IsLucent hit rate %d/%d looks miscalibrated for 1/512 odds", hits, n)
	}
}

func TestPickLineEmberForSustainedSessions(t *testing.T) {
	dna := NewDNA([]byte("ember-player"))
	window := []Digest{
		{Turns: 40, Sessions: 2, CacheRead: 1000, TokensIn: 9000}, // 20 turns/session: sustained
		{Turns: 30, Sessions: 1, CacheRead: 1000, TokensIn: 9000},
	}
	if got := PickLine(dna, window); got != species.Ember {
		t.Errorf("expected Ember line for sustained single-thread work, got %v", got)
	}
}

func TestPickLineStreamForCacheHeavyBursts(t *testing.T) {
	dna := NewDNA([]byte("stream-player"))
	window := []Digest{
		{Turns: 50, Sessions: 20, CacheRead: 90_000, TokensIn: 10_000},
		{Turns: 60, Sessions: 25, CacheRead: 95_000, TokensIn: 5_000},
	}
	if got := PickLine(dna, window); got != species.StreamLine {
		t.Errorf("expected Stream line for cache-heavy fast iteration, got %v", got)
	}
}

func TestPickLineVectorForBreadth(t *testing.T) {
	dna := NewDNA([]byte("vector-player"))
	window := []Digest{
		{Turns: 5, Sessions: 5, Projects: 6, Models: 5},
		{Turns: 5, Sessions: 5, Projects: 7, Models: 4},
	}
	if got := PickLine(dna, window); got != species.Vector {
		t.Errorf("expected Vector line for breadth across projects/models, got %v", got)
	}
}

func TestPickLineDeterministicTie(t *testing.T) {
	dna := NewDNA([]byte("tie-breaker"))
	window := []Digest{} // empty window: all scores 0, must still resolve deterministically
	a := PickLine(dna, window)
	b := PickLine(dna, window)
	if a != b {
		t.Error("PickLine must resolve ties deterministically, not randomly")
	}
}

func TestDNAMarshalsAsBase64String(t *testing.T) {
	dna := NewDNA([]byte("json-test"))
	b, err := json.Marshal(dna)
	if err != nil {
		t.Fatal(err)
	}
	if b[0] != '"' {
		t.Errorf("expected DNA to marshal as a JSON string, got: %s", b)
	}
	var back DNA
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back != dna {
		t.Error("DNA JSON roundtrip mismatch")
	}
}
