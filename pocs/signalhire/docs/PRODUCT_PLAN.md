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

## Rules for future iterations

- Never gate detection quality by tier. Gate volume/workflow only.
- No real payment processing in dev: the upgrade endpoint uses Stripe
  checkout URLs only when STRIPE_LINK_AGENCY / STRIPE_LINK_TALENT are set;
  otherwise it upgrades directly and says so in the response (dev_mode).
- Engine stays pure: subscription code lives in webapp/, never in signalhire/.
- Every milestone lands with tests green (`pytest -q`) and gates passing
  (`python eval/run.py`).
