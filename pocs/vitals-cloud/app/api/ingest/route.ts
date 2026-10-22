import { timingSafeEqual } from "node:crypto";
import { sql } from "@/lib/db";

type Sample = { t: number; bpm: number; rr?: number[]; src?: string; off?: boolean };
type Event = { t: number; n: number; data?: string };
type Rec = { n: number; body: string };
type Payload = {
  device?: { id: string; name?: string; address?: string; bonded?: boolean };
  samples?: Sample[];
  events?: Event[];
  records?: Rec[];
};

const MAX_ROWS = 20000;

function authorized(req: Request, query = false) {
  const want = process.env.INGEST_TOKEN;
  const got =
    req.headers.get("authorization")?.replace(/^Bearer /, "") ??
    (query ? new URL(req.url).searchParams.get("key") : null) ??
    "";
  if (!want || got.length !== want.length) return false;
  return timingSafeEqual(Buffer.from(got), Buffer.from(want));
}

export async function POST(req: Request) {
  if (!authorized(req)) return new Response("unauthorized", { status: 401 });

  let p: Payload;
  try {
    p = await req.json();
  } catch {
    return new Response("bad json", { status: 400 });
  }
  const samples = p.samples ?? [];
  const events = p.events ?? [];
  const records = p.records ?? [];
  if (samples.length + events.length + records.length > MAX_ROWS) {
    return new Response("batch too large", { status: 413 });
  }

  const db = sql();
  const q = [];
  // One statement per table, unnested from parallel arrays: idempotent and one round trip each.
  if (samples.length) {
    q.push(db.query(
      `INSERT INTO hr (ts_ms, bpm, rr, source, off_wrist)
       SELECT t, b, COALESCE(string_to_array(NULLIF(r, ''), ',')::int[], '{}'), s, o
       FROM unnest($1::bigint[], $2::smallint[], $3::text[], $4::text[], $5::boolean[]) AS x(t, b, r, s, o)
       ON CONFLICT (ts_ms) DO UPDATE SET bpm = EXCLUDED.bpm, rr = EXCLUDED.rr,
         source = EXCLUDED.source, off_wrist = EXCLUDED.off_wrist`,
      [
        samples.map((s) => s.t),
        samples.map((s) => s.bpm),
        samples.map((s) => (s.rr ?? []).join(",")),
        samples.map((s) => s.src ?? ""),
        samples.map((s) => !!s.off),
      ],
    ));
  }
  if (events.length) {
    q.push(db.query(
      `INSERT INTO events (ts_ms, num, data)
       SELECT * FROM unnest($1::bigint[], $2::int[], $3::text[])
       ON CONFLICT (ts_ms, num) DO UPDATE SET data = EXCLUDED.data`,
      [events.map((e) => e.t), events.map((e) => e.n), events.map((e) => e.data ?? "")],
    ));
  }
  if (records.length) {
    q.push(db.query(
      `INSERT INTO records (num, body)
       SELECT * FROM unnest($1::bigint[], $2::text[])
       ON CONFLICT (num) DO UPDATE SET body = EXCLUDED.body`,
      [records.map((r) => r.n), records.map((r) => r.body)],
    ));
  }
  if (p.device?.id) {
    q.push(db.query(
      `INSERT INTO device (id, name, address, bonded_at, last_seen)
       VALUES ($1, $2, $3, CASE WHEN $4 THEN now() END, now())
       ON CONFLICT (id) DO UPDATE SET
         name = COALESCE(NULLIF(EXCLUDED.name, ''), device.name),
         address = COALESCE(NULLIF(EXCLUDED.address, ''), device.address),
         bonded_at = COALESCE(device.bonded_at, EXCLUDED.bonded_at),
         last_seen = now()`,
      [p.device.id, p.device.name ?? "", p.device.address ?? "", !!p.device.bonded],
    ));
  }
  await Promise.all(q);

  return Response.json({ ok: true, samples: samples.length, events: events.length, records: records.length });
}

// Browser-friendly status: open /api/ingest?key=<token>. Counts and times only, no health data.
export async function GET(req: Request) {
  if (!authorized(req, true)) return new Response("unauthorized", { status: 401 });
  const [hr] = await sql()`SELECT count(*)::int AS rows, min(ts_ms) AS first_ms, max(ts_ms) AS last_ms FROM hr`;
  const [ev] = await sql()`SELECT count(*)::int AS rows FROM events`;
  const [rc] = await sql()`SELECT count(*)::int AS rows FROM records`;
  const devices = await sql()`SELECT id, name, bonded_at, last_seen FROM device ORDER BY last_seen DESC`;
  const iso = (ms: unknown) => (ms ? new Date(Number(ms)).toISOString() : null);
  return Response.json(
    { hr: { rows: hr.rows, first: iso(hr.first_ms), last: iso(hr.last_ms) }, events: ev.rows, records: rc.rows, devices },
    { headers: { "cache-control": "no-store" } },
  );
}
