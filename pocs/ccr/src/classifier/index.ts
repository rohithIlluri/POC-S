import type { Config } from "../config.js";
import type { Classification, Hints } from "../types.js";
import { classifyHeuristically } from "./heuristics.js";
import { hasAnyHint, hintsAreConclusive } from "./hints.js";
import { classifyWithLlm, type LlmClient } from "./llm.js";

export interface ClassifyOptions {
  /** Injected for tests; a real Anthropic client is created lazily otherwise. */
  llmClient?: LlmClient;
  /** Receives one-line diagnostics for --verbose. */
  log?: (line: string) => void;
}

/**
 * Hybrid classification: heuristics first; the LLM fallback runs only when the
 * heuristics are ambiguous, no explicit hint is present, and llmFallback is on.
 */
export async function classify(
  prompt: string,
  config: Config,
  hints: Hints,
  opts: ClassifyOptions = {},
): Promise<Classification> {
  const log = opts.log ?? (() => {});

  if (hintsAreConclusive(hints)) {
    log("explicit hint pins tool and model/tier; skipping classification");
    return {
      taskType: "unknown",
      complexity: "medium",
      score: 50,
      source: "heuristic",
      reasoning: ["classification skipped: explicit hint override"],
    };
  }

  const heuristic = classifyHeuristically(prompt, { ambiguousBand: config.classifier.ambiguousBand });

  if (!heuristic.ambiguous || !config.classifier.llmFallback || hasAnyHint(hints)) {
    return heuristic;
  }

  log("heuristics inconclusive; consulting LLM classifier");
  const llm = await classifyWithLlm(prompt, config, opts.llmClient);
  if (!llm.ok) {
    log(llm.error);
    // Only a dead-band score means the heuristic complexity has no conviction;
    // an unknown task type with a confident score keeps that complexity.
    const [bandLow, bandHigh] = config.classifier.ambiguousBand;
    const inBand = heuristic.score >= bandLow && heuristic.score <= bandHigh;
    return {
      ...heuristic,
      source: "heuristic-fallback",
      complexity: inBand ? "medium" : heuristic.complexity,
      reasoning: [
        ...heuristic.reasoning,
        `LLM fallback unavailable (${llm.error}); ${inBand ? "defaulting to medium" : `keeping heuristic ${heuristic.complexity}`}`,
      ],
    };
  }

  return {
    taskType: llm.value.taskType,
    complexity: llm.value.complexity,
    score: heuristic.score,
    source: "llm",
    reasoning: [...heuristic.reasoning, `LLM: ${llm.value.reasoning}`],
  };
}
