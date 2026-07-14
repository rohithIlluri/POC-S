package sim

import (
	"reflect"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

var now = time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

func newTestEgg() Pet {
	return NewEgg(NewDNA([]byte("test-pet")), now)
}

func TestTickDoesNotHatchBeforeThreshold(t *testing.T) {
	p := newTestEgg()
	// 1 qualifying session/day (2 sessions but few turns each -> falls back
	// to the coarse "at most 1 qualifying session" path), well under the
	// 5-session threshold across a few days.
	d := Digest{Turns: 3, Sessions: 2, CacheRead: 1000, TokensIn: 9000}
	window := []Digest{d, d, d}
	v := DietVerdict{XPMultiplier: 1.0}

	for day := 1; day < HatchSessionThreshold; day++ {
		res := Tick(p, itoa(day), d, v, window, now)
		p = res.Pet
		if res.HatchedNow {
			t.Fatalf("hatched too early on day %d (session count %d)", day, p.EggSessionCount)
		}
		if !p.IsEgg() {
			t.Fatalf("pet should still be an egg on day %d", day)
		}
	}
}

func TestTickHatchesSameDayFromEnthusiasticSession(t *testing.T) {
	// A single day with many real sessions (>= HatchSessionThreshold, each
	// with several turns) should hatch immediately — the whole point of
	// switching off calendar-day pacing.
	p := newTestEgg()
	d := Digest{Turns: 30, Sessions: HatchSessionThreshold, CacheRead: 1000, TokensIn: 9000}
	v := DietVerdict{XPMultiplier: 1.0}

	res := Tick(p, "day1", d, v, []Digest{d}, now)
	if !res.HatchedNow {
		t.Fatalf("expected same-day hatch from %d qualifying sessions, got EggSessionCount=%d", HatchSessionThreshold, res.Pet.EggSessionCount)
	}
	if res.Pet.IsEgg() {
		t.Fatal("pet should no longer be an egg after hatching")
	}
}

func TestTickHatchesAtSessionThresholdAcrossDays(t *testing.T) {
	p := newTestEgg()
	// 2 qualifying sessions/day (well-formed: 10 turns / 2 sessions = 5
	// turns/session, clears the qualifying-session floor).
	d := Digest{Turns: 10, Sessions: 2, CacheRead: 1000, TokensIn: 9000}
	window := []Digest{d, d, d}

	var res TickResult
	for day := 1; ; day++ {
		res = Tick(p, itoa(day), d, DietVerdict{XPMultiplier: 1.0}, window, now)
		p = res.Pet
		if res.HatchedNow || day > HatchWindowDays+2 {
			break
		}
	}
	if !res.HatchedNow {
		t.Fatal("expected hatch once accumulated qualifying sessions cross HatchSessionThreshold")
	}
	if p.IsEgg() {
		t.Fatal("pet should no longer be an egg after hatching")
	}
	if p.SpeciesID == "" {
		t.Fatal("hatched pet must have a species assigned")
	}
	sp, ok := species.ByID(p.SpeciesID)
	if !ok || sp.Stage != 1 {
		t.Fatalf("hatched species %q should be a stage-1 starter", p.SpeciesID)
	}
}

func TestTickHatchesViaCalendarSafetyValve(t *testing.T) {
	// Many days, each with a single-turn "session" that never clears the
	// qualifying-session bar at all (Turns < 2) -> EggSessionCount stays 0
	// forever, so only the calendar-day fallback can hatch this egg.
	p := newTestEgg()
	d := Digest{Turns: 1, Sessions: 1}
	window := []Digest{d, d, d, d, d}

	var res TickResult
	for day := 1; day <= HatchWindowDays; day++ {
		res = Tick(p, itoa(day), d, DietVerdict{XPMultiplier: 1.0}, window, now)
		p = res.Pet
	}
	if res.Pet.EggSessionCount != 0 {
		t.Fatalf("test setup invalid: expected 0 qualifying sessions accumulated, got %d", res.Pet.EggSessionCount)
	}
	if !res.HatchedNow {
		t.Fatal("expected the calendar-day safety valve to hatch the egg despite zero qualifying sessions")
	}
}

func TestQualifyingSessions(t *testing.T) {
	cases := []struct {
		name string
		d    Digest
		want int
	}{
		{"no sessions", Digest{}, 0},
		{"one short session", Digest{Turns: 1, Sessions: 1}, 0},
		{"one session, two turns", Digest{Turns: 2, Sessions: 1}, 1},
		{"two well-formed sessions", Digest{Turns: 10, Sessions: 2}, 2},
		{"lopsided sessions, floor applies", Digest{Turns: 3, Sessions: 3}, 1},
	}
	for _, c := range cases {
		if got := QualifyingSessions(c.d); got != c.want {
			t.Errorf("%s: QualifyingSessions(%+v) = %d, want %d", c.name, c.d, got, c.want)
		}
	}
}

func TestTickIsDeterministic(t *testing.T) {
	p := newTestEgg()
	d := Digest{Turns: 20, Sessions: 2, CacheRead: 5000, TokensIn: 5000, Models: 2, NewModels: 1}
	v := DietVerdict{XPMultiplier: 1.0}
	window := []Digest{d, d, d}

	run := func() Pet {
		pp := p
		var r TickResult
		for day := 1; day <= HatchWindowDays+5; day++ {
			r = Tick(pp, itoa(day), d, v, window, now)
			pp = r.Pet
		}
		return pp
	}

	a := run()
	b := run()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("Tick must be a pure function of (pet, day, digest, verdict, window, now): got two different results from identical inputs\na=%+v\nb=%+v", a, b)
	}
}

func TestTickGrantsXPScaledByDiet(t *testing.T) {
	p := hatchedTestPet(t)
	full := DietVerdict{XPMultiplier: 1.0}
	zero := DietVerdict{XPMultiplier: 0, AtForagingCap: true}
	d := Digest{Turns: 10}

	rFull := Tick(p, "2026-07-10", d, full, nil, now)
	rZero := Tick(p, "2026-07-10", d, zero, nil, now)

	if rFull.XPGained <= 0 {
		t.Errorf("full-diet day should grant positive XP, got %d", rFull.XPGained)
	}
	if rZero.XPGained != 0 {
		t.Errorf("foraging-cap day should grant 0 XP, got %d", rZero.XPGained)
	}
}

func TestTickHealthClamped(t *testing.T) {
	p := hatchedTestPet(t)
	p.Health = 98
	v := DietVerdict{XPMultiplier: 1.0, HealthDelta: 10}
	res := Tick(p, "2026-07-10", Digest{Turns: 1}, v, nil, now)
	if res.Pet.Health > 100 {
		t.Errorf("health must clamp at 100, got %d", res.Pet.Health)
	}

	p.Health = 3
	v = DietVerdict{XPMultiplier: 1.0, HealthDelta: -10}
	res = Tick(p, "2026-07-10", Digest{Turns: 1}, v, nil, now)
	if res.Pet.Health < 0 {
		t.Errorf("health must clamp at 0, got %d", res.Pet.Health)
	}
}

func TestTickSetsAndClearsTokenBloatStatus(t *testing.T) {
	p := hatchedTestPet(t)
	v := DietVerdict{XPMultiplier: 1.0, TokenBloat: true}
	res := Tick(p, "2026-07-10", Digest{Turns: 1}, v, nil, now)
	if !res.Pet.HasStatus(StatusTokenBloat) {
		t.Fatal("expected token_bloat status to be set")
	}

	healthy := DietVerdict{XPMultiplier: 1.0, TokenBloat: false}
	res2 := Tick(res.Pet, "2026-07-11", Digest{Turns: 1}, healthy, nil, now)
	if res2.Pet.HasStatus(StatusTokenBloat) {
		t.Fatal("token_bloat status should clear on a healthy day")
	}
}

func TestAdvanceIdleThenHibernate(t *testing.T) {
	p := hatchedTestPet(t)
	for i := 0; i < HibernateAfterIdleDays-1; i++ {
		p = AdvanceIdle(p, itoa(i+100))
	}
	if p.Mood == MoodAsleep {
		t.Fatal("should not hibernate before the idle threshold")
	}
	p = AdvanceIdle(p, itoa(999))
	if p.Mood != MoodAsleep {
		t.Fatal("expected hibernation after HibernateAfterIdleDays idle days")
	}
}

func TestWakeFromHibernationIsAlwaysHappy(t *testing.T) {
	p := hatchedTestPet(t)
	p.Mood = MoodAsleep
	p = WakeFromHibernation(p)
	if p.Mood != MoodCheerful {
		t.Errorf("waking should always be cheerful (no guilt), got %v", p.Mood)
	}
}

func TestAdvanceIdleNeverReducesHealth(t *testing.T) {
	p := hatchedTestPet(t)
	p.Health = 50
	for i := 0; i < 20; i++ {
		p = AdvanceIdle(p, itoa(i+200))
	}
	if p.Health != 50 {
		t.Errorf("idle days must never decay health directly, got %d", p.Health)
	}
}

func TestEvolutionRequiresLevelAndDominantStat(t *testing.T) {
	p := hatchedTestPet(t) // cindling, Ember line
	p.Level = EvolveLevelStage1to2
	// Make Grit NOT dominant: evolution should not fire.
	p.Stats = species.Stats{Vigor: 100, Focus: 10, Wit: 10, Grit: 20, Spark: 10}
	evolved, note := MaybeEvolve(p)
	if note != "" || evolved.SpeciesID != p.SpeciesID {
		t.Fatal("evolution should not fire when the line's required stat isn't dominant")
	}

	// Make Grit dominant: should evolve.
	p.Stats = species.Stats{Vigor: 10, Focus: 10, Wit: 10, Grit: 100, Spark: 10}
	evolved, note = MaybeEvolve(p)
	if note == "" || evolved.SpeciesID != "forgeon" {
		t.Fatalf("expected evolution into forgeon, got species=%q note=%q", evolved.SpeciesID, note)
	}
	if evolved.Stage != Stage2 {
		t.Errorf("expected Stage2 after first evolution, got %v", evolved.Stage)
	}
}

func TestEvolutionPreservesEarnedStatDelta(t *testing.T) {
	p := hatchedTestPet(t)
	p.Level = EvolveLevelStage1to2
	base, _ := species.ByID("cindling")
	// Pet earned +15 Grit above cindling's base through play.
	p.Stats = base.Base
	p.Stats.Grit += 15

	evolved, note := MaybeEvolve(p)
	if note == "" {
		t.Fatal("expected evolution to fire")
	}
	forgeonBase, _ := species.ByID("forgeon")
	want := forgeonBase.Base.Grit + 15
	if evolved.Stats.Grit != want {
		t.Errorf("evolution should preserve earned delta: got Grit=%d, want %d", evolved.Stats.Grit, want)
	}
}

func TestNoEvolutionBelowLevelGate(t *testing.T) {
	p := hatchedTestPet(t)
	p.Level = EvolveLevelStage1to2 - 1
	p.Stats = species.Stats{Grit: 999}
	_, note := MaybeEvolve(p)
	if note != "" {
		t.Error("should not evolve below the level gate regardless of stats")
	}
}

func TestLevelForXPMonotonic(t *testing.T) {
	prev := 0
	for xp := 0; xp <= 5000; xp += 37 {
		lvl := levelForXP(xp)
		if lvl < prev {
			t.Fatalf("levelForXP not monotonic at xp=%d: %d < %d", xp, lvl, prev)
		}
		prev = lvl
	}
}

func TestXpForLevelInverse(t *testing.T) {
	for lvl := 1; lvl <= 40; lvl++ {
		xp := xpForLevel(lvl)
		if got := levelForXP(xp); got < lvl {
			t.Errorf("levelForXP(xpForLevel(%d)=%d) = %d, want >= %d", lvl, xp, got, lvl)
		}
	}
}

// hatchedTestPet returns a pet already hatched into cindling (Ember line,
// stage 1), for tests that need a live pet rather than an egg.
func hatchedTestPet(t *testing.T) Pet {
	t.Helper()
	p := newTestEgg()
	d := Digest{Turns: 40, Sessions: 2, CacheRead: 1000, TokensIn: 9000}
	v := DietVerdict{XPMultiplier: 1.0}
	window := []Digest{d, d, d}
	var res TickResult
	for day := 1; day <= HatchWindowDays; day++ {
		res = Tick(p, itoa(day), d, v, window, now)
		p = res.Pet
	}
	if p.IsEgg() {
		t.Fatal("test setup failed to hatch the pet")
	}
	return p
}
