import spawn from "cross-spawn";
import type { Config } from "./config.js";
import type { RoutingDecision } from "./types.js";

/**
 * Spawn the chosen coding CLI with inherited stdio and resolve with the exit
 * code the wrapper should mirror. cross-spawn makes `claude`/`codex` resolve
 * on Windows (.cmd shims) without shell interpolation of the prompt.
 *
 * While the child runs, SIGINT/SIGTERM are forwarded instead of killing the
 * wrapper, so Ctrl-C reaches the child and its exit status is still mirrored.
 * If the child dies from a signal, the same signal is re-raised on the wrapper
 * (after restoring default handling) to preserve shell job-control semantics.
 */
export function execute(decision: RoutingDecision, config: Config): Promise<number> {
  return new Promise((resolve) => {
    const child = spawn(decision.command, decision.args, { stdio: "inherit" });

    const forward = (signal: NodeJS.Signals) => {
      if (child.exitCode === null) child.kill(signal);
    };
    const onSigint = () => forward("SIGINT");
    const onSigterm = () => forward("SIGTERM");
    process.on("SIGINT", onSigint);
    process.on("SIGTERM", onSigterm);
    const cleanup = () => {
      process.off("SIGINT", onSigint);
      process.off("SIGTERM", onSigterm);
    };

    child.on("error", (err: NodeJS.ErrnoException) => {
      cleanup();
      if (err.code === "ENOENT") {
        const hint = config.tools[decision.tool].installHint;
        console.error(`ccr: "${decision.command}" is not installed or not on PATH.`);
        console.error(`  Install it: ${hint}`);
        resolve(127);
        return;
      }
      console.error(`ccr: failed to start "${decision.command}": ${err.message}`);
      resolve(1);
    });

    child.on("close", (code, signal) => {
      cleanup();
      if (signal) {
        process.kill(process.pid, signal);
        // If the signal is caught elsewhere or ignored, still exit like the shell convention.
        resolve(128 + (signal === "SIGKILL" ? 9 : signal === "SIGTERM" ? 15 : 2));
        return;
      }
      resolve(code ?? 1);
    });
  });
}
