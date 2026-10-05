# POC-S

A collection of proof-of-concept projects. Each POC is self-contained in its own directory under `pocs/` with its own README, dependencies, tests, and a path-scoped CI workflow — nothing at the repo root belongs to any single POC.

## POCs

| POC | Description | Status |
|---|---|---|
| [`pocs/ccr`](pocs/ccr) | Multi coding tools router — classifies a coding prompt by task type/complexity and runs it with Claude Code or Codex on a matched model | ✅ Active |
| [`pocs/aipet`](pocs/aipet) | Local AI-usage companion — a terminal "pet" that reads Claude Code / Codex session logs on-device to coach token spend, rank a local leaderboard, and seed the *Codelings* game. Fully local, zero network surface | ✅ Active (v1.0.0) |
| [`pocs/sparkroom`](pocs/sparkroom) | Multiplayer-AI workspace — pitch an idea, it becomes a live room where humans and an AI peer chat and co-write one shared draft, with every edit attributed and undoable. Runs as a Claude.ai Artifact | ✅ Active (v0.1.0) |
| [`pocs/signalhire`](pocs/signalhire) | Application authenticity engine — triages a folder of job applications into genuine effort / mass-generated / needs review using document forensics, layout fingerprinting, hidden-text detection, JD-mirroring and cross-applicant clustering. Assistive only, never auto-reject | ✅ Active (v0.1.0) |
| [`pocs/mindflight`](pocs/mindflight) | Brain-controlled drone sim — a Muse EEG headband (or synthetic stand-in) is decoded into focus / blink / clench intents, run through a safety gate, and flies a simulated DJI-Mini-class drone (built-in sim or ArduPilot SITL). Simulation only | 🧪 New (v0.1.0) |
| [`pocs/dronelink`](pocs/dronelink) | DJI Mini web ground station — ON/OFF (take off / land) and a live camera feed (DJI Fly RTMP → ffmpeg → MJPEG) in a browser simulator; simulated drone today, real-aircraft bridge (DJI Mobile SDK V5) planned | 🧪 New (v0.1.0) |

## Conventions

- **One directory per POC** (`pocs/<name>/`), fully self-contained: own `package.json` / `go.mod` (or equivalent), own README with setup/usage, own tests.
- **One branch + one PR per POC**, squash-merged so master history reads as one commit per meaningful change.
- **Path-scoped CI**: each POC gets its own workflow (`.github/workflows/ci-<name>.yml`) filtered on `pocs/<name>/**`, so unrelated POCs never trigger or break each other's builds.
- **Root stays minimal**: this index, shared repo config (`.github/`, `.gitignore`, `LICENSE`), nothing else.
- **Retired POCs** move to `pocs/archive/<name>/` (history stays intact) and get marked 🗄️ in the table instead of being deleted.
