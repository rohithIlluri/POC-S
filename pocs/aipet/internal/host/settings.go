package host

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// AbortError marks a setup failure that must leave every file untouched —
// an unparseable settings.json, a foreign statusLine, a hooks.<Event> that
// isn't an array. Callers (the setup CLI) print Msg and point at --print
// rather than treating it as a generic error, per R6.
type AbortError struct {
	Msg string
}

func (e *AbortError) Error() string { return e.Msg }

// loadSettingsJSON reads and parses settings.json into a generic map, the
// only representation that can round-trip arbitrary third-party keys
// untouched (a typed struct would silently drop anything aipet doesn't know
// about). A missing file is not an error — it starts as an empty object, the
// same as a fresh Claude Code install with no settings yet. An unparseable
// file IS an abort (R6): guessing at malformed JSON risks corrupting
// something another tool depends on.
func loadSettingsJSON(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, &AbortError{Msg: fmt.Sprintf("%s is not valid JSON, refusing to touch it (see: aipet setup --print): %v", path, err)}
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// writeSettingsJSON backs up the file's current contents (if any) into this
// run's backup directory, then writes the merged map atomically (tmp +
// rename). Returns the backup path for the manifest (empty if the file was
// new).
func writeSettingsJSON(path string, m map[string]any, now time.Time) (backup string, err error) {
	backup, err = backupFile(path, now)
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return backup, nil
}

// ourStatusLineCommand is the exact statusLine value setup writes, and the
// only one it considers "already ours" on a repeat run or during --remove.
const ourStatusLineCommand = "aipet statusline"

// ensureStatusLine sets settings["statusLine"] to aipet's command, merging
// (never clobbering) per R6: a statusLine already present that isn't ours
// aborts the whole run — the doc's `--print` shows what would have been
// written instead. Returns whether it made a change (false = already
// installed, idempotent no-op) and the prior value if any (for manifest
// bookkeeping, so --remove can restore it — though by construction the only
// prior value --remove will ever see is either absent or our own).
func ensureStatusLine(m map[string]any) (changed bool, priorRaw any, err error) {
	existing, present := m["statusLine"]
	if present {
		if isOurStatusLine(existing) {
			return false, existing, nil // already installed, idempotent
		}
		return false, existing, &AbortError{Msg: "settings.json already has a custom statusLine — refusing to overwrite it (see: aipet setup --print)"}
	}
	m["statusLine"] = map[string]any{"type": "command", "command": ourStatusLineCommand}
	return true, nil, nil
}

func isOurStatusLine(v any) bool {
	obj, ok := v.(map[string]any)
	if !ok {
		return false
	}
	cmd, _ := obj["command"].(string)
	typ, _ := obj["type"].(string)
	return typ == "command" && cmd == ourStatusLineCommand
}

// ourHookCommand is the exact hook command setup appends to hooks.Stop and
// hooks.SessionStart.
const ourHookCommand = "aipet collect --quiet"

// ensureHookEntry appends aipet's hook group to settings["hooks"][event]
// (event is "Stop" or "SessionStart") without touching any existing entry.
// Per R6, hooks.<event> present but not a JSON array is an abort — some
// other tool may have written a shape aipet doesn't understand, and
// guessing risks corrupting it. Already having our own entry (matched by
// command substring, belt-and-braces alongside the manifest check per R6)
// is a no-op, not a duplicate append.
func ensureHookEntry(m map[string]any, event string) (changed bool, err error) {
	hooksRaw, hasHooks := m["hooks"]
	var hooksObj map[string]any
	if hasHooks {
		obj, ok := hooksRaw.(map[string]any)
		if !ok {
			return false, &AbortError{Msg: "settings.json's \"hooks\" key is not an object — refusing to touch it (see: aipet setup --print)"}
		}
		hooksObj = obj
	} else {
		hooksObj = map[string]any{}
		m["hooks"] = hooksObj
	}

	eventRaw, hasEvent := hooksObj[event]
	var eventArr []any
	if hasEvent {
		arr, ok := eventRaw.([]any)
		if !ok {
			return false, &AbortError{Msg: fmt.Sprintf("settings.json's \"hooks.%s\" is not an array — refusing to touch it (see: aipet setup --print)", event)}
		}
		eventArr = arr
		if hasOurHookEntry(eventArr) {
			return false, nil // already installed, idempotent
		}
	}

	entry := map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": ourHookCommand, "timeout": float64(30)},
		},
	}
	hooksObj[event] = append(eventArr, entry)
	return true, nil
}

// hasOurHookEntry scans an existing hooks.<event> array for a group that
// already contains our exact command — the belt-and-braces check R6 asks
// for alongside the manifest lookup, so a manifest lost or edited out from
// under aipet still can't produce a duplicate hook entry.
func hasOurHookEntry(arr []any) bool {
	for _, groupRaw := range arr {
		group, ok := groupRaw.(map[string]any)
		if !ok {
			continue
		}
		inner, ok := group["hooks"].([]any)
		if !ok {
			continue
		}
		for _, hRaw := range inner {
			h, ok := hRaw.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := h["command"].(string); cmd == ourHookCommand {
				return true
			}
		}
	}
	return false
}

// removeStatusLine deletes settings["statusLine"] only if it is still ours
// — if a user (or another tool) has since changed it, --remove leaves it
// alone rather than deleting a value it no longer recognizes.
func removeStatusLine(m map[string]any) (changed bool) {
	existing, present := m["statusLine"]
	if !present || !isOurStatusLine(existing) {
		return false
	}
	delete(m, "statusLine")
	return true
}

// removeHookEntry strips only the group(s) matching our exact command out
// of hooks.<event>, leaving every other entry (another tool's hooks, or a
// user's own) byte-for-byte untouched. Cleans up the surrounding "hooks"
// object/array if they become empty, so --remove doesn't leave visible
// clutter behind.
func removeHookEntry(m map[string]any, event string) (changed bool) {
	hooksRaw, ok := m["hooks"].(map[string]any)
	if !ok {
		return false
	}
	arr, ok := hooksRaw[event].([]any)
	if !ok {
		return false
	}
	var kept []any
	for _, groupRaw := range arr {
		group, ok := groupRaw.(map[string]any)
		if !ok {
			kept = append(kept, groupRaw)
			continue
		}
		inner, ok := group["hooks"].([]any)
		if !ok {
			kept = append(kept, groupRaw)
			continue
		}
		var innerKept []any
		for _, hRaw := range inner {
			h, ok := hRaw.(map[string]any)
			if ok {
				if cmd, _ := h["command"].(string); cmd == ourHookCommand {
					changed = true
					continue
				}
			}
			innerKept = append(innerKept, hRaw)
		}
		if len(innerKept) == 0 {
			continue // the whole group was ours; drop it
		}
		group["hooks"] = innerKept
		kept = append(kept, group)
	}
	if !changed {
		return false
	}
	if len(kept) == 0 {
		delete(hooksRaw, event)
	} else {
		hooksRaw[event] = kept
	}
	if len(hooksRaw) == 0 {
		delete(m, "hooks")
	}
	return true
}
