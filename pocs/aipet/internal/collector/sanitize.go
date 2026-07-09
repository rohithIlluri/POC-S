package collector

import "unicode/utf8"

// sanitizeField strips control characters from a string read out of a session
// log before it becomes part of a store.Event.
//
// Session-log CONTENT is untrusted: the files under ~/.claude/projects and
// ~/.codex/sessions are written by other tools (and by coding agents that can
// be prompt-injected), so a field like a model id, a cwd, or a filename can
// carry raw terminal escape sequences (ESC, OSC 52 clipboard writes, title
// rewrites, CSI cursor tricks). Those strings later reach an interactive
// terminal through the CLI (`aipet leaderboard`/`status`) and the Bubble Tea
// TUI, neither of which escapes control bytes. Stripping them here — at the
// single boundary where untrusted content enters the event log — keeps every
// downstream sink safe without each one having to remember to sanitize.
//
// It decodes the string as UTF-8 and drops:
//   - control runes: C0 (U+0000–U+001F), DEL/C1 (U+007F–U+009F);
//   - invalid encoding: any byte that does not form valid UTF-8 (this is how a
//     raw C1 byte like 0x9B appears when it is not a legitimate multi-byte
//     continuation).
//
// Valid printable text — ASCII and multi-byte UTF-8 such as "café" or "π" —
// passes through untouched, so legitimate model ids and project names are
// unaffected.
func sanitizeField(s string) string {
	if isCleanASCII(s) {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			// Invalid byte (e.g. a raw C1 control) — drop it.
			i++
			continue
		}
		if isControlRune(r) {
			i += size
			continue
		}
		out = append(out, s[i:i+size]...)
		i += size
	}
	return string(out)
}

// isCleanASCII fast-paths the common case: a string that is pure printable
// ASCII needs no allocation or decoding.
func isCleanASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}

// isControlRune reports whether r is a C0 control, DEL, or a C1 control — the
// runes that can drive terminal escape/OSC/CSI sequences.
func isControlRune(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f)
}
