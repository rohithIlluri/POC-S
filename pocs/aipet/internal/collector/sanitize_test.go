package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/pricing"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// hasControl reports whether any rune in s is a C0/C1 control or DEL — the
// runes sanitizeField must remove. Used to assert the post-condition.
func hasControl(s string) bool {
	for _, r := range s {
		if isControlRune(r) {
			return true
		}
	}
	return false
}

func TestSanitizeFieldStripsControlAndEscapes(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain untouched", "claude-opus-4-8", "claude-opus-4-8"},
		{"unicode kept", "café-model-π", "café-model-π"},
		{"esc stripped", "mo\x1bdel", "model"},
		{"osc52 clipboard write stripped", "\x1b]52;c;cGF5bG9hZA==\x07proj", "]52;c;cGF5bG9hZA==proj"},
		{"osc title stripped", "\x1b]0;pwned\x07", "]0;pwned"},
		{"csi cursor stripped", "\x1b[2J\x1b[Hname", "[2J[Hname"},
		{"c1 controls stripped", "a\x9bb\x84c", "abc"},
		{"del stripped", "a\x7fb", "ab"},
		{"tab and newline stripped", "a\tb\nc", "abc"},
		{"space preserved", "my project", "my project"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeField(tc.in)
			if got != tc.want {
				t.Errorf("sanitizeField(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if hasControl(got) {
				t.Errorf("control byte survived sanitization of %q -> %q", tc.in, got)
			}
		})
	}
}

// TestCollectStripsEscapesEndToEnd proves a malicious model id / cwd embedded in
// a real session log never reaches a store.Event with control bytes intact —
// the property that keeps the CLI and TUI terminal sinks safe.
//
// The exploitable vector uses JSON's \u escapes: a raw ESC byte inside a JSON
// string is invalid JSON and would be rejected outright, but \u001b decodes to
// a real ESC rune that the collector would otherwise carry through verbatim.
// The payloads below smuggle an OSC 52 clipboard-hijack (via cwd) and an OSC 0
// title rewrite (via model) through the parser exactly as an attacker would.
func TestCollectStripsEscapesEndToEnd(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","uuid":"u1","sessionId":"s1",` +
		`"cwd":"/work/\u001b]52;c;cHduZWQ=\u0007evil",` +
		`"timestamp":"2026-07-01T09:00:00Z",` +
		`"message":{"model":"opus\u001b]0;hijack\u0007","usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(home, "usage.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := CollectClaude(filepath.Join(home, ".claude", "projects"), st, pricing.Default(), nil); err != nil {
		t.Fatal(err)
	}
	events, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	for _, field := range []string{e.Model, e.Project, e.Session} {
		if hasControl(field) {
			t.Errorf("control/escape byte survived into event field %q", field)
		}
	}
	// Sanity: the human-meaningful text is preserved, only control bytes gone.
	if !strings.Contains(e.Model, "opus") {
		t.Errorf("model text lost during sanitization: %q", e.Model)
	}
	if !strings.Contains(e.Project, "evil") {
		t.Errorf("project base lost during sanitization: %q", e.Project)
	}
}
