import type { Config } from "../config.js";
import { TOOLS, type Hints, type Tier, type Tool } from "../types.js";

const TIER_WORDS: Record<string, Tier> = {
  opus: "high",
  sonnet: "medium",
  haiku: "low",
};

/**
 * Extract explicit routing hints from the prompt. Only directive phrasings
 * ("use codex", "with opus", "model=...") count — a passing mention like
 * "the codex file format" must not trigger a hint.
 */
export function extractHints(prompt: string): Hints {
  const hints: Hints = {};

  const tierMatch = /\b(?:use|using|with|via|on)\s+(?:claude\s+)?(opus|sonnet|haiku)\b/i.exec(prompt);
  if (tierMatch) {
    hints.tool = "claude";
    hints.tier = TIER_WORDS[tierMatch[1].toLowerCase()];
  }

  const toolMatch = /\b(?:use|using|with|via)\s+(codex|claude)\b/i.exec(prompt);
  if (toolMatch && !hints.tool) {
    hints.tool = toolMatch[1].toLowerCase() as Tool;
  }

  const modelMatch = /\bmodel\s*=\s*([\w.-]+)/i.exec(prompt);
  if (modelMatch) {
    hints.model = modelMatch[1];
    if (!hints.tool) {
      hints.tool = inferToolFromModel(hints.model);
    }
  }

  return hints;
}

/**
 * Work out which tool a model id belongs to: a config lookup wins, then a
 * vendor-prefix guess. Undefined when the model is unrecognizable.
 */
export function inferToolFromModel(model: string, config?: Config): Tool | undefined {
  if (config) {
    for (const tool of TOOLS) {
      if (Object.values(config.tools[tool].models).includes(model)) return tool;
    }
  }
  if (/^claude/i.test(model)) return "claude";
  if (/^gpt|codex/i.test(model)) return "codex";
  return undefined;
}

export function hasAnyHint(hints: Hints): boolean {
  return hints.tool !== undefined || hints.tier !== undefined || hints.model !== undefined;
}

/** A hint fully determines the routing when it pins both the tool and a model or tier. */
export function hintsAreConclusive(hints: Hints): boolean {
  return hints.tool !== undefined && (hints.tier !== undefined || hints.model !== undefined);
}
