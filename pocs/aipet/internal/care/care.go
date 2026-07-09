// Package care is the advisor, reborn: the same explainable, local rules that
// used to produce spend-saving suggestions now classify a day's activity into
// a diet verdict — junk food, rich food, overeating, balanced, and so on —
// per GAME_DESIGN.md §4.2. The verdict drives the pet's XP multiplier, health
// delta, mood, and any status effect (e.g. token_bloat) for that day.
//
// Every rule remains explainable: a verdict always carries a plain-language
// reason so the pet's journal can say *why* it feels the way it does.
package care

import (
	"fmt"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
)

// Signal is one diet classification a day's digest can trigger. A day can
// carry multiple signals (e.g. both JunkFood and Fragmented); XPMultiplier
// and HealthDelta combine multiplicatively/additively across all signals
// that fire, then are clamped — see Evaluate.
type Signal string

const (
	Balanced    Signal = "balanced"     // healthy day: full XP, mood up
	JunkFood    Signal = "junk_food"    // low cache reuse: uncached re-sent prompts
	RichFood    Signal = "rich_food"    // priciest model dominates spend
	Overeating  Signal = "overeating"   // very large average context per turn
	Fragmented  Signal = "fragmented"   // many short/cold-start sessions
	ForagingCap Signal = "foraging_cap" // over the soft daily budget: extra tokens give 0 XP
	UnknownFood Signal = "unknown_food" // turns with an unpriced model
)

// Verdict is the full diet read for one day: which signals fired, the
// combined effect on XP/health/mood, and human-readable reasons for the
// pet's journal.
type Verdict struct {
	Signals       []Signal
	XPMultiplier  float64 // applied to the day's raw XP (see sim/tick.go), floor 0
	HealthDelta   int     // added to the pet's health, clamped 0..100 by the caller
	MoodPenalty   bool    // true if this day should pull mood toward "tired"/"worried"
	TokenBloat    bool    // true if the day should apply the token_bloat status
	Reasons       []string
	AtForagingCap bool // true if excess tokens beyond the budget earned no XP
}

// Thresholds mirror internal/advisor's rule constants exactly, so the
// coaching behavior a v1 user already saw is unchanged — it just now grows a
// pet instead of printing a suggestion list.
const (
	lowCacheRatio        = 0.30 // below this, cache reuse counts as "junk food"
	richFoodShare        = 0.60 // one model >=60% of the day's cost counts as "rich food"
	richFoodMinCostUSD   = 0.50 // ...but only once spend is non-trivial
	overeatingAvgTokens  = 120_000
	fragmentedMinSess    = 3 // a day needs at least this many sessions to read as "fragmented" rather than just quiet
	fragmentedMaxPerSess = 4
)

// Evaluate classifies one day's digest (plus its cost breakdown, since Digest
// itself doesn't carry per-model cost — the caller passes it explicitly from
// store.Stats/collector output) into a Verdict. dailyBudgetUSD <= 0 disables
// the foraging cap.
func Evaluate(d sim.Digest, richestModelShare float64, richestModelCost float64, dailyBudgetUSD float64) Verdict {
	v := Verdict{XPMultiplier: 1.0}

	cacheRatio := d.CacheRatio()
	if d.TokensIn+d.CacheRead >= 50_000 && cacheRatio < lowCacheRatio {
		v.Signals = append(v.Signals, JunkFood)
		v.XPMultiplier *= 0.75
		v.HealthDelta -= 6
		v.MoodPenalty = true
		v.Reasons = append(v.Reasons, fmt.Sprintf(
			"Ate %s uncached tokens today (%.0f%% cache reuse). Warm the cache and I'll perk up.",
			humanCount(d.TokensIn), cacheRatio*100))
	}

	if richestModelCost >= richFoodMinCostUSD && richestModelShare >= richFoodShare {
		v.Signals = append(v.Signals, RichFood)
		v.XPMultiplier *= 0.9
		v.Reasons = append(v.Reasons, fmt.Sprintf(
			"Fancy model today (%.0f%% of spend). Tasted great. A little rich for every day, though.",
			richestModelShare*100))
	}

	if d.Turns > 0 {
		avgTokens := (d.TokensIn + d.CacheRead) / int64(d.Turns)
		if avgTokens >= overeatingAvgTokens {
			v.Signals = append(v.Signals, Overeating)
			v.HealthDelta -= 4
			v.TokenBloat = true
			v.Reasons = append(v.Reasons, fmt.Sprintf(
				"That context was bigger than it needed to be (~%dk tokens/turn). Not complaining. Loudly.",
				avgTokens/1000))
		}
	}

	if d.Sessions > 0 {
		perSession := d.Turns / d.Sessions
		if d.Sessions >= fragmentedMinSess && perSession < fragmentedMaxPerSess {
			v.Signals = append(v.Signals, Fragmented)
			v.MoodPenalty = true
			v.Reasons = append(v.Reasons, fmt.Sprintf(
				"%d cold starts today. Barely got settled before we moved on again.", d.Fragmented))
		}
	}

	if dailyBudgetUSD > 0 && d.CostUSD >= dailyBudgetUSD {
		v.Signals = append(v.Signals, ForagingCap)
		v.AtForagingCap = true
		v.XPMultiplier = 0 // past the soft budget: extra tokens give zero XP for the day
		v.Reasons = append(v.Reasons, "Hit today's foraging limit. Full, not starving — XP resumes tomorrow.")
	}

	if len(v.Signals) == 0 {
		v.Signals = append(v.Signals, Balanced)
		v.HealthDelta += 4
		v.Reasons = append(v.Reasons, "Good mix today — right-sized model, warm cache, tight scope. Textbook.")
	}

	if v.XPMultiplier < 0 {
		v.XPMultiplier = 0
	}
	return v
}

// AsSimVerdict adapts a care.Verdict to the narrower sim.DietVerdict shape
// internal/sim actually consumes, keeping the dependency direction one-way
// (care depends on sim's Digest type; sim never imports care).
func (v Verdict) AsSimVerdict() sim.DietVerdict {
	return sim.DietVerdict{
		XPMultiplier:  v.XPMultiplier,
		HealthDelta:   v.HealthDelta,
		MoodPenalty:   v.MoodPenalty,
		TokenBloat:    v.TokenBloat,
		AtForagingCap: v.AtForagingCap,
	}
}

// IsHealthyDiet reports whether a day's digest, evaluated at the given
// budget, qualifies as the "balanced diet" bar used elsewhere in the design
// (docs/design/rarity.md §2.2: no token bloat, cache-read ratio >= 1/2,
// under budget) — a stricter bar than Evaluate's own Balanced signal, kept
// separate because rarity/encounter code needs the exact threshold.
func IsHealthyDiet(d sim.Digest, dailyBudgetUSD float64) bool {
	if d.CacheRatio() < 0.5 {
		return false
	}
	if dailyBudgetUSD > 0 && d.CostUSD >= dailyBudgetUSD {
		return false
	}
	if d.Turns > 0 {
		avgTokens := (d.TokensIn + d.CacheRead) / int64(d.Turns)
		if avgTokens >= overeatingAvgTokens {
			return false
		}
	}
	return true
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
