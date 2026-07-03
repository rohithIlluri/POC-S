import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { ConfigError, DEFAULT_CONFIG, deepMerge, loadConfig, validateConfig } from "../src/config.js";

const tempDirs: string[] = [];
function tempFile(name: string, content: string): string {
  const dir = mkdtempSync(join(tmpdir(), "ccr-test-"));
  tempDirs.push(dir);
  const path = join(dir, name);
  writeFileSync(path, content);
  return path;
}
afterEach(() => {
  while (tempDirs.length) rmSync(tempDirs.pop()!, { recursive: true, force: true });
});

describe("loadConfig", () => {
  it("returns defaults when no config file exists", () => {
    expect(loadConfig(undefined)).toEqual(DEFAULT_CONFIG);
  });

  it("errors on an explicit path that does not exist", () => {
    expect(() => loadConfig("/nonexistent/ccr.yaml")).toThrow(ConfigError);
  });

  it("parses a sparse YAML config and deep-merges over defaults", () => {
    const path = tempFile("ccr.yaml", "tools:\n  codex:\n    models:\n      low: my-mini-model\n");
    const config = loadConfig(path);
    expect(config.tools.codex.models.low).toBe("my-mini-model");
    expect(config.tools.codex.models.high).toBe(DEFAULT_CONFIG.tools.codex.models.high);
    expect(config.tools.claude).toEqual(DEFAULT_CONFIG.tools.claude);
  });

  it("treats an empty config file as no overrides", () => {
    const path = tempFile("ccr.yaml", "");
    expect(loadConfig(path)).toEqual(DEFAULT_CONFIG);
  });

  it("treats a comments-only config file as no overrides", () => {
    const path = tempFile("ccr.yaml", "# nothing here yet\n# more comments\n");
    expect(loadConfig(path)).toEqual(DEFAULT_CONFIG);
  });

  it("parses a JSON config", () => {
    const path = tempFile("ccr.json", JSON.stringify({ classifier: { llmFallback: false } }));
    expect(loadConfig(path).classifier.llmFallback).toBe(false);
    expect(loadConfig(path).classifier.model).toBe(DEFAULT_CONFIG.classifier.model);
  });

  it("rejects invalid values with the issue path", () => {
    const path = tempFile("ccr.yaml", "routing:\n  default:\n    tool: emacs\n    tier: medium\n");
    expect(() => loadConfig(path)).toThrow(/routing\.default\.tool/);
  });

  it("rejects non-array rules", () => {
    const path = tempFile("ccr.yaml", "routing:\n  rules: nope\n");
    expect(() => loadConfig(path)).toThrow(ConfigError);
  });
});

describe("deepMerge", () => {
  it("replaces arrays wholesale", () => {
    const merged = deepMerge({ a: [1, 2, 3], b: { c: 1 } }, { a: [9] });
    expect(merged).toEqual({ a: [9], b: { c: 1 } });
  });

  it("merges nested objects", () => {
    const merged = deepMerge({ a: { x: 1, y: 2 } }, { a: { y: 3 } });
    expect(merged).toEqual({ a: { x: 1, y: 3 } });
  });
});

describe("validateConfig", () => {
  it("accepts the default config", () => {
    expect(validateConfig(DEFAULT_CONFIG)).toEqual(DEFAULT_CONFIG);
  });

  it("rejects unknown keys", () => {
    expect(() => validateConfig({ ...DEFAULT_CONFIG, extra: true })).toThrow(ConfigError);
  });
});
