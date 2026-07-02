import { spawn } from "node:child_process";
import type { Config } from "./config.js";
import type { RoutingDecision } from "./types.js";

/**
 * Spawn the chosen coding CLI with inherited stdio and mirror its exit status.
 * Never resolves normally on success — the process exits with the child's code.
 */
export function execute(decision: RoutingDecision, config: Config): void {
  const child = spawn(decision.command, decision.args, { stdio: "inherit" });

  child.on("error", (err: NodeJS.ErrnoException) => {
    if (err.code === "ENOENT") {
      const hint = config.tools[decision.tool].installHint;
      console.error(`ccr: "${decision.command}" is not installed or not on PATH.`);
      console.error(`  Install it: ${hint}`);
      process.exit(127);
    }
    console.error(`ccr: failed to start "${decision.command}": ${err.message}`);
    process.exit(1);
  });

  child.on("close", (code, signal) => {
    if (signal) {
      // Re-raise so the shell sees the same signal death (e.g. Ctrl-C).
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code ?? 1);
  });
}
