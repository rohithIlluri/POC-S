#!/usr/bin/env node
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { Command } from "commander";
import { run } from "./run.js";
import type { Tool } from "./types.js";

const { version } = createRequire(import.meta.url)("../package.json") as { version: string };

const rawArgs = process.argv.slice(2);
const separator = rawArgs.indexOf("--");
const ownArgs = separator >= 0 ? rawArgs.slice(0, separator) : rawArgs;
const passthrough = separator >= 0 ? rawArgs.slice(separator + 1) : [];

/**
 * Resolve the prompt from positional args, falling back to stdin when the
 * prompt is omitted and input is piped (`git diff | ccr`, `ccr < bug.txt`).
 * Reading fd 0 is only attempted when stdin is not an interactive TTY —
 * otherwise it would block waiting for the user, so there is simply no prompt.
 */
function resolvePrompt(promptParts: string[]): string {
  const fromArgs = promptParts.join(" ").trim();
  if (fromArgs) return fromArgs;
  if (!process.stdin.isTTY) {
    try {
      return readFileSync(0, "utf8").trim();
    } catch {
      return "";
    }
  }
  return "";
}

const program = new Command();

program
  .name("ccr")
  .description(
    "Multi coding tools router: classifies a coding prompt by task type and complexity, " +
      "then runs it with Claude Code or Codex using the right model. " +
      "The prompt may be given as arguments or piped on stdin (git diff | ccr). " +
      "Args after -- are passed through to the chosen CLI.",
  )
  .version(version)
  .argument("[prompt...]", "the coding prompt to route (read from stdin if omitted)")
  .option("--dry-run", "print the routing decision without executing", false)
  .option("--json", "with --dry-run, print the decision as JSON", false)
  .option("--verbose", "print classification signals and routing reasoning to stderr", false)
  .option("--config <path>", "path to a ccr config file (YAML or JSON)")
  .option("--tool <tool>", "force the tool (claude or codex)")
  .option("--model <id>", "force the model id")
  .action(async (promptParts: string[], options) => {
    if (options.tool && options.tool !== "claude" && options.tool !== "codex") {
      console.error(`ccr: invalid --tool "${options.tool}" (expected claude or codex)`);
      process.exitCode = 1;
      return;
    }
    const prompt = resolvePrompt(promptParts ?? []);
    if (!prompt) {
      console.error("ccr: no prompt given (pass it as an argument or pipe it on stdin)");
      process.exitCode = 1;
      return;
    }
    const code = await run({
      prompt,
      passthrough,
      dryRun: options.dryRun,
      json: options.json,
      verbose: options.verbose,
      configPath: options.config,
      forceTool: options.tool as Tool | undefined,
      forceModel: options.model,
    });
    process.exitCode = code;
  });

program.parseAsync(ownArgs, { from: "user" }).catch((err: unknown) => {
  console.error(`ccr: ${err instanceof Error ? err.message : String(err)}`);
  process.exitCode = 1;
});
