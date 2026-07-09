# Integration Plan — one local binary: efficiency tool + Codelings game

> **⚠ Superseded in part by implementation (2026-07-10).** The core of this
> plan is now BUILT and merged: the deterministic game layer lives in
> `internal/sim` (DNA/IVs/daily tick/evolution), `internal/care` (advisor
> rules as diet verdicts), `internal/species` (embedded 30-species Dex), and
> `internal/save` (pet.json + journal.jsonl) — wired into the daemon's
> `runCycle` and a new Pet TUI tab. Where this doc says `internal/game`, read
> those four packages; they are the implementation of record. Still-unbuilt
> sections (encounters, battles, trading — M3/M4) remain valid design
> references alongside `docs/design/rarity.md` and `docs/design/moves.md`.

**Status:** design, superseded in part by shipped code (see banner above)
**Author:** engineering
**Scope:** merge the shipped `aipet` cost/efficiency companion and the designed
`Codelings` game into a single, fully-local binary whose **backend work costs
zero API tokens**.

---

## 1. Thesis

The two halves are not two systems. They are **one deterministic engine with two
faces**:

- Today `aipet` folds `~/.claude/projects/**` + `~/.codex/sessions/**` session
  logs into a `[]store.Event` stream and derives cost/efficiency views.
- `Codelings` needs *exactly the same* event stream to raise a creature: species,
  stats, XP, evolution, encounters, and battles are all **pure functions of how
  you already worked**.

So the integration is not a rewrite. It is **one new deterministic reduction
layer** (`internal/game`) that consumes the existing `[]store.Event` and emits
game state into the existing `Snapshot`, rendered by new tabs in the existing
TUI. The advisor's efficiency suggestions *become* the game's "care tips."

### The token guarantee (the whole point)

Every backend operation is a pure Go function over data already on disk:

| Backend operation | Implementation | API tokens |
|---|---|---|
| Species assignment | deterministic classifier over event features | **0** |
| Stat derivation (VIGOR/FOCUS/WIT/GRIT/SPARK) | formulas over `store.Stats` + `leaderboard.Records` | **0** |
| XP / leveling / evolution | threshold checks | **0** |
| Encounters | predicate rules over event windows (same shape as `advisor.Rule`) | **0** |
| Voice lines / flavor | selection + templating over an **embedded** static library | **0** |
| Battles | local deterministic simulation over stats + moves | **0** |
| `.codeling` trade files | local (de)serialization | **0** |

The engine **never calls a model and opens no sockets** — it preserves aipet's
existing "zero network surface" property. The only place an LLM could appear is
an **optional, opt-in, on-device** flavor generator (§10) that talks to a *local*
runtime (Ollama/llama.cpp) — still **$0 API tokens**, off by default.

---

## 2. Current state (what we build on)

Real, tested types we hook into (do not reinvent):

```go
// store.Event — one observed model turn (the atom of everything)
type Event struct {
    Key, Source, Session, Project, Model string
    Timestamp  time.Time
    Input, Output, CacheWrite, CacheRead int64
    CostUSD    float64
}

// store.Stats — Aggregate(events) rollup (cost, tokens, cache, ByModel, ByProject, DailyCost…)
// leaderboard.Board.Records — CurrentStreak, LongestStreak, BiggestDayUSD, BusiestDay, ActiveDays, FirstSeen…
// daemon.Snapshot — the atomic JSON the TUI reads (extend this, don't replace)
```

Design source-of-truth already written: `SPEC.md`, `GAME_DESIGN.md`,
`design/lore.md`, `design/species.md` (30 species), `design/moves.md`,
`design/rarity.md`, `design/handoff/sprites.json`.

**The gap:** none of the game mechanics exist in Go yet. This plan closes it
without touching the zero-token / zero-network guarantees.

---

## 3. Target architecture

```
session logs (on disk, already written by Claude Code / Codex)
      │  collect (0 tokens)
      ▼
 store.Event log  ── Aggregate ──► store.Stats ──► advisor ──► Suggestions ─┐
      │                                    │                                 │
      │  game.Reduce (0 tokens, NEW)       └────► leaderboard ──► Records ───┤
      ▼                                                                      │
 game.State  (species, level, stats, dex, journal, barn, encounters)        │
      │                                                                      ▼
      └────────────────────────────► daemon.Snapshot (extended) ──► TUI (new tabs)
```

New packages:

| Package | Responsibility | Tokens |
|---|---|---|
| `internal/content` | `go:embed` species/moves/lore-lines/sprites as validated Go tables; generated from the design docs by `go generate` | 0 |
| `internal/game` | the deterministic reducer: `Reduce(prev State, newEvents []Event) State`; stat mapping; XP/care/bloat; evolution | 0 |
| `internal/game/encounter` | predicate rules (mirror of `advisor.Rule`) that emit encounters from event windows | 0 |
| `internal/game/battle` | local, deterministic battle simulator over a battle card | 0 |
| `internal/codeling` | `.codeling` export/import (versioned), **with untrusted-import hardening** | 0 |

`internal/tui` gains **Pet / Dex / Journal** tabs; the existing
**Overview / Suggestions / Records** tabs stay (recast as "how you care for your
Codeling"). The mood face is replaced by the active species sprite, which visibly
dims under token bloat.

---

## 4. Determinism & incremental reduction (the core engineering constraint)

Game state **must be a reproducible fold over the event log**, matching the
store's existing "append-only, aggregate on load" philosophy. Two hard rules:

1. **No wall-clock, no un-seeded randomness in the reducer.** Every "roll"
   (e.g. the Lucent cosmetic chance, encounter selection) is seeded from a hash
   of the *triggering event's* `Key`. Re-running `Reduce` over the same events
   yields byte-identical state. This is what makes it testable and cache-cheap.

2. **Incremental, not full-recompute.** The daemon already calls `st.All()` each
   cycle; folding *every* event through the game engine every 2 minutes is O(all
   events) and grows unbounded. Instead:
   - persist `game.json` with a **high-water mark** (last processed event `Key` +
     `Timestamp`);
   - each cycle, `Reduce` only consumes events strictly after the mark, in
     timestamp order;
   - invariant, enforced by test: `Reduce(fold-all) == fold(Reduce, per-event)`.

   **Edge case — late/out-of-order events.** Session logs can be written after
   the fact. Mitigation: process a **replay window** — re-fold events from
   `mark.Timestamp - W` (W = 24h) but skip any whose `Key` is already in the
   processed set (reuse the store's dedupe idea). Bounds work at O(recent) while
   tolerating late arrivals. Documented and tested.

State shape (persisted to `~/.aipet/game.json`, atomic write like the snapshot):

```go
type State struct {
    Version    int                 // save-format version, for migrations
    Keeper     Keeper              // rank, first-seen, title
    Active     *Pet                // current companion (nil until first hatch)
    Barn       []Pet               // stored / imported pets
    Dex        map[string]DexEntry // speciesID -> {seen, caught, firstAt}
    Journal    []JournalLine       // append-only, capped (ring buffer, e.g. 500)
    Encounters []Encounter         // pending/wild, resolved by user action
    Mark       Watermark           // {LastKey, LastTS} incremental cursor
    Processed  []string            // small recent-key window for replay dedupe
}

type Pet struct {
    SpeciesID string
    Nickname  string   // user- or import-supplied → SANITIZE on import (§9)
    Level     int
    XP        int
    Stats     Stats5   // VIGOR/FOCUS/WIT/GRIT/SPARK, clamped to legal bands
    Bloat     float64  // 0..1 token-bloat meter; dims sprite, softens XP
    Lucent    bool     // cosmetic; seeded roll at hatch
    BornAt    time.Time
    Journal   []string // per-pet life events (hatched, evolved…)
}
```

---

## 5. Deterministic game logic (all pure, all 0-token)

### 5.1 Stat mapping (usage → the five fixed stats)

Derived per evaluation window from existing aggregates — **no new data source**:

| Stat | Meaning (SPEC) | Formula (over window) |
|---|---|---|
| **VIGOR** | activity volume | scaled `log(turns + tokensOut)` |
| **FOCUS** | cache-read ratio | `CacheRead / (Input + CacheRead)` |
| **WIT** | model-routing quality | inverse of advisor "opus-overuse"/"unknown-model" signals |
| **GRIT** | streaks | `leaderboard.Records.CurrentStreak` normalized vs longest |
| **SPARK** | rare events | distinct models × projects + rare-encounter count |

A pet's **effective stats interpolate toward its species base stats** (from
`species.md`) as it levels, then are **scaled down by `Bloat`** — so a neglected
(bloated) pet visibly weakens and recovers with better habits, never dies. This
directly realizes the "feed the mind, not the meter" thesis using signals the
advisor already computes.

### 5.2 XP & care (quality, not volume)

XP per cycle rewards **care quality**, computed by *inverting* the advisor:
few/no `Warn`+`Tip` suggestions ⇒ high care ⇒ more XP; bloat accrues from
sustained warnings and decays with clean days. This reuses `advisor.Run` output
verbatim — no parallel logic, no extra tokens.

### 5.3 Species assignment & evolution

- **Starter (first hatch):** classify the first 3 active days by dominant stat →
  GRIT⇒Ember (`cindling`), FOCUS⇒Stream (`rivulet`), SPARK⇒Vector (`glyphit`).
- **Wild encounters:** each species' encounter hook in `species.md` becomes a
  predicate `EncounterRule(GameInputs) []Encounter` — same pattern as
  `advisor.DefaultRules()`. Examples already specified: `hoardlet` (5× cached-
  prefix reuse in a row), `nightproc` (post-midnight session), `everfile`
  (MYTHIC: one session spanning the whole repo), `uptimewyrm` (MYTHIC: 365-day
  streak). Mythics are **event-gated, not RNG** — provably rare by construction.
- **Evolution:** deterministic thresholds (level + dominant-stat branch held),
  exactly as `species.md` specifies (e.g. `cindling→forgeon` at L12 GRIT-dominant).

### 5.4 Battles (local, serverless)

A **battle card** (species, level, stats, moves, DNA hash) is exported per
`GAME_DESIGN.md`. `internal/game/battle` runs a deterministic turn simulation
seeded from both cards' DNA hashes — reproducible, offline, 0 tokens. Two local
cards → one replayable result.

---

## 6. Content pipeline (docs → embedded data, 0 runtime cost)

Design docs stay the **source of truth**; code consumes **derived, validated
data**:

```
docs/design/{species,moves,rarity,lore}.md ──go generate──► content/*.json ──go:embed──► internal/content
                                             (validator)       (checked-in)
```

- The generator parses the tables (species stats, the 56 voice lines, moves) into
  JSON, **failing the build** on any SPEC violation (BST out of band, line > 90
  chars, unknown type, duplicate Dex #). This turns the docs' hand-run "Quality
  Loop" audits into an enforced CI gate.
- `design/handoff/sprites.json` is the existing seed for sprite embedding.
- Result: all lore/flavor is static, embedded, offline — **no generation at
  runtime**, so voice lines cost nothing.

---

## 7. TUI integration

Extend the existing Bubble Tea model (currently tabs 0–2):

| Tab | Was | Becomes |
|---|---|---|
| Pet *(new, default)* | — | active species sprite (dims with bloat), name/level/stats, today's journal line |
| Overview | cost overview | unchanged, framed as "your Codeling's diet" |
| Suggestions | advisor tips | unchanged, framed as "care tips" |
| Records | leaderboard | unchanged + keeper rank/title |
| Dex *(new)* | — | species seen/caught across the 6 regions |
| Journal *(new)* | — | append-only life log (hatched, evolved, encounters) |

The mood system (`moodHappy/Thinking/Worried`) generalizes to bloat state and
drives which sprite frame renders. Non-interactive commands mirror the split:
`aipet pet`, `aipet dex`, `aipet journal`, plus existing `status`/`leaderboard`.

---

## 8. CLI surface

```
aipet                 TUI, default tab = Pet
aipet pet [status]    print active pet (species, level, stats, bloat)
aipet dex [--json]    Dex progress
aipet journal         recent journal lines
aipet trade export <file.codeling>     write a portable pet
aipet trade import <file.codeling>     import (VALIDATED, see §9)
aipet battle <a.card> <b.card>         local battle
# unchanged: daemon, status, leaderboard, config, version
```

---

## 9. Security — the new untrusted surface

The security audit's core stance: **content from outside the trust boundary is
hostile.** Today that's session logs. The game adds one new one:

> **`.codeling` / battle-card import files are UNTRUSTED.** They come from other
> keepers and must be treated exactly like session-log content.

Import hardening (all in `internal/codeling`, gated by tests + a fuzz target):

1. **Bounded read** — cap file size and field lengths before parsing.
2. **Schema + version check** — reject unknown save versions; no silent coercion.
3. **Whitelist species IDs** against the embedded Dex; reject unknown IDs.
4. **Clamp all stats** to the legal BST band for the claimed rarity — a hostile
   file cannot inject a 9999-stat pet.
5. **Sanitize free text** (nickname, journal) via the existing
   `collector.sanitizeField` against terminal-escape injection before it ever
   reaches the TUI.
6. **No path/host fields honored** — the format is pure data; import never
   touches the filesystem outside `~/.aipet` or opens a socket.

Trusted, unchanged: `~/.aipet/*` is same-user and trusted; `game.json` is ours.

---

## 10. Optional on-device flavor (default OFF)

To satisfy "almost no tokens" *without* breaking "zero network": an **opt-in**
generator can produce fresh journal lines from a **local** model (Ollama /
llama.cpp via localhost). Constraints, enforced:

- Default **disabled**; enabling requires an explicit `config` flag.
- Targets **localhost only**; if the local runtime is absent, silently falls back
  to the embedded static library (§6) — the game never degrades.
- **Never** an Anthropic/API call: **$0 API tokens** in every configuration.
- Gameplay (species/stats/XP/battles) **never** depends on it — flavor only.

This is the only "AI" in the loop and it is optional, local, and free.

---

## 11. Phasing (each phase independently shippable, gated by tests + audit)

| Phase | Deliverable | Gate |
|---|---|---|
| **0** | ~~Fix module path~~ (done — now `github.com/rohithIlluri/POC-S/pocs/aipet`, `go install` works); content pipeline (`go generate` + validator + embed) | build fails on SPEC violation |
| **1** | `internal/game` reducer skeleton + stat mapping + `game.json` persistence + `aipet pet status` (headless) | determinism + incremental-equivalence tests |
| **2** | Species assignment, XP/care, bloat, evolution | evolution threshold tests; bloat recovery test |
| **3** | Encounter rules + Dex + journal | mythic gating tests (no-RNG) |
| **4** | TUI Pet/Dex/Journal tabs; sprite-by-bloat | tui render tests (currently thin — raise coverage) |
| **5** | `.codeling` trade + local battles | **fuzz import**; clamp/sanitize tests |
| **6** | Optional local-LLM flavor (opt-in) | zero-API-token assertion; localhost-only test |

Ship Phases 0–4 as the merged v2 ("your usage raises a Codeling"); 5–6 are
additive.

---

## 12. Design iteration log (self-review passes to reach "prod ready")

- **Pass 1 — token audit.** Enumerated every backend op (§1 table); confirmed all
  are pure folds over on-disk data. Result: backend is provably 0 API tokens.
  The one LLM temptation (dynamic flavor) was quarantined to §10: opt-in, local,
  never gating gameplay.
- **Pass 2 — scaling.** Caught that folding the full event log every cycle is
  O(all events) and unbounded. Added incremental reduction with a high-water mark
  (§4) and the `full == incremental` invariant as a required test.
- **Pass 3 — determinism.** Caught that naive `rand`/wall-clock in species/Lucent
  rolls would make pets non-reproducible and untestable. Required all randomness
  to be seeded from triggering-event hashes; state is now a reproducible fold.
- **Pass 4 — late events.** Incremental + strict watermark would *drop* session
  logs written after the fact. Added the 24h replay window + processed-key dedupe
  (§4). Bounded work, no lost events.
- **Pass 5 — security.** Recognized `.codeling` import as a brand-new untrusted
  surface (the audit's whole ethos). Added §9: bounded reads, species whitelist,
  stat clamping, escape sanitization, fuzz gate. Without this a hostile pet file
  could inject illegal stats or terminal escapes.
- **Pass 6 — content integrity.** Turned the docs' manually-run "Quality Loop"
  (line length, BST bands, dup Dex #) into a build-failing validator (§6), so the
  data can't drift from SPEC silently.
- **Pass 7 — no regressions.** Confirmed the merge is *additive*: existing
  packages/tests untouched; the game reads the same `[]store.Event`; the zero-
  network property is preserved (add a CI check that no non-test file imports
  `net`/`net/http`).

### Prod-ready acceptance checklist

- [ ] `go build ./... && go vet ./...` clean; **all existing tests still pass**.
- [ ] Determinism test: `Reduce` twice over same events ⇒ identical `State`.
- [ ] Incremental == full-recompute equivalence test.
- [ ] Late/out-of-order event test (replay window).
- [ ] Mythic encounters are event-gated (no RNG path can produce them).
- [ ] Bloat rises under sustained advisor warnings and recovers on clean days.
- [ ] `.codeling` fuzz import: no panic; illegal stats clamped; escapes stripped.
- [ ] CI assertion: **no `net`/`net/http` import** outside tests (zero-network).
- [ ] CI assertion: game engine makes **no Anthropic/API call** (0 API tokens).
- [ ] Save-format `Version` migration: missing/older `game.json` ⇒ safe fresh/upgrade.
- [ ] Cross-platform: Windows signal path (`signal_windows.go`) unaffected.
- [ ] Module path corrected; `go install` works from the real repo path.

When every box is checked, the merged binary is a single fully-local companion
that raises a Codeling from your real coding activity — **at zero API-token cost
for all backend work.**
```
