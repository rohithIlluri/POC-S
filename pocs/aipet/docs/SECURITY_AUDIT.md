# Security audit — v1.0.0

Date: 2026-07-09 · Branch: `design/codelings-companions`

## Scope & method

Full review of the Go codebase ahead of the v1 release. A dedicated review
pass traced data flow from untrusted inputs to sensitive sinks, followed by an
independent false-positive filter on each candidate finding (only findings at
confidence ≥ 0.8 were actioned).

**Threat model.** aipet is a fully-local, single-user terminal tool with no
network code. Same-user files under `~/.aipet` are trusted. The one genuinely
untrusted surface is the *content* of the session logs it ingests
(`~/.claude/projects/**/*.jsonl`, `~/.codex/sessions/**`): those files are
written by other tools and by coding agents that can be prompt-injected, so
fields read out of them — model ids, `cwd`/project paths, session ids, and the
names of files that fail to parse — must be treated as attacker-influenceable.

## Findings

### 1. Terminal escape-sequence injection via log-derived strings — FIXED

- **Severity:** Medium · **Confidence:** 0.8 (confirmed by an independent pass)
- **Category:** `terminal-escape-injection`
- **Where:** untrusted `model` and `cwd` values (and error-producing filenames)
  flowed from the collectors into `store.Event.Model` / `.Project` / `.Session`
  and the collect-error strings, then reached an interactive terminal
  unescaped via the CLI (`aipet leaderboard`, `aipet status`) and the Bubble
  Tea TUI. `filepath.Base`, `fmt.Printf`, lipgloss, and Bubble Tea all pass
  control bytes through untouched, and the length `trunc` only cut length, not
  control bytes.
- **Impact:** a crafted `cwd`/`model`/filename carrying `\u`-escaped OSC/CSI
  sequences (e.g. OSC 52 clipboard write, OSC 0/2 title rewrite) would be
  emitted raw to the developer's terminal when they viewed their stats — a
  clipboard-hijack / output-spoofing vector. `encoding/json` already rejects
  *raw* ESC bytes as invalid JSON, so the exploitable path is specifically the
  `\u`-escaped form that decodes to a control rune after parsing.
- **Fix:** sanitize at the single boundary where untrusted content enters the
  event log. New `internal/collector/sanitize.go` strips C0/C1/DEL control
  runes (and invalid-encoding bytes) from `Model`, `Project`, `Session`, and
  collect-error strings. Printable ASCII and multi-byte UTF-8 (e.g. `café`,
  `π`) pass through unchanged. Covered by `sanitize_test.go`, including an
  end-to-end test that plants a `\u`-escaped OSC 52 payload in a real
  session log and asserts no control byte survives into the stored event.

## Reviewed and ruled out (no action needed)

- **No network surface.** Confirmed absence of `net/http`, `net.Dial`,
  `os/exec`, and similar — the M0 teardown removed the only outbound code.
  - *Amended (H7, 2026-07-15):* `internal/llm` is now the single, deliberate
    exception — the opt-in `voice=api` mode calls api.anthropic.com via the
    official Anthropic SDK using the user's own credentials. Bounded by
    design: runs only when the user sets `aipet config voice api`, only from
    the user-initiated card path (never hooks, statusline, or collection),
    ≤8 calls/day with a 3s timeout, credentials never stored or logged by
    aipet, and generated text is control-character-sanitized before any
    terminal render. Every other surface remains zero-network.
- **Path traversal.** No filesystem path is built from log-derived values; all
  paths derive from `os.UserHomeDir()` plus fixed constants. Dedupe keys are
  map keys and JSON values only, never paths.
- **`running.html` XSS/DOM.** `innerHTML` is built solely from a static
  in-file `DEX` array — no network, `fetch`, `eval`, or user-supplied data.
- **Daemon pidfile / snapshot / config writes.** Read-check-write and
  atomic-rename operate only within the 0700 user-owned `~/.aipet`; any symlink
  or TOCTOU concern requires an already-same-user process and crosses no
  privilege boundary.
- **JSONL parsing.** Go's `encoding/json` has no decode-to-code path; token
  counts are `int64` feeding non-blocking budget nudges only.
- **Codex dedupe hash.** FNV-64a is used for dedupe membership, never as a
  security digest and never as a path.

## Result

One Medium finding, fixed and tested. No High findings. Full suite passes under
the race detector; `go vet` and `gofmt` clean.
