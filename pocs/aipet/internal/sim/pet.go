package sim

import (
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

// Mood is a coarse emotional read, driven by recent health/diet trend.
type Mood string

const (
	MoodCheerful Mood = "cheerful"
	MoodContent  Mood = "content"
	MoodTired    Mood = "tired"
	MoodWorried  Mood = "worried"
	MoodAsleep   Mood = "asleep" // hibernating
)

// Status is a temporary condition affecting the pet, cleared by good habits.
type Status string

const (
	StatusTokenBloat Status = "token_bloat"
)

// Stage of an egg/pet's life.
type Stage int

const (
	StageEgg Stage = iota
	Stage1
	Stage2
	Stage3
)

// Pet is the full mutable state of a Daemonkeeper's companion. It is the
// on-disk shape (internal/save serializes this) and the sole argument/return
// of every function in tick.go and evolve.go — always passed and returned by
// value or via an explicit copy, never mutated through a shared pointer
// inside the sim, so a tick is trivially replayable and testable.
type Pet struct {
	SaveVersion int           `json:"save_version"`
	DNA         DNA           `json:"dna"`
	SpeciesID   string        `json:"species,omitempty"` // empty while still an egg
	Line        species.Line  `json:"line,omitempty"`
	Stage       Stage         `json:"stage"`
	Level       int           `json:"level"`
	XP          int           `json:"xp"`
	Stats       species.Stats `json:"stats"`  // grown stats, base+IV+history, NOT recomputed from scratch each tick
	Health      int           `json:"health"` // 0..100
	Mood        Mood          `json:"mood"`
	Lucent      bool          `json:"lucent"`
	Statuses    []Status      `json:"statuses,omitempty"`
	Badges      []string      `json:"badges,omitempty"`

	HatchedAt      time.Time `json:"hatched_at,omitempty"`
	EggStartedAt   time.Time `json:"egg_started_at"`
	LastTickDay    string    `json:"last_tick_day,omitempty"` // YYYY-MM-DD, local
	ActiveDayCount int       `json:"active_day_count"`        // total days that ever produced a tick
	IdleDays       int       `json:"idle_days"`               // consecutive days since the last active tick
	GritStreak     int       `json:"grit_streak"`             // consecutive ACTIVE days, resets on any idle day

	// EggSessionCount is how many qualifying sessions have been folded into
	// this egg since it started (see QualifyingSessions in tick.go). Hatch
	// gates on THIS, not on calendar days — a single enthusiastic sitting
	// with several sessions can hatch the same day it starts, while a
	// casual user still hatches within their first couple of real days.
	// Unused once hatched.
	EggSessionCount int `json:"egg_session_count,omitempty"`

	// PreTodayPet + PreTodayDay make "today" safely re-tickable multiple
	// times per calendar day (e.g. once per background collection cycle)
	// without double-counting Tick's cumulative fields (ActiveDayCount,
	// GritStreak, XP, EggSessionCount, Stats). PreTodayPet is a sealed
	// snapshot of the pet as of the END of the last COMPLETED day — i.e.
	// the clean baseline "today" is replayed from every time. PreTodayDay
	// names which calendar day that baseline is for; when it no longer
	// matches "today", the baseline is stale and gets re-sealed from the
	// (now-completed) former "today" before a new one starts.
	//
	// PreTodayPet is always a fully sealed past state: its own
	// PreTodayPet/PreTodayDay are left zero, never nested. Both fields are
	// internal replay bookkeeping — save/load and every external reader
	// should treat the OUTER Pet as the only one that matters.
	PreTodayPet *Pet   `json:"pre_today_pet,omitempty"`
	PreTodayDay string `json:"pre_today_day,omitempty"`
}

// NewEgg creates a freshly-laid egg from a new DNA seed. No species is
// assigned yet — that happens at hatch time via PickLine + HatchInto.
func NewEgg(dna DNA, now time.Time) Pet {
	return Pet{
		SaveVersion:  1,
		DNA:          dna,
		Stage:        StageEgg,
		Health:       100,
		Mood:         MoodContent,
		EggStartedAt: now,
	}
}

// HatchSessionThreshold is how many qualifying sessions (see
// QualifyingSessions in tick.go) an egg needs before it hatches. Activity-
// based rather than calendar-based: an enthusiastic single sitting with
// several real sessions hatches the same day, while a casual user still
// hatches within their first couple of real days — the species-line pick
// still reads a genuine multi-session playstyle signal either way.
const HatchSessionThreshold = 5

// HatchWindowDays is retained as a fallback safety valve, not the primary
// gate: an egg that's been active on this many distinct calendar days
// always hatches even if it fell short of HatchSessionThreshold (e.g. many
// single-session days), so a real week of casual use is never stuck.
const HatchWindowDays = 5

// Level thresholds for stage evolution, per GAME_DESIGN.md §4.4.
const (
	EvolveLevelStage1to2 = 12
	EvolveLevelStage2to3 = 30
)

// HibernateAfterIdleDays is when a neglected pet hibernates rather than
// keep decaying — per GAME_DESIGN.md §4.4, "no dead pets on vacation."
const HibernateAfterIdleDays = 7

// IsEgg reports whether the pet has not hatched yet.
func (p Pet) IsEgg() bool { return p.Stage == StageEgg }

// HasStatus reports whether the pet currently carries the given status.
func (p Pet) HasStatus(s Status) bool {
	for _, x := range p.Statuses {
		if x == s {
			return true
		}
	}
	return false
}

func (p Pet) withStatus(s Status, on bool) Pet {
	if on == p.HasStatus(s) {
		return p
	}
	if on {
		p.Statuses = append(append([]Status{}, p.Statuses...), s)
		return p
	}
	out := p.Statuses[:0:0]
	for _, x := range p.Statuses {
		if x != s {
			out = append(out, x)
		}
	}
	p.Statuses = out
	return p
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
