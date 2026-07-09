// Package pricing maps model identifiers to per-token costs so the companion can
// estimate spend entirely on-device. Prices are USD per 1M tokens. The table is
// bundled so the binary works fully offline.
package pricing

import "strings"

// Rate holds per-million-token prices for a model. CacheWrite and CacheRead cover
// prompt-caching, which is where a lot of "invisible" token spend hides.
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
		// Fable is a Claude Code model id observed in session logs; public list
		// prices are not published, so we approximate at Sonnet rates to avoid
		// silently under-counting spend (unknown models cost $0 otherwise).
		"fable": {Input: 3, Output: 15, CacheWrite: 3.75, CacheRead: 0.30},
		// OpenAI (Codex) — representative coding-model rates.
		"gpt-5":   {Input: 1.25, Output: 10, CacheWrite: 1.25, CacheRead: 0.125},
		"gpt-4.1": {Input: 2, Output: 8, CacheWrite: 2, CacheRead: 0.50},
		"o4-mini": {Input: 1.10, Output: 4.40, CacheWrite: 1.10, CacheRead: 0.275},
		"codex":   {Input: 1.25, Output: 10, CacheWrite: 1.25, CacheRead: 0.125},
	}}
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

// Known reports whether the table has a rate for model (substring match).
func (t *Table) Known(model string) bool {
	_, ok := t.Lookup(model)
	return ok
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

// RepriceEvent recomputes an event's cost from the current table when the
// model is known, leaving it unchanged (typically $0) otherwise. Historical
// rows written while a model was unpriced stay at $0 on disk; callers use
// this so live stats reflect today's rates without rewriting the
// append-only log.
func (t *Table) RepriceEvent(model string, input, output, cacheWrite, cacheRead int64, cost float64) float64 {
	if !t.Known(model) {
		return cost
	}
	return t.Cost(model, Usage{
		Input: input, Output: output, CacheWrite: cacheWrite, CacheRead: cacheRead,
	})
}
