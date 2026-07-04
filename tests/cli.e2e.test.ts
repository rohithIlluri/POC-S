import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const CLI = join(import.meta.dirname, "..", "dist", "cli.js");

function ccr(...args: string[]): string {
  // Built by the pretest hook; env stripped of ANTHROPIC_API_KEY so the LLM
  // fallback can never fire during tests.
  const env = { ...process.env };
  delete env.ANTHROPIC_API_KEY;
  return execFileSync("node", [CLI, ...args], { encoding: "utf8", env });
}

describe("ccr CLI (compiled, dry-run)", () => {
  it("routes a forced Anthropic model to claude even when the matrix picks codex", () => {
    const out = ccr("--dry-run", "--model", "claude-opus-4-8", "fix typo in readme");
    expect(out).toContain("tool:    claude");
    expect(out).toContain("--model claude-opus-4-8");
    expect(out).not.toContain("codex exec");
  });

  it("routes a tiny docs fix to codex low tier", () => {
    const out = ccr("--dry-run", "fix typo in README");
    expect(out).toContain("tool:    codex");
    expect(out).toContain("gpt-5.1-codex-mini");
  });

  it("passes args after -- through to the child command", () => {
    const out = ccr("--dry-run", "add endpoint for user metrics", "--", "--allowed-tools", "Bash");
    expect(out).toContain("--allowed-tools Bash");
  });

  it("rejects an invalid --tool", () => {
    expect(() => ccr("--dry-run", "--tool", "emacs", "do a thing")).toThrow();
  });

  it("emits machine-readable JSON with --dry-run --json", () => {
    const out = ccr("--dry-run", "--json", "fix typo in README");
    const parsed = JSON.parse(out);
    expect(parsed.tool).toBe("codex");
    expect(parsed.model).toBe("gpt-5.1-codex-mini");
    expect(Array.isArray(parsed.args)).toBe(true);
    expect(parsed.classification.taskType).toBe("docs");
  });

  it("reports the package version", () => {
    const pkg = JSON.parse(
      execFileSync("node", ["-e", "console.log(JSON.stringify(require('./package.json')))"], {
        encoding: "utf8",
        cwd: join(import.meta.dirname, ".."),
      }),
    );
    expect(ccr("--version").trim()).toBe(pkg.version);
  });
});
