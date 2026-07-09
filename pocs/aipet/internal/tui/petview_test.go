package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/daemon"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
)

func TestPetTabRendersEgg(t *testing.T) {
	m := New(config.Default())
	m.tab = 0
	egg := sim.NewEgg(sim.NewDNA([]byte("egg-test")), time.Now())
	egg.ActiveDayCount = 1
	m.snap = &daemon.Snapshot{Pet: egg}

	out := m.View()
	if !strings.Contains(out, "egg") {
		t.Errorf("expected egg language in the Pet tab, got:\n%s", out)
	}
}

func TestPetTabRendersHatchling(t *testing.T) {
	m := New(config.Default())
	m.tab = 0
	egg := sim.NewEgg(sim.NewDNA([]byte("hatchling-test")), time.Now())
	egg.SpeciesID = "cindling"
	egg.Line = "ember"
	egg.Stage = sim.Stage1
	egg.Level = 3
	egg.XP = 40
	egg.Health = 88
	egg.Mood = sim.MoodCheerful
	egg.Stats.Grit = 65
	m.snap = &daemon.Snapshot{Pet: egg}

	out := m.View()
	for _, want := range []string{"Cindling", "CHEERFUL", "GRIT", "Curls up"} {
		if !strings.Contains(out, want) {
			t.Errorf("Pet tab missing %q; got:\n%s", want, out)
		}
	}
}

func TestPetTabShowsPetError(t *testing.T) {
	m := New(config.Default())
	m.tab = 0
	m.snap = &daemon.Snapshot{PetError: "boom"}
	out := m.View()
	if !strings.Contains(out, "boom") {
		t.Errorf("expected pet error surfaced in the Pet tab, got:\n%s", out)
	}
}

func TestPetTabShowsJournalEntries(t *testing.T) {
	m := New(config.Default())
	m.tab = 0
	egg := sim.NewEgg(sim.NewDNA([]byte("journal-test")), time.Now())
	egg.SpeciesID = "cindling"
	egg.Stage = sim.Stage1
	m.snap = &daemon.Snapshot{Pet: egg}
	m.journalEntries = []save.Entry{
		{Day: "2026-07-08", Kind: "diet", Text: "Good mix today."},
		{Day: "2026-07-09", Kind: "diet", Text: "Cold starts galore."},
	}

	out := m.View()
	if !strings.Contains(out, "Cold starts galore.") {
		t.Errorf("expected latest journal entry in the Pet tab, got:\n%s", out)
	}
}

func TestLucentPetIsMarked(t *testing.T) {
	m := New(config.Default())
	m.tab = 0
	egg := sim.NewEgg(sim.NewDNA([]byte("lucent-test")), time.Now())
	egg.SpeciesID = "rivulet"
	egg.Stage = sim.Stage1
	egg.Lucent = true
	m.snap = &daemon.Snapshot{Pet: egg}

	out := m.View()
	if !strings.Contains(out, "Lucent") {
		t.Errorf("expected Lucent marker for a lucent pet, got:\n%s", out)
	}
}
