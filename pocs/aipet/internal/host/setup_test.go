package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// isolateHome points HOME at a temp dir so setup writes only test-owned
// state — mirrors internal/daemon's isolateHome helper (R7: every
// setup/collect/card test must isolate HOME; no test may touch the real
// ~/.claude or ~/.codex).
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func mkClaudeDir(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func mkCodexDir(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}

// TestInstallFreshWritesAllFiles: a fresh install with both hosts present
// must write the command file(s), statusLine, both hook events, and a valid
// manifest.
func TestInstallFreshWritesAllFiles(t *testing.T) {
	home := isolateHome(t)
	mkClaudeDir(t, home)
	mkCodexDir(t, home)

	res, err := Install(Options{Now: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if res.NoHosts || res.WindowsSkip {
		t.Fatalf("unexpected result: %+v", res)
	}

	cmdPath := filepath.Join(home, ".claude", "commands", "aipet.md")
	if b, err := os.ReadFile(cmdPath); err != nil {
		t.Errorf("command file not written: %v", err)
	} else if string(b) != claudeCommandMD {
		t.Errorf("command file content mismatch:\n%s", string(b))
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	settings := readJSON(t, settingsPath)
	sl, ok := settings["statusLine"].(map[string]any)
	if !ok || sl["command"] != "aipet statusline" {
		t.Errorf("statusLine not installed: %+v", settings["statusLine"])
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks missing: %+v", settings)
	}
	for _, event := range []string{"Stop", "SessionStart"} {
		arr, ok := hooks[event].([]any)
		if !ok || len(arr) != 1 {
			t.Errorf("hooks.%s not installed correctly: %+v", event, hooks[event])
		}
	}

	codexPath := filepath.Join(home, ".codex", "prompts", "aipet.md")
	if b, err := os.ReadFile(codexPath); err != nil {
		t.Errorf("codex prompt not written: %v", err)
	} else if string(b) != codexPromptMD {
		t.Errorf("codex prompt content mismatch:\n%s", string(b))
	}

	m, ok, err := LoadManifest()
	if err != nil || !ok {
		t.Fatalf("manifest not found/loadable: ok=%v err=%v", ok, err)
	}
	if len(m.Entries) == 0 {
		t.Error("manifest has no entries")
	}
}

// TestInstallTwiceIsIdempotent: a second Install call must not duplicate
// any writes — byte-identical files, no doubled hook entries.
func TestInstallTwiceIsIdempotent(t *testing.T) {
	home := isolateHome(t)
	mkClaudeDir(t, home)
	mkCodexDir(t, home)

	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if _, err := Install(Options{Now: now}); err != nil {
		t.Fatal(err)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	cmdPath := filepath.Join(home, ".claude", "commands", "aipet.md")
	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeCmd, err := os.ReadFile(cmdPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Install(Options{Now: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	afterCmd, err := os.ReadFile(cmdPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(before) != string(after) {
		t.Errorf("settings.json changed on second install:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if string(beforeCmd) != string(afterCmd) {
		t.Error("command file changed on second install")
	}

	settings := readJSON(t, settingsPath)
	hooks := settings["hooks"].(map[string]any)
	for _, event := range []string{"Stop", "SessionStart"} {
		arr := hooks[event].([]any)
		if len(arr) != 1 {
			t.Errorf("hooks.%s should still have exactly 1 entry after second install, got %d", event, len(arr))
		}
	}
}

// TestInstallAbortsOnForeignStatusLine: a pre-populated custom statusLine
// that isn't ours must abort the whole run, leaving the file untouched.
func TestInstallAbortsOnForeignStatusLine(t *testing.T) {
	home := isolateHome(t)
	mkClaudeDir(t, home)

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	original := `{"statusLine": {"type": "command", "command": "my-custom-statusline"}, "theme": "dark"}`
	if err := os.WriteFile(settingsPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Install(Options{Claude: true, Now: time.Now()})
	if err == nil {
		t.Fatal("expected an abort error for a foreign statusLine")
	}
	if _, ok := err.(*AbortError); !ok {
		t.Errorf("expected *AbortError, got %T: %v", err, err)
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Errorf("settings.json was modified despite the abort:\nbefore: %s\nafter:  %s", original, after)
	}

	// Command file and manifest must not have been written either — the
	// abort must be all-or-nothing for the run, not leave a half-installed
	// command file.
	if _, err := os.Stat(filepath.Join(home, ".claude", "commands", "aipet.md")); !os.IsNotExist(err) {
		t.Error("command file should not exist after an aborted install")
	}
}

// TestInstallAppendsToForeignHooks: existing hooks.Stop entries from
// another tool must survive byte-identical, with only our entry appended.
func TestInstallAppendsToForeignHooks(t *testing.T) {
	home := isolateHome(t)
	mkClaudeDir(t, home)

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	original := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{"type": "command", "command": "some-other-tool --flag", "timeout": float64(10)},
					},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(original, "", "  ")
	if err := os.WriteFile(settingsPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(Options{Claude: true, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, settingsPath)
	hooks := settings["hooks"].(map[string]any)
	stopArr := hooks["Stop"].([]any)
	if len(stopArr) != 2 {
		t.Fatalf("expected 2 entries in hooks.Stop (foreign + ours), got %d: %+v", len(stopArr), stopArr)
	}

	foreignGroup := stopArr[0].(map[string]any)
	foreignInner := foreignGroup["hooks"].([]any)[0].(map[string]any)
	if foreignInner["command"] != "some-other-tool --flag" {
		t.Errorf("foreign hook entry was altered: %+v", foreignInner)
	}
	if foreignGroup["matcher"] != "*" {
		t.Errorf("foreign hook group's matcher was altered: %+v", foreignGroup)
	}

	ourGroup := stopArr[1].(map[string]any)
	ourInner := ourGroup["hooks"].([]any)[0].(map[string]any)
	if ourInner["command"] != "aipet collect --quiet" {
		t.Errorf("our hook entry not appended correctly: %+v", ourInner)
	}
}

// TestInstallAbortsOnUnparseableSettings: malformed JSON must abort and
// leave the file untouched.
func TestInstallAbortsOnUnparseableSettings(t *testing.T) {
	home := isolateHome(t)
	mkClaudeDir(t, home)

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	original := `{ this is not valid json`
	if err := os.WriteFile(settingsPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Install(Options{Claude: true, Now: time.Now()})
	if err == nil {
		t.Fatal("expected an abort error for unparseable JSON")
	}
	if _, ok := err.(*AbortError); !ok {
		t.Errorf("expected *AbortError, got %T: %v", err, err)
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Error("settings.json was modified despite being unparseable")
	}
}

// TestRemoveRestoresPreInstallState: --remove must delete our files, strip
// only our settings.json entries, and leave foreign hooks intact.
func TestRemoveRestoresPreInstallState(t *testing.T) {
	home := isolateHome(t)
	mkClaudeDir(t, home)
	mkCodexDir(t, home)

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	foreignHook := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "foreign-tool --run", "timeout": float64(5)},
					},
				},
			},
		},
		"theme": "dark",
	}
	b, _ := json.MarshalIndent(foreignHook, "", "  ")
	if err := os.WriteFile(settingsPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(Options{Now: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if _, err := Remove(); err != nil {
		t.Fatal(err)
	}

	// Our files gone.
	if _, err := os.Stat(filepath.Join(home, ".claude", "commands", "aipet.md")); !os.IsNotExist(err) {
		t.Error("command file should be removed")
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "prompts", "aipet.md")); !os.IsNotExist(err) {
		t.Error("codex prompt should be removed")
	}
	if _, _, err := LoadManifest(); err == nil {
		if m, ok, _ := LoadManifest(); ok {
			t.Errorf("manifest should be gone, got %+v", m)
		}
	}

	// settings.json: foreign hook intact, ours gone, unrelated keys intact.
	settings := readJSON(t, settingsPath)
	if settings["theme"] != "dark" {
		t.Errorf("unrelated key 'theme' should survive removal, got %+v", settings["theme"])
	}
	if _, has := settings["statusLine"]; has {
		t.Error("our statusLine should be removed")
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("hooks key should still exist (foreign entry remains)")
	}
	stopArr, ok := hooks["Stop"].([]any)
	if !ok || len(stopArr) != 1 {
		t.Fatalf("expected exactly the foreign Stop entry to remain, got %+v", hooks["Stop"])
	}
	group := stopArr[0].(map[string]any)
	inner := group["hooks"].([]any)
	if len(inner) != 1 || inner[0].(map[string]any)["command"] != "foreign-tool --run" {
		t.Errorf("foreign hook entry was not preserved: %+v", inner)
	}
	// SessionStart had only our entry, so it should be gone entirely.
	if _, has := hooks["SessionStart"]; has {
		t.Errorf("hooks.SessionStart should be fully removed (it only ever held our entry), got %+v", hooks["SessionStart"])
	}
}

// TestNoHostsFound: with neither ~/.claude nor ~/.codex present, Install
// must report NoHosts and write nothing.
func TestNoHostsFound(t *testing.T) {
	home := isolateHome(t)

	res, err := Install(Options{Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoHosts {
		t.Errorf("expected NoHosts, got %+v", res)
	}
	if _, ok, _ := LoadManifest(); ok {
		t.Error("no manifest should be written when no hosts are found")
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == ".aipet" {
			aipetEntries, _ := os.ReadDir(filepath.Join(home, ".aipet"))
			if len(aipetEntries) != 0 {
				t.Errorf("~/.aipet should be empty, got %v", aipetEntries)
			}
		}
	}
}

// TestPrintWritesNothing: --print must describe the would-be writes without
// touching disk at all (no files, no manifest).
func TestPrintWritesNothing(t *testing.T) {
	home := isolateHome(t)
	mkClaudeDir(t, home)
	mkCodexDir(t, home)

	res, err := Install(Options{Print: true, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) == 0 {
		t.Error("--print should describe what would be written")
	}

	if _, err := os.Stat(filepath.Join(home, ".claude", "commands", "aipet.md")); !os.IsNotExist(err) {
		t.Error("--print must not write the command file")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Error("--print must not write settings.json")
	}
	if _, ok, _ := LoadManifest(); ok {
		t.Error("--print must not write a manifest")
	}
}

// TestClaudeOnlyFlagRestrictsHost: --claude with both hosts present should
// install only for Claude.
func TestClaudeOnlyFlagRestrictsHost(t *testing.T) {
	home := isolateHome(t)
	mkClaudeDir(t, home)
	mkCodexDir(t, home)

	if _, err := Install(Options{Claude: true, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "commands", "aipet.md")); err != nil {
		t.Error("claude command file should be installed")
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "prompts", "aipet.md")); !os.IsNotExist(err) {
		t.Error("codex prompt should NOT be installed when --claude is passed alone")
	}
}
