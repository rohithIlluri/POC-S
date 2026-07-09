import type { Complexity, HeuristicResult, TaskType } from "../types.js";

interface Bucket {
  taskType: TaskType;
  patterns: RegExp[];
}

// Priority-ordered: first bucket with a match wins.
const TASK_BUCKETS: Bucket[] = [
  {
    taskType: "architecture",
    patterns: [
      /\bre-?architect\b/i,
      /\barchitect\b/i,
      /\bredesign\b/i,
      /\bsystem design\b/i,
      /\bmigrate\b.*\bto\b/i,
      /\brestructure\b/i,
    ],
  },
  {
    taskType: "refactor",
    patterns: [
      /\brefactor\b/i,
      /\brewrite\b/i,
      /\bclean ?up\b/i,
      /\bextract\b.*\b(class|module|function)\b/i,
      /\brename across\b/i,
    ],
  },
  {
    taskType: "debug",
    patterns: [
      /\bdebug\b/i,
      /\bfix\b.*\bbug\b/i,
      /\brace condition\b/i,
      /\bcrash\b/i,
      /\bstack ?trace\b/i,
      /\bmemory leak\b/i,
      /\bflaky\b/i,
      /\binvestigate\b/i,
    ],
  },
  {
    taskType: "docs",
    patterns: [/\bdocs?\b/i, /\bdocumentation\b/i, /\breadme\b/i, /\bdocstrings?\b/i, /\bcomments?\b/i, /\bchangelog\b/i],
  },
  {
    taskType: "boilerplate",
    patterns: [/\bboilerplate\b/i, /\bscaffold\b/i, /\bstub\b/i, /\bskeleton\b/i, /\btemplate\b/i, /\binit(?:ialize)?\b.*\bproject\b/i],
  },
  {
    taskType: "script",
    patterns: [/\bscript\b/i, /\bone-?off\b/i, /\bcron\b/i, /\bshell command\b/i, /\bbash\b/i],
  },
  {
    taskType: "quick-edit",
    patterns: [
      /\btypo\b/i,
      /\bone-?liner\b/i,
      /\bquick\b/i,
      /\brename \w+ to\b/i,
      /\bbump\b/i,
      /\bnull check\b/i,
      /\bsmall (fix|change)\b/i,
      /\badd\b.*\bimport\b/i,
    ],
  },
  {
    taskType: "feature",
    patterns: [/\badd\b/i, /\bimplement\b/i, /\bbuild\b/i, /\bcreate\b/i, /\bsupport\b/i, /\bendpoint\b/i, /\bintegrate\b/i],
  },
];

const TASK_TYPE_PRIOR: Record<TaskType, number> = {
  architecture: 20,
  refactor: 10,
  debug: 10,
  feature: 0,
  script: -10,
  boilerplate: -10,
  docs: -20,
  "quick-edit": -20,
  unknown: 0,
};

const BROAD_SCOPE = [/\bentire\b/i, /\bwhole\b/i, /\ball files\b/i, /\bacross the (codebase|repo)\b/i, /\bevery\b/i, /\bend-to-end\b/i];
const DEEP_WORK = [/\barchitecture\b/i, /\bconcurrency\b/i, /\bperformance\b/i, /\bsecurity\b/i, /\bmigration\b/i, /\bdeadlock\b/i, /\bdistributed\b/i];
const SMALLNESS = [/\bone-?liner\b/i, /\btypo\b/i, /\bquick\b/i, /\bsmall\b/i, /\bjust\b/i, /\bsimple\b/i, /\bminor\b/i, /\bsingle line\b/i];
const MULTI_STEP = [/\bthen\b/i, /\bafter that\b/i, /\band also\b/i, /^\s*\d+\./m];
const FILE_MENTION = /[\w./-]+\.(ts|tsx|js|jsx|mjs|cjs|py|go|rs|java|rb|c|cpp|h|hpp|cs|php|swift|kt|scala|sh|yaml|yml|json|toml|md|sql|css|html|vue|svelte)\b/gi;

function detectTaskType(prompt: string, reasoning: string[]): TaskType {
  for (const bucket of TASK_BUCKETS) {
    const hit = bucket.patterns.find((p) => p.test(prompt));
    if (hit) {
      reasoning.push(`task type "${bucket.taskType}" matched ${hit}`);
      return bucket.taskType;
    }
  }
  reasoning.push("no task-type keywords matched (unknown)");
  return "unknown";
}

function countMatches(prompt: string, patterns: RegExp[]): RegExp[] {
  return patterns.filter((p) => p.test(prompt));
}

export interface HeuristicOptions {
  ambiguousBand: [number, number];
}

export function classifyHeuristically(prompt: string, opts: HeuristicOptions): HeuristicResult {
  const reasoning: string[] = [];
  const taskType = detectTaskType(prompt, reasoning);

  let score = 50;
  reasoning.push("base score 50");

  const broad = countMatches(prompt, BROAD_SCOPE);
  if (broad.length > 0) {
    const delta = Math.min(broad.length * 20, 30);
    score += delta;
    reasoning.push(`broad scope (${broad.length} signal${broad.length > 1 ? "s" : ""}): +${delta}`);
  }

  const deep = countMatches(prompt, DEEP_WORK);
  if (deep.length > 0) {
    const delta = Math.min(deep.length * 15, 30);
    score += delta;
    reasoning.push(`deep-work words (${deep.length}): +${delta}`);
  }

  const sentences = prompt.split(/[.!?]+/).filter((s) => s.trim().length > 0);
  if (MULTI_STEP.some((p) => p.test(prompt)) || sentences.length >= 3) {
    score += 10;
    reasoning.push("multi-step phrasing: +10");
  }

  if (prompt.length > 800) {
    score += 20;
    reasoning.push("prompt length > 800 chars: +20");
  } else if (prompt.length > 400) {
    score += 10;
    reasoning.push("prompt length > 400 chars: +10");
  }

  const files = prompt.match(FILE_MENTION) ?? [];
  const fileCount = new Set(files.map((f) => f.toLowerCase())).size;
  if (fileCount === 1) {
    score -= 10;
    reasoning.push("single file mentioned: -10");
  } else if (fileCount >= 2 && fileCount <= 3) {
    score += 5;
    reasoning.push(`${fileCount} files mentioned: +5`);
  } else if (fileCount >= 4) {
    score += 15;
    reasoning.push(`${fileCount} files mentioned: +15`);
  }

  const small = countMatches(prompt, SMALLNESS);
  if (small.length > 0) {
    const delta = Math.min(small.length * 20, 40);
    score -= delta;
    reasoning.push(`smallness cues (${small.length}): -${delta}`);
  }

  if (prompt.length < 60) {
    score -= 10;
    reasoning.push("prompt length < 60 chars: -10");
  }

  const prior = TASK_TYPE_PRIOR[taskType];
  if (prior !== 0) {
    score += prior;
    reasoning.push(`task-type prior (${taskType}): ${prior > 0 ? "+" : ""}${prior}`);
  }

  score = Math.max(0, Math.min(100, score));
  reasoning.push(`final score ${score}`);

  const complexity: Complexity = score >= 65 ? "high" : score <= 35 ? "low" : "medium";

  const [bandLow, bandHigh] = opts.ambiguousBand;
  const ambiguous = (score >= bandLow && score <= bandHigh) || taskType === "unknown";
  if (ambiguous) {
    reasoning.push(
      taskType === "unknown"
        ? "ambiguous: task type unknown"
        : `ambiguous: score ${score} inside dead band [${bandLow}, ${bandHigh}]`,
    );
  }

  return { taskType, complexity, score, source: "heuristic", reasoning, ambiguous };
}
