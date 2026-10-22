# vitals-cloud

Cloud side of the vitals strap project: a Next.js app on Vercel with a Neon Postgres
database. The strap's recordings are uploaded here by a relay so the data survives
the laptop being off. The local app is `../vitals`.

- `app/api/ingest/route.ts`: `POST` stores a batch of samples, events and raw history
  records (idempotent upserts); `GET ?key=<token>` returns counts and the last sample time.
  Both need `INGEST_TOKEN`.
- `lib/schema.sql` and `scripts/migrate.mjs`: the four tables (`hr`, `events`, `records`, `device`).
- `TODO.md`: open items (relay, sync latency, metrics).

## Setup

1. Create a Vercel project and add the Neon integration (it provides `DATABASE_URL`).
2. Add a long random `INGEST_TOKEN` environment variable.
3. `vercel env pull .env.local`, then `node --env-file=.env.local scripts/migrate.mjs`.
4. `vercel deploy --prod`.

Nothing in this folder is a secret: `.env*` and `.vercel` are git-ignored.
