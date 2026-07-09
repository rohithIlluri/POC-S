export const TASK_TYPES = [
  "architecture",
  "refactor",
  "debug",
  "feature",
  "quick-edit",
  "boilerplate",
  "script",
  "docs",
  "unknown",
] as const;
export type TaskType = (typeof TASK_TYPES)[number];

export const COMPLEXITIES = ["low", "medium", "high"] as const;
export type Complexity = (typeof COMPLEXITIES)[number];

export const TOOLS = ["claude", "codex"] as const;
export type Tool = (typeof TOOLS)[number];

export const TIERS = ["high", "medium", "low"] as const;
export type Tier = (typeof TIERS)[number];

export type ClassificationSource = "heuristic" | "llm" | "heuristic-fallback";

export interface Classification {
  taskType: TaskType;
  complexity: Complexity;
  /** Raw heuristic complexity score, 0-100. */
  score: number;
  source: ClassificationSource;
  /** Human-readable signal trail for --verbose. */
  reasoning: string[];
}

export interface HeuristicResult extends Classification {
  source: "heuristic";
  /** True when the heuristics have no conviction and the LLM fallback should run. */
  ambiguous: boolean;
}

export interface Hints {
  tool?: Tool;
  tier?: Tier;
  model?: string;
}

export interface RoutingDecision {
  tool: Tool;
  model: string;
  tier: Tier;
  command: string;
  args: string[];
  /** Which routing rule fired, for --verbose. */
  matchedRule: string;
}
