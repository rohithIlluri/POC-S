// Package pricing maps model identifiers to per-token costs so the companion can
// estimate spend entirely on-device. Prices are USD per 1M tokens. These defaults
// are bundled so the binary works offline; the enterprise feed can override them
// at runtime (see internal/feed) without shipping a new binary.
package pricing

import "strings"

// Rate holds per-million-token prices for a model. CacheWrite and CacheRead cover
// prompt-caching, which is where a lot of "invisible" enterprise spend hides.
type Rate struct {
	Input      float64 `json:"input"`       // per 1M input tokens
	Output     float64 `json:"output"`      // per 1M output tokens
	CacheWrite float64 `json:"cache_write"` // per 1M cache-creation tokens
	CacheRead  float64 `json:"cache_read"`  // per 1M cache-read tokens
}

// Table is a set of model rates. The zero value is unusable; use Default().
type Table struct {
	rates map[string]Rate
}

// Default returns the bundled price table. Keys are matched as case-insensitive
// substrings of the model id (e.g. "claude-opus-4-8" matches the "opus" entry),
// so new dated model ids keep working without an update.
func Default() *Table {
	return &Table{rates: map[string]Rate{
		// Anthropic (Claude Code)
		"opus":   {Input: 15, Output: 75, CacheWrite: 18.75, CacheRead: 1.50},
		"sonnet": {Input: 3, Output: 15, CacheWrite: 3.75, CacheRead: 0.30},
		"haiku":  {Input: 0.80, Output: 4, CacheWrite: 1.0, CacheRead: 0.08},
		// OpenAI (Codex) — representative coding-model rates.
		"gpt-5":     {Input: 1.25, Output: 10, CacheWrite: 1.25, CacheRead: 0.125},
		"gpt-4.1":   {Input: 2, Output: 8, CacheWrite: 2, CacheRead: 0.50},
		"o4-mini":   {Input: 1.10, Output: 4.40, CacheWrite: 1.10, CacheRead: 0.275},
		"codex":     {Input: 1.25, Output: 10, CacheWrite: 1.25, CacheRead: 0.125},
	}}
}

// Override merges feed-supplied rates over the defaults, keyed by the same
// substring convention. Unknown keys are simply added.
func (t *Table) Override(rates map[string]Rate) {
	for k, v := range rates {
		t.rates[strings.ToLower(k)] = v
	}
}

// Lookup returns the rate for a model id and whether a match was found. The
// longest matching key wins so "gpt-4.1" beats a hypothetical "gpt".
func (t *Table) Lookup(model string) (Rate, bool) {
	m := strings.ToLower(model)
	var best string
	for key := range t.rates {
		if strings.Contains(m, key) && len(key) > len(best) {
			best = key
		}
	}
	if best == "" {
		return Rate{}, false
	}
	return t.rates[best], true
}

// Usage is a normalized token count for one model turn, independent of provider.
type Usage struct {
	Input      int64
	Output     int64
	CacheWrite int64
	CacheRead  int64
}

// Cost returns the estimated USD cost of a turn. Unknown models cost 0 so the
// companion never invents spend it can't justify; the advisor flags unknowns.
func (t *Table) Cost(model string, u Usage) float64 {
	r, ok := t.Lookup(model)
	if !ok {
		return 0
	}
	const per = 1_000_000.0
	return float64(u.Input)/per*r.Input +
		float64(u.Output)/per*r.Output +
		float64(u.CacheWrite)/per*r.CacheWrite +
		float64(u.CacheRead)/per*r.CacheRead
}
