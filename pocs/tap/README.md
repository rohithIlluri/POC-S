# Tap — MVP

A social network where the only way to connect is to physically tap phones. This POC is the backend, the tap protocol and a two-phone browser demo of the MVP described in the [design doc](https://claude.ai/artifact/PUtRvRD1RmEU9RvBGcD7LF) (design tab + build plan tab).

## Run it

```bash
cd pocs/tap
go run ./backend/cmd/tapd -dev        # open http://localhost:8080
```

The demo shows two phones side by side. Each one is a separate client with its own non-extractable WebCrypto device key, talking to the real API. Sign both in, press **Hold phones together**, then try:

- dragging the distance to 400 cm (a relay attack) — both sides are refused with `too_far`;
- **Simulate a meetup** — five simulated attendees tap Phone 1, an event forms, and its recap card appears under Events;
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

Retries are idempotent on the nonce pair, so a dropped response never makes a second edge. The exact bytes are in [`protocol/`](protocol/protocol.go) and pinned by [`protocol/testdata/vectors.json`](protocol/testdata/vectors.json), which the Go and JS implementations both pass.

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
| `GET /v1/me`, `GET /v1/me/export`, `DELETE /v1/me` | Profile, data export, deletion |

## Real vs. stubbed

| Piece | In this POC | For the pilot |
| --- | --- | --- |
| Claim matching, receipts, replay/relay/clock/location checks, rate limits | Real | Same code |
| Device keys and signatures | Real (P-256, raw r‖s) | Secure Enclave / StrongBox keys |
| Attestation | `dev:` tokens accepted | App Attest, Key Attestation + Play Integrity |
| SMS OTP | Code returned in dev mode | A verification provider |
| Storage | In-memory, behind `store.Tx` | Postgres adapter, then Spanner |
| Receipt key | Generated at start | Cloud KMS |
| Short-range radio | Simulated (demo slider, `sim` package) | BLE + ultrasonic, UWB in the native apps |
| Props (tbh-style polls) | Not in the MVP | After the pilot gate, with Moments |

## Layout

```
protocol/   claim + receipt encoding, signatures, session keys, geohash, vectors
backend/    cmd/tapd (API + demo), cmd/tapsim (scenarios), internal/server, internal/store
sim/        virtual phones used by tests, tapsim and CI
web/        browser demo + protocol.js (checked against the Go vectors)
```

## Tests

```bash
gofmt -l . && go vet ./... && go test -race ./...   # unit, server, e2e scenarios
node web/protocol.test.mjs                          # JS protocol vs Go vectors
TAP_UPDATE_VECTORS=1 go test ./protocol             # regenerate vectors (changes the signature only)
```
