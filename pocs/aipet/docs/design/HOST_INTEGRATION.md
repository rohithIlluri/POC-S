# Host Integration Plan — the pet lives inside Claude Code / Codex

**Status:** design, not yet built
**Scope:** collapse the multi-command CLI into a single install + a single
human-facing command, and move the pet's primary home from a standalone
terminal app into the hosts that generate its food: users type `/aipet`
inside Claude Code or Codex, and the pet is simply *there* — in the status
line, in chat, growing from the session it's living in.

Companion to `INTEGRATION_PLAN.md` (the engine merge, shipped). This plan
changes **where the pet is experienced**, not how it works. `internal/sim`,
`internal/save`, `internal/care`, the event store — all untouched.

---

## 1. Thesis

Today the pet is a destination: you must leave your work, open a second
terminal, run `aipet`, and remember `daemon`, `status`, `dex`, `leaderboard`.
But every gram of the pet's food is produced *inside* Claude Code and Codex
sessions. The pet should live where it eats.

Target experience:

```
go install github.com/rohithIlluri/POC-S/pocs/aipet/cmd/aipet@latest && aipet
```

That second word runs first-time setup automatically. From then on the user
never runs `aipet` in a shell again:

- **`/aipet` in Claude Code** → the pet appears in chat: sprite, mood, level,
  hatch progress, latest journal line — and Claude relays one line in the
  pet's own voice. `/aipet dex`, `/aipet records` for the other views.
- **The status line** of every Claude Code session shows the pet
  persistently: `( ^.^ ) Cindling lv4 · cheerful · $0.42 today`.
- **Growth is automatic**: a Claude Code `Stop` hook runs a silent
  incremental collect after every completed turn. No daemon, no timers, no
  second terminal — the pet ticks *because you coded*.
- **`/aipet` in Codex** → same card, rendered by the Codex agent.

The standalone TUI survives as `aipet tui` for people who want the
full-screen app, but it is no longer the front door.

### Why this wins

| Before (M0–M3 shape) | After (this plan) |
|---|---|
| `aipet daemon` in a second terminal, or remember to open the TUI | zero processes; hooks collect exactly when new data exists |
| 7 subcommands to learn | 1 command ever typed in a shell (`aipet`, once); `/aipet` after that |
| pet visible only when you go look | pet permanently in the status line of the tool it feeds on |
| growth invisible between launches | pet visibly ticks turn-by-turn while you work |

---

## 2. Integration surfaces (what the hosts actually give us)

### Claude Code (all three used)

1. **Custom slash command** — `~/.claude/commands/aipet.md`. Markdown prompt
   with YAML frontmatter; supports `$ARGUMENTS`, restricted tool grants via
   `allowed-tools`, and `` !`cmd` `` pre-execution: the command's stdout is
   injected as context *before* Claude sees the prompt, so the card data is
   deterministic — Claude presents it, it doesn't improvise it.
2. **Status line** — `settings.json` → `"statusLine": {"type": "command",
   "command": "aipet statusline"}`. Runs our binary on each render; receives
   session JSON on stdin; one line of (ANSI-colorable) text out. Must be
   fast: read-only, no collection.
3. **Hooks** — `settings.json` → `"hooks"`. `Stop` fires after each completed
   assistant turn; `SessionStart` fires when a session opens. Both run
   `aipet collect --quiet` so the event log is always current. This **fully
   replaces the daemon** for Claude Code users.

### Codex CLI (one surface)

1. **Custom prompt** — `~/.codex/prompts/aipet.md` → typed as `/aipet`.
   Plain prompt (no pre-execution): it instructs the agent to run
   `aipet card "$ARGUMENTS"` in the shell and present the output. Codex has
   no statusline/hook equivalents, so freshness comes from `aipet card`
   itself running an incremental collect first (it already does — same
   pattern as the TUI's launch collect).

---

## 3. New command surface

Human-facing (the only things a person types):

```
aipet            first run: setup wizard (installs /aipet, statusline, hooks)
                 later runs: print the pet card + "type /aipet in Claude Code"
/aipet [view]    inside Claude Code / Codex — pet | dex | records | overview
aipet tui        the full Bubble Tea app (today's bare `aipet`)
```

Plumbing (installed integrations call these; hidden from `aipet help`):

```
aipet card [view] [--width N]   one-shot plain-text render for chat
aipet statusline                stdin: host session JSON → one line, <20ms
aipet collect [--quiet]         one incremental collect; single-flight lock
aipet setup [--claude|--codex|--remove|--print]
```

Deprecated (kept as aliases for one release, then removed):

- `aipet daemon` → warns "hooks replace the daemon; run `aipet setup`", then
  still works (non-Claude-Code users may want it).
- `aipet status` → alias of `aipet card overview`.
- `aipet dex` / `aipet leaderboard` → aliases of `aipet card dex|records`.
- `aipet config` stays (hidden) — it's rarely needed.

---

## 4. Component design

### 4.1 `aipet card` — the chat renderer (new, core)

Chat is markdown, not an ANSI grid, so the card is a **plain-text render**
of the existing views with the sprite in a fenced code block:

```
( ^.^ )  Cindling · lv 4 · cheerful          stage 1/3 · runtime

    ▴
 (• ◡ •)
 ( ─── )
  ˘   ˘

xp   ██████████░░░░░░░░░░  67
hp   █████████████████░░░  86/100
diet Good mix today — right-sized model, warm cache. Textbook.

streak 3 days · dex 4/30 · today $0.42
```

Implementation: reuse `internal/tui`'s render functions with a `--plain`
lipgloss profile (colors stripped, `NO_COLOR` honored) at a fixed `--width`
(default 60 — safe in chat). Views: `pet` (default), `dex`, `records`,
`overview`. Runs one incremental collect first so it's always current.
Egg state renders the hatch meter + `N/5 sessions` copy. **No new render
logic — this is a projection of existing views.**

### 4.2 `aipet statusline` (new, tiny)

Reads `~/.aipet/snapshot.json` only — **never collects, never scans logs**
(the Stop hook keeps the snapshot fresh; statusline must render in <20ms).
Output, by state:

```
( • ) egg 3/5 · $0.42 today            # pre-hatch
( ^.^ ) Cindling lv4 · cheerful · $0.42 today
( ;_; ) Cindling lv4 · worried · budget over
(no pet yet — run /aipet)              # no snapshot
```

Face reuses `headerFaces`/`eggFaces`; ANSI mood color with plain fallback.

### 4.3 `aipet collect` (expose + harden existing path)

`daemon.Run(cfg)` already is one incremental cycle. New wrapper adds:

- `--quiet` (no output; hooks must be silent),
- **single-flight lock** (`~/.aipet/collect.lock`): concurrent Stop hooks
  from parallel sessions skip instead of queueing,
- **debounce**: skip if the last cycle finished <30s ago (mtime of
  snapshot.json) — parallel sessions and rapid turns don't thrash,
- hard timeout on itself; a hook must never hang a session.

### 4.4 `aipet setup` — the installer (new, the risky one)

Writes three things for Claude Code, one for Codex. **All writes are
merges, never overwrites**; every touched file is backed up to
`~/.aipet/backup/<ts>/` first; `--remove` reverses using recorded state;
`--print` shows the diff without writing.

1. `~/.claude/commands/aipet.md`:

```markdown
---
description: Your Codeling — the pet that grows from your coding sessions
argument-hint: [pet|dex|records|overview]
allowed-tools: Bash(aipet card:*)
---

## Context
- Pet card: !`aipet card "$ARGUMENTS"`

## Your task
Show the pet card above verbatim in a fenced code block. Then add ONE short
line reacting in the pet's voice (match its mood shown on the card — warm,
a little odd, never sycophantic). If the card shows a warning (worried mood,
junk-food diet, budget over), briefly say why in plain words and name the
one habit that would fix it. Nothing else.
```

2. `settings.json` → `statusLine` key (merge; **refuse politely if the user
   already has a custom statusLine**, print manual instructions instead —
   we do not clobber someone's prompt).

3. `settings.json` → `hooks.Stop` + `hooks.SessionStart` (append our hook
   entry; never touch existing entries):

```json
{"hooks": [{"type": "command", "command": "aipet collect --quiet", "timeout": 30}]}
```

4. `~/.codex/prompts/aipet.md`:

```markdown
Run the shell command `aipet card "$ARGUMENTS"` (view defaults to "pet").
Show its output verbatim in a fenced code block, then add one short line
in the pet's voice matching its mood. If the command is missing, tell the
user to run: go install github.com/rohithIlluri/POC-S/pocs/aipet/cmd/aipet@latest
```

Setup autodetects hosts (`~/.claude` and/or `~/.codex` present), installs
for whichever exist, and prints exactly what it wrote and how to undo it.
Bare `aipet` triggers the wizard when no integration marker
(`~/.aipet/setup.json`) exists — so the README one-liner is genuinely it.

---

## 5. What happens to the daemon

Nothing is deleted. `daemon.Serve` (+ heartbeat) remains for users who
don't use Claude Code, but for the primary audience:

- **Stop hook** = collection exactly when new data appears (better than any
  timer — the daemon polls every 2m; hooks fire the second a turn lands),
- **SessionStart hook** = catch-up tick when a session opens after days away
  (replaces daemon catch-up + the TUI launch collect),
- **statusline** = the always-visible face (replaces the TUI header),
- **the TUI's own collect ticker** still covers `aipet tui` standalone use.

The sim's determinism law is untouched: hooks call the same
`daemon.Run → runPetTick` path with the same sealed-baseline replay — the
same day re-ticking many times per hour is already the tested, supported
case (that's what the M-pass re-tick fix was for).

---

## 6. Phases

| Phase | Deliverable | Gate |
|---|---|---|
| **H1** | `aipet card` plain renderer (4 views) + `collect --quiet` with lock/debounce | golden-file render tests; concurrent-collect test; NO_COLOR test |
| **H2** | `aipet statusline` | <20ms benchmark; renders all 4 states from fixture snapshots |
| **H3** | `aipet setup` writer/remover | idempotence test (run 3×, byte-identical); **merge test against a settings.json with pre-existing hooks/statusLine** (never clobbered); `--remove` restores backups; wizard on bare `aipet` |
| **H4** | Live verification in real hosts | in a real Claude Code session: `/aipet` renders, statusline shows pet, one coding turn → Stop hook → statusline hatch-count moves. Codex: `/aipet` renders card |
| **H5** | Surface consolidation + docs + ship | deprecation aliases wired; README/site/running.html rewritten around the one-liner; tag `pocs/aipet/v1.1.0` so `@latest` resolves (see §8 R1) |

Each phase is independently shippable; H1+H2 are pure additions with zero
risk to existing users.

## 7. Risks & mitigations

| Risk | Mitigation |
|---|---|
| `setup` corrupts a user's `settings.json` (other tools' hooks live there) | parse-merge-rewrite only; timestamped backup; `--print` dry-run; refuse on unparseable JSON; the H3 merge test is the release gate |
| Slash command needs Bash permission → prompts every time | `allowed-tools: Bash(aipet card:*)` in frontmatter scopes the grant to exactly our command |
| Stop hook latency annoys users | collect is incremental (scan-state skips unchanged files); debounce + lock + 30s timeout; `--quiet` means zero output on the happy path |
| statusline too slow (runs on every render) | snapshot-read only; never collects; benchmark-gated |
| Two sources of truth for "is it set up" | single marker `~/.aipet/setup.json` recording what was written where, used by both the wizard and `--remove` |
| Codex has no hooks — stale pet between `/aipet` calls | `aipet card` collects first, so every view is fresh at the moment it's asked for |
| User has both the old daemon and new hooks running | fine by design — collect is idempotent and single-flighted; daemon prints a deprecation note under Claude Code |

## 8. Architecture review — hardening pass (binding decisions)

An adversarial review of §1–7 against the real codebase produced these
corrections. Where this section conflicts with anything above, **this
section wins.**

- **R1 · Versioning.** The repo's existing `v1.0.0` tag is unprefixed and
  does **not** version the nested `pocs/aipet` module — `@latest` currently
  resolves to a pseudo-version of the default branch. The ship tag is
  **`pocs/aipet/v1.1.0`** — prefixed for the nested module, and staying in
  major v1 because a v2 tag would require the module path to gain a `/v2`
  suffix (Go semantic import versioning), breaking every import.
- **R2 · Injection.** `$ARGUMENTS` is user-typed text spliced into a shell
  line. Defense in depth: the command files quote `"$ARGUMENTS"`; `aipet
  card` hard-whitelists its view argument (`pet|dex|records|overview`,
  empty ⇒ `pet`, anything else ⇒ usage text, exit 2, no render);
  `allowed-tools` stays scoped to `Bash(aipet card:*)`.
- **R3 · Renderer boundary.** `card` does NOT reuse `Model.View` (that's
  host chrome: tabs, footer, frame sizing). New `internal/tui/card.go`
  exposes pure functions over `(snapshot, journal, width)`, sharing only
  leaf helpers (`meter`, `moodBubble`, `headerFaces`/`eggFaces`, `wrap`,
  species art). `lipgloss.SetColorProfile(termenv.Ascii)` is forced in the
  card path so output is byte-deterministic for golden tests and identical
  under host pipes.
- **R4 · Statusline realism.** lipgloss strips ANSI on non-TTY stdout and
  the statusline runs as a pipe — colored output would silently degrade.
  v1 is deliberately plain text. The command **drains stdin** (the host
  writes session JSON; a writer blocking on a full pipe must never happen).
- **R5 · One freshness owner.** `collect` owns the lock AND the 30s
  debounce (snapshot mtime; `--force` overrides). `card` invokes the same
  code path in-process, then reads the snapshot. `statusline` never
  collects. The lock reuses the daemon's existing pidfile stale-detection
  (extract into a shared helper) — no second lock implementation.
- **R6 · Manifest-driven setup.** `~/.aipet/setup.json` records every
  insertion at write time: target file, JSON path, exact value, backup
  location. `--remove` replays the manifest in reverse. Idempotence checks
  the manifest first, then scans for our command substring as a belt-and-
  braces. Abort (pointing at `--print`) on: unparseable settings JSON,
  `hooks.Stop` present but not an array, a `statusLine` we didn't write.
  All writes are tmp+rename atomic.
- **R7 · Test isolation is mandatory.** Every setup/collect/card test sets
  `HOME` to a temp dir (`t.Setenv`). No test — and no development
  invocation — may touch the real `~/.claude` or `~/.codex`.
- **R8 · Windows.** `setup` detects Windows and prints manual instructions
  instead of writing files (deferred automation). The binary itself still
  cross-compiles for all 5 release targets.
- **R9 · Alias policy.** `daemon`/`status`/`dex`/`leaderboard` keep working
  but disappear from `usage()`; `daemon` prints a one-line pointer to
  `aipet setup`. Bare `aipet` = setup wizard when `~/.aipet/setup.json` is
  absent, else card + `/aipet` hint. `aipet tui` is the full app.
- **R10 · Golden tests are the H1 gate.** Fixture snapshots under
  `internal/tui/testdata/`, golden outputs per view × (egg, hatched,
  worried, empty), regenerated via `go test -run TestCard -update`.
- **R11 · Absolute binary paths in everything setup writes** *(found during
  H4 live verification)*. The host's statusline/hook commands run in an
  environment where the go-install bin dir is not necessarily on PATH — on
  the first real machine tested, `~/go/bin` wasn't, so every bare `aipet …`
  command in settings.json would have been "command not found" inside
  Claude Code while working fine in the user's own shell. Setup therefore
  resolves `os.Executable()` and writes the absolute path into the
  statusLine command, both hook entries, the slash command's pre-execution
  line AND its `allowed-tools` grant, and the Codex prompt. Recognition
  (idempotence, `--remove`) matches any command whose binary base name is
  `aipet` with the exact expected arguments, so legacy bare-form installs
  and moved binaries are still recognized. §4.4's file contents are
  templates in this one respect; the prose is literal.

## 9. Acceptance checklist

- [ ] Fresh machine: `go install …@latest && aipet` → wizard → `/aipet`
      works in Claude Code with no other steps.
- [ ] `/aipet`, `/aipet dex`, `/aipet records` render correctly in chat.
- [ ] Statusline shows egg → (5 qualifying sessions) → hatched, without any
      manual command.
- [ ] A settings.json with pre-existing statusLine + hooks survives
      `aipet setup` byte-identical except our appended hook entries.
- [ ] `aipet setup --remove` restores everything; `/aipet` gone.
- [ ] All existing tests still pass; sim/save untouched.
- [ ] gofmt/vet/-race clean; 5-target cross-compile.
