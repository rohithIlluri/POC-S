package sim

import (
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

// DietVerdict is the subset of internal/care.Verdict the sim needs, expressed
// here rather than importing internal/care directly — that keeps the
// dependency direction one-way (care -> sim, for the Digest type) and lets
// tick.go stay testable without constructing a full care.Verdict.
type DietVerdict struct {
	XPMultiplier  float64
	HealthDelta   int
	MoodPenalty   bool
	TokenBloat    bool
	AtForagingCap bool
}

// TickResult is what one day's tick produced, for the caller (the daemon) to
// persist and journal.
type TickResult struct {
	Pet         Pet
	HatchedNow  bool   // this tick is the one that hatched the egg
	EvolvedNote string // non-empty if an evolution happened this tick (journal voice-line id)
	XPGained    int
	LeveledUp   bool
}

// Tick advances a pet by exactly one calendar day, given that day's digest
// and the care verdict already computed for it. Tick is a PURE function of
// its three arguments — no wall-clock reads, no I/O, no randomness beyond
// what's already baked into p.DNA — so replaying the same (pet, digest,
// verdict) always produces the same result. hatchWindow is the list of
// digests from egg-start through today, needed only if this tick hatches
// the egg (to run PickLine); pass nil once the pet has already hatched.
func Tick(p Pet, day string, digest Digest, verdict DietVerdict, hatchWindow []Digest, now time.Time) TickResult {
	res := TickResult{Pet: p}

	// An idle day (no activity at all) never calls Tick — the daemon only
	// ticks on days with a non-empty digest. Idle-day/hibernation handling
	// lives in AdvanceIdle, called once per calendar day regardless of
	// activity. Tick itself always represents an ACTIVE day.
	p.LastTickDay = day
	p.ActiveDayCount++
	p.GritStreak++
	p.IdleDays = 0
	p.Mood = moodFor(p, verdict)

	if p.IsEgg() {
		if p.ActiveDayCount >= HatchWindowDays {
			line := PickLine(p.DNA, hatchWindow)
			starterID, ok := species.LineStarter(line)
			if ok {
				p = hatchInto(p, starterID, line, now)
				res.HatchedNow = true
			}
		}
		res.Pet = p
		return res
	}

	// Growth: each stat inches toward its underlying signal for the day,
	// scaled by the diet multiplier. Integer-only, small fixed steps so
	// growth is gradual and never dependent on absolute token volume (a
	// whale day and a modest day both count as "one good day of X").
	xp := dailyXP(digest, verdict)
	res.XPGained = xp
	beforeLevel := p.Level
	p.XP += xp
	p.Level = levelForXP(p.XP)
	res.LeveledUp = p.Level > beforeLevel

	p.Stats = growStats(p.Stats, digest, verdict)
	p.Health = clamp(p.Health+verdict.HealthDelta, 0, 100)
	p = p.withStatus(StatusTokenBloat, verdict.TokenBloat)

	evolved, note := MaybeEvolve(p)
	p = evolved
	res.EvolvedNote = note
	res.Pet = p
	return res
}

// AdvanceIdle is called once per calendar day the daemon observes with NO
// activity at all. It never decays health to punish absence beyond mood, and
// puts the pet to sleep after HibernateAfterIdleDays — GAME_DESIGN.md §4.4:
// "Neglect decays mood, never kills."
func AdvanceIdle(p Pet, day string) Pet {
	if p.LastTickDay == day {
		return p // already ticked today; avoid double-counting idle days
	}
	p.IdleDays++
	p.GritStreak = 0
	if p.IdleDays >= HibernateAfterIdleDays {
		p.Mood = MoodAsleep
	}
	return p
}

// WakeFromHibernation is called the next time an active day arrives after a
// hibernation — the pet always wakes happy, per SPEC rule 5 (no guilt).
func WakeFromHibernation(p Pet) Pet {
	if p.Mood == MoodAsleep {
		p.Mood = MoodCheerful
	}
	return p
}

func hatchInto(p Pet, starterID string, line species.Line, now time.Time) Pet {
	sp, ok := species.ByID(starterID)
	if !ok {
		return p
	}
	iv := RollIVs(p.DNA)
	p.SpeciesID = sp.ID
	p.Line = line
	p.Stage = Stage1
	p.Level = 1
	p.XP = 0
	p.Stats = addStats(sp.Base, iv.AsStats())
	p.Lucent = IsLucent(p.DNA, p.GritStreak)
	p.HatchedAt = now
	return p
}

// dailyXP is the day's raw XP before diet scaling: turns contribute a small
// flat amount (activity matters), diet multiplier then scales the whole
// thing down for a bad day or to zero at the foraging cap.
func dailyXP(d Digest, v DietVerdict) int {
	raw := 8 + d.Turns*2 // a quiet day still earns a little; more turns, more XP
	scaled := float64(raw) * v.XPMultiplier
	return int(scaled) // truncate toward zero, never round up past what was earned
}

// growStats nudges each stat toward that day's signal, small integer steps.
// This is deliberately gentle — GAME_DESIGN.md §4.3 stats are "grown from
// real behavior" over weeks, not swung by a single big day.
func growStats(s species.Stats, d Digest, v DietVerdict) species.Stats {
	step := func(base int, earned bool) int {
		if !earned || v.XPMultiplier <= 0 {
			return base
		}
		return base + 1
	}
	return species.Stats{
		Vigor: step(s.Vigor, d.Turns > 0),
		Focus: step(s.Focus, d.CacheRatio() >= 0.5),
		Wit:   step(s.Wit, d.NewModels > 0 || d.Models >= 2),
		Grit:  step(s.Grit, true), // any active day feeds Grit — it's the streak stat
		Spark: step(s.Spark, d.NewModels > 0 || d.Projects >= 2 || d.NightSession),
	}
}

func addStats(a, b species.Stats) species.Stats {
	return species.Stats{
		Vigor: a.Vigor + b.Vigor,
		Focus: a.Focus + b.Focus,
		Wit:   a.Wit + b.Wit,
		Grit:  a.Grit + b.Grit,
		Spark: a.Spark + b.Spark,
	}
}

func moodFor(p Pet, v DietVerdict) Mood {
	switch {
	case v.AtForagingCap, v.MoodPenalty && p.Health < 40:
		return MoodWorried
	case v.MoodPenalty:
		return MoodTired
	case p.Health >= 80:
		return MoodCheerful
	default:
		return MoodContent
	}
}
