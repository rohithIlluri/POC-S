package host

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// claudeCommandMD is byte-exact with HOST_INTEGRATION.md §4.4 item 1: the
// slash command's frontmatter, its pre-execution `aipet card` call (scoped
// by allowed-tools to exactly that command, R2), and the instructions for
// how Claude should present the card. Any drift from the doc here is a
// silent behavior change to what ships, so this stays a literal constant
// rather than being reconstructed with fmt.Sprintf.
const claudeCommandMD = `---
description: Your Codeling — the pet that grows from your coding sessions
argument-hint: [pet|dex|records|overview]
allowed-tools: Bash(aipet card:*)
---

## Context
- Pet card: !` + "`aipet card \"$ARGUMENTS\"`" + `

## Your task
Show the pet card above verbatim in a fenced code block. Then add ONE short
line reacting in the pet's voice (match its mood shown on the card — warm,
a little odd, never sycophantic). If the card shows a warning (worried mood,
junk-food diet, budget over), briefly say why in plain words and name the
one habit that would fix it. Nothing else.
`

// ClaudeDir returns ~/.claude. Presence is how setup detects the host.
func ClaudeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

func claudeCommandPath(claudeDir string) string {
	return filepath.Join(claudeDir, "commands", "aipet.md")
}

func claudeSettingsPath(claudeDir string) string {
	return filepath.Join(claudeDir, "settings.json")
}

// InstallClaude writes the three Claude Code integration pieces (§4.4
// items 1-3): the /aipet command file, the statusLine key, and the
// hooks.Stop/hooks.SessionStart entries. Each piece is independently
// idempotent (manifest hit or an existing entry found in the file itself
// skips that piece and reports "already installed") so a partial prior
// install — or a second `aipet setup` run — never duplicates work or data.
//
// dryPrint, when non-nil, receives what WOULD be written (full file
// contents, or a description of the settings.json change) instead of any
// file being touched — the --print path.
func InstallClaude(claudeDir string, m *Manifest, now time.Time, dryPrint func(string)) ([]Entry, []string, error) {
	var entries []Entry
	var notes []string

	// settings.json's checks run first and share one parsed copy: R6's
	// abort conditions (foreign statusLine, unparseable JSON, a hooks.<event>
	// that isn't an array) must be caught before ANY file is touched, or a
	// mid-run abort would leave the command file written but settings.json
	// not — a half-finished install. Loading it once also means the
	// statusLine and both hook events are checked/merged into a single
	// in-memory map before one write, rather than three separate
	// read-modify-write round trips racing each other.
	settingsPath := claudeSettingsPath(claudeDir)
	settingsEntries, settingsNotes, err := planSettings(settingsPath, m, now, dryPrint)
	if err != nil {
		return entries, notes, err
	}

	cmdPath := claudeCommandPath(claudeDir)
	if m.HasEntry(cmdPath, "") {
		notes = append(notes, "~/.claude/commands/aipet.md: already installed")
	} else if dryPrint != nil {
		dryPrint(fmt.Sprintf("WOULD WRITE %s:\n%s", cmdPath, claudeCommandMD))
	} else {
		e, err := writeCommandFile(cmdPath, "claude", now)
		if err != nil {
			return entries, notes, err
		}
		entries = append(entries, e)
		notes = append(notes, "~/.claude/commands/aipet.md: installed")
	}

	entries = append(entries, settingsEntries...)
	notes = append(notes, settingsNotes...)

	return entries, notes, nil
}

// planSettings validates and merges the statusLine + hooks.Stop +
// hooks.SessionStart pieces into settings.json's parsed content, aborting
// before any write if any piece fails R6's rules. All three pieces share one
// read-modify-write so a partial abort can never leave the file half-edited.
func planSettings(settingsPath string, m *Manifest, now time.Time, dryPrint func(string)) (entries []Entry, notes []string, err error) {
	statusDone := m.HasEntry(settingsPath, "statusLine")
	stopDone := m.HasEntry(settingsPath, "hooks.Stop")
	startDone := m.HasEntry(settingsPath, "hooks.SessionStart")
	if statusDone && stopDone && startDone {
		return nil, []string{
			"settings.json statusLine: already installed",
			"settings.json hooks.Stop: already installed",
			"settings.json hooks.SessionStart: already installed",
		}, nil
	}

	settings, err := loadSettingsJSON(settingsPath)
	if err != nil {
		return nil, nil, err
	}

	var changedStatus, changedStop, changedStart bool
	if !statusDone {
		changedStatus, _, err = ensureStatusLine(settings)
		if err != nil {
			return nil, nil, err
		}
	}
	if !stopDone {
		changedStop, err = ensureHookEntry(settings, "Stop")
		if err != nil {
			return nil, nil, err
		}
	}
	if !startDone {
		changedStart, err = ensureHookEntry(settings, "SessionStart")
		if err != nil {
			return nil, nil, err
		}
	}

	noteFor := func(done, changed bool, label string) string {
		switch {
		case done:
			return label + ": already installed"
		case !changed:
			return label + ": already installed (found in file)"
		case dryPrint != nil:
			return label + ": would install"
		default:
			return label + ": installed"
		}
	}
	notes = append(notes,
		noteFor(statusDone, changedStatus, "settings.json statusLine"),
		noteFor(stopDone, changedStop, "settings.json hooks.Stop"),
		noteFor(startDone, changedStart, "settings.json hooks.SessionStart"),
	)

	if !changedStatus && !changedStop && !changedStart {
		return nil, notes, nil // nothing new to write
	}
	if dryPrint != nil {
		dryPrint(fmt.Sprintf("WOULD MERGE %s (statusLine/hooks.Stop/hooks.SessionStart as noted above)", settingsPath))
		return nil, notes, nil
	}

	backup, err := writeSettingsJSON(settingsPath, settings, now)
	if err != nil {
		return nil, nil, err
	}
	if changedStatus {
		entries = append(entries, Entry{Host: "claude", File: settingsPath, Kind: KindJSONKey, JSONPath: "statusLine", Backup: backup})
	}
	if changedStop {
		entries = append(entries, Entry{Host: "claude", File: settingsPath, Kind: KindHookEntry, JSONPath: "hooks.Stop", Backup: backup})
	}
	if changedStart {
		entries = append(entries, Entry{Host: "claude", File: settingsPath, Kind: KindHookEntry, JSONPath: "hooks.SessionStart", Backup: backup})
	}
	return entries, notes, nil
}

// writeCommandFile backs up any existing file at path, then writes content
// atomically, returning the manifest entry to record.
func writeCommandFile(path, hostName string, now time.Time) (Entry, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Entry{}, err
	}
	backup, err := backupFile(path, now)
	if err != nil {
		return Entry{}, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(claudeCommandMDFor(hostName)), 0o600); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return Entry{}, err
	}
	return Entry{Host: hostName, File: path, Kind: KindFile, Backup: backup}, nil
}

// claudeCommandMDFor returns the command-file content for a host. Codex's
// content differs (§4.4 item 4); Claude's is claudeCommandMD. Centralized
// so writeCommandFile stays a single implementation for both hosts.
func claudeCommandMDFor(hostName string) string {
	if hostName == "codex" {
		return codexPromptMD
	}
	return claudeCommandMD
}

// RemoveClaude reverses every Claude-host entry in the manifest: deletes
// files we wrote, restores statusLine/hook entries to their pre-install
// state (only if they're still ours — see removeStatusLine/removeHookEntry).
// It is intentionally forgiving of a file already missing or already
// changed by the user: --remove's job is "get back to no-aipet", not to
// fail loudly on drift that happened after install.
func RemoveClaude(entries []Entry) []string {
	var notes []string
	// settings.json can carry multiple entries (statusLine + two hook
	// events); batch them so the file is loaded/saved once rather than
	// three times, and so a mid-batch failure doesn't leave it half-edited
	// relative to the manifest's bookkeeping.
	bySettingsFile := map[string][]Entry{}

	for _, e := range entries {
		switch e.Kind {
		case KindFile:
			if err := os.Remove(e.File); err != nil && !os.IsNotExist(err) {
				notes = append(notes, fmt.Sprintf("%s: failed to remove: %v", e.File, err))
			} else {
				notes = append(notes, fmt.Sprintf("%s: removed", e.File))
			}
		case KindJSONKey, KindHookEntry:
			bySettingsFile[e.File] = append(bySettingsFile[e.File], e)
		}
	}

	for file, es := range bySettingsFile {
		settings, err := loadSettingsJSON(file)
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s: could not read for removal: %v", file, err))
			continue
		}
		var anyChanged bool
		for _, e := range es {
			switch {
			case e.JSONPath == "statusLine":
				if removeStatusLine(settings) {
					anyChanged = true
				}
			case e.JSONPath == "hooks.Stop":
				if removeHookEntry(settings, "Stop") {
					anyChanged = true
				}
			case e.JSONPath == "hooks.SessionStart":
				if removeHookEntry(settings, "SessionStart") {
					anyChanged = true
				}
			}
		}
		if !anyChanged {
			notes = append(notes, fmt.Sprintf("%s: nothing to remove (already changed since install)", file))
			continue
		}
		if _, err := writeSettingsJSON(file, settings, time.Now()); err != nil {
			notes = append(notes, fmt.Sprintf("%s: failed to write during removal: %v", file, err))
			continue
		}
		notes = append(notes, fmt.Sprintf("%s: aipet entries removed", file))
	}

	return notes
}
