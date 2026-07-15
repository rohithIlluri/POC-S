package host

import (
	"fmt"
	"os"
	"runtime"
	"time"
)

// Options controls one `aipet setup` invocation.
type Options struct {
	Claude bool // install for Claude Code
	Codex  bool // install for Codex
	Print  bool // dry-run: describe writes, touch nothing
	Now    time.Time
}

// Result summarizes what Install did, for the CLI layer to print.
type Result struct {
	Notes       []string // one line per piece, e.g. "~/.claude/commands/aipet.md: installed"
	NoHosts     bool     // neither ~/.claude nor ~/.codex exists
	WindowsSkip bool     // ran on Windows; nothing was written (R8)
}

// DetectHosts reports which of ~/.claude and ~/.codex exist on this
// machine — setup only installs for hosts actually present, and installs
// for whichever exist unless narrowed by --claude/--codex.
func DetectHosts() (claude, codex bool, err error) {
	cd, err := ClaudeDir()
	if err != nil {
		return false, false, err
	}
	if fi, statErr := os.Stat(cd); statErr == nil && fi.IsDir() {
		claude = true
	}
	xd, err := CodexDir()
	if err != nil {
		return false, false, err
	}
	if fi, statErr := os.Stat(xd); statErr == nil && fi.IsDir() {
		codex = true
	}
	return claude, codex, nil
}

// Install runs the wizard: detect hosts (or use the caller's explicit
// choice), write each host's integration pieces, and persist a manifest
// recording exactly what was written (skipped entirely under --print or on
// Windows, per R6/R8).
func Install(opts Options) (Result, error) {
	if runtime.GOOS == "windows" {
		return Result{WindowsSkip: true, Notes: windowsInstructions()}, nil
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	detectedClaude, detectedCodex, err := DetectHosts()
	if err != nil {
		return Result{}, err
	}
	wantClaude := detectedClaude
	wantCodex := detectedCodex
	if opts.Claude || opts.Codex {
		// An explicit --claude/--codex flag restricts to just that host,
		// even if the other is also present — the user asked for one.
		wantClaude = opts.Claude
		wantCodex = opts.Codex
	}
	if !wantClaude && !wantCodex {
		return Result{NoHosts: true, Notes: []string{
			"No Claude Code (~/.claude) or Codex (~/.codex) directory found on this machine.",
			"Install one of them first, then run `aipet setup` again.",
		}}, nil
	}

	var m *Manifest
	if opts.Print {
		m = &Manifest{Version: ManifestVersion} // dry-run never consults or mutates the real manifest
	} else {
		loaded, ok, err := LoadManifest()
		if err != nil {
			return Result{}, err
		}
		if ok {
			m = loaded
		} else {
			m = &Manifest{Version: ManifestVersion, InstalledAt: now}
		}
	}

	var notes []string
	var dryPrint func(string)
	if opts.Print {
		dryPrint = func(s string) { notes = append(notes, s) }
	}

	if wantClaude {
		cd, err := ClaudeDir()
		if err != nil {
			return Result{}, err
		}
		entries, hostNotes, err := InstallClaude(cd, m, now, dryPrint)
		if err != nil {
			return Result{}, err
		}
		notes = append(notes, hostNotes...)
		m.Entries = append(m.Entries, entries...)
	}
	if wantCodex {
		xd, err := CodexDir()
		if err != nil {
			return Result{}, err
		}
		entries, hostNotes, err := InstallCodex(xd, m, now, dryPrint)
		if err != nil {
			return Result{}, err
		}
		notes = append(notes, hostNotes...)
		m.Entries = append(m.Entries, entries...)
	}

	if !opts.Print {
		if m.InstalledAt.IsZero() {
			m.InstalledAt = now
		}
		if err := m.Save(); err != nil {
			return Result{}, err
		}
	}

	return Result{Notes: notes}, nil
}

// windowsInstructions is what `aipet setup` prints on Windows instead of
// writing anything (R8: automation is deferred, the binary itself still
// cross-compiles and runs there).
func windowsInstructions() []string {
	return []string{
		"aipet setup does not yet write files on Windows — set these up by hand:",
		"",
		"1. Create %USERPROFILE%\\.claude\\commands\\aipet.md with the contents from",
		"   docs/design/HOST_INTEGRATION.md §4.4 item 1.",
		"2. In %USERPROFILE%\\.claude\\settings.json, add:",
		`   "statusLine": {"type": "command", "command": "aipet statusline"}`,
		`   and append {"hooks": [{"type": "command", "command": "aipet collect --quiet", "timeout": 30}]}`,
		"   to both hooks.Stop and hooks.SessionStart.",
		"3. For Codex, create %USERPROFILE%\\.codex\\prompts\\aipet.md with the",
		"   contents from HOST_INTEGRATION.md §4.4 item 4.",
	}
}

// Remove reverses everything recorded in the manifest (R6): our files
// deleted, statusLine/hook entries restored, then the manifest itself
// deleted. Missing pieces (already hand-edited away) are reported, not
// treated as failures — Remove's contract is "converge to no-aipet."
func Remove() (Result, error) {
	m, ok, err := LoadManifest()
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{Notes: []string{"aipet is not installed (no ~/.aipet/setup.json found)."}}, nil
	}

	var claudeEntries, codexEntries []Entry
	for _, e := range m.Entries {
		switch e.Host {
		case "claude":
			claudeEntries = append(claudeEntries, e)
		case "codex":
			codexEntries = append(codexEntries, e)
		}
	}

	var notes []string
	notes = append(notes, RemoveClaude(claudeEntries)...)
	notes = append(notes, RemoveCodex(codexEntries)...)

	p, err := ManifestPath()
	if err != nil {
		return Result{Notes: notes}, err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		notes = append(notes, fmt.Sprintf("failed to remove manifest: %v", err))
	} else {
		notes = append(notes, "~/.aipet/setup.json: removed")
	}

	return Result{Notes: notes}, nil
}
