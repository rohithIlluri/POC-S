// Tiny zero-dependency static server so `npm start` works anywhere Node does.
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { extname, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL(".", import.meta.url));
const port = Number(process.env.PORT) || 4173;

const types = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".ico": "image/x-icon",
};

createServer(async (req, res) => {
  try {
    const path = normalize(decodeURIComponent(new URL(req.url, "http://x").pathname));
    const file = path === "/" ? "index.html" : path.replace(/^\/+/, "");
    const full = join(root, file);
    if (!full.startsWith(root)) throw Object.assign(new Error("forbidden"), { code: 403 });
    const body = await readFile(full);
    res.writeHead(200, { "content-type": types[extname(full)] ?? "application/octet-stream" });
    res.end(body);
  } catch (err) {
    res.writeHead(err.code === 403 ? 403 : 404, { "content-type": "text/plain" });
    res.end(err.code === 403 ? "forbidden" : "not found");
  }
}).listen(port, () => {
  console.log(`IMAX Frame → http://localhost:${port}`);
});
