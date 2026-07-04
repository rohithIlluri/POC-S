import { z } from "zod";
import type AnthropicSdk from "@anthropic-ai/sdk";
import type { Config } from "../config.js";
import type { Complexity, TaskType } from "../types.js";

const LlmClassificationSchema = z.object({
  taskType: z.enum(["architecture", "refactor", "debug", "feature", "quick-edit", "boilerplate", "script", "docs"]),
  complexity: z.enum(["low", "medium", "high"]),
  reasoning: z.string(),
});

export interface LlmClassification {
  taskType: TaskType;
  complexity: Complexity;
  reasoning: string;
}

/** Narrow structural view of the Anthropic client, so tests can inject a mock. */
export interface LlmClient {
  messages: {
    parse(params: unknown, options?: { timeout?: number }): Promise<{ parsed_output: unknown }>;
  };
}

export type LlmResult = { ok: true; value: LlmClassification } | { ok: false; error: string };

const SYSTEM_PROMPT =
  "You classify coding prompts for a routing system. " +
  "Given a user's coding request, respond with its task type and complexity. " +
  "complexity: low = trivial edits/boilerplate, medium = standard feature work, high = deep multi-file/architectural work.";

export async function classifyWithLlm(prompt: string, config: Config, client?: LlmClient): Promise<LlmResult> {
  // The Anthropic SDK is a heavy dependency; load it only when the fallback
  // actually fires (never for dry-runs or confident heuristic routing).
  let Anthropic: typeof AnthropicSdk | undefined;
  let outputFormat: unknown;
  let llm: LlmClient;

  if (client) {
    llm = client;
    outputFormat = { type: "json_schema" }; // unused by injected mocks
  } else {
    if (!process.env.ANTHROPIC_API_KEY) {
      return { ok: false, error: "ANTHROPIC_API_KEY not set; skipping LLM classifier" };
    }
    const sdk = await import("@anthropic-ai/sdk");
    const { zodOutputFormat } = await import("@anthropic-ai/sdk/helpers/zod");
    Anthropic = sdk.default;
    outputFormat = zodOutputFormat(LlmClassificationSchema);
    llm = new Anthropic() as unknown as LlmClient;
  }

  try {
    const res = await llm.messages.parse(
      {
        model: config.classifier.model,
        max_tokens: 256,
        system: SYSTEM_PROMPT,
        messages: [{ role: "user", content: prompt }],
        output_config: { format: outputFormat },
      },
      { timeout: config.classifier.timeoutMs },
    );
    const parsed = LlmClassificationSchema.safeParse(res.parsed_output);
    if (!parsed.success) {
      return { ok: false, error: "LLM returned unparseable classification" };
    }
    return { ok: true, value: parsed.data };
  } catch (err) {
    if (Anthropic && err instanceof Anthropic.AuthenticationError) {
      return { ok: false, error: "Anthropic auth failed (set ANTHROPIC_API_KEY or disable classifier.llmFallback)" };
    }
    if (Anthropic && err instanceof Anthropic.RateLimitError) {
      return { ok: false, error: "Anthropic rate limit hit; using heuristic result" };
    }
    return { ok: false, error: `LLM classifier failed: ${(err as Error).message}` };
  }
}
