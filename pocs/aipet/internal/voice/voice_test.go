package voice

import (
	"strings"
	"testing"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
)

// TestPacksComplete enforces the phrasebook invariant Line depends on:
// every listed personality covers the egg state and all five moods with at
// least one line, and every line obeys the lore voice rules the package
// promises (short, no trailing whitespace).
func TestPacksComplete(t *testing.T) {
	moods := []string{
		eggMood,
		string(sim.MoodCheerful),
		string(sim.MoodContent),
		string(sim.MoodTired),
		string(sim.MoodWorried),
		string(sim.MoodAsleep),
	}
	for _, p := range Personalities() {
		pack, ok := packs[p]
		if !ok {
			t.Fatalf("Personalities() lists %q but packs has no such pack", p)
		}
		for _, m := range moods {
			lines := pack[m]
			if len(lines) == 0 {
				t.Errorf("pack %q has no lines for mood %q", p, m)
			}
			for _, l := range lines {
				if len(l) > 90 {
					t.Errorf("pack %q mood %q line too long (%d chars): %q", p, m, len(l), l)
				}
				if strings.TrimSpace(l) != l || l == "" {
					t.Errorf("pack %q mood %q line has stray whitespace: %q", p, m, l)
				}
			}
		}
	}
}

func TestLineDeterministicAndRotating(t *testing.T) {
	a := Line("playful", false, sim.MoodCheerful, "2026-07-15", "cindling")
	b := Line("playful", false, sim.MoodCheerful, "2026-07-15", "cindling")
	if a != b {
		t.Errorf("same inputs must give the same line: %q vs %q", a, b)
	}

	// Across many days the line must change at least once — the whole point
	// of hashing the day in is that the pet doesn't repeat itself forever.
	days := []string{"2026-07-15", "2026-07-16", "2026-07-17", "2026-07-18", "2026-07-19"}
	seen := map[string]bool{}
	for _, d := range days {
		seen[Line("playful", false, sim.MoodCheerful, d, "cindling")] = true
	}
	if len(seen) < 2 {
		t.Error("line never rotated across five days")
	}
}

func TestLineFallbacks(t *testing.T) {
	if got := Line("no-such-personality", false, sim.MoodContent, "2026-07-15", "x"); got == "" {
		t.Error("unknown personality must fall back, not return empty")
	}
	if got := Line("funny", false, sim.Mood("no-such-mood"), "2026-07-15", "x"); got == "" {
		t.Error("unknown mood must fall back to content lines, not return empty")
	}
	if got := Line("snarky", true, sim.MoodCheerful, "2026-07-15", "x"); got == "" {
		t.Error("egg state must produce a line")
	}
}

func TestValid(t *testing.T) {
	for _, p := range Personalities() {
		if !Valid(p) {
			t.Errorf("Valid(%q) = false for a listed personality", p)
		}
	}
	if Valid("chaotic-evil") {
		t.Error("Valid must reject unknown personalities")
	}
}
