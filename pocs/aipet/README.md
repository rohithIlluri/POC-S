# aipet — your local AI-usage companion

A small, terminal-native **pet that lives on your machine** and helps you get
the most out of your AI coding tools — **Claude Code** and **Codex** — without
ever sending your data anywhere.

It watches how you use these tools (from the session logs they *already write
to disk*) and:

- 🪙 **coaches you to spend fewer tokens** — flags Opus overuse, low cache reuse, context bloat
- ⚡ **improves efficiency** — model-routing tips, session hygiene, prompt caching
- 🏆 **keeps score** — a local leaderboard of your top projects, models, best cache-reuse days, and streaks
- 🐣 **raises a Codeling** — a real pocket-monster-style companion that hatches
  from an egg after 3 active days, grows stats from how you actually work
  (cache reuse, model routing, streaks, variety), and evolves — the same
  advisor rules that produce Suggestions double as its diet
  (see [`docs/GAME_DESIGN.md`](docs/GAME_DESIGN.md) and [`docs/design/`](docs/design/) for the full 30-species Dex, economy, and lore)

## Why it's safe

- **Entirely local.** It reads the session logs Claude Code and Codex *already
  write to disk* (`~/.claude/projects`, `~/.codex/sessions`). No proxy, no
  interception, no code or prompts ever leave the machine.
- **Zero network surface.** There is no outbound code at all — nothing to
  configure, nothing to trust, nothing to leak. Usage stays in `~/.aipet`.
- **Zero token cost to run.** Token counts are already in those logs, so
  attributing spend and generating advice costs **nothing** — the companion
  never calls a model.
- **Hardened against hostile logs.** Session-log content is treated as
  untrusted (it's written by other tools and prompt-injectable agents), so
  fields are sanitized against terminal-escape injection before display. See
  [`docs/SECURITY_AUDIT.md`](docs/SECURITY_AUDIT.md).

## Architecture

```
Claude Code / Codex                ~/.aipet/
  session logs (on disk)             usage.db       append-only event log
        │                            snapshot.json  daemon → TUI state
        │                            config.json    local settings
        │                            scanstate.json skip-unchanged fingerprints
        │                            pet.json       the Codeling's save
        ▼                            journal.jsonl  pet's life log
  ┌───────────┐   collect    ┌──────────┐  advise   ┌──────────┐
  │ collector │ ───────────▶ │  store   │ ────────▶ │ advisor  │
  └───────────┘  (0 tokens)  └──────────┘           └──────────┘
        ▲                          │                      │
        │                          ▼                      ▼
  ┌───────────┐             ┌──────────────┐        ┌──────────┐
  │  daemon   │ ──────────▶ │ leaderboard  │        │   TUI    │ ← the "pet"
  └───────────┘   │ snap    └──────────────┘        └──────────┘
                   │
                   ▼  digest ──▶ care (diet) ──▶ sim (tick/evolve) ──▶ save
```

- **`internal/collector`** — parses Claude Code / Codex session logs into normalized usage events (no network, no LLM), sanitizing untrusted fields.
- **`internal/pricing`** — bundled per-model rates.
- **`internal/store`** — append-only JSONL event log with idempotent dedupe (no external DB).
- **`internal/advisor`** — explainable rules that turn usage into money-saving suggestions.
- **`internal/leaderboard`** — rankings and personal records, computed on-device.
- **`internal/species`** — the embedded 30-species Codelings Dex (stats, evolution rules, sprites, flavor).
- **`internal/care`** — the advisor's rules, reborn as diet verdicts (junk food, rich food, balanced…) that drive the pet's health/XP.
- **`internal/sim`** — the deterministic pet simulation: DNA/IVs, daily tick, leveling, evolution. A pure function of (pet, digest, seed) — no wall clock, no floats, fully replayable.
- **`internal/save`** — atomic `pet.json` + append-only `journal.jsonl`.
- **`internal/daemon`** — background collect loop; runs at most one pet tick per calendar day (with catch-up for missed days) and publishes an atomic snapshot.
- **`internal/tui`** — the Bubble Tea pet (Pet / Overview / Suggestions / Records).

## Install

One command (needs Go 1.25+):

```bash
go install github.com/rohithIlluri/POC-S/pocs/aipet/cmd/aipet@latest
```

Then just run `aipet` — the first launch collects your existing session logs
and opens the pet. No config, no accounts, no network.

Or download a binary from the [latest release](https://github.com/rohithIlluri/POC-S/releases/latest)
(darwin/linux/windows × amd64/arm64, with SHA-256 `checksums.txt`), `chmod +x`,
and put it on your `PATH`.

Or build from source — this POC lives under `pocs/aipet/`:

```bash
cd pocs/aipet
make build      # builds ./bin/aipet
make install    # installs to $GOBIN
make release    # cross-platform binaries + checksums into ./bin/release
```

## Quick start

```bash
aipet status         # one-shot collect + summary (great first run)
aipet                # launch the interactive pet (TUI)
aipet leaderboard    # rankings + personal records (add --json for scripts)
aipet daemon         # run the background watcher
```

The TUI has four tabs — **Pet** (your Codeling: egg or hatchling, level, health,
stats, recent journal), **Overview** (spend, budget bar, top models/projects),
**Suggestions** (efficiency advice with estimated savings), and **Records** (the
local leaderboard). Navigate with `tab`/`←→` or `1`–`4`; `q` quits.

### Your Codeling

An egg starts warming the first time the daemon (or `aipet status`) runs. After
3 active days, it hatches — which of the three starter lines it picks depends
on how those days looked:

- **Ember** (long, focused sessions) → Cindling → Forgeon → Pyrolith
- **Stream** (fast, cache-heavy iteration) → Rivulet → Cascada → Torrentide
- **Vector** (breadth across projects/models) → Glyphit → Polyglyph → Omniglyph

From there it grows daily: the same signals the advisor already coaches
(cache reuse, model routing, session hygiene, budget discipline) become its
diet — a healthy day is a full-XP "balanced diet," a low-cache-reuse day is
"junk food," blowing past budget caps XP for the day at zero. Evolution needs
both a level (12, then 30) and the right dominant stat, so it's earned by
habit, not by grinding. Neglect never punishes: an idle pet's mood just fades,
and after 7 days it quietly hibernates — waking up happy, with zero guilt,
whenever you're back. The full 30-species Dex, rarity tiers, and battle system
are designed in [`docs/design/`](docs/design/) for a future release.

## Configuration

```bash
aipet config                              # show current settings
aipet config daily_budget_usd 15          # soft per-day guidance budget
aipet config collect_interval_min 5       # how often the daemon re-scans logs
```

Config lives at `~/.aipet/config.json`. There is nothing to configure for it to
work — sensible local defaults apply out of the box.

## Suggestions the advisor produces

| Rule            | Fires when…                                            |
|-----------------|--------------------------------------------------------|
| Budget          | today's spend nears or passes the soft daily budget    |
| Opus overuse    | the priciest model dominates spend (estimates savings) |
| Low cache reuse | large prompts are re-sent without cache hits           |
| Context bloat   | average turn carries a very large context window       |
| Unknown model   | a turn's model has no known price (spend under-counted)|
| Fragmentation   | many short sessions pay repeated cold-start costs      |

All advice is explainable: each suggestion states what was observed, why it
costs money, and the specific action to take.

## The leaderboard

`aipet leaderboard` (aliases `board`, `lb`) ranks your top projects and models
by lifetime spend, your best cache-reuse days (volume-gated so a lucky quiet day
can't top the board), and your personal records — biggest day, busiest day,
current and longest activity streaks. Pass `--json` for a machine-readable dump.
Everything is computed locally from your own event log.

## Development

```bash
make test     # unit tests (race-clean)
make vet      # go vet
make fmt      # gofmt
```

## Status

**v1.0.0.** The collectors, store, advisor, leaderboard, daemon, and TUI are
fully functional against real Claude Code data, fully local, with a completed
[security audit](docs/SECURITY_AUDIT.md). The next chapter is *Codelings* — the
game layer designed in [`docs/GAME_DESIGN.md`](docs/GAME_DESIGN.md) and the
[`docs/design/`](docs/design/) content bible.
