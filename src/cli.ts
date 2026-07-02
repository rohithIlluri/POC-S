#!/usr/bin/env node
import { Command } from "commander";
import { run } from "./run.js";
import type { Tool } from "./types.js";

const rawArgs = process.argv.slice(2);
const separator = rawArgs.indexOf("--");
const ownArgs = separator >= 0 ? rawArgs.slice(0, separator) : rawArgs;
const passthrough = separator >= 0 ? rawArgs.slice(separator + 1) : [];

const program = new Command();

program
  .name("ccr")
  .description(
    "Multi coding tools router: classifies a coding prompt by task type and complexity, " +
      "then runs it with Claude Code or Codex using the right model. " +
      "Args after -- are passed through to the chosen CLI.",
  )
  .version("0.1.0")
  .argument("<prompt...>", "the coding prompt to route")
  .option("--dry-run", "print the routing decision without executing", false)
  .option("--verbose", "print classification signals and routing reasoning to stderr", false)
  .option("--config <path>", "path to a ccr config file (YAML or JSON)")
  .option("--tool <tool>", "force the tool (claude or codex)")
  .option("--model <id>", "force the model id")
  .action(async (promptParts: string[], options) => {
    if (options.tool && options.tool !== "claude" && options.tool !== "codex") {
      console.error(`ccr: invalid --tool "${options.tool}" (expected claude or codex)`);
      process.exit(1);
    }
    await run({
      prompt: promptParts.join(" "),
      passthrough,
      dryRun: options.dryRun,
      verbose: options.verbose,
      configPath: options.config,
      forceTool: options.tool as Tool | undefined,
      forceModel: options.model,
    });
  });

program.parseAsync(ownArgs, { from: "user" }).catch((err) => {
  console.error(`ccr: ${err instanceof Error ? err.message : String(err)}`);
  process.exit(1);
});
