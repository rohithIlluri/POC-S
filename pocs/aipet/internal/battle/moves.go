// Package battle implements Codelings battles exactly as specified by
// docs/design/moves.md: the 45-move table, 6 status effects, and the
// deterministic battle-resolution algorithm (§3). A battle is a pure
// function of (cardA, cardB, UTC date) — no wall clock, no floats, no map
// iteration anywhere in the resolution path, so two machines replay the
// identical fight from the same two cards (GAME_DESIGN §4.6's serverless
// battle contract).
package battle

import (
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

// Kind classifies what a move does (moves.md §1).
type Kind string

const (
	Strike Kind = "STRIKE" // direct damage, VIGOR-scaled
	Guard  Kind = "GUARD"  // +1/2 DEF charge for next incoming hit, or cleanse
	Hex    Kind = "HEX"    // small WIT-scaled damage + status-inflict chance
	Boost  Kind = "BOOST"  // +1/2 ATK charge for next outgoing hit
)

// Status is one of the six battle statuses (moves.md §2). A pet holds at
// most one at a time.
type Status string

const (
	NoStatus    Status = ""
	Deprecated  Status = "DEPRECATED"
	RateLimited Status = "RATE_LIMITED"
	MemoryLeak  Status = "MEMORY_LEAK"
	TokenBloat  Status = "TOKEN_BLOAT"
	Hotpatched  Status = "HOTPATCHED"
	WarmedUp    Status = "WARMED_UP"
)

// statusDuration is each status's turn count on inflict (moves.md §2).
var statusDuration = map[Status]int{
	Deprecated:  3,
	RateLimited: 2,
	MemoryLeak:  3,
	TokenBloat:  2,
	Hotpatched:  1,
	WarmedUp:    1,
}

// Frac is an exact power-of-two fraction — the only probability shape the
// design allows (SPEC rule: no floats anywhere).
type Frac struct {
	Num, Den int
}

// One is the 1/1 "never misses" accuracy.
var One = Frac{1, 1}

// Move is one row of the 45-move table. Power is 0 for GUARD/BOOST.
// Inflicts/InflictChance are zero-valued for moves with no status effect.
type Move struct {
	ID            string
	Name          string
	Type          species.Type
	Kind          Kind
	Power         int
	Accuracy      Frac
	WitCost       int
	Inflicts      Status
	InflictChance Frac
	Flavor        string
}

// BareMetal is the implicit fallback STRIKE (moves.md §3.6): 0-cost,
// power 40, 1/1 accuracy, the user's own species type. It is not one of
// the 45 — a Focus-drained pet is never unable to act.
func BareMetal(t species.Type) Move {
	return Move{ID: "bare_metal", Name: "Bare Metal", Type: t, Kind: Strike, Power: 40, Accuracy: One,
		Flavor: "No abstractions left. Just hits the hardware directly."}
}

// Pools is the six type pools (moves.md §1.1–1.6), each exactly
// 3 STRIKE + 1 GUARD + 1 BOOST + 1 HEX (§1.7 audit shape).
var Pools = map[species.Type][]Move{
	species.Cache: {
		{ID: "cache_flush", Name: "Cache Flush", Type: species.Cache, Kind: Strike, Power: 55, Accuracy: Frac{7, 8},
			Flavor: "Dumps the whole cache at once. Somehow this hurts them, not you."},
		{ID: "warm_start", Name: "Warm Start", Type: species.Cache, Kind: Strike, Power: 40, Accuracy: One,
			Flavor: "Skips the cold boot entirely. Reliable, unglamorous, effective."},
		{ID: "evict_lru", Name: "Evict LRU", Type: species.Cache, Kind: Strike, Power: 65, Accuracy: Frac{3, 4},
			Flavor: "Kicks out whatever hasn't been touched in a while. Ruthless."},
		{ID: "pin_shard", Name: "Pin Shard", Type: species.Cache, Kind: Guard, Accuracy: One, WitCost: 4,
			Flavor: "Pins the hot data down. Nothing's knocking it loose this turn."},
		{ID: "prefetch", Name: "Prefetch", Type: species.Cache, Kind: Boost, Accuracy: One, WitCost: 4,
			Flavor: "Grabs what it'll need before it's asked. Smug about it."},
		{ID: "stale_read", Name: "Stale Read", Type: species.Cache, Kind: Hex, Power: 25, Accuracy: Frac{7, 8}, WitCost: 6,
			Inflicts: Deprecated, InflictChance: Frac{1, 4},
			Flavor: "Serves you an answer from three versions ago. Technically a response."},
	},
	species.Context: {
		{ID: "scope_creep", Name: "Scope Creep", Type: species.Context, Kind: Strike, Power: 50, Accuracy: Frac{7, 8},
			Flavor: `"Just one more file" turns into the whole repo. Every time.`},
		{ID: "long_diff", Name: "Long Diff", Type: species.Context, Kind: Strike, Power: 70, Accuracy: Frac{5, 8},
			Flavor: "Four thousand lines changed. Reviewer not included."},
		{ID: "grep_sweep", Name: "Grep Sweep", Type: species.Context, Kind: Strike, Power: 45, Accuracy: One,
			Flavor: "Finds every match in the tree. Doesn't miss. Can't miss."},
		{ID: "context_window", Name: "Context Window", Type: species.Context, Kind: Guard, Accuracy: One, WitCost: 4,
			Flavor: "Widens the frame. Sees the hit coming from further away."},
		{ID: "rubber_duck", Name: "Rubber Duck", Type: species.Context, Kind: Boost, Accuracy: One, WitCost: 4,
			Flavor: "Explains the plan out loud first. Somehow this always helps."},
		{ID: "todo_bomb", Name: "TODO Bomb", Type: species.Context, Kind: Hex, Power: 20, Accuracy: Frac{7, 8}, WitCost: 6,
			Inflicts: RateLimited, InflictChance: Frac{1, 4},
			Flavor: "Leaves forty `// TODO` comments and walks away whistling."},
	},
	species.Runtime: {
		{ID: "segfault", Name: "Segfault", Type: species.Runtime, Kind: Strike, Power: 80, Accuracy: Frac{5, 8},
			Flavor: "Reaches into memory it was never given. Devastating when it lands."},
		{ID: "force_push", Name: "Force Push", Type: species.Runtime, Kind: Strike, Power: 60, Accuracy: Frac{3, 4},
			Flavor: "Overwrites whatever was there. History optional."},
		{ID: "hotfix", Name: "Hotfix", Type: species.Runtime, Kind: Strike, Power: 45, Accuracy: One,
			Flavor: "Small, ugly, ships in the next five minutes. Works."},
		{ID: "sandbox", Name: "Sandbox", Type: species.Runtime, Kind: Guard, Accuracy: One, WitCost: 4,
			Flavor: "Runs the risky part in isolation first. Nothing escapes."},
		{ID: "overclock", Name: "Overclock", Type: species.Runtime, Kind: Boost, Accuracy: One, WitCost: 4,
			Flavor: "Pushes the clock past the sane range. Fans scream. Output rises."},
		{ID: "panic_unwind", Name: "Panic Unwind", Type: species.Runtime, Kind: Hex, Power: 30, Accuracy: Frac{7, 8}, WitCost: 6,
			Inflicts: MemoryLeak, InflictChance: Frac{1, 4},
			Flavor: "Doesn't crash cleanly. Leaves a mess on the way down."},
	},
	species.Syntax: {
		{ID: "off_by_one", Name: "Off By One", Type: species.Syntax, Kind: Strike, Power: 50, Accuracy: Frac{7, 8},
			Flavor: "Almost exactly right. That's the problem."},
		{ID: "type_coerce", Name: "Type Coerce", Type: species.Syntax, Kind: Strike, Power: 55, Accuracy: Frac{3, 4},
			Flavor: "Forces the wrong shape into the right slot. It fits. Barely."},
		{ID: "semicolon_jab", Name: "Semicolon Jab", Type: species.Syntax, Kind: Strike, Power: 35, Accuracy: One,
			Flavor: "Small, precise, technically optional. Lands anyway."},
		{ID: "linter_pass", Name: "Linter Pass", Type: species.Syntax, Kind: Guard, Accuracy: One, WitCost: 4,
			Flavor: "Cleans up every loose end before the next hit arrives."},
		{ID: "refactor", Name: "Refactor", Type: species.Syntax, Kind: Boost, Accuracy: One, WitCost: 4,
			Flavor: "Same behavior, better shape. Somehow hits harder now."},
		{ID: "syntax_error", Name: "Syntax Error", Type: species.Syntax, Kind: Hex, Power: 15, Accuracy: Frac{7, 8}, WitCost: 6,
			Inflicts: RateLimited, InflictChance: Frac{1, 4},
			Flavor: "Refuses to even parse the request. Everyone waits."},
	},
	species.Stream: {
		{ID: "race_condition", Name: "Race Condition", Type: species.Stream, Kind: Strike, Power: 70, Accuracy: Frac{5, 8},
			Flavor: "Two things happen at once. Only one was supposed to win."},
		{ID: "flash_flush", Name: "Flash Flush", Type: species.Stream, Kind: Strike, Power: 50, Accuracy: Frac{7, 8},
			Flavor: "Pushes the whole buffer out in one burst. Downstream copes."},
		{ID: "heartbeat", Name: "Heartbeat", Type: species.Stream, Kind: Strike, Power: 40, Accuracy: One,
			Flavor: "A small, steady ping. Never once fails to land."},
		{ID: "backpressure", Name: "Backpressure", Type: species.Stream, Kind: Guard, Accuracy: One, WitCost: 4,
			Flavor: "Slows the incoming flow to something survivable."},
		{ID: "pipeline", Name: "Pipeline", Type: species.Stream, Kind: Boost, Accuracy: One, WitCost: 4,
			Flavor: "Chains three small steps into one big one. Efficient. Fast."},
		{ID: "buffer_overrun", Name: "Buffer Overrun", Type: species.Stream, Kind: Hex, Power: 25, Accuracy: Frac{7, 8}, WitCost: 6,
			Inflicts: MemoryLeak, InflictChance: Frac{1, 4},
			Flavor: "Writes a little past where it was told to stop."},
	},
	species.Daemon: {
		{ID: "zombie_process", Name: "Zombie Process", Type: species.Daemon, Kind: Strike, Power: 60, Accuracy: Frac{3, 4},
			Flavor: "Already dead. Still holding a PID. Still swinging."},
		{ID: "cron_strike", Name: "Cron Strike", Type: species.Daemon, Kind: Strike, Power: 55, Accuracy: Frac{7, 8},
			Flavor: "Arrives exactly on schedule. Every single time."},
		{ID: "heartbeat_check", Name: "Heartbeat Check", Type: species.Daemon, Kind: Strike, Power: 40, Accuracy: One,
			Flavor: "A routine ping that somehow always connects."},
		{ID: "graceful_shutdown", Name: "Graceful Shutdown", Type: species.Daemon, Kind: Guard, Accuracy: One, WitCost: 4,
			Flavor: "Finishes the current job before anything gets to land."},
		{ID: "nohup", Name: "Nohup", Type: species.Daemon, Kind: Boost, Accuracy: One, WitCost: 4,
			Flavor: "Detaches from the terminal that spawned it. Keeps running regardless."},
		{ID: "orphan_signal", Name: "Orphan Signal", Type: species.Daemon, Kind: Hex, Power: 20, Accuracy: Frac{7, 8}, WitCost: 6,
			Inflicts: Deprecated, InflictChance: Frac{1, 4},
			Flavor: "Sent by a parent process that no longer exists. Lands anyway."},
	},
}

// Signatures maps starter species ID → that species' signature move
// (moves.md §1.9): one id per stage so lookup is a direct key, no runtime
// power interpolation. Signature types can differ from the species' own
// type (the Ember line's signatures are RUNTIME, Vector's are SYNTAX) and
// count toward type effectiveness like any pool move.
var Signatures = map[string]Move{
	// Ember line — a held burn that never quite goes out.
	"cindling": {ID: "pilot_light", Name: "Pilot Light", Type: species.Runtime, Kind: Strike, Power: 45, Accuracy: Frac{7, 8},
		Flavor: "A tiny flame that refuses to be `Ctrl+C`'d out."},
	"forgeon": {ID: "slow_burn", Name: "Slow Burn", Type: species.Runtime, Kind: Strike, Power: 65, Accuracy: Frac{7, 8},
		Flavor: "Runs hot for hours. Doesn't care that you're tired."},
	"pyrolith": {ID: "uptime_inferno", Name: "Uptime Inferno", Type: species.Runtime, Kind: Strike, Power: 90, Accuracy: Frac{3, 4},
		Flavor: "Hasn't cold-started once. Isn't planning to start now."},
	// Stream line — turns that arrive faster than you can react.
	"rivulet": {ID: "quick_turn", Name: "Quick Turn", Type: species.Stream, Kind: Strike, Power: 40, Accuracy: One,
		Flavor: "Small and fast. Already done before you'd have started."},
	"cascada": {ID: "cache_cascade", Name: "Cache Cascade", Type: species.Stream, Kind: Strike, Power: 60, Accuracy: One,
		Flavor: "One hit that fans out into a dozen free ones."},
	"torrentide": {ID: "torrent_of_hits", Name: "Torrent of Hits", Type: species.Stream, Kind: Strike, Power: 85, Accuracy: Frac{7, 8},
		Flavor: "Every prompt lands somewhere it's already been. Brutal rhythm."},
	// Vector line — knowing exactly the right tool, right now.
	"glyphit": {ID: "diff_flutter", Name: "Diff Flutter", Type: species.Syntax, Kind: Hex, Power: 30, Accuracy: Frac{7, 8}, WitCost: 5,
		Inflicts: RateLimited, InflictChance: Frac{1, 4},
		Flavor: "Wings full of `+`/`-` hunks. Lands wherever changed last."},
	"polyglyph": {ID: "polyglot_strike", Name: "Polyglot Strike", Type: species.Syntax, Kind: Hex, Power: 50, Accuracy: Frac{7, 8}, WitCost: 5,
		Inflicts: RateLimited, InflictChance: Frac{1, 4},
		Flavor: "Fluent in whatever this fight needs. Picks the right word."},
	"omniglyph": {ID: "omniglot_burst", Name: "Omniglot Burst", Type: species.Syntax, Kind: Hex, Power: 75, Accuracy: Frac{3, 4}, WitCost: 5,
		Inflicts: RateLimited, InflictChance: Frac{1, 2},
		Flavor: "Has touched every model in the logs. Speaks all of them at once."},
}

// byID indexes every real move (45 = 36 pool + 9 signature) for card
// validation and import checks. Built in init from the tables above so it
// can never drift from them.
var byID = map[string]Move{}

func init() {
	for _, pool := range Pools {
		for _, m := range pool {
			byID[m.ID] = m
		}
	}
	for _, m := range Signatures {
		byID[m.ID] = m
	}
}

// MoveByID returns a move from the 45-move table. bare_metal is not
// included — it is implicit and never appears on a card.
func MoveByID(id string) (Move, bool) {
	m, ok := byID[id]
	return m, ok
}

// LegalMoves returns the set of move ids a species may equip: its type's
// 6-move pool plus its own signature move if it has one (moves.md §1.9).
func LegalMoves(sp species.Species) map[string]bool {
	legal := make(map[string]bool, 7)
	for _, m := range Pools[sp.Type] {
		legal[m.ID] = true
	}
	if sig, ok := Signatures[sp.ID]; ok {
		legal[sig.ID] = true
	}
	return legal
}

// wheel is the fixed type-effectiveness ring (SPEC): each type is 2×
// against the NEXT type and ½× against the PREVIOUS.
var wheel = []species.Type{
	species.Cache, species.Context, species.Runtime,
	species.Syntax, species.Stream, species.Daemon,
}

// Effectiveness returns 2 (super), 0 (resisted — caller halves), or 1
// (neutral) for attack type vs defender type, per the wheel. Encoding as
// an enum keeps the damage math explicitly integer (moves.md §3.4 step 2).
type Effect int

const (
	Neutral Effect = iota
	Super
	Resisted
)

func Effectiveness(atk, def species.Type) Effect {
	for i, t := range wheel {
		if t != atk {
			continue
		}
		if wheel[(i+1)%len(wheel)] == def {
			return Super
		}
		if wheel[(i+len(wheel)-1)%len(wheel)] == def {
			return Resisted
		}
		return Neutral
	}
	return Neutral
}
