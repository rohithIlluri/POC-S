package advisor

import (
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

func hasID(sugs []Suggestion, id string) bool {
	for _, s := range sugs {
		if s.ID == id {
			return true
		}
	}
	return false
}

func TestBudgetRuleFires(t *testing.T) {
	in := Inputs{
		DailyBudgetUSD: 10,
		Stats:          store.Stats{TodayCost: 12, ByModel: map[string]float64{}},
	}
	got := Run(in, []Rule{ruleBudget})
	if !hasID(got, "budget-over") {
		t.Errorf("expected budget-over suggestion, got %+v", got)
	}
	if got[0].Severity != Warn {
		t.Errorf("expected Warn severity, got %v", got[0].Severity)
	}
}

func TestOpusOveruseFires(t *testing.T) {
	in := Inputs{Stats: store.Stats{
		TotalCost: 10,
		ByModel:   map[string]float64{"claude-opus-4-8": 9, "claude-sonnet-4-6": 1},
	}}
	got := Run(in, []Rule{ruleOpusOveruse})
	if !hasID(got, "opus-overuse") {
		t.Fatalf("expected opus-overuse, got %+v", got)
	}
	if got[0].SavingUSD <= 0 {
		t.Errorf("expected a positive estimated saving")
	}
}

func TestCacheRuleQuietWhenHealthy(t *testing.T) {
	in := Inputs{Stats: store.Stats{TokensIn: 1_000_000, CacheRead: 800_000}}
	if got := Run(in, []Rule{ruleCacheMisses}); len(got) != 0 {
		t.Errorf("healthy cache reuse should produce no suggestion, got %+v", got)
	}
}

func TestUnknownModelRule(t *testing.T) {
	in := Inputs{Events: []store.Event{
		{Model: "mystery-1", Input: 100, Output: 50, CostUSD: 0},
	}}
	got := Run(in, []Rule{ruleUnknownModel})
	if !hasID(got, "unknown-model") {
		t.Errorf("expected unknown-model suggestion, got %+v", got)
	}
}

func TestBudgetNearThreshold(t *testing.T) {
	in := Inputs{
		DailyBudgetUSD: 10,
		Stats:          store.Stats{TodayCost: 8, ByModel: map[string]float64{}},
	}
	got := Run(in, []Rule{ruleBudget})
	if !hasID(got, "budget-near") {
		t.Errorf("expected budget-near at 80%%, got %+v", got)
	}
	// Below 75% the rule must stay quiet.
	in.Stats.TodayCost = 5
	if got := Run(in, []Rule{ruleBudget}); len(got) != 0 {
		t.Errorf("no budget nudge expected at 50%%, got %+v", got)
	}
	// Disabled budget never fires.
	in.DailyBudgetUSD = 0
	in.Stats.TodayCost = 999
	if got := Run(in, []Rule{ruleBudget}); len(got) != 0 {
		t.Errorf("budget 0 disables the rule, got %+v", got)
	}
}

func TestContextBloatFires(t *testing.T) {
	in := Inputs{Stats: store.Stats{Turns: 10, TokensIn: 500_000, CacheRead: 1_000_000}}
	got := Run(in, []Rule{ruleContextBloat})
	if !hasID(got, "context-bloat") {
		t.Errorf("expected context-bloat at 150k/turn, got %+v", got)
	}
	// Modest context stays quiet.
	in = Inputs{Stats: store.Stats{Turns: 10, TokensIn: 100_000}}
	if got := Run(in, []Rule{ruleContextBloat}); len(got) != 0 {
		t.Errorf("10k/turn should not fire, got %+v", got)
	}
}

func TestFragmentationFires(t *testing.T) {
	var events []store.Event
	for i := 0; i < 10; i++ {
		events = append(events, store.Event{Session: string(rune('a' + i)), Model: "m"})
	}
	in := Inputs{Events: events, Stats: store.Stats{Turns: 10}} // 1 turn/session
	got := Run(in, []Rule{ruleIdleSessions})
	if !hasID(got, "fragmented") {
		t.Errorf("expected fragmentation nudge, got %+v", got)
	}
	// Long sessions (many turns each) are healthy.
	in.Stats.Turns = 100
	if got := Run(in, []Rule{ruleIdleSessions}); len(got) != 0 {
		t.Errorf("10 turns/session should not fire, got %+v", got)
	}
}

func TestOpusOveruseQuietWhenBalanced(t *testing.T) {
	in := Inputs{Stats: store.Stats{
		TotalCost: 10,
		ByModel:   map[string]float64{"claude-opus-4-8": 4, "claude-sonnet-4-6": 6},
	}}
	if got := Run(in, []Rule{ruleOpusOveruse}); len(got) != 0 {
		t.Errorf("40%% opus share should not fire, got %+v", got)
	}
}

func TestEmptyInputsProduceNothing(t *testing.T) {
	got := Run(Inputs{Stats: store.Stats{ByModel: map[string]float64{}}}, DefaultRules())
	if len(got) != 0 {
		t.Errorf("no data should produce no suggestions, got %+v", got)
	}
}

func TestRunSortsBySeverity(t *testing.T) {
	in := Inputs{
		DailyBudgetUSD: 10,
		Stats: store.Stats{
			TodayCost: 20, TotalCost: 20,
			ByModel: map[string]float64{"opus": 20},
		},
		Events: []store.Event{{Model: "x", Input: 1, CostUSD: 0, Timestamp: time.Now()}},
	}
	got := Run(in, DefaultRules())
	if len(got) < 2 {
		t.Fatalf("expected multiple suggestions, got %d", len(got))
	}
	// Warn must come before Info.
	if got[0].Severity < got[len(got)-1].Severity {
		t.Errorf("suggestions not sorted by severity: %+v", got)
	}
}
