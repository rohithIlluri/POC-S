package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/daemon"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/leaderboard"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
)

// TestViewRenders ensures the pet composes a non-trivial frame even with no
// snapshot present (the cold-start path a user hits before the daemon runs).
func TestViewRenders(t *testing.T) {
	m := New(config.Default())
	out := m.View()
	if len(out) < 50 {
		t.Fatalf("View() too short: %d bytes", len(out))
	}
	if !strings.Contains(out, "Codelings") {
		t.Errorf("View() missing title; got:\n%s", out)
	}
	for _, want := range []string{"Pet", "Overview", "Suggestions", "Records"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() missing tab %q", want)
		}
	}
}

// TestTabSwitching verifies tab navigation wraps correctly across all 4 tabs.
func TestTabSwitching(t *testing.T) {
	m := New(config.Default())
	if m.tab != 0 {
		t.Fatalf("expected initial tab 0, got %d", m.tab)
	}
	m.tab = (m.tab + tabCount - 1) % tabCount // simulate "left" from 0: wraps to the last tab
	if m.tab != tabCount-1 {
		t.Errorf("expected wrap to tab %d, got %d", tabCount-1, m.tab)
	}
}

// TestRecordsTabRenders drives the leaderboard tab against a populated
// snapshot — rankings and personal records must both appear.
func TestRecordsTabRenders(t *testing.T) {
	m := New(config.Default())
	m.tab = 3
	m.snap = &daemon.Snapshot{
		Board: leaderboard.Board{
			TopProjects: []leaderboard.Entry{{Name: "webapp", Value: 12.34}},
			Records: leaderboard.Records{
				CurrentStreak: 3, LongestStreak: 5,
				BiggestDayUSD: leaderboard.Entry{Name: "2026-07-01", Value: 9.99},
				FirstSeen:     "2026-06-01", ActiveDays: 20,
			},
		},
	}
	out := m.View()
	for _, want := range []string{"webapp", "12.34", "Streak", "best 5", "2026-07-01"} {
		if !strings.Contains(out, want) {
			t.Errorf("records tab missing %q; got:\n%s", want, out)
		}
	}
}

// TestCollectionLoopChain verifies the internal collection loop wiring:
// Init arms a collect timer; when it fires (collectTickMsg), Update must
// kick off an actual collection (a non-nil Cmd); when THAT finishes
// (collectDoneMsg), Update must refresh and re-arm the next tick.
func TestCollectionLoopChain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New(config.Default())
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init() should return a non-nil Cmd batch (animation tick + collect scheduler)")
	}

	updated, cmd := m.Update(collectTickMsg(time.Now()))
	if cmd == nil {
		t.Fatal("collectTickMsg should trigger a collection Cmd, got nil")
	}
	msg := cmd()
	if _, ok := msg.(collectDoneMsg); !ok {
		t.Fatalf("expected the collection Cmd to eventually produce collectDoneMsg, got %T", msg)
	}

	_, cmd2 := updated.Update(collectDoneMsg{})
	if cmd2 == nil {
		t.Fatal("collectDoneMsg should re-arm the next collect tick, got nil Cmd")
	}
}

// TestRefreshKeyTriggersCollection ensures pressing "r" does a real
// collect, not just a snapshot re-stat — this is the fix for the finding
// that "refresh" previously did nothing if no external daemon was running.
func TestRefreshKeyTriggersCollection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New(config.Default())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("pressing r should return a collection Cmd, not nil")
	}
	msg := cmd()
	if _, ok := msg.(collectDoneMsg); !ok {
		t.Fatalf("expected r to trigger a real collect (collectDoneMsg), got %T", msg)
	}
}

// TestHeaderReflectsPetMoodNotBudget verifies the header's face/bubble come
// from the pet's own sim.Mood, not a budget-vs-spend calculation — the fix
// for the finding that "Looking efficient!" showed up next to an egg with
// zero data and had nothing to do with the pet itself.
func TestHeaderReflectsPetMoodNotBudget(t *testing.T) {
	m := New(config.Default())
	m.snap = &daemon.Snapshot{
		Pet: sim.Pet{Stage: sim.Stage1, SpeciesID: "cindling", Mood: sim.MoodWorried, Health: 20},
	}
	out := m.View()
	if !strings.Contains(out, "Something's not sitting right") {
		t.Errorf("expected the header bubble to reflect the pet's own worried mood, got:\n%s", out)
	}
	if strings.Contains(out, "Looking efficient") {
		t.Error("old budget-mood copy should be fully retired from the header")
	}
}

func TestHeaderShowsEggStateBeforeHatch(t *testing.T) {
	m := New(config.Default())
	m.snap = &daemon.Snapshot{Pet: sim.NewEgg(sim.NewDNA([]byte("header-egg")), time.Now())}
	out := m.View()
	if !strings.Contains(out, "Warming up") {
		t.Errorf("expected egg-specific header bubble pre-hatch, got:\n%s", out)
	}
}
