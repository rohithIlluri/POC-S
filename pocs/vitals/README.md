# vitals — a private, local-first health tracker

Log how you're doing in one line from the terminal, **sync your WHOOP**, then
see trends and plain-language insights — in the terminal or a local web
dashboard — computed from **your own data, on your own machine**.

```console
$ vitals log sleep 7.5 --note "woke up rested"
$ vitals log mood 4
$ vitals log steps 9200
$ vitals log symptom headache 3
$ vitals log med vitamin-d
$ vitals insights
Insights
  ! Averaging 6.7 h of sleep over the last 7 days (6 nights logged). Most adults need 7–9 h.
  ! headache logged 4× in the last 14 days (avg severity 3.0/5). If it persists or worsens, talk to a clinician.
  · steps goal (>= 8000 steps) met on 7 of 13 logged days in the last 14 days.
  · On days with more sleep, your mood tends to be higher (r = +0.97 over 13 days). Correlation, not causation.
  · On headache days you slept 5.8 h on average vs 7.3 h on other days.
  ✓ 14-day logging streak — consistent data makes every other insight sharper.
```

> vitals is a personal log, **not medical advice**. For health concerns, talk to a clinician.

## Why it's private

- **Local storage.** Everything lives in `~/.vitals` (or `$VITALS_HOME`): an
  append-only `log.jsonl`, `config.json` for goals, and `whoop.json` for WHOOP
  credentials. Directory `0700`, files `0600`.
- **One network destination, on request.** The only outbound calls go to
  `api.prod.whoop.com`, and only when you connect, sync or disconnect WHOOP.
  No telemetry, no other accounts, no model calls. Insights are transparent
  rules and correlations you can read in
  [`internal/insights`](internal/insights/insights.go).
- **Read-only WHOOP access.** vitals requests only `read:*` scopes (plus
  `offline` for a refresh token) and never writes to WHOOP.
- **Bluetooth strap data stays local.** Live heart rate goes strap →
  browser → the local vitals server; no WHOOP account or network involved.
- **Local-only dashboard.** `vitals serve` binds loopback only (it refuses
  `0.0.0.0`), rejects non-localhost `Host` headers (DNS rebinding), requires a
  CSRF token on every POST, and ships a strict CSP with no inline script.
- **Zero dependencies.** Go standard library only.
- **Your data stays portable.** `vitals export` dumps everything as CSV or
  JSON. The CSV export defuses spreadsheet formula injection (cells starting
  with `= + - @` are prefixed with `'`).

## Install

```sh
cd pocs/vitals
make build          # → bin/vitals
make install        # → $GOBIN/vitals
```

Requires Go 1.24+.

## Use your strap directly (no WHOOP membership)

The dashboard talks to a heart-rate strap straight from the browser via Web
Bluetooth — no account, no API, no cloud.

**WHOOP 4.0 — direct mode.** vitals speaks the strap's own Bluetooth
protocol: it asks the strap for its realtime stream (the same non-persistent
command the WHOOP app sends when you start an activity) and decodes heart
rate plus up to four beat-to-beat (RR) intervals per second. The WHOOP app
and your WHOOP account are not involved, and nothing is changed on the
strap: Disconnect sends the matching stop command.

**Anything else** (chest straps, watches, or a WHOOP with *Broadcast Heart
Rate* on) works through the standard Bluetooth Heart Rate service.

1. Charge the strap and **close the WHOOP app** (it holds the connection).
2. `vitals serve --open` in **Chrome or Edge** (desktop or Android — Safari
   and iOS have no Web Bluetooth) → **Connect strap** → pick it. The status
   line says `WHOOP direct` or `heart-rate broadcast`.
3. Then:
   - **Resting check · 2 min** — sit still; saves resting `heartrate`, and
     `hrv` (RMSSD) when beat-to-beat intervals arrive.
   - **Start workout / Stop & save** — saves `exercise` minutes, noted with
     average and max bpm.

The page only records raw samples; the maths is in
[`internal/hrv`](internal/hrv/hrv.go) — artifact filtering (impossible
intervals, >30% from the median, >20% beat-to-beat jumps), RMSSD from ≥ 30
clean beats, resting HR from RR when available or the median bpm otherwise.
Entries are tagged `strap`.

### How the WHOOP protocol support was built

[`internal/whoopble`](internal/whoopble/whoopble.go) implements framing,
both checksums (CRC-8/SMBUS header, CRC-32 body), the realtime-stream
command, and decoders for realtime and history records. It was written for
vitals from the packet captures published in
[Reverse Engineering Whoop 4.0 for fun and FREEDOM](https://github.com/bWanShiTong/reverse-engineering-whoop-post):
the checksum parameters were derived from those captures, and the tests
check every builder and decoder against them byte for byte (including
history records re-paired from the write-up by matching their CRCs).
[OpenStrap](https://github.com/OpenStrap) — a full subscription-free WHOOP
app — was consulted for protocol facts and safety guidance only; it is
AGPL-3.0, so none of its code is copied here.

Safety: the only command vitals can build is realtime on/off. The generic
command builder is unexported, so the strap's erase, flash-trim, reboot,
unpair and persistent-setting commands cannot be constructed.

**Not yet:** downloading the history stored on the strap (sleep, overnight
HRV). The record format is decoded and tested, but a sync must set the
strap's clock and acknowledge batches in a firmware-specific way — and an
acknowledgement lets the strap free that data — so it will only ship once
it can be verified against a real strap.

## Connect WHOOP (API, needs a membership)

WHOOP data comes through the official
[WHOOP Developer Platform](https://developer.whoop.com) (API v2), which needs
your own (free) developer app — that's what lets vitals talk to WHOOP
directly, with no third-party service in between.

1. Sign in at **developer.whoop.com** with your WHOOP account → **Create app**.
   - Redirect URL: `http://localhost:8737/whoop/callback`
   - Scopes: `read:recovery`, `read:cycles`, `read:sleep`, `read:workout`,
     `read:profile`, `read:body_measurement` (and `offline`, if listed)
2. Save the app's credentials (the secret is prompted for, so it stays out of
   your shell history):
   ```sh
   vitals whoop setup --client-id <your-client-id>
   ```
   `WHOOP_CLIENT_ID` / `WHOOP_CLIENT_SECRET` / `WHOOP_REDIRECT_URL` env vars
   work too and override the saved values.
3. Connect and sync — from the terminal:
   ```sh
   vitals whoop login      # opens the browser, catches the callback on localhost:8737
   vitals whoop sync       # first sync pulls 30 days; later syncs are incremental
   ```
   or from the browser: `vitals serve --open` → **Connect WHOOP** → **Sync now**.

On a machine without a browser (SSH, a server), use `vitals whoop login
--paste`: open the printed URL anywhere, approve, then paste back the address
the browser lands on (the page itself won't load — that's expected).

| WHOOP record | becomes | stamped at |
|---|---|---|
| Sleep (scored) | `sleep` (light + deep + REM, h), `sleepperf` (%), `resprate` | wake time |
| Nap | `sleep` (note "nap") — naps add to the day | wake time |
| Recovery | `recovery` (%), `hrv` (ms), `heartrate` (resting), `spo2`, `skintemp` | that night's wake time |
| Cycle | `strain` (0–21; note "in progress" until the day ends) | cycle start |
| Workout | `exercise` (minutes, note = sport) | workout start |

Synced entries carry IDs derived from WHOOP's record IDs, so **sync is
idempotent**: re-running never duplicates, and records WHOOP rescores (or
today's strain as it climbs) are updated in place. Each sync re-reads the
last 3 days to catch those. Records WHOOP hasn't scored yet are skipped and
picked up next time. Tokens refresh automatically (WHOOP rotates the refresh
token each time; the new one is saved). `vitals whoop logout` revokes access
at WHOOP and forgets the tokens; synced entries stay in your log.

## Web dashboard

```sh
vitals serve --open      # http://localhost:8737
```

Latest-value tiles (recovery with WHOOP's green/yellow/red zone), insights,
a quick-log form, one trend chart per metric over 7/30/90 days (hover for
daily values; the dashed line is the period average), and the last 7 days of
entries with one-click removal. Light and dark themes follow your system.
Works without JavaScript; JS only adds chart hover.

## Usage

| Command | What it does |
|---|---|
| `vitals log <metric> <value>` | Log a measurement — `vitals log sleep 7.5` |
| `vitals log symptom <name> [1-5]` | Log a symptom with severity (default 3) |
| `vitals log med <name> [doses]` | Log a medication / supplement (default 1 dose) |
| &nbsp;&nbsp;`--note "text"` | Attach a note |
| &nbsp;&nbsp;`--at <when>` | Backdate: `yesterday`, `07:30`, `2026-10-20`, `2026-10-20T07:30` |
| `vitals` / `vitals today` | Today's entries and your logging streak |
| `vitals list [--days 7] [--metric m]` | Recent entries, with ids |
| `vitals trends [--days 30]` | Per-metric avg / min / max, week-over-week change, sparkline |
| `vitals insights` | Observations from your data (see below) |
| `vitals goal [<metric> <target>]` | Show or set daily goals — `vitals goal steps 8000`, `vitals goal weight <=75` |
| `vitals goal <metric> --clear` | Remove a goal |
| `vitals metrics` | What you can track |
| `vitals export [--format csv\|json]` | Everything, to stdout |
| `vitals undo` | Remove the most recently *written* entry (even if backdated) |
| `vitals delete <id>` | Remove a specific entry |
| `vitals whoop` | WHOOP connection status / setup steps |
| `vitals whoop setup\|login\|sync\|logout` | See [Connect WHOOP](#connect-whoop) |
| `vitals serve [--addr localhost:8737] [--open]` | Local web dashboard |

Flags can go anywhere on the line.

### Metrics

| Metric | Unit | Range | A day's value is… |
|---|---|---|---|
| `sleep` | h | 0–24 | sum (naps add up) |
| `steps` | steps | 0–200000 | sum |
| `exercise` (`workout`) | min | 0–1440 | sum |
| `water` | L | 0–20 | sum |
| `mood` | 1–5 | 1–5 | mean |
| `energy` | 1–5 | 1–5 | mean |
| `heartrate` (`hr`, `rhr`) | bpm | 20–250 | mean |
| `weight` | kg | 1–500 | latest reading |
| `recovery` | % | 0–100 | mean |
| `hrv` | ms (RMSSD) | 0–500 | mean |
| `strain` | 0–21 | 0–21 | latest reading |
| `sleepperf` (`sleepscore`) | % | 0–100 | mean |
| `resprate` | rpm | 0–60 | mean |
| `spo2` | % | 50–100 | mean |
| `skintemp` | °C | 20–45 | mean |
| `symptom <name>` | severity 1–5 | 1–5 | event, counted per name |
| `med <name>` | doses | 0–100 | event, counted per name |

Days are local calendar days. Log sleep on the morning you wake up and it
lines up with that day's mood, energy and symptoms.

### Trends

```
  metric     days  avg         min   max    week vs prior  trend
  sleep      13    6.8 h       5     8.5    ▽ -0.3         ▃▆▂▇▅▄█▁▅▆▃▇▄·
  steps      13    7692 steps  3000  12000  ▽ -1131        ▃▆▂▇▅▄█▁▅▆▃▆▄·
  mood       13    3.6/5       2     5      ▽ -0.2         ▃▆▁█▆▃█▁▆▆▃█▃·
```

`▲`/`▼` is a move in a neutral or good direction, `△`/`▽` a move the wrong way
for that metric (e.g. less sleep, higher resting heart rate). Week-over-week
compares the last 7 days with the 7 before, and only when both weeks have at
least 3 days of data. `·` in a sparkline is a day with nothing logged.

### Insights

Each rule is deliberately simple and says what it looked at:

| Rule | Fires when |
|---|---|
| Sleep average | ≥3 nights logged in the last 7 days; flags < 7 h |
| Goal progress | A goal is set; share of logged days in the last 14 that met it |
| Correlations | sleep↔mood, sleep↔energy, exercise↔mood, steps↔mood, water↔energy, sleep↔recovery, recovery↔mood, recovery↔energy share ≥7 days in the last 30 and \|r\| ≥ 0.4 |
| Recovery zone | 7-day average recovery < 34% (red) or ≥ 67% (green) |
| HRV drop | HRV down ≥ 15% week over week |
| Recurring symptom | Same symptom ≥3× in 14 days, plus its sleep contrast if ≥0.75 h |
| Resting heart rate | Up ≥5 bpm week over week |
| Weight | Change across the last 30 days |
| Streak | ≥3 consecutive days with any entry |

## Layout

```
cmd/vitals/          CLI: argument parsing, output formatting, whoop + serve commands
internal/metrics/    metric registry: units, ranges, aggregation, direction
internal/entry/      input validation shared by the CLI and the web form
internal/store/      append-only JSONL log; idempotent upsert; atomic rewrites
internal/config/     goals (config.json)
internal/stats/      daily roll-ups, windows, sparklines, Pearson r, streaks
internal/insights/   the rules above
internal/whoop/      WHOOP OAuth, API client (refresh, pagination, 429 backoff), record → entry mapping
internal/web/        the dashboard: server, view model, SVG charts, strap sessions, embedded assets
internal/hrv/        RR-interval cleaning, RMSSD, resting HR, workout summary
internal/whoopble/   WHOOP 4.0 Bluetooth protocol: framing, checksums, realtime command, decoders
internal/fsutil/     atomic 0600 file writes
```

Everything below `cmd/` takes "now", the time zone and (for WHOOP) the API
base URL as inputs, so it is fully tested without the clock or the network:
the WHOOP flow — browser login, paste login, token refresh and rotation,
rate limits, pagination, rescoring — runs against an in-process fake WHOOP.

## Development

```sh
make test    # go test ./...
make vet
make fmt
```

## Ideas / next steps

- Import from Apple Health / Google Fit / Oura exports
- WHOOP webhooks for push instead of pull (needs a public endpoint)
- Lagged correlations (does last night's sleep predict today's headache?)
- Medication schedules and adherence
- Optional encryption at rest
