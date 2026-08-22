# SignalHire — product plan (ralph-loop state)

The algorithm is the product. Detection quality is identical on every tier —
we sell volume, workflow and the compounding signature DB, never "better
detection for richer customers" (that would poison trust in the labels).

Positioning: the AI-slop job boards run one-shot AI screens; we run
population-level forensics that get stronger with every batch scanned.

## Tiers (subscription)

| | Scout (free) | Agency ($149/mo) | Talent Cloud ($499/mo) |
|---|---|---|---|
| Scans / month | 5 | 200 | unlimited |
| Files / scan | 25 | 200 | 500 |
| Detection | full engine | full engine | full engine |
| Cross-scan population memory | 90 days | 90 days | 90 days |
| JD mirroring | yes | yes | yes |
| Sensitivity slider | yes | yes | yes |
| JSON export / API access | — | yes | yes |
| Signature DB | seeds + weekly | seeds + weekly | + custom collection runs |
| Seats | 1 | 5 | unlimited |

Demo mode: no account → 1 scan of ≤5 files, results watermarked "demo".

## Milestones

- [x] M0 Phase-0 engine (PR #28) + review fixes + multi-format parsing
- [x] M1 Web MVP: upload → triage dashboard (webapp/)
- [x] M2 Subscription backbone: accounts, API keys, tier gating, usage
      metering (SQLite, stdlib), demo mode (it1)
- [x] M3 Pricing page + signup/key management + plan/usage in the UI (it1)
- [x] M4 Billing: Stripe checkout links behind env config, webhook
      (X-Webhook-Secret) to flip tiers on checkout.session.completed (it2)
- [x] M5 Team seats: owner-invited seats gated by tier (1/5/unlimited),
      org-wide quota metering, seat upgrades upgrade the org (it3)
- [x] M6 Recruiter workflow v1: requisition-named scans + per-account scan
      history with label counts (it2). Later: re-scan, per-req rollups
- [x] M7 API section on the site (curl example, /docs schema link) (it3)
- [x] M8 Deploy story: Dockerfile + volume-backed SQLite + env-armed
      billing; signup rate limiting; API key rotation (it4)
- [x] M9 Population memory: an account's scans accumulate into one
      cross-scan population, so a farm that trickles is still a farm (it10)

## Iteration log

- it1 (done): PRODUCT_PLAN.md; webapp/store.py (users/scans/quotas, SQLite);
  tier gating + demo mode in /api/scan (402 + upgrade_required); signup/me/
  upgrade/pricing endpoints; pricing cards + signup dialog + account chip +
  demo watermark + gated JSON download in the UI; 8 tests in
  tests/test_subscription.py; verified live (signup → scout → dev upgrade →
  agency, quota shown in header, "Current plan" state on cards).
  Next (it2): M4 Stripe webhook stub + M6 scan history per account.
- it2 (done): ALGORITHM — analyzer F (contact.py): CONTACT_COLLISION
  (STRONG, risk code: same email/phone hash under different names, catches
  recycled contact infrastructure even with unique bodies) +
  DISPOSABLE_CONTACT (WEAK, throwaway-mail domains, clear-text domain only);
  BATCH_TIMESTAMP_CLUSTER (WEAK: ≥5 distinct applicants generated in one
  10-min window); stable cluster ids via union-find over verified pairs
  (closes the skipped review finding); producer matches deduped to the
  strongest per direction (no more double-counting Puppeteer+HeadlessChrome).
  Corpus attack set +2 contact-collision docs, recall still 100%, humans 0%.
  PRODUCT — billing webhook (503 unset / 401 bad secret / flips tier);
  req-named scans; /api/history + Recent Scans panel. 77 tests.
  Next (it3): M5 seats, M7 API docs page, evasion-hardening pass on layout
  fingerprints (perturbation-resistant secondary hash).
- it3 (done): ALGORITHM — loose_fingerprint (font/size profile, no
  x-buckets/page count): survives margin nudges and added runs; used only
  for KNOWN_TEMPLATE matching (never swarm counting, never allowlisting);
  collector proposes layout_hash_loose entries behind the same human-corpus
  gate. PRODUCT — M5 seats (users.owner_id one level deep; invite owner-only,
  seat caps 1/5/unlimited; org-wide monthly metering; member upgrade
  upgrades the org; Team panel with invite in UI) + M7 API section.
  80 tests. Remaining: M8 deploy story (Dockerfile), per-req rollups,
  email delivery for invites, Stripe links when real keys exist.
- it4 (done): ALGORITHM — analyzer G (boilerplate.py): SHARED_BOILERPLATE
  phrase-swarm detection over 8-word shingles shared by ≥4 distinct
  applicants; catches paraphrase farms below the MinHash 0.8 threshold
  (verified E2E: 5 rewritten docs, dedupe silent, fraction 0.52). WEAK at
  25% shared, STRONG at 60%. Humans still 0% flagged. PRODUCT — M8
  Dockerfile/.dockerignore; per-IP signup throttle (429, env-tunable);
  POST /api/rotate-key. 85 tests, gates green.
  Product is feature-complete for the MVP definition. Remaining backlog:
  invite email delivery, per-req rollups, real Stripe keys, hosted deploy.
- it5 (done): ADVERSARIAL EVAL — new wrapper_evasion corpus set (metadata-
  stripped PDFs + HTML-laundered farm output) with its own gate (floor 50%).
  Two engine fixes it forced: (1) source-collapsed idf — df counted over
  distinct layout groups, so a 50-doc farm can't vote its own mirrored
  vocabulary into commonness and blind the JD mirror; (2) weak-convergence
  scoring — weak signals from ≥4 independent analyzer families below the
  review line escalate to mass_generated (a weak pile from ≤3 families still
  caps at needs_review). Stripped PDFs: 0% → 100% flagged. HTML-laundered:
  still miss at trickle scale (2 families only) — honest gap, gate at 50%.
  PRODUCT — per-req rollups (org-scoped, label totals accumulate) + JD
  memory (datalist recall in UI). 89 tests, 5 gates green.
  Next: raise evasion floor as detection improves; invite email delivery;
  real Stripe keys; hosted deploy.
- it6 (done): ALGORITHM — synthetic text-format layouts (plaintext/html/
  rtf/odt/doc) excluded from fingerprinting and swarm counting: 30 pasted
  ATS text bodies share one parser-made structure and were one batch away
  from a mass TEMPLATE_SWARM false positive (latent FP bug, now tested).
  Boilerplate industrial escalation: fraction ≥0.35 with median ≥15 owners
  is STRONG (a study group converges with classmates, not 15 strangers) —
  HTML-laundered farm docs moved genuine → needs_review; every evasion doc
  now reaches the review queue. PRODUCT — self-contained HTML triage report
  as an Agency+ export (report_html on /api/scan, Download report button).
  92 tests, 5 gates green.
  Frontier: HTML-launder full flagging (needs a 3rd family at trickle
  scale), invite email, Stripe keys, hosted deploy.
- it7 (done): PERF — profile-driven 2.6x: MinHash update_batch (was 65%
  of runtime at one update per shingle), per-scan Context caches (JD
  terms/ngrams once, identity-masked bodies shared across dedupe/shingle
  index/boilerplate, contact handles pre-indexed to O(1) per doc).
  300 docs 2.41s → 0.91s; 500-doc Talent Cloud ceiling 1.53s. No
  behavioral change; 92 tests + 5 gates identical.
  Remaining work is operator-gated (Stripe keys, deploy target, email
  provider) or research (3rd family for trickle-scale laundering).
- it8 (done): SECURITY, pre-payments — API keys hashed at rest (sha256;
  clear key exists only in the signup/rotate response), with an in-place
  migration for pre-hashing DBs; webhook secret compared with
  hmac.compare_digest; total-batch byte cap (SIGNALHIRE_MAX_BATCH_MB,
  413); nosniff / DENY / no-referrer headers. The live dev DB caught a
  migration bug the single-row test missed (UNIQUE + NOT NULL on the old
  column) — fixed and covered with a two-row test. 95 tests, gates green.
- it9 (done): ALGORITHM — correlation-aware evidence combination
  (evidence.py). The seed scoring summed score_impact, which double-counts
  measurably correlated signals: JD_MIRROR_HIGH and JD_PHRASE_LIFT co-occur
  at Jaccard 1.00 on the corpus, as do BATCH_TIMESTAMP_CLUSTER and
  FRESH_GENERATION. Signals now combine in log-odds — each carries a
  likelihood ratio from its severity tier, the strongest in a family counts
  fully and each additional one is discounted 0.45^rank, and families combine
  undiscounted because four families agreeing is four observations. Output is
  a posterior, which is what makes effort_score comparable across reqs and
  signature-DB versions. Two invariants had to be taught explicitly: no
  evidence scores exactly 100 (the prior is a starting point, never a penalty
  for existing), and exculpatory evidence can cancel suspicion but never beat
  clean. Evasion recall 50% -> 100%; humans still 0%. Thresholds re-derived
  from the corpus distribution rather than guessed against the old additive
  scale. 101 tests, 5 gates.
- it10 (done): ALGORITHM — analyzer H (recurrence) + memory.py, the
  cross-scan population memory. Every population analyzer we have is a
  *batch* analyzer, and the batch size is the farm's choice: delivered two
  per scan, the corpus's own evasion documents scored genuine with nothing
  said about them at all (measured: 0 of 30 caught). Documents now leave
  behind one-way keys — MinHash bands of the masked body, layout hash,
  hashed contact handles, a bottom-k phrase sketch — and the next scan asks
  what it has seen. RECURRING_IDENTITY / RECURRING_CONTACT are the
  cross-scan counterparts of the in-batch risk codes; RECURRING_BODY,
  RECURRING_TEMPLATE (WEAK always, like TEMPLATE_SWARM) and RECURRING_PHRASES
  carry effort. The phrase escalation *imports* the in-batch industrial rule
  rather than restating it, so the two cannot drift: a third of a document
  shared with fifteen strangers is the same fact whether they arrived in one
  batch or over three months. Trickle recall 0% -> 100% once the memory holds
  a population (14/30 overall across a cold start), human false flags 0%.
  New corpus set + two gates, run as a delivered-batch-by-batch sequence with
  a no-memory control alongside. Counting is an aggregate query, not a fetch:
  the hot phrase of a 5,000-document farm must not make the engine slowest
  against the adversary it exists for (192 docs against a 5,000-doc history:
  0.44s vs 0.38s with no memory at all). PRODUCT — org-scoped SQLite memory
  (every seat shares one, since one farm hits the whole agency), 90-day
  retention, never gated by tier, never written by demo scans; the dashboard
  and the exported report both state how much history a scan was compared
  against, because "genuine" and "we have not seen this rig yet" must not
  look the same. 128 tests, 7 gates green.
  Next: cross-tenant memory needs a consent and contract story before the
  keys can be shared; invite email; Stripe keys; hosted deploy.

## Rules for future iterations

- Never gate detection quality by tier. Gate volume/workflow only.
- No real payment processing in dev: the upgrade endpoint uses Stripe
  checkout URLs only when STRIPE_LINK_AGENCY / STRIPE_LINK_TALENT are set;
  otherwise it upgrades directly and says so in the response (dev_mode).
- Engine stays pure: subscription code lives in webapp/, never in signalhire/.
- Every milestone lands with tests green (`pytest -q`) and gates passing
  (`python eval/run.py`).
- Detection that accumulates state about applicants earns its false-positive
  tests first: a re-scanned batch, one candidate applying to many reqs, and a
  scan never being part of the population it is judged against are all pinned
  in tests/test_memory.py. Add the equivalent before adding memory of
  anything else.
- Never lower a threshold to make an eval pass. it10's phrase escalation is
  the in-batch rule imported, not a new number chosen to clear a gate; where
  the honest answer was "the engine cannot catch this yet" (the cold start),
  the gate scopes to the warm slice and the report prints both.
- post-loop: split into two PRs — #28 (Phase-0 engine + review fixes) and
  #29 (the product build, stacked on #28). CI on 3.11 then exposed a real
  engine bug the local 3.13-only runs could not see: scores were computed
  in binary floating point, so weights summing to exactly 1.1 put the raw
  score on 39.5 and landed on either side of it depending on interpreter —
  the same resume scoring 39 on 3.11 and 40 on 3.12. Now accumulated in
  Decimal, which is what the versioned-signature audit promise requires.
  Lesson for future weight tuning: never assert a hard-coded score at a
  .5 boundary; assert the property.
