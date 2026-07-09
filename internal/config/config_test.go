package config

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateHome redirects the user home directory to a temp dir so tests never
// read or write the developer's real ~/.aipet state.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestLoadDefaultsWhenMissing(t *testing.T) {
	isolateHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	def := Default()
	if cfg != def {
		t.Errorf("missing file should yield defaults: got %+v want %+v", cfg, def)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	isolateHome(t)
	want := Config{
		DailyBudgetUSD:     25.5,
		CollectIntervalMin: 7,
	}
	if err := want.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("roundtrip mismatch: got %+v want %+v", got, want)
	}
}

func TestConfigFilePermissions(t *testing.T) {
	isolateHome(t)
	if err := Default().Save(); err != nil {
		t.Fatal(err)
	}
	p, _ := Path()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config should be 0600, got %o", perm)
	}
	// The ~/.aipet dir itself must be user-only too.
	di, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("~/.aipet should be 0700, got %o", perm)
	}
}

func TestLoadRejectsCorruptJSON(t *testing.T) {
	isolateHome(t)
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Error("corrupt config should return an error, not silently reset")
	}
}

func TestPartialConfigKeepsDefaults(t *testing.T) {
	isolateHome(t)
	p, _ := Path()
	// Only one key set — the rest must come from defaults, not zero values.
	if err := os.WriteFile(p, []byte(`{"daily_budget_usd": 3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DailyBudgetUSD != 3 {
		t.Errorf("explicit key lost: %v", cfg.DailyBudgetUSD)
	}
	if cfg.CollectIntervalMin != Default().CollectIntervalMin {
		t.Errorf("unset key should keep default %d, got %d", Default().CollectIntervalMin, cfg.CollectIntervalMin)
	}
}

func TestToolDirDetection(t *testing.T) {
	home := isolateHome(t)
	if _, ok := ClaudeProjectsDir(); ok {
		t.Error("empty home should have no claude dir")
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := ClaudeProjectsDir(); !ok {
		t.Error("claude projects dir should be detected after creation")
	}
	if _, ok := CodexSessionsDir(); ok {
		t.Error("codex dir should not be detected when absent")
	}
}
