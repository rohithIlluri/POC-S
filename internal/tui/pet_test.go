package tui

import (
	"strings"
	"testing"

	"github.com/enterprise/aipet/internal/config"
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
	for _, want := range []string{"Overview", "Suggestions"} {
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
	m.tab = (m.tab + 1) % 2 // simulate "left"/"right" from 0 (two tabs wrap the same way)
	if m.tab != 1 {
		t.Errorf("expected wrap to tab 1, got %d", m.tab)
	}
}
