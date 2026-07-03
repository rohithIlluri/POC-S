# ccr — Multi Coding Tools Router

`ccr` takes a coding prompt, classifies its **task type** and **complexity**, then runs it with the right coding agent — [Claude Code](https://github.com/anthropics/claude-code) (`claude`) or [OpenAI Codex](https://github.com/openai/codex) (`codex`) — using a model matched to the task.

```
prompt → hint extraction → heuristic classifier → (ambiguous? → Haiku LLM classifier)
       → routing matrix → spawn claude/codex with the resolved model
```

## Install

```sh
npm install
npm run build
npm link        # installs the `ccr` command globally
```

Requires Node 18+. The target CLIs are installed separately (`npm i -g @anthropic-ai/claude-code`, `npm i -g @openai/codex`) — `ccr` tells you which one is missing if it can't spawn it.

## Usage

```sh
ccr "fix typo in README"                                  # → codex, mini model
ccr "refactor entire auth architecture across the repo"   # → claude, opus tier
ccr "use codex to add a helper"                           # explicit hint wins
ccr --dry-run "add a users endpoint"                      # print the decision, don't run
ccr --verbose "debug the crash in worker.ts"              # show classification reasoning
ccr "add endpoint" -- --allowed-tools Bash                # args after -- pass through to the tool
```

| Flag | Effect |
|---|---|
| `--dry-run` | Print tool, model, matched rule, and full command; don't execute |
| `--verbose` | Print heuristic signals, score, classifier source, and matched rule to stderr |
| `--config <path>` | Use an explicit config file |
| `--tool <claude\|codex>` | Force the tool |
| `--model <id>` | Force the model |

Inline prompt hints also work: `use codex`, `with opus`, `use claude sonnet`, `model=gpt-5.1-codex-mini`.

## How routing works

1. **Hints** — explicit tool/model requests in the prompt or flags override everything.
2. **Heuristics** — keyword buckets detect the task type (architecture, refactor, debug, feature, quick-edit, boilerplate, script, docs); weighted signals (scope words, prompt length, file mentions, smallness cues) produce a 0–100 complexity score. Score ≥ 65 is high, ≤ 35 low, else medium.
3. **LLM fallback** — only when the score lands in the dead band `[42, 58]` or no task type matched, a fast Haiku call classifies the prompt (needs `ANTHROPIC_API_KEY`; degrades gracefully to the heuristic result if unavailable — the router never blocks on it).
4. **Routing matrix** — ordered rules map (task type × complexity) to a tool + model tier; first match wins.

## Configuration

Config discovery order: `--config <path>` → `./ccr.config.yaml` / `./ccr.config.json` → `~/.config/ccr/config.yaml`. User config is deep-merged over the defaults, so you can override a single value. All model IDs live in config — update them without touching code.

Full default config:

```yaml
classifier:
  llmFallback: true          # set false to never call the LLM classifier
  model: claude-haiku-4-5
  timeoutMs: 5000
  ambiguousBand: [42, 58]    # heuristic scores in this band trigger the LLM

tools:
  claude:
    command: claude
    argsTemplate: ["-p", "{prompt}", "--model", "{model}"]
    models:
      high: claude-opus-4-8
      medium: claude-sonnet-5
      low: claude-haiku-4-5
    installHint: "npm install -g @anthropic-ai/claude-code"
  codex:
    command: codex
    argsTemplate: ["exec", "{prompt}", "-m", "{model}"]
    models:
      high: gpt-5.2-codex
      medium: gpt-5.2-codex
      low: gpt-5.1-codex-mini
    installHint: "npm install -g @openai/codex"

routing:
  # ordered; first match wins. taskType/complexity: list or "*"
  rules:
    - { taskType: [architecture, refactor, debug], complexity: [high],   tool: claude, tier: high }
    - { taskType: [architecture, refactor, debug], complexity: [medium], tool: claude, tier: medium }
    - { taskType: [feature],                       complexity: "*",      tool: claude, tier: medium }
    - { taskType: [quick-edit, boilerplate, script, docs], complexity: "*", tool: codex, tier: low }
    - { taskType: "*", complexity: [high], tool: claude, tier: high }
    - { taskType: "*", complexity: [low],  tool: codex,  tier: low }
  default: { tool: claude, tier: medium }
```

## Known limitations

- Conflicting hints resolve deterministically in favor of the tier word: "use codex with opus" routes to claude/opus.
- Quote your prompt — an unquoted token like `-p` is parsed as a flag by the shell/CLI, not as prompt text.
- The file-mention heuristic counts dotted names like `node.js` as file paths (a small score nudge, not a routing error).

## Development

```sh
npm test         # vitest — heuristics, hints, router, config, hybrid classifier (no network)
npm run build    # tsc → dist/
```

Layout: pure logic (`src/classifier/heuristics.ts`, `src/classifier/hints.ts`, `src/router.ts`) is separated from I/O (`src/config.ts`, `src/classifier/llm.ts`, `src/executor.ts`); `src/run.ts` orchestrates and `src/cli.ts` is the commander entrypoint.
