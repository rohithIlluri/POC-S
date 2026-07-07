import { describe, expect, it, vi } from "vitest";
import { DEFAULT_CONFIG } from "../src/config.js";
import { execute } from "../src/executor.js";
import type { RoutingDecision } from "../src/types.js";

function decision(command: string, args: string[]): RoutingDecision {
  return { tool: "claude", model: "m", tier: "low", command, args, matchedRule: "test" };
}

describe("execute", () => {
  it("mirrors the child's exit code", async () => {
    const code = await execute(decision("node", ["-e", "process.exit(3)"]), DEFAULT_CONFIG);
    expect(code).toBe(3);
  });

  it("resolves 0 for a successful child", async () => {
    const code = await execute(decision("node", ["-e", ""]), DEFAULT_CONFIG);
    expect(code).toBe(0);
  });

  it("resolves 127 with an install hint when the command is missing", async () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const code = await execute(decision("definitely-not-a-real-cmd-xyz", []), DEFAULT_CONFIG);
    expect(code).toBe(127);
    expect(errorSpy.mock.calls.flat().join("\n")).toContain(DEFAULT_CONFIG.tools.claude.installHint);
    errorSpy.mockRestore();
  });

  it("does not leave signal listeners behind", async () => {
    const before = process.listenerCount("SIGINT");
    await execute(decision("node", ["-e", ""]), DEFAULT_CONFIG);
    expect(process.listenerCount("SIGINT")).toBe(before);
  });
});
