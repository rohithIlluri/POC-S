import { readFileSync } from "node:fs";
import { neon } from "@neondatabase/serverless";

const sql = neon(process.env.DATABASE_URL);
const stmts = readFileSync(new URL("../lib/schema.sql", import.meta.url), "utf8")
  .split(";")
  .map((s) => s.replace(/^\s*--.*$/gm, "").trim())
  .filter(Boolean);
for (const s of stmts) await sql.query(s);
console.log(`applied ${stmts.length} statements`);
