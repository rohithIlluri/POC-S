import { readFileSync, existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { z } from "zod";
import { parse as parseYaml } from "yaml";
import { TASK_TYPES, COMPLEXITIES, TOOLS, TIERS } from "./types.js";

const taskTypeEnum = z.enum(TASK_TYPES);
const complexityEnum = z.enum(COMPLEXITIES);
const toolEnum = z.enum(TOOLS);
const tierEnum = z.enum(TIERS);

const ToolConfigSchema = z
  .object({
    command: z.string(),
    argsTemplate: z.array(z.string()),
    models: z.object({ high: z.string(), medium: z.string(), low: z.string() }),
    installHint: z.string(),
  })
  .strict();

const RuleSchema = z
  .object({
    taskType: z.union([z.literal("*"), z.array(taskTypeEnum)]),
    complexity: z.union([z.literal("*"), z.array(complexityEnum)]),
    tool: toolEnum,
    tier: tierEnum,
  })
  .strict();
export type RoutingRule = z.infer<typeof RuleSchema>;

const ConfigSchema = z
  .object({
    classifier: z
      .object({
        llmFallback: z.boolean(),
        model: z.string(),
        timeoutMs: z.number().positive(),
        ambiguousBand: z.tuple([z.number(), z.number()]),
      })
      .strict(),
    tools: z.object({ claude: ToolConfigSchema, codex: ToolConfigSchema }).strict(),
    routing: z
      .object({
        rules: z.array(RuleSchema),
        default: z.object({ tool: toolEnum, tier: tierEnum }).strict(),
      })
      .strict(),
  })
  .strict();
export type Config = z.infer<typeof ConfigSchema>;

export const DEFAULT_CONFIG: Config = {
  classifier: {
    llmFallback: true,
    model: "claude-haiku-4-5",
    timeoutMs: 5000,
    ambiguousBand: [42, 58],
  },
  tools: {
    claude: {
      command: "claude",
      argsTemplate: ["-p", "{prompt}", "--model", "{model}"],
      models: {
        high: "claude-opus-4-8",
        medium: "claude-sonnet-5",
        low: "claude-haiku-4-5",
      },
      installHint: "npm install -g @anthropic-ai/claude-code",
    },
    codex: {
      command: "codex",
      argsTemplate: ["exec", "{prompt}", "-m", "{model}"],
      models: {
        high: "gpt-5.2-codex",
        medium: "gpt-5.2-codex",
        low: "gpt-5.1-codex-mini",
      },
      installHint: "npm install -g @openai/codex",
    },
  },
  routing: {
    rules: [
      { taskType: ["architecture", "refactor", "debug"], complexity: ["high"], tool: "claude", tier: "high" },
      { taskType: ["architecture", "refactor", "debug"], complexity: ["medium"], tool: "claude", tier: "medium" },
      { taskType: ["feature"], complexity: "*", tool: "claude", tier: "medium" },
      { taskType: ["quick-edit", "boilerplate", "script", "docs"], complexity: "*", tool: "codex", tier: "low" },
      { taskType: "*", complexity: ["high"], tool: "claude", tier: "high" },
      { taskType: "*", complexity: ["low"], tool: "codex", tier: "low" },
    ],
    default: { tool: "claude", tier: "medium" },
  },
};

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Recursively merge `override` onto `base`. Arrays and scalars replace wholesale. */
export function deepMerge<T>(base: T, override: unknown): T {
  if (!isPlainObject(base) || !isPlainObject(override)) {
    return (override === undefined ? base : override) as T;
  }
  const out: Record<string, unknown> = { ...base };
  for (const [key, value] of Object.entries(override)) {
    out[key] = deepMerge((base as Record<string, unknown>)[key], value);
  }
  return out as T;
}

export class ConfigError extends Error {}

function parseConfigFile(path: string): unknown {
  const raw = readFileSync(path, "utf8");
  try {
    return path.endsWith(".json") ? JSON.parse(raw) : parseYaml(raw);
  } catch (err) {
    throw new ConfigError(`Failed to parse config file ${path}: ${(err as Error).message}`);
  }
}

function discoverConfigPath(explicitPath?: string): string | undefined {
  if (explicitPath) {
    if (!existsSync(explicitPath)) {
      throw new ConfigError(`Config file not found: ${explicitPath}`);
    }
    return explicitPath;
  }
  const candidates = [
    join(process.cwd(), "ccr.config.yaml"),
    join(process.cwd(), "ccr.config.json"),
    join(homedir(), ".config", "ccr", "config.yaml"),
  ];
  return candidates.find((p) => existsSync(p));
}

export function validateConfig(merged: unknown): Config {
  const result = ConfigSchema.safeParse(merged);
  if (!result.success) {
    const issues = result.error.issues
      .map((i) => `  ${i.path.join(".") || "(root)"}: ${i.message}`)
      .join("\n");
    throw new ConfigError(`Invalid config:\n${issues}`);
  }
  return result.data;
}

export function loadConfig(explicitPath?: string): Config {
  const path = discoverConfigPath(explicitPath);
  if (!path) return DEFAULT_CONFIG;
  // An empty or comments-only file parses to null; treat it as "no overrides".
  const user = parseConfigFile(path) ?? {};
  return validateConfig(deepMerge<unknown>(DEFAULT_CONFIG, user));
}
