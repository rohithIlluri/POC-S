import { describe, expect, it } from "vitest";
import { classifyHeuristically } from "../src/classifier/heuristics.js";
import { DEFAULT_CONFIG } from "../src/config.js";

const opts = { ambiguousBand: DEFAULT_CONFIG.classifier.ambiguousBand };

describe("task-type detection", () => {
  const cases: Array<[string, string]> = [
    ["redesign the caching layer as a system design exercise", "architecture"],
    ["refactor the payment module", "refactor"],
    ["debug the crash in the worker pool", "debug"],
    ["update the README documentation", "docs"],
    ["scaffold a new express project skeleton", "boilerplate"],
    ["write a bash script to rotate logs", "script"],
    ["fix a typo in the error message", "quick-edit"],
    ["implement pagination for the users endpoint", "feature"],
    ["hmm what should we do here", "unknown"],
  ];

  it.each(cases)("%s -> %s", (prompt, expected) => {
    expect(classifyHeuristically(prompt, opts).taskType).toBe(expected);
  });

  it("respects bucket priority (refactor beats docs)", () => {
    expect(classifyHeuristically("refactor the docs generator", opts).taskType).toBe("refactor");
  });
});

describe("complexity scoring", () => {
  it("scores a tiny docs fix as low", () => {
    const result = classifyHeuristically("fix typo in readme", opts);
    expect(result.complexity).toBe("low");
    expect(result.score).toBeLessThanOrEqual(35);
  });

  it("scores a broad architectural refactor as high", () => {
    const result = classifyHeuristically("refactor the entire auth architecture across the codebase", opts);
    expect(result.complexity).toBe("high");
    expect(result.score).toBeGreaterThanOrEqual(65);
  });

  it("caps broad-scope bonus at +30", () => {
    const one = classifyHeuristically("investigate every module", opts);
    const three = classifyHeuristically("investigate every module in the entire repo, all files", opts);
    // one broad signal = +20, three signals capped at +30
    expect(three.score - one.score).toBe(10);
  });

  it("caps smallness penalty at -40", () => {
    const result = classifyHeuristically(
      "just a quick simple minor small change to the build pipeline configuration setup",
      opts,
    );
    expect(result.reasoning).toContainEqual(expect.stringContaining("-40"));
  });

  it("penalizes a single file mention and rewards many", () => {
    const single = classifyHeuristically("investigate the logic in server.ts please and thanks", opts);
    expect(single.reasoning).toContainEqual(expect.stringContaining("single file mentioned: -10"));
    const many = classifyHeuristically(
      "investigate a.ts b.ts c.ts d.ts for the inconsistent handling", opts);
    expect(many.reasoning).toContainEqual(expect.stringContaining("+15"));
  });

  it("adds the multi-step bonus", () => {
    const result = classifyHeuristically(
      "add caching to the users endpoint then invalidate the entries whenever a write happens",
      opts,
    );
    expect(result.reasoning).toContainEqual(expect.stringContaining("multi-step phrasing: +10"));
  });

  it("penalizes very short prompts", () => {
    const result = classifyHeuristically("fix the build", opts);
    expect(result.reasoning).toContainEqual(expect.stringContaining("< 60 chars: -10"));
  });

  it("adds length bonuses", () => {
    const long = "investigate the request handling. ".repeat(15); // > 400 chars
    expect(classifyHeuristically(long, opts).reasoning).toContainEqual(
      expect.stringContaining("> 400 chars: +10"),
    );
    const veryLong = "investigate the request handling. ".repeat(30); // > 800 chars
    expect(classifyHeuristically(veryLong, opts).reasoning).toContainEqual(
      expect.stringContaining("> 800 chars: +20"),
    );
  });
});

describe("ambiguity detection", () => {
  it("flags unknown task type as ambiguous", () => {
    const result = classifyHeuristically("hmm what should we do here about the thing", opts);
    expect(result.taskType).toBe("unknown");
    expect(result.ambiguous).toBe(true);
  });

  it("flags a dead-band score as ambiguous", () => {
    // feature task, base 50, prior 0, >= 60 chars, no other signals -> score 50
    const result = classifyHeuristically("implement pagination for the users list endpoint in the admin panel", opts);
    expect(result.score).toBeGreaterThanOrEqual(42);
    expect(result.score).toBeLessThanOrEqual(58);
    expect(result.ambiguous).toBe(true);
  });

  it("does not flag confident scores as ambiguous", () => {
    const result = classifyHeuristically("fix typo in readme", opts);
    expect(result.ambiguous).toBe(false);
  });
});

describe("reasoning trail", () => {
  it("records fired signals and the final score", () => {
    const result = classifyHeuristically("refactor the entire auth architecture across the codebase", opts);
    expect(result.reasoning).toContainEqual(expect.stringContaining("broad scope"));
    expect(result.reasoning).toContainEqual(expect.stringContaining("deep-work"));
    expect(result.reasoning).toContainEqual(expect.stringContaining(`final score ${result.score}`));
  });
});
