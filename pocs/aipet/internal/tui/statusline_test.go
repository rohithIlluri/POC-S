package tui

import (
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/daemon"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

func TestStatusLineNoSnapshot(t *testing.T) {
	got := StatusLine(nil, 10)
	want := "(no pet yet — run /aipet)"
	if got != want {
		t.Errorf("StatusLine(nil, 10) = %q, want %q", got, want)
	}
}

func TestStatusLineEgg(t *testing.T) {
	egg := sim.NewEgg(sim.NewDNA([]byte("statusline-egg")), time.Now())
	egg.EggSessionCount = 3
	snap := &daemon.Snapshot{Pet: egg, Stats: store.Stats{TodayCost: 0.42}}

	got := StatusLine(snap, 10)
	want := "( • ) egg 3/5 · $0.42 today"
	if got != want {
		t.Errorf("StatusLine egg = %q, want %q", got, want)
	}
}

// TestStatusLineEggCapsAtThreshold ensures a stale/over-count EggSessionCount
// (e.g. a session counted the same run it hatches) never prints N/5 with N>5.
func TestStatusLineEggCapsAtThreshold(t *testing.T) {
	egg := sim.NewEgg(sim.NewDNA([]byte("statusline-egg-cap")), time.Now())
	egg.EggSessionCount = 9
	snap := &daemon.Snapshot{Pet: egg}

	got := StatusLine(snap, 0)
	want := "( • ) egg 5/5 · $0.00 today"
	if got != want {
		t.Errorf("StatusLine capped egg = %q, want %q", got, want)
	}
}

func TestStatusLineHatchedCheerful(t *testing.T) {
	p := sim.NewEgg(sim.NewDNA([]byte("statusline-cheerful")), time.Now())
	p.SpeciesID = "cindling"
	p.Stage = sim.Stage1
	p.Level = 4
	p.Mood = sim.MoodCheerful
	snap := &daemon.Snapshot{Pet: p, Stats: store.Stats{TodayCost: 0.42}}

	got := StatusLine(snap, 0)
	want := "( ^_^ ) Cindling lv4 · cheerful · $0.42 today"
	if got != want {
		t.Errorf("StatusLine cheerful = %q, want %q", got, want)
	}
}

func TestStatusLineHatchedWorriedOverBudget(t *testing.T) {
	p := sim.NewEgg(sim.NewDNA([]byte("statusline-worried")), time.Now())
	p.SpeciesID = "cindling"
	p.Stage = sim.Stage1
	p.Level = 4
	p.Mood = sim.MoodWorried
	snap := &daemon.Snapshot{Pet: p, Stats: store.Stats{TodayCost: 12.00}}

	got := StatusLine(snap, 10)
	want := "( ;_; ) Cindling lv4 · worried · $12.00 today · budget over"
	if got != want {
		t.Errorf("StatusLine worried+over-budget = %q, want %q", got, want)
	}
}

// TestStatusLineBudgetOverButPetNotWorried verifies the face always reflects
// the pet's OWN mood, never a synthetic "worried because of budget" face —
// mirroring the header rule in petview.go's faceAndBubble.
func TestStatusLineBudgetOverButPetNotWorried(t *testing.T) {
	p := sim.NewEgg(sim.NewDNA([]byte("statusline-cheerful-over")), time.Now())
	p.SpeciesID = "cindling"
	p.Stage = sim.Stage1
	p.Level = 4
	p.Mood = sim.MoodCheerful
	snap := &daemon.Snapshot{Pet: p, Stats: store.Stats{TodayCost: 12.00}}

	got := StatusLine(snap, 10)
	want := "( ^_^ ) Cindling lv4 · cheerful · $12.00 today · budget over"
	if got != want {
		t.Errorf("StatusLine cheerful+over-budget = %q, want %q", got, want)
	}
}

func TestStatusLineBudgetDisabled(t *testing.T) {
	p := sim.NewEgg(sim.NewDNA([]byte("statusline-no-budget")), time.Now())
	p.SpeciesID = "cindling"
	p.Stage = sim.Stage1
	p.Mood = sim.MoodWorried
	snap := &daemon.Snapshot{Pet: p, Stats: store.Stats{TodayCost: 999}}

	got := StatusLine(snap, 0)
	if got != "( ;_; ) Cindling lv0 · worried · $999.00 today" {
		t.Errorf("budget=0 should never append 'budget over', got %q", got)
	}
}

// TestStatusLineIsPlainText is the R4 gate: no ANSI escapes, ever, in any
// state.
func TestStatusLineIsPlainText(t *testing.T) {
	p := sim.NewEgg(sim.NewDNA([]byte("statusline-plain")), time.Now())
	p.SpeciesID = "cindling"
	p.Stage = sim.Stage1
	p.Mood = sim.MoodWorried
	snap := &daemon.Snapshot{Pet: p, Stats: store.Stats{TodayCost: 5}}

	for _, line := range []string{
		StatusLine(nil, 10),
		StatusLine(snap, 10),
		StatusLine(&daemon.Snapshot{Pet: sim.NewEgg(sim.NewDNA([]byte("plain-egg")), time.Now())}, 0),
	} {
		for _, r := range line {
			if r == '\x1b' {
				t.Fatalf("StatusLine must never contain ANSI escapes, got %q", line)
			}
		}
	}
}
