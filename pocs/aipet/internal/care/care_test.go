package care

import (
	"testing"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
)

func TestBalancedDietFullXP(t *testing.T) {
	d := sim.Digest{
		Turns: 10, Sessions: 2, TokensIn: 40_000, CacheRead: 60_000, CostUSD: 1.0,
	}
	v := Evaluate(d, 0, 0, 10)
	if v.XPMultiplier != 1.0 {
		t.Errorf("balanced day should give full XP, got %v", v.XPMultiplier)
	}
	if len(v.Signals) != 1 || v.Signals[0] != Balanced {
		t.Errorf("expected only Balanced signal, got %v", v.Signals)
	}
	if v.HealthDelta <= 0 {
		t.Errorf("balanced day should raise health, got delta %d", v.HealthDelta)
	}
}

func TestJunkFoodLowCacheReuse(t *testing.T) {
	d := sim.Digest{Turns: 5, Sessions: 1, TokensIn: 100_000, CacheRead: 5_000, CostUSD: 1.0}
	v := Evaluate(d, 0, 0, 10)
	if !hasSignal(v, JunkFood) {
		t.Fatalf("expected JunkFood signal, got %v", v.Signals)
	}
	if v.XPMultiplier >= 1.0 {
		t.Errorf("junk food should reduce XP multiplier, got %v", v.XPMultiplier)
	}
	if v.HealthDelta >= 0 {
		t.Errorf("junk food should reduce health, got delta %d", v.HealthDelta)
	}
	if len(v.Reasons) == 0 {
		t.Error("verdict must always carry an explainable reason")
	}
}

func TestRichFoodOpusOveruse(t *testing.T) {
	d := sim.Digest{Turns: 3, Sessions: 1, TokensIn: 60_000, CacheRead: 60_000, CostUSD: 2.0}
	v := Evaluate(d, 0.8, 1.6, 10)
	if !hasSignal(v, RichFood) {
		t.Fatalf("expected RichFood signal, got %v", v.Signals)
	}
}

func TestOvereatingLargeContext(t *testing.T) {
	d := sim.Digest{Turns: 1, Sessions: 1, TokensIn: 200_000, CacheRead: 0, CostUSD: 1.0}
	v := Evaluate(d, 0, 0, 10)
	if !hasSignal(v, Overeating) {
		t.Fatalf("expected Overeating signal, got %v", v.Signals)
	}
	if !v.TokenBloat {
		t.Error("overeating should set the token_bloat status")
	}
}

func TestFragmentedManyShortSessions(t *testing.T) {
	d := sim.Digest{Turns: 8, Sessions: 4, TokensIn: 10_000, CacheRead: 30_000, CostUSD: 0.2, Fragmented: 4}
	v := Evaluate(d, 0, 0, 10)
	if !hasSignal(v, Fragmented) {
		t.Fatalf("expected Fragmented signal, got %v", v.Signals)
	}
}

func TestForagingCapZeroesXP(t *testing.T) {
	d := sim.Digest{Turns: 5, Sessions: 1, TokensIn: 40_000, CacheRead: 60_000, CostUSD: 15.0}
	v := Evaluate(d, 0, 0, 10)
	if !v.AtForagingCap {
		t.Fatal("expected AtForagingCap")
	}
	if v.XPMultiplier != 0 {
		t.Errorf("over budget should zero XP for the day, got %v", v.XPMultiplier)
	}
}

func TestZeroBudgetDisablesForagingCap(t *testing.T) {
	d := sim.Digest{Turns: 5, Sessions: 1, TokensIn: 40_000, CacheRead: 60_000, CostUSD: 999.0}
	v := Evaluate(d, 0, 0, 0)
	if v.AtForagingCap {
		t.Error("dailyBudgetUSD<=0 should disable the foraging cap")
	}
}

func TestXPMultiplierNeverNegative(t *testing.T) {
	// Stack junk food + rich food + over budget: multiplier should floor at 0,
	// not go negative.
	d := sim.Digest{Turns: 5, Sessions: 1, TokensIn: 200_000, CacheRead: 1_000, CostUSD: 50.0}
	v := Evaluate(d, 0.9, 45, 10)
	if v.XPMultiplier < 0 {
		t.Errorf("XP multiplier must never be negative, got %v", v.XPMultiplier)
	}
}

func TestIsHealthyDietMatchesRarityDocBar(t *testing.T) {
	healthy := sim.Digest{Turns: 4, TokensIn: 10_000, CacheRead: 20_000, CostUSD: 1.0}
	if !IsHealthyDiet(healthy, 10) {
		t.Error("expected healthy digest to pass the balanced-diet bar")
	}
	unhealthy := sim.Digest{Turns: 4, TokensIn: 50_000, CacheRead: 5_000, CostUSD: 1.0}
	if IsHealthyDiet(unhealthy, 10) {
		t.Error("expected low cache-ratio digest to fail the balanced-diet bar")
	}
}

func hasSignal(v Verdict, want Signal) bool {
	for _, s := range v.Signals {
		if s == want {
			return true
		}
	}
	return false
}
