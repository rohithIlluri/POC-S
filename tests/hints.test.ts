import { describe, expect, it } from "vitest";
import { extractHints, hintsAreConclusive } from "../src/classifier/hints.js";

describe("extractHints", () => {
  it("extracts a codex tool hint", () => {
    expect(extractHints("use codex to add a helper")).toEqual({ tool: "codex" });
  });

  it("extracts a claude tool hint", () => {
    expect(extractHints("do this with claude please")).toEqual({ tool: "claude" });
  });

  it("maps tier words to claude tiers", () => {
    expect(extractHints("use opus to redesign this")).toEqual({ tool: "claude", tier: "high" });
    expect(extractHints("with sonnet, add the endpoint")).toEqual({ tool: "claude", tier: "medium" });
    expect(extractHints("use claude haiku for this")).toEqual({ tool: "claude", tier: "low" });
  });

  it("extracts model= hints and infers the tool", () => {
    expect(extractHints("model=gpt-5.1-codex-mini fix the tests")).toEqual({
      tool: "codex",
      model: "gpt-5.1-codex-mini",
    });
    expect(extractHints("model=claude-opus-4-8 refactor this")).toEqual({
      tool: "claude",
      model: "claude-opus-4-8",
    });
  });

  it("does not false-positive on passing mentions", () => {
    expect(extractHints("parse the codex file format spec")).toEqual({});
    expect(extractHints("the claude integration is broken")).toEqual({});
  });
});

describe("hintsAreConclusive", () => {
  it("is true only when tool plus tier or model are pinned", () => {
    expect(hintsAreConclusive({ tool: "claude", tier: "high" })).toBe(true);
    expect(hintsAreConclusive({ tool: "codex", model: "gpt-5.2-codex" })).toBe(true);
    expect(hintsAreConclusive({ tool: "codex" })).toBe(false);
    expect(hintsAreConclusive({})).toBe(false);
  });
});
