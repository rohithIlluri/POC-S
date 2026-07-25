// Bundle the app into a single self-contained dist/index.html.
//
// The source stays as small ES modules (which is what the tests import), while
// what ships is one file: no module round-trips, nothing to misconfigure on a
// static host, and the whole app is one request.
//
// The "bundler" is deliberately tiny — it only has to handle this app's own
// modules, which use plain top-level `export`/`import ... from "./x.js"`.

import { readFile, writeFile, mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = dirname(fileURLToPath(import.meta.url));

// dependency order: ratios and scene define what app.js uses
const MODULES = ["ratios.js", "scene.js", "app.js"];

const IMPORT_RE = /^import\s+[\s\S]*?from\s+["'][^"']+["'];?\s*$/gm;
const EXPORT_RE = /^export\s+(?=const |let |function |class |async function )/gm;

function stripModuleSyntax(source, file) {
  const out = source.replace(IMPORT_RE, "").replace(EXPORT_RE, "");
  const leftover = out.match(/^\s*(import|export)\b.*$/m);
  if (leftover) {
    throw new Error(`${file}: unhandled module syntax → ${leftover[0].trim()}`);
  }
  return out;
}

const parts = [];
for (const file of MODULES) {
  const source = await readFile(join(root, file), "utf8");
  parts.push(`/* ---- ${file} ---- */\n${stripModuleSyntax(source, file)}`);
}
const bundle = parts.join("\n");

if (/<\/script/i.test(bundle)) {
  throw new Error("bundle contains a closing script tag and would break the page");
}

const html = await readFile(join(root, "index.html"), "utf8");
const TAG = '<script type="module" src="./app.js"></script>';
if (!html.includes(TAG)) throw new Error(`index.html no longer contains ${TAG}`);
const out = html.replace(TAG, `<script type="module">\n${bundle}\n</script>`);

await mkdir(join(root, "dist"), { recursive: true });
await writeFile(join(root, "dist/index.html"), out);
console.log(`dist/index.html — ${(out.length / 1024).toFixed(1)} kB, no external files`);
