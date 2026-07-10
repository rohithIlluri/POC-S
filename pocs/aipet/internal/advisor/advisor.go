// Package advisor turns observed usage into concrete, local suggestions for
// spending fewer tokens and working more efficiently. Every rule runs on already
// collected data — it never calls a model — so the guidance itself is free.
//
// Rules are deliberately explainable: each suggestion states what was observed,
// why it costs money, and the specific action to take.
package advisor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// Severity ranks how much a suggestion is worth acting on.
type Severity int

const (
	Info Severity = iota
	Tip
	Warn
)

func (s Severity) String() string {
	switch s {
	case Warn:
		return "warn"
	case Tip:
		return "tip"
	default:
		return "info"
	}
}

// Suggestion is one piece of advice with the evidence behind it.
type Suggestion struct {
	ID        string
	Severity  Severity
	Title     string
	Detail    string
	SavingUSD float64 // estimated potential daily saving, 0 if not quantifiable
	Source    string  // suggestion origin; always "rule" in v1
}

// Inputs is everything a rule may inspect.
type Inputs struct {
	Stats          store.Stats
	Events         []store.Event
	DailyBudgetUSD float64
}

// Rule evaluates inputs and returns zero or more suggestions.
type Rule func(Inputs) []Suggestion

// DefaultRules is the bundled rule set. New rules can be appended freely.
func DefaultRules() []Rule {
	return []Rule{
		ruleBudget,
		ruleOpusOveruse,
		ruleCacheMisses,
		ruleContextBloat,
		ruleUnknownModel,
		ruleIdleSessions,
	}
}

// Run executes all rules and returns suggestions sorted by severity then saving.
func Run(in Inputs, rules []Rule) []Suggestion {
	var out []Suggestion
	for _, r := range rules {
		out = append(out, r(in)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		return out[i].SavingUSD > out[j].SavingUSD
	})
	return out
}

// ruleBudget warns when today's spend is tracking past the soft daily budget.
func ruleBudget(in Inputs) []Suggestion {
	if in.DailyBudgetUSD <= 0 || in.Stats.TodayCost == 0 {
		return nil
	}
	ratio := in.Stats.TodayCost / in.DailyBudgetUSD
	switch {
	case ratio >= 1.0:
		return []Suggestion{{
			ID: "budget-over", Severity: Warn, Source: "rule",
			Title: fmt.Sprintf("Over today's budget: $%.2f of $%.2f", in.Stats.TodayCost, in.DailyBudgetUSD),
			Detail: "You've passed your soft daily guidance budget. Nothing is blocked — but " +
				"consider switching to a cheaper model for routine edits and trimming context.",
		}}
	case ratio >= 0.75:
		return []Suggestion{{
			ID: "budget-near", Severity: Tip, Source: "rule",
			Title:  fmt.Sprintf("Approaching budget: $%.2f of $%.2f", in.Stats.TodayCost, in.DailyBudgetUSD),
			Detail: "You're at 75%+ of today's budget. A good moment to wrap up large sessions.",
		}}
	}
	return nil
}

// ruleOpusOveruse flags heavy spend on the most expensive model when a cheaper
// one would likely do, estimating the saving from a model swap.
func ruleOpusOveruse(in Inputs) []Suggestion {
	var opusCost float64
	for model, cost := range in.Stats.ByModel {
		if strings.Contains(strings.ToLower(model), "opus") {
			opusCost += cost
		}
	}
	if opusCost < 0.5 || in.Stats.TotalCost == 0 {
		return nil
	}
	share := opusCost / in.Stats.TotalCost
	if share < 0.6 {
		return nil
	}
	// Sonnet is ~5x cheaper input/output than Opus; assume half of Opus turns
	// could move to Sonnet for a conservative ~40% saving on that portion.
	saving := opusCost * 0.5 * 0.8
	return []Suggestion{{
		ID: "opus-overuse", Severity: Tip, Source: "rule", SavingUSD: saving,
		Title:  fmt.Sprintf("Opus is %.0f%% of your spend ($%.2f)", share*100, opusCost),
		Detail: "Reserve Opus for hard reasoning. For routine edits, refactors, and tests, switch to Sonnet (~5x cheaper) — you can change model mid-session.",
	}}
}

// ruleCacheMisses spots large input volume with little cache reuse, which means
// the same context is being re-sent and re-billed instead of cached.
func ruleCacheMisses(in Inputs) []Suggestion {
	in0 := in.Stats.TokensIn
	cr := in.Stats.CacheRead
	if in0 < 200_000 {
		return nil
	}
	if cr*100 >= in0*30 { // >=30% cache reuse is healthy
		return nil
	}
	return []Suggestion{{
		ID: "low-cache", Severity: Tip, Source: "rule",
		Title:  "Low prompt-cache reuse",
		Detail: "Large prompts are being re-sent without cache hits. Keep a stable file/context set within a session and avoid reshuffling early messages so caching kicks in — cached reads cost ~10x less.",
	}}
}

// ruleContextBloat flags sessions whose average input per turn is very large,
// a sign of an overstuffed context window driving cost.
func ruleContextBloat(in Inputs) []Suggestion {
	if in.Stats.Turns == 0 {
		return nil
	}
	avgIn := (in.Stats.TokensIn + in.Stats.CacheRead) / int64(in.Stats.Turns)
	if avgIn < 120_000 {
		return nil
	}
	return []Suggestion{{
		ID: "context-bloat", Severity: Tip, Source: "rule",
		Title:  fmt.Sprintf("Heavy context: ~%dk tokens/turn", avgIn/1000),
		Detail: "Your average turn carries a very large context. Use /clear or start a fresh session between unrelated tasks, and add only the files you need — every turn re-pays for the whole window.",
	}}
}

// ruleUnknownModel surfaces turns whose model we couldn't price, so spend isn't
// silently under-counted.
func ruleUnknownModel(in Inputs) []Suggestion {
	unpriced := map[string]bool{}
	for _, e := range in.Events {
		if e.CostUSD == 0 && (e.Input+e.Output) > 0 {
			unpriced[e.Model] = true
		}
	}
	if len(unpriced) == 0 {
		return nil
	}
	models := make([]string, 0, len(unpriced))
	for m := range unpriced {
		models = append(models, m)
	}
	sort.Strings(models)
	return []Suggestion{{
		ID: "unknown-model", Severity: Info, Source: "rule",
		Title:  "Unpriced model(s) detected",
		Detail: "No price is known for: " + strings.Join(models, ", ") + ". Spend for these turns shows as $0 until a rate is added to the bundled pricing table.",
	}}
}

// ruleIdleSessions is a light efficiency nudge when there are many sessions but
// little reuse, hinting at fragmented work.
func ruleIdleSessions(in Inputs) []Suggestion {
	sessions := map[string]struct{}{}
	for _, e := range in.Events {
		sessions[e.Session] = struct{}{}
	}
	if len(sessions) < 8 || in.Stats.Turns == 0 {
		return nil
	}
	perSession := in.Stats.Turns / len(sessions)
	if perSession >= 4 {
		return nil
	}
	return []Suggestion{{
		ID: "fragmented", Severity: Info, Source: "rule",
		Title:  fmt.Sprintf("%d short sessions", len(sessions)),
		Detail: "Many short sessions mean repeated cold-start context costs. Batching related work into one session reuses cache and lowers spend.",
	}}
}
