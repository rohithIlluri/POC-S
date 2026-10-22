import { neon } from "@neondatabase/serverless";

const make = () => neon(process.env.DATABASE_URL!);
let _sql: ReturnType<typeof make> | null = null;

export function sql() {
  if (!_sql) _sql = make();
  return _sql;
}
