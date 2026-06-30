// Package config holds the companion's local configuration and resolves the
// on-disk locations it reads from and writes to. Everything lives under the
// user's home directory; nothing here points at a network location except the
// enterprise feed URL, which an admin sets explicitly.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the companion's settings. It is intentionally small: the POC favors
// sensible local defaults over a sprawling config surface.
type Config struct {
	// FeedURL is the enterprise-hosted manifest the binary polls for tips,
	// pricing overrides, and self-update info. Empty means "local sample feed".
	FeedURL string `json:"feed_url"`

	// FeedPublicKey is the base64 ed25519 key used to verify feed signatures.
	// Empty disables verification (acceptable for the local POC feed only).
	FeedPublicKey string `json:"feed_public_key"`

	// DailyBudgetUSD is a soft per-developer guidance budget. The companion never
	// blocks work — it nudges. 0 disables budget nudges.
	DailyBudgetUSD float64 `json:"daily_budget_usd"`

	// PollInterval is how often the daemon refreshes the feed, in minutes.
	PollIntervalMin int `json:"poll_interval_min"`
}

// Default returns config with safe local-only defaults.
func Default() Config {
	return Config{
		FeedURL:         "", // local sample feed by default
		DailyBudgetUSD:  10,
		PollIntervalMin: 360, // 6h
	}
}

// Dir returns the companion's home (~/.aipet), creating it if needed.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".aipet")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// Path returns the path to the config file.
func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// Load reads config from disk, falling back to defaults for a missing file.
func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes config to disk with restrictive permissions.
func (c Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// DBPath returns the path to the local SQLite-free aggregation store.
func DBPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "usage.db"), nil
}

// ClaudeProjectsDir returns ~/.claude/projects, the Claude Code session root.
func ClaudeProjectsDir() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	p := filepath.Join(home, ".claude", "projects")
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		return p, true
	}
	return p, false
}

// CodexSessionsDir returns ~/.codex/sessions, the Codex session root.
func CodexSessionsDir() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	p := filepath.Join(home, ".codex", "sessions")
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		return p, true
	}
	return p, false
}
