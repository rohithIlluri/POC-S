# aipet — your local AI-usage companion

A small, terminal-native **pet that lives on your machine** and helps you get
the most out of your AI coding tools — **Claude Code** and **Codex** — without
ever sending your data anywhere.

It watches how you use these tools (from the session logs they *already write
to disk*) and:

- 🪙 **coaches you to spend fewer tokens** — flags Opus overuse, low cache reuse, context bloat
- ⚡ **improves efficiency** — model-routing tips, session hygiene, prompt caching
- 🏆 **keeps score** — a local leaderboard of your top projects, models, best cache-reuse days, and streaks
- 🐣 **is the seed of a game** — the same on-device engine powers *Codelings*, a
  pocket-monster game where your real coding activity raises a companion creature
  (see [`docs/GAME_DESIGN.md`](docs/GAME_DESIGN.md))

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
  session logs (on disk)             usage.db      append-only event log
        │                            snapshot.json daemon → TUI state
        ▼                            config.json   local settings
  ┌───────────┐   collect    ┌──────────┐  advise   ┌──────────┐
  │ collector │ ───────────▶ │  store   │ ────────▶ │ advisor  │
  └───────────┘  (0 tokens)  └──────────┘           └──────────┘
        ▲                          │                      │
        │                          ▼                      ▼
  ┌───────────┐             ┌──────────────┐        ┌──────────┐
  │  daemon   │ ──────────▶ │ leaderboard  │        │   TUI    │ ← the "pet"
  └───────────┘  snapshot   └──────────────┘        └──────────┘
```

- **`internal/collector`** — parses Claude Code / Codex session logs into normalized usage events (no network, no LLM), sanitizing untrusted fields.
- **`internal/pricing`** — bundled per-model rates.
- **`internal/store`** — append-only JSONL event log with idempotent dedupe (no external DB).
- **`internal/advisor`** — explainable rules that turn usage into money-saving suggestions.
- **`internal/leaderboard`** — rankings and personal records, computed on-device.
- **`internal/daemon`** — background collect loop; publishes an atomic snapshot.
- **`internal/tui`** — the Bubble Tea pet (Overview / Suggestions / Records).

## Install

Download a binary from the [latest release](https://github.com/rohithIlluri/POC-S/releases/latest)
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

The TUI has three tabs — **Overview** (spend, budget bar, top models/projects),
**Suggestions** (efficiency advice with estimated savings), and **Records** (the
local leaderboard). Navigate with `tab`/`←→` or `1`/`2`/`3`; `q` quits.

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
