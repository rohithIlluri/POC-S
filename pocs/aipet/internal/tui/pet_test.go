package tui

import (
	"strings"
	"testing"

	"github.com/enterprise/aipet/internal/config"
	"github.com/enterprise/aipet/internal/daemon"
	"github.com/enterprise/aipet/internal/leaderboard"
)

// TestViewRenders ensures the pet composes a non-trivial frame even with no
// snapshot present (the cold-start path a user hits before the daemon runs).
func TestViewRenders(t *testing.T) {
	m := New(config.Default())
	out := m.View()
	if len(out) < 50 {
		t.Fatalf("View() too short: %d bytes", len(out))
	}
	if !strings.Contains(out, "aipet") {
		t.Errorf("View() missing title; got:\n%s", out)
	}
	for _, want := range []string{"Overview", "Suggestions", "Records"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() missing tab %q", want)
		}
	}
}

// TestTabSwitching verifies tab navigation wraps correctly.
func TestTabSwitching(t *testing.T) {
	m := New(config.Default())
	if m.tab != 0 {
		t.Fatalf("expected initial tab 0, got %d", m.tab)
	}
	m.tab = (m.tab + 2) % 3 // simulate "left" from 0
	if m.tab != 2 {
		t.Errorf("expected wrap to tab 2, got %d", m.tab)
	}
}

// TestRecordsTabRenders drives the leaderboard tab against a populated
// snapshot — rankings and personal records must both appear.
func TestRecordsTabRenders(t *testing.T) {
	m := New(config.Default())
	m.tab = 2
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
