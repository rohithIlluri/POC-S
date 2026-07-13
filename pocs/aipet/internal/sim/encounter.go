package sim

import (
	"sort"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

// Encounters implement docs/design/rarity.md: wild Codelings appear on real
// usage signals, with tier odds, a diet band-shift, whiff pity, and
// uncaught-first species selection — all pure functions of
// (dna, day, digest, dex snapshot), replayable like everything else in sim.

// Trigger is one real signal that surfaces a wild Codeling. A day fires at
// most one of each kind, giving the 0-3 triggers/day rate rarity.md §2
// designs around.
type Trigger string

const (
	TriggerNewProject Trigger = "new_project" // first-ever event in a project dir
	TriggerNewModel   Trigger = "new_model"   // a model never seen before today
	TriggerCleanDay   Trigger = "clean_day"   // the day met the balanced-diet bar
)

// Tier ranges for the /1024 roll, per rarity.md §2.1:
// [0,8)=RELIC, [8,72)=RARE, [72,328)=UNCOMMON, [328,1024)=COMMON.
const (
	tierRollDenom   = 1024
	relicCeil       = 8
	rareCeil        = 72
	uncommonCeil    = 328
	whiffFloorRare  = 40  // whiffs_since_rare >= 40 floors the roll at RARE (§2.5)
	whiffFloorRelic = 120 // >= 120 floors at RELIC
)

// DexView is the read-only snapshot of collection state the roll needs:
// which species are caught, and the current whiff counter. The caller owns
// the mutable state; sim only ever reads it.
type DexView struct {
	Caught          map[string]bool
	WhiffsSinceRare int
	GritStreak      int // for mythic gating and the Lucent roll on catch
}

// Encounter is one resolved wild appearance.
type Encounter struct {
	Day       string
	Trigger   Trigger
	SpeciesID string
	Rarity    species.Rarity
	Caught    bool // catch-by-doing: joined because the day was healthy
	Lucent    bool
	Mythic    bool
}

// DayTriggers derives which encounter triggers a completed day fired. All
// three are observable from the digest alone; healthyDiet is care's
// IsHealthyDiet verdict, passed in so sim keeps its one-way dependency on
// care (care -> sim, never the reverse).
func DayTriggers(d Digest, healthyDiet bool) []Trigger {
	var out []Trigger
	if d.NewProjects > 0 {
		out = append(out, TriggerNewProject)
	}
	if d.NewModels > 0 {
		out = append(out, TriggerNewModel)
	}
	if healthyDiet {
		out = append(out, TriggerCleanDay)
	}
	return out
}

// RollEncounters resolves all of a day's triggers into encounters, in
// trigger order, threading the whiff counter through consecutive rolls the
// way a live day would. It returns the encounters plus the updated whiff
// counter for the caller to persist.
//
// The catch rule (GAME_DESIGN §4.5 "catch-by-doing"): a wild Codeling joins
// you if the day it appeared met the balanced-diet bar; otherwise it is
// only "seen". Mythics are exempt from the odds table entirely (species.md:
// "vanishingly rare by construction") — see MythicEncounter.
func RollEncounters(dna DNA, day string, d Digest, healthyDiet bool, dex DexView) ([]Encounter, int) {
	whiffs := dex.WhiffsSinceRare
	var out []Encounter
	for ordinal, trig := range DayTriggers(d, healthyDiet) {
		tier := rollTier(dna, day, ordinal, healthyDiet, whiffs)
		id := pickSpecies(dna, day, ordinal, tier, dex.Caught)
		if id == "" {
			continue // tier has no species (cannot happen with the launch roster; defensive)
		}
		if tier == species.Rare || tier == species.Relic {
			whiffs = 0
		} else {
			whiffs++
		}
		out = append(out, Encounter{
			Day: day, Trigger: trig, SpeciesID: id, Rarity: tier,
			Caught: healthyDiet,
			Lucent: healthyDiet && IsLucent(dna, dex.GritStreak),
			Mythic: false,
		})
	}
	return out, whiffs
}

// rollTier draws the day's tier for one trigger: base /1024 roll, then the
// §2.2 diet band-shift, then the §2.5 pity floor — both post-roll remaps,
// never extra draws, so the same inputs always replay identically.
func rollTier(dna DNA, day string, ordinal int, healthyDiet bool, whiffs int) species.Rarity {
	r := derive(dna, "tier", day+"|"+itoa(ordinal)) % tierRollDenom
	var tier species.Rarity
	switch {
	case r < relicCeil:
		tier = species.Relic
	case r < rareCeil:
		tier = species.Rare
	case r < uncommonCeil:
		tier = species.Uncommon
	default:
		tier = species.Common
	}
	if healthyDiet {
		tier = shiftUp(tier)
	}
	if whiffs >= whiffFloorRelic {
		tier = floorAt(tier, species.Relic)
	} else if whiffs >= whiffFloorRare {
		tier = floorAt(tier, species.Rare)
	}
	return tier
}

// shiftUp promotes a tier one band (COMMON→UNCOMMON→RARE→RELIC; RELIC caps).
func shiftUp(t species.Rarity) species.Rarity {
	switch t {
	case species.Common:
		return species.Uncommon
	case species.Uncommon:
		return species.Rare
	case species.Rare:
		return species.Relic
	default:
		return t
	}
}

// tierRank orders the four rollable tiers for the pity floor comparison.
func tierRank(t species.Rarity) int {
	switch t {
	case species.Common:
		return 0
	case species.Uncommon:
		return 1
	case species.Rare:
		return 2
	case species.Relic:
		return 3
	default:
		return 4 // mythic never comes from the roll table
	}
}

func floorAt(t, floor species.Rarity) species.Rarity {
	if tierRank(t) < tierRank(floor) {
		return floor
	}
	return t
}

// pickSpecies chooses the species within a tier, uncaught-first per
// rarity.md §7.4: while any species in the tier is uncaught, the roll is
// uniform over the uncaught subset only, so no encounter is wasted on a
// duplicate while gaps remain. Pools are sorted by Dex number so the
// modulo index is deterministic regardless of map iteration order.
func pickSpecies(dna DNA, day string, ordinal int, tier species.Rarity, caught map[string]bool) string {
	var pool []species.Species
	for _, s := range species.All {
		if s.Rarity == tier {
			pool = append(pool, s)
		}
	}
	if len(pool) == 0 {
		return ""
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].Dex < pool[j].Dex })

	var uncaught []species.Species
	for _, s := range pool {
		if !caught[s.ID] {
			uncaught = append(uncaught, s)
		}
	}
	if len(uncaught) > 0 {
		pool = uncaught
	}
	roll := derive(dna, "species", day+"|"+itoa(ordinal)) % uint64(len(pool))
	return pool[roll].ID
}

// MythicEncounter checks the two event-gated Mythic appearances. Per
// docs/design/species.md these are "vanishingly rare by construction, not
// by a rolled odds table":
//   - uptimewyrm: a full year-long active-day streak (365 days, no gap)
//   - everfile: a single day whose total context volume (fresh input +
//     cache reads) crosses an extraordinary threshold — the closest
//     observable proxy for "read the whole repository in one turn sequence"
//     available from session logs.
//
// A Mythic that appears is always caught (the event IS the catch), and is
// never rolled for Lucent — Mythics have one canonical look.
func MythicEncounter(day string, d Digest, gritStreak int, caught map[string]bool) *Encounter {
	const everfileContextTokens = 2_000_000
	if gritStreak >= 365 && !caught["uptimewyrm"] {
		return &Encounter{
			Day: day, Trigger: "mythic_streak", SpeciesID: "uptimewyrm",
			Rarity: species.Mythic, Caught: true, Mythic: true,
		}
	}
	if d.TokensIn+d.CacheRead >= everfileContextTokens && !caught["everfile"] {
		return &Encounter{
			Day: day, Trigger: "mythic_context", SpeciesID: "everfile",
			Rarity: species.Mythic, Caught: true, Mythic: true,
		}
	}
	return nil
}
