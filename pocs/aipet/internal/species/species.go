// Package species holds the embedded, static Codelings launch roster: 30
// species (9 canon starters across 3 evolution lines, plus 21 original
// species) with their base stats, evolution rules, sprites, and flavor text.
//
// This is pure data — no game logic lives here. internal/sim consumes it to
// pick a starter line, resolve evolutions, and compute grown stats.
package species

// Type is one of the six fixed Codelings types, arranged in a closed
// effectiveness wheel: Cache > Context > Runtime > Syntax > Stream > Daemon
// > Cache (each 2x the next, 0.5x the previous, 1x otherwise).
type Type string

const (
	Cache   Type = "cache"
	Context Type = "context"
	Runtime Type = "runtime"
	Syntax  Type = "syntax"
	Stream  Type = "stream"
	Daemon  Type = "daemon"
)

// Rarity is one of the five fixed tiers, each with a base-stat-total band.
type Rarity string

const (
	Common   Rarity = "common"
	Uncommon Rarity = "uncommon"
	Rare     Rarity = "rare"
	Relic    Rarity = "relic"
	Mythic   Rarity = "mythic"
)

// Line identifies one of the three starter evolution lines. Non-starter
// species have an empty Line.
type Line string

const (
	NoLine     Line = ""
	Ember      Line = "ember"  // Cindling -> Forgeon -> Pyrolith (deep work)
	StreamLine Line = "stream" // Rivulet -> Cascada -> Torrentide (fast iteration)
	Vector     Line = "vector" // Glyphit -> Polyglyph -> Omniglyph (breadth)
)

// Stats is the five-stat block every species and every raised pet has.
// Species.Stats are BASE stats (before IVs/growth); the identical struct
// shape is reused by internal/sim for a live pet's grown stats.
type Stats struct {
	Vigor int `json:"vigor"` // total healthy activity (turns, sessions)
	Focus int `json:"focus"` // cache-read ratio
	Wit   int `json:"wit"`   // model-routing quality
	Grit  int `json:"grit"`  // streaks, consecutive active days
	Spark int `json:"spark"` // rare events (new model, new project, odd hours)
}

// Sum returns the base stat total (BST).
func (s Stats) Sum() int { return s.Vigor + s.Focus + s.Wit + s.Grit + s.Spark }

// Species is one immutable Dex entry.
type Species struct {
	Dex       int
	ID        string // stable snake_case identifier, also the save-file key
	Name      string
	Type      Type
	Rarity    Rarity
	Habitat   string
	Base      Stats
	Line      Line
	Stage     int    // 0 for non-line species, 1/2/3 for starter lines
	EvolvesTo string // ID of the next stage, empty if final/standalone
	Art       string // Unicode sprite, \n-separated, alignment-safe in a centered <pre>/TUI cell
	DexEntry  string
	Encounter string // human-readable description of the real signal that surfaces this species
}

// All is the full 30-species launch roster, ordered by Dex number.
var All = []Species{
	{
		Dex: 1, ID: "cindling", Name: "Cindling", Type: Runtime, Rarity: Common, Habitat: "Runtime Ridge",
		Base: Stats{Vigor: 55, Focus: 30, Wit: 25, Grit: 60, Spark: 30},
		Line: Ember, Stage: 1, EvolvesTo: "forgeon",
		Art:       "▴\n(• ◡ •)\n( ─── )\n˘   ˘",
		DexEntry:  "Curls up inside whatever process has been running longest and naps there. Wakes up cranky if you Ctrl+C it before it's ready.",
		Encounter: "Hatches from the starter egg when the first 3 active days are dominated by long, single-focus sessions with few context switches.",
	},
	{
		Dex: 2, ID: "forgeon", Name: "Forgeon", Type: Runtime, Rarity: Rare, Habitat: "Runtime Ridge",
		Base: Stats{Vigor: 75, Focus: 40, Wit: 35, Grit: 90, Spark: 45},
		Line: Ember, Stage: 2, EvolvesTo: "pyrolith",
		Art:       "▴▴▴\n(◕ ◡ ◕)\n(═════)\n˘   ˘",
		DexEntry:  "Runs hot for hours without complaint, the way a build server does right before a release. Doesn't love being interrupted mid-compile.",
		Encounter: "Evolution-only — appears once your cindling evolves at level 12 with GRIT dominant.",
	},
	{
		Dex: 3, ID: "pyrolith", Name: "Pyrolith", Type: Runtime, Rarity: Relic, Habitat: "Runtime Ridge",
		Base: Stats{Vigor: 90, Focus: 45, Wit: 45, Grit: 110, Spark: 55},
		Line: Ember, Stage: 3,
		Art:       "▲▲▲\n(◆ ◡ ◆)\n(══▣══)\n˘˘   ˘˘",
		DexEntry:  "A living uptime counter. Keepers say it hasn't cold-started since the day it hatched — and it intends to keep it that way.",
		Encounter: "Evolution-only — the reward for a Daemonkeeper who never broke a deep-work streak long enough to reach level 30.",
	},
	{
		Dex: 4, ID: "rivulet", Name: "Rivulet", Type: Stream, Rarity: Common, Habitat: "Streamfall",
		Base: Stats{Vigor: 40, Focus: 60, Wit: 25, Grit: 35, Spark: 40},
		Line: StreamLine, Stage: 1, EvolvesTo: "cascada",
		Art:       "∿∿∿\n≋(° ◡ °)>\n‿‿‿",
		DexEntry:  "A ribbon of scrolling text that darts between short turns. Happiest when a prompt hits the cache and it barely has to think.",
		Encounter: "Hatches from the starter egg when the first 3 active days are dominated by many short, cheap, cache-heavy turns.",
	},
	{
		Dex: 5, ID: "cascada", Name: "Cascada", Type: Stream, Rarity: Rare, Habitat: "Streamfall",
		Base: Stats{Vigor: 60, Focus: 90, Wit: 40, Grit: 50, Spark: 60},
		Line: StreamLine, Stage: 2, EvolvesTo: "torrentide",
		Art:       "∿∿∿∿\n≋≋(˘ ◡ ˘)>\n‿‿‿‿",
		DexEntry:  "Splits into a dozen quick turns before a forgeon finishes warming up. Cache misses make it visibly wince.",
		Encounter: "Evolution-only — appears once your rivulet evolves at level 12 with FOCUS dominant.",
	},
	{
		Dex: 6, ID: "torrentide", Name: "Torrentide", Type: Stream, Rarity: Relic, Habitat: "Streamfall",
		Base: Stats{Vigor: 65, Focus: 105, Wit: 45, Grit: 55, Spark: 65},
		Line: StreamLine, Stage: 3,
		Art:       "∿∿∿∿∿\n≋≋≋(˘ ◡ ˘)≫\n≈≈≈≈≈",
		DexEntry:  "Moves like a river that has memorized its own bed — every prompt lands somewhere it's already been. Almost never pays full price.",
		Encounter: "Evolution-only — the reward for a Daemonkeeper who kept cache reuse high across a long stretch of iteration.",
	},
	{
		Dex: 7, ID: "glyphit", Name: "Glyphit", Type: Syntax, Rarity: Common, Habitat: "Syntax Thicket",
		Base: Stats{Vigor: 35, Focus: 25, Wit: 60, Grit: 30, Spark: 50},
		Line: Vector, Stage: 1, EvolvesTo: "polyglyph",
		Art:       "˖ ˖\n\\+ +/\n(• ◡ •)\n/- -\\",
		DexEntry:  "Wings made of diff hunks — every flutter shows a + and a -. Can't resist landing on whatever file just changed.",
		Encounter: "Hatches from the starter egg when the first 3 active days touch several different projects or models rather than one deep track.",
	},
	{
		Dex: 8, ID: "polyglyph", Name: "Polyglyph", Type: Syntax, Rarity: Rare, Habitat: "Syntax Thicket",
		Base: Stats{Vigor: 50, Focus: 40, Wit: 95, Grit: 45, Spark: 70},
		Line: Vector, Stage: 2, EvolvesTo: "omniglyph",
		Art:       "˖ ˖\n\\\\+ +//\n((• ◡ •))\n//- -\\\\",
		DexEntry:  "Fluent in three languages by Tuesday and a fourth by Friday. Gets restless if you keep it in one repo too long.",
		Encounter: "Evolution-only — appears once your glyphit evolves at level 12 with SPARK dominant.",
	},
	{
		Dex: 9, ID: "omniglyph", Name: "Omniglyph", Type: Syntax, Rarity: Relic, Habitat: "Syntax Thicket",
		Base: Stats{Vigor: 55, Focus: 40, Wit: 110, Grit: 48, Spark: 80},
		Line: Vector, Stage: 3,
		Art:       "˖ ˖ ˖\n\\\\\\+ +///\n=((◆ ◡ ◆))=\n///- -\\\\\\",
		DexEntry:  "Has touched every model in your logs at least once and remembers what each one is good for. The Shellwoods' best-traveled wing.",
		Encounter: "Evolution-only — the reward for a Daemonkeeper who kept switching projects and models without ever settling into a rut.",
	},
	{
		Dex: 10, ID: "hoardlet", Name: "Hoardlet", Type: Cache, Rarity: Common, Habitat: "The Cachefen",
		Base:      Stats{Vigor: 35, Focus: 75, Wit: 35, Grit: 45, Spark: 25},
		Art:       "╭─◠─╮\n(o• ◡ •o)\n╰ $$ ╯",
		DexEntry:  "Stuffs its cheeks with anything that might get reused later. Mostly right about that. Occasionally hoards a prompt nobody will repeat.",
		Encounter: "A session that reuses the same cached prefix five or more times in a row.",
	},
	{
		Dex: 11, ID: "memoize", Name: "Memoize", Type: Cache, Rarity: Common, Habitat: "The Cachefen",
		Base:      Stats{Vigor: 30, Focus: 70, Wit: 45, Grit: 40, Spark: 30},
		EvolvesTo: "memoizard",
		Art:       "╭────╮\n(◕ ◡ ◕)\n╰─◡◡─╯",
		DexEntry:  "Never answers the same question twice — it just remembers the first answer and hands it back, a little smug about it.",
		Encounter: "First session in a project where cache-read tokens outnumber fresh input tokens.",
	},
	{
		Dex: 12, ID: "memoizard", Name: "Memoizard", Type: Cache, Rarity: Uncommon, Habitat: "The Cachefen",
		Base:      Stats{Vigor: 35, Focus: 90, Wit: 55, Grit: 45, Spark: 30},
		Art:       "╔════╗\n(◕ ◡ ◕)\n╰─◡◡─╯",
		DexEntry:  "A memo table with legs. Keeps every answer it's ever given on hand, filed, and ready before you finish typing the question.",
		Encounter: "Evolution-only — appears once your memoize evolves at level 12 with sustained high cache-read ratio.",
	},
	{
		Dex: 13, ID: "pinshell", Name: "Pinshell", Type: Cache, Rarity: Uncommon, Habitat: "The Cachefen",
		Base:      Stats{Vigor: 45, Focus: 75, Wit: 40, Grit: 65, Spark: 25},
		Art:       "╭▩▩▩╮\n(◕ ◡ ◕)\n˘╰──╯˘",
		DexEntry:  "Pulls into a shell at the first sign of a version bump and refuses to come out until something forces a lockfile update.",
		Encounter: "A project directory whose dependency lockfile hasn't changed across many active sessions in a row.",
	},
	{
		Dex: 14, ID: "staleout", Name: "Staleout", Type: Cache, Rarity: Rare, Habitat: "The Cachefen",
		Base:      Stats{Vigor: 50, Focus: 70, Wit: 55, Grit: 55, Spark: 60},
		Art:       "╭ ╌╌ ╮\n(◔ ╌ ◔)\n╰ ╌╌ ╯",
		DexEntry:  "Flickers half-transparent when a cache entry finally expires. Reappears solid the moment something warms it back up.",
		Encounter: "A session where a previously high cache-read ratio drops sharply after a long gap between sessions.",
	},
	{
		Dex: 15, ID: "widecope", Name: "Widecope", Type: Context, Rarity: Common, Habitat: "The Contexta Canopy",
		Base:      Stats{Vigor: 35, Focus: 35, Wit: 80, Grit: 30, Spark: 40},
		Art:       "╭─────╮\n(◕ ◕ ◕)\n╰──╥──╯",
		DexEntry:  "Has three eyes and uses all of them — one on the diff, one on the terminal, one on whatever tab you forgot was open.",
		Encounter: "A session that reads from more than a handful of files before writing a single line.",
	},
	{
		Dex: 16, ID: "tabsprout", Name: "Tabsprout", Type: Context, Rarity: Common, Habitat: "The Contexta Canopy",
		Base:      Stats{Vigor: 30, Focus: 30, Wit: 75, Grit: 25, Spark: 45},
		EvolvesTo: "tabgrove",
		Art:       "▐▌ ▐▌\n(˘ ◡ ˘)\n╰───╯",
		DexEntry:  "Grows a new little tab-shaped leaf every time you open one more file than you meant to. It is judging your open-tab count.",
		Encounter: "A session touching a moderate handful of files in the same working tree.",
	},
	{
		Dex: 17, ID: "tabgrove", Name: "Tabgrove", Type: Context, Rarity: Uncommon, Habitat: "The Contexta Canopy",
		Base:      Stats{Vigor: 35, Focus: 35, Wit: 95, Grit: 30, Spark: 55},
		Art:       "▐▌▐▌▐▌\n(˘ ◡ ˘)\n╰────╯",
		DexEntry:  "A whole thicket of tab-leaves now, rustling every time a new file enters the context window. Somehow still keeps track of all of them.",
		Encounter: "Evolution-only — appears once your tabsprout evolves at level 12 with sustained wide-file-span sessions.",
	},
	{
		Dex: 18, ID: "longwindow", Name: "Longwindow", Type: Context, Rarity: Relic, Habitat: "The Contexta Canopy",
		Base:      Stats{Vigor: 55, Focus: 45, Wit: 120, Grit: 55, Spark: 65},
		Art:       "▐▌▐▌▐▌▐▌\n(◕ │ ◕)\n╰─────╯",
		DexEntry:  "Holds an entire architecture in its head without breaking a sweat — right up until someone asks it to also remember lunch.",
		Encounter: "A single session with an unusually large total context size sustained across many turns.",
	},
	{
		Dex: 19, ID: "everfile", Name: "Everfile", Type: Context, Rarity: Mythic, Habitat: "The Contexta Canopy",
		Base:      Stats{Vigor: 75, Focus: 60, Wit: 140, Grit: 70, Spark: 55},
		Art:       "▐█▐█▐█▐█\n(◆ ║ ◆)\n╚══════╝\n˅      ˅",
		DexEntry:  "Legend says it read an entire repository in a single turn and never forgot a line of it. Keepers who've seen it describe total silence.",
		Encounter: "MYTHIC, encounter-only: a single session whose context spans the whole repository (every tracked file touched in one continuous turn sequence).",
	},
	{
		Dex: 20, ID: "cronkin", Name: "Cronkin", Type: Daemon, Rarity: Uncommon, Habitat: "The Daemon Deep",
		Base:      Stats{Vigor: 45, Focus: 35, Wit: 40, Grit: 85, Spark: 50},
		EvolvesTo: "cronarch",
		Art:       "╭─◷─╮\n(• ◡ •)\n╰┬──┬╯",
		DexEntry:  "Checks the clock obsessively and shows up exactly on schedule, every single day, whether or not you remembered to.",
		Encounter: "A streak of consecutive active days reaching a solid week without a gap.",
	},
	{
		Dex: 21, ID: "cronarch", Name: "Cronarch", Type: Daemon, Rarity: Rare, Habitat: "The Daemon Deep",
		Base:      Stats{Vigor: 55, Focus: 40, Wit: 45, Grit: 105, Spark: 60},
		Art:       "╔═◷═╗\n(◕ ◡ ◕)\n╰┬──┬╯",
		DexEntry:  "Has never once missed its scheduled run. Other Codelings set their internal clocks by it, whether it asked them to or not.",
		Encounter: "Evolution-only — appears once your cronkin evolves at level 12 with a sustained streak.",
	},
	{
		Dex: 22, ID: "nightproc", Name: "Nightproc", Type: Daemon, Rarity: Common, Habitat: "The Daemon Deep",
		Base:      Stats{Vigor: 40, Focus: 30, Wit: 35, Grit: 65, Spark: 45},
		Art:       "☾\n(˘ ω ˘)\n╰─◡─╯\nᶻ ᶻ ᶻ",
		DexEntry:  "Most active well after midnight, when everyone sane is asleep and the only sound is a save file quietly writing itself.",
		Encounter: "A session that starts and ends well after midnight local time.",
	},
	{
		Dex: 23, ID: "zombierun", Name: "Zombierun", Type: Daemon, Rarity: Uncommon, Habitat: "The Cachefen",
		Base:      Stats{Vigor: 50, Focus: 35, Wit: 40, Grit: 70, Spark: 55},
		Art:       "╭ ╌╌ ╮\n(✕ ╌ ✕)\n╰ ╌╌ ╯\n?  ?",
		DexEntry:  "Wanders the old cache halls looking for a parent process that stopped calling back. Not dead. Not exactly working either. Technically fine.",
		Encounter: "A session resumes against a stale cache from a project untouched for a long stretch of time.",
	},
	{
		Dex: 24, ID: "uptimewyrm", Name: "Uptimewyrm", Type: Daemon, Rarity: Mythic, Habitat: "The Daemon Deep",
		Base:      Stats{Vigor: 80, Focus: 55, Wit: 60, Grit: 140, Spark: 45},
		Art:       "∿═∿═∿\n(◕ ω ◕)\n∿═∿═∿\n·365·",
		DexEntry:  "A coil of daemon processes so old the Shellwoods lost count of its restarts — except it says there weren't any. Not one, in a whole year.",
		Encounter: "MYTHIC, encounter-only: a full year (365 days) of active-day streak with no gap.",
	},
	{
		Dex: 25, ID: "stackrail", Name: "Stackrail", Type: Runtime, Rarity: Common, Habitat: "Runtime Ridge",
		Base:      Stats{Vigor: 60, Focus: 30, Wit: 35, Grit: 45, Spark: 30},
		Art:       "▤▤▤▤▤\n(• ◡ •)\n(═════)\nʌ ʌ ʌ ʌ",
		DexEntry:  "A long centipede of stack frames. Panics beautifully, then unwinds itself one segment at a time until it finds where things went wrong.",
		Encounter: "A session with an unusually high volume of turns in a single sitting.",
	},
	{
		Dex: 26, ID: "threadwolf", Name: "Threadwolf", Type: Runtime, Rarity: Rare, Habitat: "The Daemon Deep",
		Base:      Stats{Vigor: 85, Focus: 40, Wit: 50, Grit: 60, Spark: 45},
		Art:       "▲ ▲  ▲ ▲\n(◕ᴥ◕)(◕ᴥ◕)\n˘ ˘  ˘ ˘",
		DexEntry:  "Never travels alone — where there's one, there are several more running in parallel, all somehow finishing at almost the same moment.",
		Encounter: "A session with multiple long-running turns clearly overlapping in time rather than running one after another.",
	},
	{
		Dex: 27, ID: "bufferpup", Name: "Bufferpup", Type: Stream, Rarity: Uncommon, Habitat: "Runtime Ridge",
		Base:      Stats{Vigor: 45, Focus: 65, Wit: 35, Grit: 35, Spark: 65},
		Art:       "◠   ◠\n(◕ ᴥ ◕)\n╰─◡─╯\n∪   ∪",
		DexEntry:  "Chases every keystroke the instant it lands, tail wagging in time with the scrollback. Gets antsy if a response takes more than a beat.",
		Encounter: "A burst of many very short turns in quick succession.",
	},
	{
		Dex: 28, ID: "flashecho", Name: "Flashecho", Type: Stream, Rarity: Rare, Habitat: "Streamfall",
		Base:      Stats{Vigor: 55, Focus: 65, Wit: 40, Grit: 40, Spark: 85},
		Art:       "≫≫≫\n(> ◡ <)≫\n‿‿‿",
		DexEntry:  "Answers before you've finished asking. Keepers still argue about whether it's actually fast or just really good at guessing early.",
		Encounter: "A session where a new model appears in the logs for the first time and immediately produces very low-latency turns.",
	},
	{
		Dex: 29, ID: "bracketail", Name: "Bracketail", Type: Syntax, Rarity: Common, Habitat: "Syntax Thicket",
		Base:      Stats{Vigor: 40, Focus: 40, Wit: 65, Grit: 40, Spark: 40},
		Art:       "{ [ ] }\n[◕ ◡ ◕]\n{ [ ] }",
		DexEntry:  "Counts every open bracket out loud and will not rest until it finds the matching close. Deeply unsettled by unbalanced parens.",
		Encounter: "A session that resolves a large, deeply nested merge conflict cleanly in one pass.",
	},
	{
		Dex: 30, ID: "lintmoth", Name: "Lintmoth", Type: Syntax, Rarity: Uncommon, Habitat: "Syntax Thicket",
		Base:      Stats{Vigor: 35, Focus: 45, Wit: 75, Grit: 40, Spark: 45},
		Art:       "˖ ˖\n\\] [/\n(˘ ◡ ˘)\n/] [\\",
		DexEntry:  "Drawn to trailing whitespace like a moth to a lamp. Leaves every file it visits a little tidier than it found it.",
		Encounter: "A session that runs a linter or formatter clean with zero new warnings introduced.",
	},
}

var (
	byID  = map[string]*Species{}
	lines = map[Line][]string{} // Line -> ordered []ID by stage
)

func init() {
	for i := range All {
		s := &All[i]
		byID[s.ID] = s
		if s.Line != NoLine {
			lines[s.Line] = append(lines[s.Line], s.ID)
		}
	}
}

// ByID looks up a species by its stable id. ok is false for an unknown id.
func ByID(id string) (Species, bool) {
	s, ok := byID[id]
	if !ok {
		return Species{}, false
	}
	return *s, true
}

// LineStarter returns the stage-1 species id for a starter line.
func LineStarter(l Line) (string, bool) {
	ids, ok := lines[l]
	if !ok || len(ids) == 0 {
		return "", false
	}
	// All lines are authored stage-1..3 in Dex order, so the lowest Dex
	// number in the line is the starter — resolve by walking All rather
	// than assuming append order survives future edits.
	best := ""
	bestDex := -1
	for _, id := range ids {
		sp := byID[id]
		if bestDex == -1 || sp.Dex < bestDex {
			best, bestDex = id, sp.Dex
		}
	}
	return best, true
}
