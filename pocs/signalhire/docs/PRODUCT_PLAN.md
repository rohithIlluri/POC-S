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
- [ ] M5 Team seats: invite by email hash, per-org rollups
- [x] M6 Recruiter workflow v1: requisition-named scans + per-account scan
      history with label counts (it2). Later: re-scan, per-req rollups
- [ ] M7 API docs page for Agency+ (POST /api/scan with X-API-Key)
- [ ] M8 Hosted deploy story (Dockerfile; Fluid Compute later)

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

## Rules for future iterations

- Never gate detection quality by tier. Gate volume/workflow only.
- No real payment processing in dev: the upgrade endpoint uses Stripe
  checkout URLs only when STRIPE_LINK_AGENCY / STRIPE_LINK_TALENT are set;
  otherwise it upgrades directly and says so in the response (dev_mode).
- Engine stays pure: subscription code lives in webapp/, never in signalhire/.
- Every milestone lands with tests green (`pytest -q`) and gates passing
  (`python eval/run.py`).
