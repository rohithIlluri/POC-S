import type { Config, RoutingRule } from "./config.js";
import type { Classification, Hints, RoutingDecision, Tier, Tool } from "./types.js";

function ruleMatches(rule: RoutingRule, classification: Classification): boolean {
  const typeOk = rule.taskType === "*" || rule.taskType.includes(classification.taskType);
  const complexityOk = rule.complexity === "*" || rule.complexity.includes(classification.complexity);
  return typeOk && complexityOk;
}

function describeRule(rule: RoutingRule, index: number): string {
  const types = rule.taskType === "*" ? "*" : rule.taskType.join(",");
  const complexities = rule.complexity === "*" ? "*" : rule.complexity.join(",");
  return `rule #${index + 1} (taskType: ${types}; complexity: ${complexities}) -> ${rule.tool}/${rule.tier}`;
}

/** Map a hinted model name to the tier it occupies in the tool's config, if any. */
function tierForModel(model: string, tool: Tool, config: Config): Tier | undefined {
  const models = config.tools[tool].models;
  return (Object.keys(models) as Tier[]).find((tier) => models[tier] === model);
}

function buildArgs(template: string[], prompt: string, model: string, passthrough: string[]): string[] {
  // Single-pass substitution: a prompt containing a literal "{model}" must not
  // get the model id injected into it by a second replacement pass.
  const args = template.map((arg) =>
    arg.replace(/\{prompt\}|\{model\}/g, (token) => (token === "{prompt}" ? prompt : model)),
  );
  return [...args, ...passthrough];
}

export function route(
  classification: Classification,
  hints: Hints,
  config: Config,
  prompt: string,
  passthrough: string[] = [],
): RoutingDecision {
  let tool: Tool;
  let tier: Tier;
  let matchedRule: string;

  if (hints.tool && (hints.tier || hints.model)) {
    tool = hints.tool;
    tier = hints.tier ?? tierForModel(hints.model!, hints.tool, config) ?? "medium";
    matchedRule = "hint override (tool + model/tier)";
  } else {
    const rules = config.routing.rules;
    const index = rules.findIndex((rule) => ruleMatches(rule, classification));
    if (index >= 0) {
      const rule = rules[index];
      tool = rule.tool;
      tier = rule.tier;
      matchedRule = describeRule(rule, index);
    } else {
      tool = config.routing.default.tool;
      tier = config.routing.default.tier;
      matchedRule = `default -> ${tool}/${tier}`;
    }
    if (hints.tool && hints.tool !== tool) {
      tool = hints.tool;
      matchedRule += ` (tool overridden by hint -> ${tool})`;
    }
  }

  const toolConfig = config.tools[tool];
  const model = hints.model ?? toolConfig.models[tier];

  return {
    tool,
    tier,
    model,
    command: toolConfig.command,
    args: buildArgs(toolConfig.argsTemplate, prompt, model, passthrough),
    matchedRule,
  };
}
