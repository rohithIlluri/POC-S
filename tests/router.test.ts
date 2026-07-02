import { describe, expect, it } from "vitest";
import { DEFAULT_CONFIG, type Config } from "../src/config.js";
import { route } from "../src/router.js";
import type { Classification } from "../src/types.js";

function classification(overrides: Partial<Classification> = {}): Classification {
  return {
    taskType: "feature",
    complexity: "medium",
    score: 50,
    source: "heuristic",
    reasoning: [],
    ...overrides,
  };
}

describe("routing matrix", () => {
  it("routes high-complexity debug to claude opus tier", () => {
    const d = route(classification({ taskType: "debug", complexity: "high" }), {}, DEFAULT_CONFIG, "p");
    expect(d.tool).toBe("claude");
    expect(d.tier).toBe("high");
    expect(d.model).toBe("claude-opus-4-8");
  });

  it("routes docs to codex low tier via wildcard complexity", () => {
    const d = route(classification({ taskType: "docs", complexity: "low" }), {}, DEFAULT_CONFIG, "p");
    expect(d.tool).toBe("codex");
    expect(d.model).toBe("gpt-5.1-codex-mini");
  });

  it("routes unknown/high through the wildcard-taskType rule", () => {
    const d = route(classification({ taskType: "unknown", complexity: "high" }), {}, DEFAULT_CONFIG, "p");
    expect(d.tool).toBe("claude");
    expect(d.tier).toBe("high");
  });

  it("falls through to routing.default when nothing matches", () => {
    const config: Config = {
      ...DEFAULT_CONFIG,
      routing: { rules: [], default: { tool: "codex", tier: "medium" } },
    };
    const d = route(classification(), {}, config, "p");
    expect(d.tool).toBe("codex");
    expect(d.tier).toBe("medium");
    expect(d.matchedRule).toContain("default");
  });

  it("respects user rule ordering (first match wins)", () => {
    const config: Config = {
      ...DEFAULT_CONFIG,
      routing: {
        rules: [
          { taskType: "*", complexity: "*", tool: "codex", tier: "high" },
          { taskType: ["feature"], complexity: "*", tool: "claude", tier: "medium" },
        ],
        default: DEFAULT_CONFIG.routing.default,
      },
    };
    const d = route(classification(), {}, config, "p");
    expect(d.tool).toBe("codex");
    expect(d.matchedRule).toContain("rule #1");
  });
});

describe("hint overrides", () => {
  it("tool + tier hint bypasses the matrix", () => {
    const d = route(classification({ taskType: "docs", complexity: "low" }), { tool: "claude", tier: "high" }, DEFAULT_CONFIG, "p");
    expect(d.tool).toBe("claude");
    expect(d.model).toBe("claude-opus-4-8");
    expect(d.matchedRule).toContain("hint override");
  });

  it("model hint is used verbatim and maps to its configured tier", () => {
    const d = route(classification(), { tool: "claude", model: "claude-opus-4-8" }, DEFAULT_CONFIG, "p");
    expect(d.model).toBe("claude-opus-4-8");
    expect(d.tier).toBe("high");
  });

  it("unrecognized model hint passes through with medium tier", () => {
    const d = route(classification(), { tool: "codex", model: "gpt-6-experimental" }, DEFAULT_CONFIG, "p");
    expect(d.model).toBe("gpt-6-experimental");
    expect(d.tier).toBe("medium");
  });

  it("tool-only hint overrides the matrix tool but keeps the tier", () => {
    const d = route(classification({ taskType: "debug", complexity: "high" }), { tool: "codex" }, DEFAULT_CONFIG, "p");
    expect(d.tool).toBe("codex");
    expect(d.tier).toBe("high");
    expect(d.model).toBe("gpt-5.2-codex");
  });
});

describe("argv construction", () => {
  it("substitutes prompt and model into the template", () => {
    const d = route(classification({ taskType: "debug", complexity: "high" }), {}, DEFAULT_CONFIG, "fix the race condition");
    expect(d.command).toBe("claude");
    expect(d.args).toEqual(["-p", "fix the race condition", "--model", "claude-opus-4-8"]);
  });

  it("keeps a prompt with quotes and spaces as one argv element", () => {
    const prompt = 'rename "old thing" to new thing';
    const d = route(classification(), {}, DEFAULT_CONFIG, prompt);
    expect(d.args).toContain(prompt);
  });

  it("appends passthrough args", () => {
    const d = route(classification({ taskType: "docs" }), {}, DEFAULT_CONFIG, "p", ["--allowed-tools", "Bash"]);
    expect(d.args.slice(-2)).toEqual(["--allowed-tools", "Bash"]);
  });
});
