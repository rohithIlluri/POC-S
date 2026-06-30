# aipet — the enterprise AI-spend companion

A local, terminal-native **companion that lives on each developer's machine** and
helps them get the most out of AI coding tools — **Claude Code** and **Codex** —
without anyone worrying about the budget blowing up.

It's a small "pet" that watches how you use these tools and proactively:

- 🪙 **coaches you to spend fewer tokens** — flags Opus overuse, low cache reuse, context bloat
- ⚡ **improves efficiency** — model-routing tips, session hygiene, prompt caching
- 📰 **keeps you current** — market & pricing updates from an enterprise-controlled feed
- 🔄 **self-updates** — pulls new tips, pricing, and release info from a signed feed

## Why it's safe for enterprises

- **Entirely local.** It reads the session logs Claude Code and Codex *already
  write to disk* (`~/.claude/projects`, `~/.codex/sessions`). No proxy, no
  interception, no code or prompts ever leave the machine.
- **Zero token cost to run.** Token counts are already in those logs, so
  attributing spend and generating advice costs **nothing** — the companion never
  calls a model. The only tokens it could ever use ride on the enterprise's
  provider billing, and the analysis itself uses none.
- **No data leakage.** Usage stays in `~/.aipet`. The single optional outbound
  call is to an **enterprise-hosted, signed feed** the admin controls.
- **Tamper-proof updates.** The feed is verified with an ed25519 signature.

## Architecture

```
Claude Code / Codex                ~/.aipet/
  session logs (on disk)             usage.db      append-only event log
        │                            snapshot.json daemon → TUI state
        ▼                            config.json   local settings
  ┌───────────┐   collect    ┌──────────┐  advise   ┌──────────┐
  │ collector │ ───────────▶ │  store   │ ────────▶ │ advisor  │
  └───────────┘  (0 tokens)  └──────────┘           └──────────┘
        ▲                          ▲                      │
        │                          │                      ▼
  ┌───────────┐  signed feed  ┌──────────┐          ┌──────────┐
  │  daemon   │ ◀──────────── │   feed   │          │   TUI    │ ← the "pet"
  └───────────┘  (enterprise) └──────────┘          └──────────┘
```

- **`internal/collector`** — parses Claude Code / Codex session logs into normalized usage events (no network, no LLM).
- **`internal/pricing`** — bundled per-model rates; overridable by the feed.
- **`internal/store`** — append-only JSONL event log with idempotent dedupe (no external DB).
- **`internal/advisor`** — explainable rules that turn usage into money-saving suggestions.
- **`internal/feed`** — enterprise-hosted signed manifest: pricing overrides, market tips, self-update info.
- **`internal/daemon`** — background loop; publishes an atomic snapshot.
- **`internal/tui`** — the Bubble Tea pet (Overview / Suggestions / Market).

## Quick start

```bash
make build

./bin/aipet status      # one-shot collect + summary (great first run)
./bin/aipet             # launch the interactive pet (TUI)
./bin/aipet daemon      # run the background watcher
```

The TUI has three tabs — **Overview** (spend, budget bar, top models/projects),
**Suggestions** (efficiency advice with estimated savings), and **Market** (feed
tips + update notices). Navigate with `tab`/`←→` or `1`/`2`/`3`; `q` quits.

## Configuration

```bash
aipet config                              # show current settings
aipet config daily_budget_usd 15          # soft per-day guidance budget
aipet config feed_url https://feed.corp/aipet.json
aipet config feed_public_key <base64>     # enables signature verification
aipet config poll_interval_min 360
```

Config lives at `~/.aipet/config.json`. With no `feed_url`, the bundled
`feed/sample-feed.json` is used so everything works offline.

## The enterprise feed

An admin publishes a JSON manifest (pricing overrides, tips, update info) at a URL
the company controls, signed with an ed25519 key:

```bash
make build
./bin/aipet-feedsign keygen                                # make a keypair
./bin/aipet-feedsign sign <private-key> feed/sample-feed.json > signed-feed.json
# host signed-feed.json, then on each client:
aipet config feed_url https://feed.corp/aipet.json
aipet config feed_public_key <public-key>
```

Clients verify the signature before trusting any pricing, tip, or update. See
[`feed/sample-feed.json`](feed/sample-feed.json) for the schema.

## Suggestions the advisor produces

| Rule            | Fires when…                                            |
|-----------------|--------------------------------------------------------|
| Budget          | today's spend nears or passes the soft daily budget    |
| Opus overuse    | the priciest model dominates spend (estimates savings) |
| Low cache reuse | large prompts are re-sent without cache hits           |
| Context bloat   | average turn carries a very large context window       |
| Unknown model   | a turn's model has no known price (spend under-counted)|
| Fragmentation   | many short sessions pay repeated cold-start costs      |

All advice is explainable: each suggestion states what was observed, why it costs
money, and the specific action to take.

## Development

```bash
make test     # unit tests (pricing, advisor, collector, feed, tui)
make vet      # go vet
make fmt      # gofmt
```

## Status

This is a proof of concept. The collectors, store, advisor, feed client +
signature verification, daemon, and TUI are fully functional against real
Claude Code data. The `update` command reports new versions but does not yet
self-replace the binary — in production the daemon would download, verify the
SHA-256, and swap atomically.
