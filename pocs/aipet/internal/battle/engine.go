package battle

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

// Card is the compact, self-contained pet snapshot both battle and trade
// carry (GAME_DESIGN §4.6): everything a replay needs, nothing more. Stats
// are the pet's already-grown values (IVs included) straight from the save.
// DNAHash stands in for the raw DNA in the seed derivation — the card is
// the interchange format and never carries raw DNA, so the §3.1
// `sort(dnaA, dnaB)` is performed over the two cards' hash strings; both
// machines hold the same two cards, so the seed still derives identically
// on each side.
type Card struct {
	SpeciesID string        `json:"species"`
	Nickname  string        `json:"nickname,omitempty"`
	Level     int           `json:"level"`
	Stats     species.Stats `json:"stats"`
	Moves     []string      `json:"moves"` // 1..4 ids from LegalMoves(species)
	DNAHash   string        `json:"dna_hash"`
}

// Name is what the transcript calls this pet: nickname if set, else the
// species display name.
func (c Card) Name() string {
	if c.Nickname != "" {
		return c.Nickname
	}
	if sp, ok := species.ByID(c.SpeciesID); ok {
		return sp.Name
	}
	return c.SpeciesID
}

// ValidateCard enforces battle-legality (not import hardening — that lives
// in internal/codeling): known species, sane level, 1..4 moves all legal
// for the species per moves.md §1.
func ValidateCard(c Card) error {
	sp, ok := species.ByID(c.SpeciesID)
	if !ok {
		return fmt.Errorf("unknown species %q", c.SpeciesID)
	}
	if c.Level < 1 || c.Level > 100 {
		return fmt.Errorf("level %d out of range 1..100", c.Level)
	}
	if len(c.Moves) < 1 || len(c.Moves) > 4 {
		return fmt.Errorf("cards carry 1 to 4 moves, got %d", len(c.Moves))
	}
	legal := LegalMoves(sp)
	seen := map[string]bool{}
	for _, id := range c.Moves {
		if !legal[id] {
			return fmt.Errorf("move %q is not legal for %s", id, sp.Name)
		}
		if seen[id] {
			return fmt.Errorf("duplicate move %q", id)
		}
		seen[id] = true
	}
	return nil
}

// Result is a finished battle: Winner is 0 or 1 in CANONICAL (DNA-sorted)
// order, or -1 for a draw; Log is the full ordered transcript per §4.
type Result struct {
	Winner int
	Turns  int
	Cards  [2]Card // canonical order — Cards[Winner] is the winning card
	Log    []string
}

// petState is one side's mutable battle state. Fixed [2]petState array in
// the engine — never a map — per §3's determinism rules.
type petState struct {
	card        Card
	sp          species.Species
	hp          int
	hpMax       int
	focus       int // Focus Pool (§3.1)
	status      Status
	statusTurns int
	statusFresh bool // set when applied mid-action; skips one countdown tick

	guardCharge bool // +1/2 DEF for next incoming hit (§3.4 step 5)
	boostCharge bool // +1/2 ATK for next outgoing hit (§3.4 step 4)

	consecStrikesLanded int // HOTPATCHED trigger counter (§2)
	consecBoosts        int // WARMED_UP trigger counter (§2)
}

// Fight replays the battle for two validated cards on the given UTC date
// (ISO 8601 day, e.g. "2026-07-16"). Pure and deterministic: the same
// (cards, date) always produces the identical Result on any machine.
func Fight(a, b Card, dateUTC string) (Result, error) {
	if err := ValidateCard(a); err != nil {
		return Result{}, fmt.Errorf("card A: %w", err)
	}
	if err := ValidateCard(b); err != nil {
		return Result{}, fmt.Errorf("card B: %w", err)
	}

	// §3.1 canonical ordering: lexicographic sort of the two DNA hash
	// strings decides pet[0]/pet[1] for ALL bookkeeping, regardless of who
	// loaded which card — this is what makes the replay symmetric. Hash
	// ties (same-save clones, hand-built cards) fall back to the full card
	// identity so the ordering is still total and load-order independent.
	first, second := a, b
	if canonicalKey(a) > canonicalKey(b) {
		first, second = b, a
	}
	seed := sha256.Sum256([]byte(first.DNAHash + second.DNAHash + dateUTC))
	rng := newStream(seed)

	var pets [2]petState
	for i, c := range [2]Card{first, second} {
		sp, _ := species.ByID(c.SpeciesID)
		hp := HPMax(c)
		pets[i] = petState{card: c, sp: sp, hp: hp, hpMax: hp, focus: c.Stats.Focus}
	}

	res := Result{Winner: -1, Cards: [2]Card{first, second}}
	log := func(format string, args ...any) { res.Log = append(res.Log, fmt.Sprintf(format, args...)) }

	const turnCap = 40
	for turn := 1; ; turn++ {
		res.Turns = turn
		log("Turn %d — %s vs %s (%d/%d · %d/%d)",
			turn, pets[0].card.Name(), pets[1].card.Name(),
			pets[0].hp, pets[0].hpMax, pets[1].hp, pets[1].hpMax)

		// §3.3 turn order: pure function of stats, DNA-sort tiebreak,
		// recomputed every round, no RNG ever.
		order := [2]int{0, 1}
		if speed(pets[1]) > speed(pets[0]) {
			order = [2]int{1, 0}
		}

		for _, idx := range order {
			me, them := &pets[idx], &pets[1-idx]
			if me.hp <= 0 {
				continue // KO'd earlier this round; never acts
			}

			// §3.5 draw 1 — RATE_LIMITED skip check, drawn ONLY while the
			// status is held.
			if me.status == RateLimited {
				if rng.next()%4 == 0 {
					log("%s is RATE_LIMITED and can't move!", me.card.Name())
					endOfAction(me, them, &res, log)
					if winner := winCheck(&pets, &res, log); winner {
						return res, nil
					}
					continue
				}
			}

			// §3.6 move selection (§3.5 draw 2 — always exactly one draw).
			mv := selectMove(me, rng.next())
			me.focus -= mv.WitCost
			// TOKEN_BLOAT triggers on the transition to exactly 0 (§2) —
			// selection guarantees focus never goes negative.
			if mv.WitCost > 0 && me.focus == 0 && me.status == NoStatus {
				applyStatus(me, TokenBloat)
				log("%s is now TOKEN_BLOAT.", me.card.Name())
			}

			resolveMove(me, them, mv, rng, log)

			endOfAction(me, them, &res, log)
			if winner := winCheck(&pets, &res, log); winner {
				return res, nil
			}
		}

		if turn >= turnCap {
			log("Turn %d — no clean winner. Checking HP%%...", turnCap)
			// §3.7 integer-only HP%% comparison via cross-multiplication.
			l := pets[0].hp * pets[1].hpMax
			r := pets[1].hp * pets[0].hpMax
			switch {
			case l > r:
				res.Winner = 0
			case r > l:
				res.Winner = 1
			default:
				res.Winner = -1
			}
			closeOut(&res, log)
			return res, nil
		}
	}
}

// canonicalKey is the total order Fight sorts cards by: DNA hash first
// (the §3.1 rule), then the remaining card fields as tiebreaks.
func canonicalKey(c Card) string {
	return c.DNAHash + "|" + c.SpeciesID + "|" + fmt.Sprintf("%03d", c.Level) + "|" +
		strings.Join(c.Moves, ",") + "|" + c.Nickname
}

// applyStatus sets a status with its full duration, marking it fresh so
// the holder's imminent end-of-action doesn't immediately tick it — a
// "1 turn" charge like HOTPATCHED/WARMED_UP must survive to the hit that
// consumes it.
func applyStatus(p *petState, st Status) {
	p.status = st
	p.statusTurns = statusDuration[st]
	p.statusFresh = true
}

// HPMax derives a card's battle HP per moves.md §3.2 — integer division,
// VIGOR and GRIT only (FOCUS is the Focus Pool; WIT/SPARK feed offense).
func HPMax(c Card) int {
	return 50 + ((2*c.Stats.Vigor + c.Stats.Grit) * c.Level / 25)
}

func speed(p petState) int { return p.card.Stats.Vigor + p.card.Stats.Spark }

// selectMove implements §3.6: affordable moves weighted 3:1 STRIKE over
// utility, flat-expanded in the card's fixed move order, indexed by the
// (always-consumed) draw-2 roll. Focus-drained pets fall back to Bare Metal.
func selectMove(p *petState, roll uint64) Move {
	var legal []Move
	for _, id := range p.card.Moves {
		m, ok := MoveByID(id)
		if ok && p.focus >= m.WitCost {
			legal = append(legal, m)
		}
	}
	if len(legal) == 0 {
		return BareMetal(p.sp.Type)
	}
	var flat []Move
	for _, m := range legal {
		w := 1
		if m.Kind == Strike {
			w = 3
		}
		for i := 0; i < w; i++ {
			flat = append(flat, m)
		}
	}
	return flat[int(roll%uint64(len(flat)))]
}

// resolveMove runs §3.4/§3.5 draws 3–6 for one action.
func resolveMove(me, them *petState, mv Move, rng *stream, log func(string, ...any)) {
	name, target := me.card.Name(), them.card.Name()

	switch mv.Kind {
	case Guard:
		me.consecStrikesLanded = 0
		me.consecBoosts = 0
		if me.status != NoStatus {
			// Cleanse takes priority over charging when statused (§3.4).
			log("%s used %s, shaking off %s.", name, mv.Name, me.status)
			me.status = NoStatus
			me.statusTurns = 0
		} else {
			me.guardCharge = true
			log("%s used %s, bracing for the next hit.", name, mv.Name)
		}
		return
	case Boost:
		me.consecStrikesLanded = 0
		me.boostCharge = true
		me.consecBoosts++
		log("%s used %s, winding up.", name, mv.Name)
		if me.consecBoosts == 2 && me.status == NoStatus {
			applyStatus(me, WarmedUp)
			log("%s is now WARMED_UP.", name)
		}
		return
	}

	if mv.ID == "bare_metal" {
		log("%s is out of Focus and goes Bare Metal!", name)
	}
	me.consecBoosts = 0

	// §3.5 draw 3 — accuracy (1/1 moves still fire the draw only when the
	// kind requires it; all STRIKE/HEX draw here, GUARD/BOOST never reach
	// this point).
	acc := rng.next() % uint64(mv.Accuracy.Den)
	if int(acc) >= mv.Accuracy.Num {
		log("%s used %s... it missed.", name, mv.Name)
		me.consecStrikesLanded = 0
		return
	}

	// §3.5 draw 4 — status inflict, only when the move carries one, it
	// hit, and the defender is not already statused.
	inflicted := NoStatus
	if mv.Inflicts != NoStatus && them.status == NoStatus {
		roll := rng.next() % uint64(mv.InflictChance.Den)
		if int(roll) < mv.InflictChance.Num {
			inflicted = mv.Inflicts
		}
	}

	// §3.4 damage pipeline, fixed modifier order.
	atk := me.card.Stats.Vigor
	if mv.Kind == Hex {
		atk = me.card.Stats.Wit
	}
	def := (them.card.Stats.Grit + them.card.Stats.Focus) / 2
	if def < 1 {
		def = 1
	}
	base := mv.Power*atk/(def*2) + 1
	if base < 1 {
		base = 1
	}

	// 1. Attacker status power modifiers.
	if me.status == Deprecated {
		base = base * 3 / 4
	}
	if me.status == WarmedUp {
		base = base * 3 / 2
		me.status = NoStatus // consumed
		me.statusTurns = 0
	}
	// 2. Type effectiveness.
	eff := Effectiveness(mv.Type, them.sp.Type)
	switch eff {
	case Super:
		base *= 2
	case Resisted:
		base /= 2
	}
	// 3. Target status damage modifier.
	if them.status == TokenBloat {
		base = base * 5 / 4
	}
	// 4. BOOST charge (attacker).
	if me.boostCharge {
		base = base * 3 / 2
		me.boostCharge = false
	}
	// 5. GUARD charge (defender).
	if them.guardCharge {
		base = base * 2 / 3
		them.guardCharge = false
	}
	// 6. §3.5 draw 5 — damage variance.
	roll16 := rng.next() % 16
	base = base * (14 + int(roll16%3)) / 16
	if base < 1 {
		base = 1
	}
	// 7. §3.5 draw 6 — crit; HOTPATCHED (held by defender, from this
	// attacker) skips the roll and forces the crit.
	crit := false
	if them.status == Hotpatched {
		crit = true
		them.status = NoStatus
		them.statusTurns = 0
	} else {
		crit = rng.next()%16 == 0
	}
	if crit {
		base *= 2
	}
	if base < 1 {
		base = 1
	}

	them.hp -= base
	if them.hp < 0 {
		them.hp = 0
	}

	switch {
	case crit:
		log("%s used %s! CRIT — %s takes %d!", name, mv.Name, target, base)
	case eff == Super:
		log("%s used %s! Super effective — %s takes %d!", name, mv.Name, target, base)
	case eff == Resisted:
		log("%s used %s. Not very effective... %s takes %d.", name, mv.Name, target, base)
	default:
		log("%s used %s! %s takes %d.", name, mv.Name, target, base)
	}

	if inflicted != NoStatus {
		applyStatus(them, inflicted)
		log("%s is now %s.", target, inflicted)
	}

	// HOTPATCHED trigger: fires on exactly the 2nd consecutive landed
	// STRIKE (§2's literal wording) — a longer chain does not re-trigger
	// until a miss or non-STRIKE resets the counter.
	if mv.Kind == Strike {
		me.consecStrikesLanded++
		if me.consecStrikesLanded == 2 && them.status == NoStatus {
			applyStatus(them, Hotpatched)
			log("%s is now HOTPATCHED.", target)
		}
	} else {
		me.consecStrikesLanded = 0
	}
}

// endOfAction applies the acting pet's end-of-turn status effects
// (MEMORY_LEAK tick) and duration countdown — "each time the turn counter
// advances past that pet" per §2.
func endOfAction(me, them *petState, res *Result, log func(string, ...any)) {
	_ = them
	if me.status == MemoryLeak {
		tick := me.hpMax / 16
		if tick < 1 {
			tick = 1
		}
		me.hp -= tick
		if me.hp < 0 {
			me.hp = 0
		}
		log("%s takes %d from MEMORY_LEAK.", me.card.Name(), tick)
	}
	if me.status != NoStatus {
		if me.statusFresh {
			me.statusFresh = false // applied mid-action; first tick is free
		} else {
			me.statusTurns--
			if me.statusTurns <= 0 {
				me.status = NoStatus
			}
		}
	}
}

// winCheck applies §3.7 after every resolved action. Returns true when the
// battle ended (res filled in).
func winCheck(pets *[2]petState, res *Result, log func(string, ...any)) bool {
	a, b := pets[0].hp <= 0, pets[1].hp <= 0
	switch {
	case a && b:
		res.Winner = -1
	case a:
		res.Winner = 1
	case b:
		res.Winner = 0
	default:
		return false
	}
	closeOut(res, log)
	return true
}

// closeOut appends the §4 victory/defeat/draw lines.
func closeOut(res *Result, log func(string, ...any)) {
	if res.Winner < 0 {
		log("Both Codelings hit the ground at once. Nobody's calling this one.")
		return
	}
	w := res.Cards[res.Winner]
	l := res.Cards[1-res.Winner]
	log("%s", voiceLine(w, true))
	log("%s", voiceLine(l, false))
}

// voiceLine picks the §4 line-keyed victory/defeat flavor.
func voiceLine(c Card, won bool) string {
	sp, _ := species.ByID(c.SpeciesID)
	name := c.Name()
	switch sp.Line {
	case species.Ember:
		if won {
			return name + `: "Still running. Didn't even need to reboot for that one."`
		}
		return name + `: "...worth it. Going back to sleep on a warmer core."`
	case species.StreamLine:
		if won {
			return name + `: "Cache hit. Cache hit. Cache hit. GG."`
		}
		return name + `: "Cold start. Every single turn. Rough."`
	case species.Vector:
		if won {
			return name + `: "Turns out I'd seen this exact matchup before. Somewhere."`
		}
		return name + `: "New opponent, new lesson. Logging it for next time."`
	}
	if won {
		return name + `: "gg — that's a clean exit code."`
	}
	return name + `: "Well. That's a stack trace I'll remember."`
}
