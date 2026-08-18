package battle

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

// forgeon20 / cascada20 are the exact stat lines from moves.md §5.1 —
// the balance table's worked example is reused verbatim as a fixture so
// the engine's arithmetic is checked against the design doc's own numbers.
func forgeon20() Card {
	return Card{
		SpeciesID: "forgeon", Level: 20,
		Stats:   species.Stats{Vigor: 75, Focus: 40, Wit: 35, Grit: 90, Spark: 45},
		Moves:   []string{"force_push", "hotfix", "sandbox", "slow_burn"},
		DNAHash: "aaaa1111",
	}
}

func cascada20() Card {
	return Card{
		SpeciesID: "cascada", Level: 20,
		Stats:   species.Stats{Vigor: 60, Focus: 90, Wit: 40, Grit: 50, Spark: 60},
		Moves:   []string{"cache_cascade", "heartbeat", "backpressure", "flash_flush"},
		DNAHash: "bbbb2222",
	}
}

// TestMoveTableAudit re-runs moves.md §1.7's self-audit in code: 45 unique
// ids, every pool exactly 3 STRIKE + 1 GUARD + 1 BOOST + 1 HEX, every
// accuracy drawn from the §1.8 legend set.
func TestMoveTableAudit(t *testing.T) {
	legend := map[Frac]bool{{1, 1}: true, {7, 8}: true, {3, 4}: true, {5, 8}: true}
	ids := map[string]bool{}
	count := 0

	for typ, pool := range Pools {
		if len(pool) != 6 {
			t.Errorf("%s pool has %d moves, want 6", typ, len(pool))
		}
		kinds := map[Kind]int{}
		for _, m := range pool {
			if ids[m.ID] {
				t.Errorf("duplicate move id %q", m.ID)
			}
			ids[m.ID] = true
			count++
			kinds[m.Kind]++
			if !legend[m.Accuracy] {
				t.Errorf("%s accuracy %v not in the §1.8 legend", m.ID, m.Accuracy)
			}
			if m.Type != typ {
				t.Errorf("%s is in the %s pool but has type %s", m.ID, typ, m.Type)
			}
		}
		if kinds[Strike] != 3 || kinds[Guard] != 1 || kinds[Boost] != 1 || kinds[Hex] != 1 {
			t.Errorf("%s pool shape %v, want 3 STRIKE / 1 GUARD / 1 BOOST / 1 HEX", typ, kinds)
		}
	}
	for spID, m := range Signatures {
		if ids[m.ID] {
			t.Errorf("signature %q collides with a pool id", m.ID)
		}
		ids[m.ID] = true
		count++
		if !legend[m.Accuracy] {
			t.Errorf("signature %s accuracy %v not in legend", m.ID, m.Accuracy)
		}
		if _, ok := species.ByID(spID); !ok {
			t.Errorf("signature keyed to unknown species %q", spID)
		}
	}
	if count != 45 {
		t.Errorf("move table has %d ids, want exactly 45", count)
	}
}

// TestEffectivenessWheel checks the fixed CACHE→CONTEXT→RUNTIME→SYNTAX→
// STREAM→DAEMON→CACHE ring: 2× vs next, ½× vs previous, 1× otherwise.
func TestEffectivenessWheel(t *testing.T) {
	ring := []species.Type{species.Cache, species.Context, species.Runtime, species.Syntax, species.Stream, species.Daemon}
	for i, atk := range ring {
		next := ring[(i+1)%6]
		prev := ring[(i+5)%6]
		if Effectiveness(atk, next) != Super {
			t.Errorf("%s vs %s should be Super", atk, next)
		}
		if Effectiveness(atk, prev) != Resisted {
			t.Errorf("%s vs %s should be Resisted", atk, prev)
		}
		if Effectiveness(atk, atk) != Neutral {
			t.Errorf("%s vs itself should be Neutral", atk)
		}
	}
}

// TestHPDerivationWorkedExamples pins §5.1/§5.2's hand-computed HP values.
func TestHPDerivationWorkedExamples(t *testing.T) {
	if got := HPMax(forgeon20()); got != 242 {
		t.Errorf("HP_max(forgeon lvl20) = %d, want 242 (moves.md §5.1)", got)
	}
	if got := HPMax(cascada20()); got != 186 {
		t.Errorf("HP_max(cascada lvl20) = %d, want 186 (moves.md §5.1)", got)
	}
	f30 := forgeon20()
	f30.Level = 30
	if got := HPMax(f30); got != 338 {
		t.Errorf("HP_max(forgeon lvl30) = %d, want 338 (moves.md §5.2)", got)
	}
}

// TestFightDeterministic: the same cards and date must produce a byte-
// identical transcript, twice in the same process and for any caller.
func TestFightDeterministic(t *testing.T) {
	r1, err := Fight(forgeon20(), cascada20(), "2026-07-16")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Fight(forgeon20(), cascada20(), "2026-07-16")
	if err != nil {
		t.Fatal(err)
	}
	if r1.Winner != r2.Winner || r1.Turns != r2.Turns {
		t.Fatalf("non-deterministic outcome: %d/%d vs %d/%d", r1.Winner, r1.Turns, r2.Winner, r2.Turns)
	}
	if strings.Join(r1.Log, "\n") != strings.Join(r2.Log, "\n") {
		t.Fatal("transcripts differ between identical replays")
	}
}

// TestFightSymmetric: loading the cards in either order must replay the
// identical battle — the §3.1 DNA-sort canonicalization at work.
func TestFightSymmetric(t *testing.T) {
	ab, err := Fight(forgeon20(), cascada20(), "2026-07-16")
	if err != nil {
		t.Fatal(err)
	}
	ba, err := Fight(cascada20(), forgeon20(), "2026-07-16")
	if err != nil {
		t.Fatal(err)
	}
	if ab.Winner != ba.Winner {
		t.Fatalf("winner depends on load order: %d vs %d", ab.Winner, ba.Winner)
	}
	if strings.Join(ab.Log, "\n") != strings.Join(ba.Log, "\n") {
		t.Fatal("transcript depends on load order")
	}
}

// TestFightDifferentDatesDiffer: the date is part of the seed, so replays
// on different days should (virtually always) diverge.
func TestFightDifferentDatesDiffer(t *testing.T) {
	r1, _ := Fight(forgeon20(), cascada20(), "2026-07-16")
	r2, _ := Fight(forgeon20(), cascada20(), "2026-07-17")
	if strings.Join(r1.Log, "\n") == strings.Join(r2.Log, "\n") {
		t.Error("two dates produced identical transcripts — seed likely ignores the date")
	}
}

// TestBattleLengthInBand runs the §5.1 matchup across many dates and
// checks the average length sits in the designed 6–12 turn band.
func TestBattleLengthInBand(t *testing.T) {
	total, n := 0, 200
	for i := 0; i < n; i++ {
		r, err := Fight(forgeon20(), cascada20(), fmt.Sprintf("2026-01-%02d+%d", (i%28)+1, i))
		if err != nil {
			t.Fatal(err)
		}
		if r.Turns >= 40 {
			t.Fatalf("neutral matchup hit the turn cap (turns=%d) — engine likely stalling", r.Turns)
		}
		total += r.Turns
	}
	avg := total / n
	if avg < 4 || avg > 14 {
		t.Errorf("average battle length %d turns, want roughly the 6-12 design band", avg)
	}
}

// TestLevelGapFavorsHigher approximates §5.2's Monte Carlo: a +10-level
// clone should win the large majority of replays but not literally all —
// the doc's own simulation reports ~96-97%.
func TestLevelGapFavorsHigher(t *testing.T) {
	low := forgeon20()
	high := forgeon20()
	high.Level = 30
	high.DNAHash = "cccc3333"

	wins, n := 0, 400
	for i := 0; i < n; i++ {
		r, err := Fight(low, high, fmt.Sprintf("2026-02-%02d+%d", (i%28)+1, i))
		if err != nil {
			t.Fatal(err)
		}
		if r.Winner >= 0 && r.Cards[r.Winner].Level == 30 {
			wins++
		}
	}
	rate := wins * 100 / n
	if rate < 80 {
		t.Errorf("level-30 win rate %d%%, want the strong-favorite band (≥80%%)", rate)
	}
	if rate == 100 {
		t.Logf("note: level-30 won all %d replays — §5.2 flags the overshoot vs the 7/8 target as an open design question", n)
	}
}

// TestValidateCardRejects covers the battle-legality gate.
func TestValidateCardRejects(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Card)
	}{
		{"unknown species", func(c *Card) { c.SpeciesID = "gremlin" }},
		{"level 0", func(c *Card) { c.Level = 0 }},
		{"level 101", func(c *Card) { c.Level = 101 }},
		{"no moves", func(c *Card) { c.Moves = nil }},
		{"five moves", func(c *Card) { c.Moves = []string{"force_push", "hotfix", "sandbox", "overclock", "segfault"} }},
		{"foreign-pool move", func(c *Card) { c.Moves = []string{"cache_flush"} }},
		{"other line's signature", func(c *Card) { c.Moves = []string{"cache_cascade"} }},
		{"duplicate move", func(c *Card) { c.Moves = []string{"hotfix", "hotfix"} }},
	}
	for _, tc := range cases {
		c := forgeon20()
		tc.mut(&c)
		if err := ValidateCard(c); err == nil {
			t.Errorf("%s: expected a validation error", tc.name)
		}
	}
	if err := ValidateCard(forgeon20()); err != nil {
		t.Errorf("known-good card rejected: %v", err)
	}
}

// TestTranscriptShape spot-checks the §4 line formats appear.
func TestTranscriptShape(t *testing.T) {
	r, err := Fight(forgeon20(), cascada20(), "2026-07-16")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(r.Log, "\n")
	if !strings.Contains(joined, "Turn 1 — ") {
		t.Error("missing turn header line")
	}
	if !strings.Contains(joined, " used ") {
		t.Error("missing action lines")
	}
	if r.Winner >= 0 && !strings.Contains(joined, `"`) {
		t.Error("missing the winner/loser voice lines")
	}
	for _, line := range r.Log {
		if len([]rune(line)) > 100 {
			t.Errorf("transcript line exceeds budget: %q", line)
		}
	}
}
