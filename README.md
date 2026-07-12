# POC-S

A collection of proof-of-concept projects. Each POC is self-contained in its own directory under `pocs/` with its own README, dependencies, tests, and a path-scoped CI workflow — nothing at the repo root belongs to any single POC.

## POCs

| POC | Description | Status |
|---|---|---|
| [`pocs/ccr`](pocs/ccr) | Multi coding tools router — classifies a coding prompt by task type/complexity and runs it with Claude Code or Codex on a matched model | ✅ Active |
| [`pocs/aipet`](pocs/aipet) | Local AI-usage companion — a terminal "pet" that reads Claude Code / Codex session logs on-device to coach token spend, rank a local leaderboard, and seed the *Codelings* game. Fully local, zero network surface | ✅ Active (v1.0.0) |
| [`pocs/pitstop`](pocs/pitstop) | Car maintenance tracker PWA — one dashboard for oil/tires/brakes with mileage-based due-soon alerts, GPS trip logging, and nearby repair shops via OpenStreetMap. Offline-first, zero backend | ✅ Active |

## Conventions

- **One directory per POC** (`pocs/<name>/`), fully self-contained: own `package.json` / `go.mod` (or equivalent), own README with setup/usage, own tests.
- **One branch + one PR per POC**, squash-merged so master history reads as one commit per meaningful change.
- **Path-scoped CI**: each POC gets its own workflow (`.github/workflows/ci-<name>.yml`) filtered on `pocs/<name>/**`, so unrelated POCs never trigger or break each other's builds.
- **Root stays minimal**: this index, shared repo config (`.github/`, `.gitignore`, `LICENSE`), nothing else.
- **Retired POCs** move to `pocs/archive/<name>/` (history stays intact) and get marked 🗄️ in the table instead of being deleted.
