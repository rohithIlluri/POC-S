import { describe, expect, it, vi } from "vitest";
import { classify } from "../src/classifier/index.js";
import type { LlmClient } from "../src/classifier/llm.js";
import { DEFAULT_CONFIG, type Config } from "../src/config.js";

const AMBIGUOUS_PROMPT = "hmm what should we do here about the thing"; // unknown task type

function mockClient(parsedOutput: unknown): LlmClient {
  return { messages: { parse: vi.fn().mockResolvedValue({ parsed_output: parsedOutput }) } };
}

function failingClient(error: Error): LlmClient {
  return { messages: { parse: vi.fn().mockRejectedValue(error) } };
}

describe("hybrid classify", () => {
  it("returns the heuristic result without calling the LLM when confident", async () => {
    const client = mockClient(null);
    const result = await classify("fix typo in readme", DEFAULT_CONFIG, {}, { llmClient: client });
    expect(result.source).toBe("heuristic");
    expect(client.messages.parse).not.toHaveBeenCalled();
  });

  it("consults the LLM on ambiguous prompts and maps its output", async () => {
    const client = mockClient({ taskType: "feature", complexity: "high", reasoning: "multi-service change" });
    const result = await classify(AMBIGUOUS_PROMPT, DEFAULT_CONFIG, {}, { llmClient: client });
    expect(client.messages.parse).toHaveBeenCalledOnce();
    expect(result.source).toBe("llm");
    expect(result.taskType).toBe("feature");
    expect(result.complexity).toBe("high");
    expect(result.reasoning).toContainEqual(expect.stringContaining("multi-service change"));
  });

  it("degrades to heuristic-fallback when the LLM call fails", async () => {
    const client = failingClient(new Error("connect ETIMEDOUT"));
    const result = await classify(AMBIGUOUS_PROMPT, DEFAULT_CONFIG, {}, { llmClient: client });
    expect(result.source).toBe("heuristic-fallback");
    expect(result.complexity).toBe("medium");
  });

  it("keeps a confident heuristic complexity when only the task type was ambiguous", async () => {
    // Unknown task type but heavy scope/deep-work signals -> score well above the dead band.
    const prompt =
      "the entire distributed system chokes under concurrency, every request stalls across the codebase";
    const client = failingClient(new Error("connect ETIMEDOUT"));
    const result = await classify(prompt, DEFAULT_CONFIG, {}, { llmClient: client });
    expect(result.taskType).toBe("unknown");
    expect(result.source).toBe("heuristic-fallback");
    expect(result.complexity).toBe("high");
  });

  it("forces medium on degrade only for dead-band scores", async () => {
    // feature task, score 50 -> inside [42, 58]
    const prompt = "implement pagination for the users list endpoint in the admin panel";
    const client = failingClient(new Error("connect ETIMEDOUT"));
    const result = await classify(prompt, DEFAULT_CONFIG, {}, { llmClient: client });
    expect(result.source).toBe("heuristic-fallback");
    expect(result.complexity).toBe("medium");
    expect(result.reasoning).toContainEqual(expect.stringContaining("defaulting to medium"));
  });

  it("degrades when the LLM returns unparseable output", async () => {
    const client = mockClient({ nonsense: true });
    const result = await classify(AMBIGUOUS_PROMPT, DEFAULT_CONFIG, {}, { llmClient: client });
    expect(result.source).toBe("heuristic-fallback");
  });

  it("skips the LLM when llmFallback is disabled", async () => {
    const config: Config = {
      ...DEFAULT_CONFIG,
      classifier: { ...DEFAULT_CONFIG.classifier, llmFallback: false },
    };
    const client = mockClient(null);
    const result = await classify(AMBIGUOUS_PROMPT, config, {}, { llmClient: client });
    expect(result.source).toBe("heuristic");
    expect(client.messages.parse).not.toHaveBeenCalled();
  });

  it("skips the LLM when any hint is present", async () => {
    const client = mockClient(null);
    const result = await classify(AMBIGUOUS_PROMPT, DEFAULT_CONFIG, { tool: "codex" }, { llmClient: client });
    expect(client.messages.parse).not.toHaveBeenCalled();
    expect(result.source).toBe("heuristic");
  });

  it("skips classification entirely for conclusive hints", async () => {
    const client = mockClient(null);
    const result = await classify("whatever", DEFAULT_CONFIG, { tool: "claude", tier: "high" }, { llmClient: client });
    expect(client.messages.parse).not.toHaveBeenCalled();
    expect(result.reasoning).toContainEqual(expect.stringContaining("hint override"));
  });
});
