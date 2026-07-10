# aipet → Codelings: a pocket-monster game for developers

**Pivot:** aipet stops being a plain AI-spend dashboard and becomes a
local, terminal-native creature-raising game. Your real development activity —
Claude Code and Codex sessions the collectors already parse — is the game's
energy source. No backend, no servers, no accounts. The repo's collector/store
engine stays; everything dashboard-only goes.

> **IP note:** "like Pokémon" means the *genre* (creature collecting, raising,
> evolving, trading, battling). All names, species, art, and lore below are
> original. Never ship Pokémon names, sprites, or trade dress.

---

## 1. Vision & pillars

A creature lives in your terminal. It hatches from an egg, feeds on the tokens
you already burn, grows with your habits, and evolves based on *how* you work —
not how much you pay. Good engineering hygiene raises a healthy, rare creature.

**Pillars**

1. **Zero backend.** Everything derives from local files. The only remote
   surface is GitHub (releases for distribution, gists/files for trading).
2. **Real play, no grind.** You never play *instead of* working. Working *is*
   playing. The game reads logs; it never asks you to do anything artificial.
3. **Zero tokens, zero cost.** The game never calls a model. All simulation is
   deterministic local computation.
4. **Craftsmanship is the meta.** The old advisor rules (cache reuse, model
   routing, context hygiene) become *care mechanics*. An efficient developer
   raises a stronger creature — the spend-coaching DNA survives as gameplay.

---

## 2. Backend teardown (what gets removed)

| Component | Action |
|---|---|
| `internal/feed` (client, ed25519 verify, version check) | **Delete** |
| `cmd/aipet-feedsign` + `bin/aipet-feedsign` | **Delete** |
| `feed/sample-feed.json` | **Delete** |
| Daemon feed polling + `feedcache` | **Strip** — daemon keeps only the collect loop + snapshot |
| `aipet update` self-update command | **Delete** (distribution moves to brew/go install) |
| Config: `feed_url`, `feed_public_key`, `poll_interval_min` | **Remove** (add `collect_interval_min`) |
| TUI "Market" tab, `FeedOK`/`FeedError` footer | **Remove** — replaced by game tabs |
| Advisor suggestions with `Source == "feed"` | **Remove** the source; local rules stay |
| Pricing feed overrides | **Remove**; bundled `internal/pricing` table stays (it prices "food") |

**What stays untouched:** `internal/collector` (the game's sensory organ),
`internal/store` (append-only event log + stats), `internal/pricing`,
the daemon's snapshot mechanism, the Bubble Tea TUI shell.

**What transforms:** `internal/advisor` → `internal/care` (rules become health
effects), `internal/tui` → game screens.

---

## 3. Lore

### The world: the Shellwoods

Beneath every filesystem lies the **Shellwoods** — a forest that grows in the
warm exhaust of computation. Long ago, when the first mainframes ran hot
through the night, stray cycles condensed in the dark and became living things:
**Codelings**, small creatures woven from discarded tokens and cache echoes.

Codelings feed on **tokens** — the crumbs of thought that fall when a developer
converses with a machine. But like all foragers, they thrive on *quality*, not
volume. A Codeling raised on cached, well-routed, tightly-scoped work grows
bright-eyed and quick. One gorged on bloated contexts and needlessly expensive
calls grows sluggish and dim — the Shellwoods call this **token bloat**.

### The Daemonkeepers

Developers who bond with a Codeling are called **Daemonkeepers** — because the
creature literally lives in a daemon. The old Keepers' proverb is the game's
thesis: *"Feed the mind, not the meter."*

### The species lines (launch set: 9 species, 3 lines × 3 stages)

Species are original; each line keys off a *playstyle* the store can measure.

| Line | Egg hatches when… | Stage 1 | Stage 2 | Stage 3 | Temperament |
|---|---|---|---|---|---|
| **Ember line** (deep work) | long, focused sessions dominate | **Cindling** — a coal-colored salamander that naps in warm CPUs | **Forgeon** | **Pyrolith** | slow, powerful, hates fragmentation |
| **Stream line** (fast iteration) | many short, cheap, cache-heavy turns | **Rivulet** — a ribbon-fish of scrolling text | **Cascada** | **Torrentide** | quick, cheerful, loves cache hits |
| **Vector line** (breadth) | many projects & models touched | **Glyphit** — a moth whose wings are diff hunks | **Polyglyph** | **Omniglyph** | curious, flighty, bonuses for variety |

Rare variants ("**Shinies**" analog → "**Lucents**") roll from the DNA seed at
hatch, ~1/512, purely cosmetic (alternate palette).

### Types (combat/flavor affinities)

Six original types drawn from dev life, with a rock-paper-scissors wheel:

`Cache → Context → Runtime → Syntax → Stream → Daemon → Cache`

Each species has one type; moves have types; effectiveness is a simple 2×/1×/½×
table. Types are flavor-first — battles are a minigame, not the core.

---

## 4. Game design

### 4.1 The core loop

```
you work with Claude Code / Codex
        │  (collector parses logs — already built)
        ▼
usage events → store → daily digest
        ▼
pet simulation tick (deterministic):
  food eaten, XP gained, health/mood updated,
  evolution & encounter checks
        ▼
TUI: pet reacts, stats move, events land in the journal
```

### 4.2 Feeding & health (the advisor, reborn)

Tokens are food. The old advisor rules become nutrition:

| Old advisor rule | New care mechanic |
|---|---|
| Low cache reuse | **Junk food.** Uncached re-sent prompts digest poorly → health slowly drops, pet looks tired |
| Opus overuse | **Rich food.** Fine sometimes; a diet of it makes the pet lethargic (XP multiplier drops) |
| Context bloat | **Overeating.** Huge average contexts → "token bloat" status until habits improve |
| Session fragmentation | **Interrupted naps.** Many cold starts → mood penalty |
| Budget pressure | **Foraging limit.** Past the soft daily budget the pet is "full" — extra tokens give zero XP (spend coaching, gamified) |
| Healthy usage | **Balanced diet.** Full XP, mood up, evolution progress |

Health/mood are shown, explained, and always actionable — the "explainable
advice" property survives: the pet's journal says *why* it feels how it feels
("I ate 40k uncached tokens today… warm the cache and I'll perk up!").

### 4.3 Stats (grown from real behavior)

Five stats, each mapped to a signal `store.Stats` already tracks or trivially can:

- **Vigor** — total healthy activity (turns, sessions)
- **Focus** — cache-read ratio (CacheRead vs Input)
- **Wit** — model-routing quality (right-sized model for the job)
- **Grit** — streaks (consecutive active days)
- **Spark** — rare events (first use of a new model, late-night save, new project)

Plus hidden **IVs** rolled from DNA at hatch (small permanent multipliers) —
two pets with the same history still differ.

### 4.4 Progression

- **Egg** (day 0): hatches after ~3 active days; the dominant playstyle in the
  hatch window picks the species line.
- **Stage 1 → 2** at level 12, **2 → 3** at level 30. Level = f(XP), XP from
  the daily digest with diet multipliers.
- **Branching:** stage-3 evolution rolls a variant if one stat is dominant
  (e.g., Focus-dominant Rivulet line evolves into a Lucent-eligible form).
- **Neglect decays mood, never kills.** No dead pets on vacation — the pet
  "hibernates" after 7 idle days and wakes happy.

### 4.5 Encounters & the Dex

Wild Codelings appear as **encounters** triggered by real events: a new
project directory, a new model in the logs, an unusually efficient day.
Encounters are catch-by-doing: "A wild Glyphit appeared! It'll join you if you
finish today with >50% cache reuse." The **Dex** tab tracks species seen/caught.
Launch roster: **30 species** — the 9 canon starters (3 lines × 3 stages) plus
21 original species spread across all 6 types, all 5 rarity tiers (COMMON
through RELIC), and all 6 habitats, plus exactly 2 MYTHIC species that are
always encounter-only. Full roster defined in `docs/design/species.md`.

### 4.6 Battles (deterministic, serverless)

Two Daemonkeepers battle with **no server and no live connection**:

1. Each exports a **battle card** (`aipet export --battle`) — a compact,
   self-contained pet snapshot (species, level, stats, moves, DNA hash).
2. Either player runs `aipet battle <card-file-or-paste>`.
3. The sim seeds its RNG with `SHA256(sort(dnaA, dnaB) + UTC-date)` — both
   machines replay the identical battle, same result, turn-by-turn in the TUI.

Cheating is possible (it's local files) — the design accepts this: battles are
for fun between colleagues, not ranked ladders. The DNA hash makes *casual*
tampering evident (a card that doesn't re-derive is flagged "counterfeit").

### 4.7 Trading

`aipet trade export` writes a `.codeling` file (JSON, self-describing,
versioned). Share it over Slack, gist, a repo — anything. `aipet trade import`
adopts it. A traded pet keeps its history summary and gains a "traveler" badge
(small XP bonus, Pokémon-style traded-pet flavor). One active pet at a time;
the **Barn** holds the rest.

---

## 5. Architecture

### 5.1 Package layout (after pivot)

```
cmd/aipet/               CLI: play (TUI) | daemon | status | dex | trade | battle | config
internal/collector/      unchanged — parses Claude/Codex logs
internal/store/          unchanged — append-only events + stats
internal/pricing/        unchanged — prices "food" (bundled table only)
internal/care/           was advisor — diet/health rules (pure functions)
internal/sim/            NEW — deterministic pet simulation:
                           dna.go      seed, IVs, species roll, Lucent roll
                           tick.go     daily digest → XP/health/mood deltas
                           evolve.go   level & evolution rules
                           battle.go   seeded battle resolver
internal/species/        NEW — species table, types chart, movesets (embedded data)
internal/save/           NEW — pet save files: ~/.aipet/pet.json, barn/, journal.jsonl
internal/daemon/         slimmed — collect loop + sim tick + snapshot (no feed)
internal/tui/            game screens: Pet | Journal | Dex | Barn
```

### 5.2 Determinism rule (the one architectural law)

`internal/sim` must be a **pure function of (save state, usage digest, seed)**.
No wall-clock reads inside the sim, no map-iteration-order dependence, no
floats in battle math (fixed-point ints). This is what makes serverless battles
and replayable saves possible, and it makes the whole game unit-testable the
way the advisor already is.

### 5.3 Data model

```jsonc
// ~/.aipet/pet.json (atomic writes, like the existing snapshot)
{
  "save_version": 1,
  "dna": "b64…",              // 32B seed rolled at egg creation
  "species": "rivulet",
  "stage": 2, "level": 17, "xp": 4210,
  "stats": {"vigor": 41, "focus": 63, "wit": 28, "grit": 12, "spark": 7},
  "health": 86, "mood": "cheerful",
  "statuses": ["token_bloat"],
  "badges": ["traveler"],
  "hatched_at": "2026-07-01T…", "last_tick_day": "2026-07-07"
}
```

- `journal.jsonl` — append-only life events (hatched, evolved, ate junk food,
  encounter, battle result). Powers the Journal tab; same pattern as `usage.db`.
- `barn/*.codeling` — inactive/imported pets.
- The `.codeling` interchange format = `pet.json` + `format_version` +
  provenance block. Versioned from day one so trades survive upgrades.

### 5.4 The daemon

Keeps its current shape (pidfile, atomic snapshot, signals) minus the feed:
every N minutes it collects new events, runs at most one **sim tick per
calendar day** (catch-up ticks if the machine was off), and publishes the
snapshot the TUI polls. The TUI stays read-only and responsive, exactly as now.

---

## 6. Distribution

Zero-server distribution, developer-native channels:

1. **GitHub Releases via goreleaser** — cross-compiled static binaries
   (darwin/linux/windows × amd64/arm64), SHA-256 checksums, changelog.
   Single `go` binary, no CGO — this is the artifact everything else wraps.
2. **Homebrew tap** — `brew install <org>/tap/aipet` (goreleaser generates the
   formula). Primary channel for the target audience.
3. **`go install github.com/<org>/aipet/cmd/aipet@latest`** — free with the repo.
4. **curl installer** — `curl -fsSL …/install.sh | sh` for READMEs and demos.
5. Later, for reach: `npx aipet` wrapper and an `mise`/`asdf` plugin.

Self-update is gone with the feed; `aipet version` just compares against the
GitHub Releases API (unauthenticated, cached, fully optional) and prints
"new version available — brew upgrade aipet".

**Community without infra:** a public **`codeling-dex`** GitHub repo — species
art (ASCII), lore pages, and a `FOUND.md` where players PR their rare catches.
Discussions/issues host trades and battle cards. GitHub *is* the backend.

## 7. Infrastructure

| Concern | Answer |
|---|---|
| Servers | **None.** |
| Accounts/auth | **None.** Identity = your pet file. |
| Telemetry | **None.** Keeps the original privacy story intact. |
| Network calls | Optional GitHub Releases version check only. Offline-first always. |
| CI | GitHub Actions: `make test && make vet`, goreleaser on tag. |
| Data at rest | `~/.aipet/` only, 0600/0700 perms as today. |
| Cost to operate | $0 — GitHub free tier covers releases, dex repo, CI. |

---

## 8. Roadmap

**M0 — Teardown (small):** delete feed/feedsign/update, slim daemon & config,
remove Market tab. All existing tests green. *The repo is a clean local core.*

**M1 — A pet is born:** `internal/sim` (DNA, tick, XP, levels), `internal/save`,
egg→hatch→stage-2 for the three lines, Pet tab with per-species ASCII faces and
mood animation. *Playable tamagotchi.*

**M2 — Care & journal:** advisor→`internal/care`, diet/health/statuses,
journal.jsonl + Journal tab, foraging limit (budget mechanic). *The
efficiency-coaching soul returns as gameplay.*

**M3 — Dex & encounters:** species/type tables, encounter triggers,
catch-by-doing, Dex tab, Lucents. *Collection loop.*

**M4 — Trade & battle:** `.codeling` format, export/import, Barn, seeded
battle resolver + battle TUI. *Social loop, still serverless.*

**M5 — Ship it:** goreleaser, brew tap, install script, README rewrite,
`codeling-dex` community repo. *Distribution.*

Each milestone keeps the bar the POC already set: deterministic pure-function
cores with unit tests (`sim` and `care` should be as test-dense as `advisor`
and `feed` are today), atomic file writes, no network in the hot path.
