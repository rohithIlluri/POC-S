package sim

import "github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"

// xpForLevel returns the total XP required to REACH a given level. A simple
// quadratic curve keeps early levels fast (so a new egg's hatchling feels
// alive within days) and later levels slower, without floats: level*level*10.
func xpForLevel(level int) int {
	if level <= 1 {
		return 0
	}
	return level * level * 10
}

// levelForXP is the inverse of xpForLevel: the highest level fully reached
// by a given XP total. Deterministic integer search — the curve is small
// enough (levels top out well under 100) that a linear scan is simplest and
// keeps the "pure function, no floats" law trivially auditable.
func levelForXP(xp int) int {
	level := 1
	for xpForLevel(level+1) <= xp {
		level++
	}
	return level
}

// dominantStat returns the highest of the five stats, with a fixed
// tie-break order (Grit > Focus > Spark > Wit > Vigor) so evolution branch
// selection never depends on map iteration or floating-point comparison.
// The order itself is arbitrary but MUST stay fixed for replay stability.
func dominantStat(s species.Stats) string {
	type kv struct {
		name string
		val  int
	}
	// Order chosen once and frozen: this is the tiebreak priority, not a
	// ranking of stat importance.
	candidates := []kv{
		{"grit", s.Grit},
		{"focus", s.Focus},
		{"spark", s.Spark},
		{"wit", s.Wit},
		{"vigor", s.Vigor},
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.val > best.val {
			best = c
		}
	}
	return best.name
}

// lineStatRequirement is which stat must be dominant for each starter line's
// evolution to proceed, per docs/design/species.md (Ember->GRIT,
// Stream->FOCUS, Vector->SPARK).
func lineStatRequirement(l species.Line) string {
	switch l {
	case species.Ember:
		return "grit"
	case species.StreamLine:
		return "focus"
	case species.Vector:
		return "spark"
	default:
		return ""
	}
}

// MaybeEvolve checks level + dominant-stat gates and, if satisfied, advances
// the pet to its next stage — updating SpeciesID, Stage, and re-deriving
// Stats' base component from the new species (IV/grown deltas carry over
// unchanged; only the species base line shifts). Returns the pet unchanged
// if no evolution is due. journalNote is non-empty only when an evolution
// happened, for the caller to append to the journal.
func MaybeEvolve(p Pet) (Pet, string) {
	if p.IsEgg() || p.SpeciesID == "" {
		return p, ""
	}
	sp, ok := species.ByID(p.SpeciesID)
	if !ok || sp.EvolvesTo == "" {
		return p, "" // final stage or unknown species: nothing to do
	}

	var levelGate int
	switch p.Stage {
	case Stage1:
		levelGate = EvolveLevelStage1to2
	case Stage2:
		levelGate = EvolveLevelStage2to3
	default:
		return p, "" // stage 3 has no EvolvesTo target per the roster anyway
	}
	if p.Level < levelGate {
		return p, ""
	}

	// Starter lines additionally require their line's stat to be dominant
	// (GAME_DESIGN.md §3 species table, "while GRIT is the dominant stat"
	// etc). Non-line species (standalone/pairs) evolve on level alone.
	if req := lineStatRequirement(p.Line); req != "" && dominantStat(p.Stats) != req {
		return p, ""
	}

	next, ok := species.ByID(sp.EvolvesTo)
	if !ok {
		return p, ""
	}

	before := p
	p.SpeciesID = next.ID
	p.Stage = Stage(next.Stage)
	// Re-base stats onto the new species' base line, preserving the delta
	// (IVs + accumulated growth) the pet already earned above its OLD base,
	// so evolving is a strict upgrade, never a stat reset.
	p.Stats = species.Stats{
		Vigor: next.Base.Vigor + (before.Stats.Vigor - speciesBase(before.SpeciesID).Vigor),
		Focus: next.Base.Focus + (before.Stats.Focus - speciesBase(before.SpeciesID).Focus),
		Wit:   next.Base.Wit + (before.Stats.Wit - speciesBase(before.SpeciesID).Wit),
		Grit:  next.Base.Grit + (before.Stats.Grit - speciesBase(before.SpeciesID).Grit),
		Spark: next.Base.Spark + (before.Stats.Spark - speciesBase(before.SpeciesID).Spark),
	}
	note := "evolve_stage2to3_01"
	if p.Stage == Stage2 {
		note = "evolve_stage1to2_01"
	}
	return p, note
}

// speciesBase looks up a species' base stats, returning the zero value for
// an unknown/empty id (used defensively; callers only invoke this with an
// id that was valid a moment earlier in the same tick).
func speciesBase(id string) species.Stats {
	sp, ok := species.ByID(id)
	if !ok {
		return species.Stats{}
	}
	return sp.Base
}
