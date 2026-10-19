# Tap — MVP

A social network where the only way to connect is to physically tap phones. This POC is the backend, the tap protocol and a two-phone browser demo of the MVP described in the [design doc](https://claude.ai/artifact/PUtRvRD1RmEU9RvBGcD7LF) (design tab + build plan tab).

## Run it

```bash
cd pocs/tap
go run ./backend/cmd/tapd -dev        # in-memory store, open http://localhost:8080
```

With Postgres (data, accounts and receipts survive restarts):

```bash
go run ./backend/cmd/tapd -gen-secrets > tap-secrets.env   # once; keep it with your DB credentials
. ./tap-secrets.env
DATABASE_URL='postgres://user:pass@localhost:5432/tap?sslmode=disable' go run ./backend/cmd/tapd -dev
```

Migrations in `backend/internal/store/migrations/` apply on start. `TAP_PHONE_SALT` finds returning users by phone hash and `TAP_RECEIPT_KEY` keeps old receipts verifiable, so both must stay the same for the life of the database; `tapd` refuses to start against a database without them. On SIGTERM it finishes in-flight claims and drains the event queue before exiting.

The demo shows two phones side by side. Each one is a separate client with its own non-extractable WebCrypto device key, talking to the real API. Sign both in, press **Hold phones together**, then try:

- dragging the distance to 400 cm (a relay attack) — both sides are refused with `too_far`;
- **Simulate a meetup** — five simulated attendees tap Phone 1, an event forms, and its recap card appears under Events;
- **Simulate a Props week** — the attendees answer their weekly polls; open the Props tab on either phone to answer polls yourself, see what you were picked for, and share the weekly card;
- Remove / Block in People, or Delete account under You.

Headless end-to-end run, including a venue-peak burst:

```bash
go run ./backend/cmd/tapsim -burst 500
```

```
ok    happy path                       Alice saw Bob, Bob saw Alice, 3ms
ok    replay rejected                  spent nonce refused (nonce_used)
ok    relay across rooms rejected      4 m measurement refused (too_far)
ok    meetup forms an event            Met 5 humans at STL Go Meetup. Zero bots.
ok    burst of 500 simultaneous taps   500 taps in 644ms, p50 538ms, p99 625ms
```

## What a tap is

1. Each phone fetches a single-use nonce (valid 20 s) and makes an ephemeral X25519 key.
2. The phones swap nonces and ephemeral keys over the short-range channel and derive a shared session key.
3. Each phone signs a claim with its device key: both nonces, both ephemeral keys, measured distance, time since its nonce, geohash-7, channel.
4. `POST /v1/tap/claim` verifies the signature, nonce, distance and clock, then holds the first claim up to 10 s for its partner.
5. When the mirror claim arrives, the server checks both describe one handshake (same keys, measurements within 2 s on the server's clock, same ~1 km cell, no block), writes the tap and edge in one transaction, and returns a signed receipt to both phones.
6. After commit, an event worker (the Pub/Sub consumer of the design) assigns the tap to an event. Keeping this out of the claim transaction matters: every tap at a venue lands in the same cell, and doing it inline made a 200-tap burst on Postgres conflict until claims timed out.

Retries are idempotent on the nonce pair, so a dropped response never makes a second edge. On Postgres every transaction is SERIALIZABLE and retried on conflict; a contract test runs 16 concurrent check-then-insert transactions and requires exactly one tap. The exact bytes are in [`protocol/`](protocol/protocol.go) and pinned by [`protocol/testdata/vectors.json`](protocol/testdata/vectors.json), which the Go and JS implementations both pass.

## API

| Endpoint | Purpose |
| --- | --- |
| `POST /v1/auth/otp/start`, `POST /v1/auth/otp/verify` | Phone sign-in (dev mode returns the code) |
| `POST /v1/devices` | Register a device key + attestation |
| `POST /v1/tap/nonce` | 20 s single-use nonce |
| `POST /v1/tap/claim` | Signed claim; waits for the partner, returns `matched` + receipt or `expired` |
| `GET /v1/receipt-key` | Public key that signs receipts |
| `GET /v1/people` | Connections grouped by event or month met |
| `PUT /v1/people/{id}/note`, `PUT /v1/people/{id}/place` | Private note, hide where you met |
| `DELETE /v1/people/{id}` | Silent removal |
| `POST /v1/blocks`, `POST /v1/reports` | Safety; 3 distinct reporters freeze tapping |
| `GET /v1/events`, `PUT /v1/events/{id}` | Events (5 taps nearby within an hour), name once |
| `GET /v1/events/{id}/recap`, `…/recap.svg` | Recap headline and 1080×1920 story card |
| `GET /v1/props/round` | This week's 12 polls (unlocks at 5 connections) |
| `POST /v1/props/vote` | Pick one of a poll's options, or skip |
| `GET /v1/props/inbox` | What you were picked for: prompt, date, event hint; never the voter |
| `GET /v1/props/card`, `…/card.svg` | Weekly card ("N people I've actually met picked me") at story size |
| `GET /props` | Where a shared card's link lands: "You can't send Props online. Meet them." |
| `GET /v1/me`, `GET /v1/me/export`, `DELETE /v1/me` | Profile, data export, deletion |

## Props

tbh-style compliment polls where only people you have tapped can pick you ([design section](https://claude.ai/artifact/PUtRvRD1RmEU9RvBGcD7LF)). The safety rules are enforced in [`props.go`](backend/internal/server/props.go) and pinned by tests:

- prompts come only from a curated, positive-only list; there is no free text anywhere;
- every pick is a real vote; nothing is generated (the FTC's 2024 NGL case was about fake ones);
- only active connections appear as options or count on a card, so a removal or block drops someone out in both directions at once, and deleting an account takes its votes with it;
- Props unlocks at 5 connections, picks show a date (never a time), and an event hint ("someone from DevFest") appears only when that event holds 5+ of your connections;
- no response carries the voter — a test checks the inbox, card and card image for the voter's id and name.

## Real vs. stubbed

| Piece | In this POC | For the pilot |
| --- | --- | --- |
| Claim matching, receipts, replay/relay/clock/location checks, rate limits | Real | Same code |
| Device keys and signatures | Real (P-256, raw r‖s) | Secure Enclave / StrongBox keys |
| Attestation | `dev:` tokens accepted | App Attest, Key Attestation + Play Integrity |
| SMS OTP | Code returned in dev mode | A verification provider |
| Storage | Postgres (SERIALIZABLE + retry) or in-memory, behind `store.Tx` | Cloud SQL Postgres, Spanner adapter later |
| Receipt key | `TAP_RECEIPT_KEY` env (generated in-memory mode) | Cloud KMS |
| Event queue | In-process worker, drained on SIGTERM | Pub/Sub with at-least-once delivery |
| Short-range radio | Simulated (demo slider, `sim` package) | BLE + ultrasonic, UWB in the native apps |
| Props (tbh-style polls) | Real: curated prompts, real votes only, unlock at 5, removal/block/deletion honoured | Instagram Sharing to Stories + Snapchat Creative Kit in the apps |

## Layout

```
protocol/   claim + receipt encoding, signatures, session keys, geohash, vectors
backend/    cmd/tapd (API + demo), cmd/tapsim (scenarios), internal/server, internal/store (memory, postgres, migrations)
sim/        virtual phones used by tests, tapsim and CI
web/        browser demo + protocol.js (checked against the Go vectors)
```

## Tests

```bash
gofmt -l . && go vet ./... && go test -race ./...   # unit, server, e2e scenarios (in-memory)
TAP_TEST_PG='postgres://postgres:postgres@localhost:5432/tap_test?sslmode=disable' \
  go test -race ./...                               # the same suites on Postgres, a fresh schema per test
node web/protocol.test.mjs                          # JS protocol vs Go vectors
TAP_UPDATE_VECTORS=1 go test ./protocol             # regenerate vectors (changes the signature only)
```
