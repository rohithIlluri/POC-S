package host

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// codexPromptContent renders HOST_INTEGRATION.md §4.4 item 4 with the
// running binary's absolute path substituted (§8 R11 — same PATH rationale
// as the Claude command file). Codex has no pre-execution/allowed-tools
// mechanism like Claude Code's slash commands (§2 "Codex CLI (one
// surface)"), so the prompt just instructs the agent to shell out to the
// card command itself.
func codexPromptContent() string {
	return `Run the shell command ` + "`" + aipetPath + ` card "$ARGUMENTS"` + "`" + ` (view defaults to "pet").
Show its output verbatim in a fenced code block, then add one short line
in the pet's voice matching its mood. If the command is missing, tell the
user to run: go install github.com/rohithIlluri/POC-S/pocs/aipet/cmd/aipet@latest
`
}

// CodexDir returns ~/.codex. Presence is how setup detects the host.
func CodexDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

func codexPromptPath(codexDir string) string {
	return filepath.Join(codexDir, "prompts", "aipet.md")
}

// InstallCodex writes ~/.codex/prompts/aipet.md (§4.4 item 4) — the only
// Codex integration piece, since Codex has no statusline or hook
// equivalents (freshness instead comes from `aipet card` collecting on
// every call, same as the TUI's launch collect).
func InstallCodex(codexDir string, m *Manifest, now time.Time, dryPrint func(string)) ([]Entry, []string, error) {
	path := codexPromptPath(codexDir)
	if m.HasEntry(path, "") {
		return nil, []string{"~/.codex/prompts/aipet.md: already installed"}, nil
	}
	if dryPrint != nil {
		dryPrint(fmt.Sprintf("WOULD WRITE %s:\n%s", path, codexPromptContent()))
		return nil, []string{"~/.codex/prompts/aipet.md: would install"}, nil
	}
	e, err := writeCommandFile(path, "codex", now)
	if err != nil {
		return nil, nil, err
	}
	return []Entry{e}, []string{"~/.codex/prompts/aipet.md: installed"}, nil
}

// RemoveCodex deletes the Codex prompt file(s) recorded in the manifest.
func RemoveCodex(entries []Entry) []string {
	var notes []string
	for _, e := range entries {
		if e.Kind != KindFile {
			continue
		}
		if err := os.Remove(e.File); err != nil && !os.IsNotExist(err) {
			notes = append(notes, fmt.Sprintf("%s: failed to remove: %v", e.File, err))
		} else {
			notes = append(notes, fmt.Sprintf("%s: removed", e.File))
		}
	}
	return notes
}
