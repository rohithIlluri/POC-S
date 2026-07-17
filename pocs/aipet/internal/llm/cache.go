package llm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
)

// voiceCache is the on-disk memo that makes api mode cost one call per day
// instead of one per /aipet: the generated line is reused for the whole
// (day, mood, personality) tuple, and CallsToday enforces the daily cap
// across mood changes.
type voiceCache struct {
	Day         string `json:"day"`
	Mood        string `json:"mood"`
	Personality string `json:"personality"`
	Line        string `json:"line"`
	CallsToday  int    `json:"calls_today"`
}

func cachePath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "voice_cache.json"), nil
}

func loadCache() voiceCache {
	var c voiceCache
	p, err := cachePath()
	if err != nil {
		return c
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c) // corrupt cache = empty cache; it regenerates
	return c
}

func saveCache(c voiceCache) {
	p, err := cachePath()
	if err != nil {
		return
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return // best-effort: a lost cache only means one extra tiny call
	}
	_ = os.Rename(tmp, p)
}

// Line is the public entry for api-mode voice: cached line if today's
// state matches, otherwise ONE budgeted generation — and ("", false) on
// any failure or cap hit, telling the caller to use the embedded canned
// line instead. It never returns an error: api voice is a garnish with a
// built-in downgrade path, not a dependency.
//
// extraOpts is test plumbing (mock server base URL/key); production
// callers pass none.
func Line(ctx context.Context, model, personality string, isEgg bool, mood, statusHint, day string, extraOpts ...option.RequestOption) (string, bool) {
	if model == "" {
		model = DefaultModel
	}
	c := loadCache()
	if c.Day == day && c.Mood == mood && c.Personality == personality && c.Line != "" {
		return c.Line, true
	}

	calls := c.CallsToday
	if c.Day != day {
		calls = 0
	}
	if calls >= maxCallsPerDay {
		return "", false
	}

	line, err := generateLine(ctx, model, personality, isEgg, mood, statusHint, extraOpts...)
	if err != nil || line == "" {
		// Count the attempt, not just successes: a failed call costs no
		// tokens, but without this an offline machine (or broken key)
		// would re-dial the API on every /aipet all day. The cap bounds
		// network attempts as well as spend; tomorrow it resets.
		saveCache(voiceCache{Day: day, Mood: c.Mood, Personality: c.Personality, Line: c.Line, CallsToday: calls + 1})
		return "", false
	}
	saveCache(voiceCache{Day: day, Mood: mood, Personality: personality, Line: line, CallsToday: calls + 1})
	return line, true
}
