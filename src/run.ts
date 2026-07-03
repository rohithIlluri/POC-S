import { classify } from "./classifier/index.js";
import { extractHints, inferToolFromModel } from "./classifier/hints.js";
import { loadConfig, ConfigError } from "./config.js";
import { execute } from "./executor.js";
import { route } from "./router.js";
import type { Tool } from "./types.js";

export interface RunOptions {
  prompt: string;
  passthrough: string[];
  dryRun: boolean;
  verbose: boolean;
  configPath?: string;
  forceTool?: Tool;
  forceModel?: string;
}

export async function run(opts: RunOptions): Promise<void> {
  const log = opts.verbose ? (line: string) => console.error(`[ccr] ${line}`) : () => {};

  let config;
  try {
    config = loadConfig(opts.configPath);
  } catch (err) {
    if (err instanceof ConfigError) {
      console.error(`ccr: ${err.message}`);
      process.exit(1);
    }
    throw err;
  }

  const hints = extractHints(opts.prompt);
  if (opts.forceTool) hints.tool = opts.forceTool;
  if (opts.forceModel) {
    hints.model = opts.forceModel;
    // A forced model must not end up on the other vendor's CLI.
    if (!hints.tool) hints.tool = inferToolFromModel(opts.forceModel, config);
  }
  if (hints.tool || hints.tier || hints.model) {
    log(`hints: ${JSON.stringify(hints)}`);
  }

  const classification = await classify(opts.prompt, config, hints, { log });
  log(`classification: ${classification.taskType}/${classification.complexity} (score ${classification.score}, source ${classification.source})`);
  for (const line of classification.reasoning) log(`  ${line}`);

  const decision = route(classification, hints, config, opts.prompt, opts.passthrough);
  log(`matched: ${decision.matchedRule}`);

  if (opts.dryRun) {
    console.log(`tool:    ${decision.tool}`);
    console.log(`model:   ${decision.model} (tier: ${decision.tier})`);
    console.log(`task:    ${classification.taskType}/${classification.complexity} (source: ${classification.source})`);
    console.log(`rule:    ${decision.matchedRule}`);
    console.log(`command: ${[decision.command, ...decision.args].map((a) => (/\s/.test(a) ? JSON.stringify(a) : a)).join(" ")}`);
    return;
  }

  execute(decision, config);
}
