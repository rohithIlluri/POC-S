# vitals backend: recording, storage and metrics

## Where the data lives, and what records it

Two recorders, and only one needs your computer:

1. **The strap records by itself.** It keeps heart data in its own memory whether or
   not anything is connected. A night with the computer off is still on the strap in
   the morning.
2. **vitals downloads it.** "Sync history" asks the strap for what it recorded,
   stores it here, and only then tells the strap it may free that memory. Nothing is
   lost if the download is interrupted, because the strap is not told to free
   anything until the data is safely stored.

While the computer is on and connected, the live 1 Hz stream is also stored as it
arrives (batched every 5 seconds). That is a bonus: it fills in the moment, but
history sync is what covers the hours nothing was connected.

What this means in practice:

| Situation | What is recorded |
|---|---|
| Computer on, tab open, strap connected | Live stream stored as it arrives |
| Computer off or asleep (overnight) | The strap records on its own; nothing reaches the computer |
| Next morning, you connect and press **Sync history** | The night is downloaded, stored, then metrics are computed |

Limits to know about: the strap's memory is finite (its capacity is not known yet; the
strap reports how much is pending), so sync every day or two. And the strap's clock
must be set first (`Set strap clock`), or downloaded records cannot be dated (see below).
A strap that reboots loses its clock again.

## Day zero

Today is the first day. vitals stores nothing from before the strap's clock was valid:
a strap that lost its clock stamps data from 1971-01-01, and those records cannot be
dated. They are still kept raw (`records` bucket), so they can be re-dated later if a
reference point turns up, but they never become samples or metrics. Day zero is the
local date of the first accepted sample.

## Database

`internal/strapdb`: one embedded, pure-Go file (bbolt) at `$VITALS_HOME/strap.db`
(default `~/.vitals/strap.db`, mode 0600). No server, no cgo, no network.

| Bucket | Key | Value |
|---|---|---|
| `hr` | unix ms | source, heart rate, RR intervals |
| `events` | unix ms + event number | event data |
| `records` | record number | raw 0x2F history body (kept verbatim) |
| `days` | YYYY-MM-DD | JSON day summary (a cache; dropped when its data changes) |
| `sync` | named | last record, last token, time of last sync |
| `meta` | named | schema, day zero, profile (birth year, sex, weight) |

Keys start with big-endian time, so a day or a night is one cursor range. Writes are
idempotent (a repeated sample, event or record replaces itself).

Measured on this Mac (`go test ./internal/strapdb -bench .`):

- reading a full 8-hour night (28,800 samples): about 1.2 ms
- writing a 5-sample batch: about 38 ms (mostly the disk sync; at one batch per 5 s
  that is under 1% of the time)
- summarising one uncached day (`ingest` benchmark): about 40 ms; a cached day is microseconds

Only one vitals process can open the file at a time. A second one gets a clear error.

Restarting the server issues a new write token. The page notices the refusal, fetches the new token from `/api/csrf` and retries, so recording does not stall silently. (A page opened before this fix existed still needs one reload.)

## Pipeline

```
strap ──BLE──▶ dashboard page ──JSON──▶ local server ──▶ strapdb
   live 1 Hz samples            POST /api/samples
   history frames               POST /api/history/frames (server runs whoopble.HistorySync,
                                 stores the burst, THEN returns the ack to write back)
```

## API (loopback only; writes need the page's CSRF token and a same-origin request)

| Endpoint | What |
|---|---|
| `POST /api/samples` | store live samples (max 5,000 per call, within the last 24 h) |
| `GET /api/status` | day zero, days and nights stored, every metric's readiness |
| `GET /api/day?date=YYYY-MM-DD` | the day's summary (zones, wear time, live and overnight metrics), plus recovery when a baseline exists |
| `GET /api/trend?days=N` | the last N days of strain, resting HR and HRV, for sparklines |
| `GET /api/samples?from&to&points` | heart rate over a range, averaged into at most `points` buckets |
| `GET /api/profile`, `POST /api/profile` | date of birth (`birth_date`, gives an exact age), sex, weight |
| `GET /api/health` | database integrity check, age of the newest sample, sync position |
| `GET /api/sleep?date=YYYY-MM-DD` | the night ending on that date, from raw records: motion sleep (van Hees), the strap's own label, and `stages` (wake/light/deep/REM runs, totals, cycles, checks, caveats; a rule-based, unvalidated estimate, see `docs/sleep-staging-research.md`) |
| `GET /api/csrf` | the current write token, same-origin only; lets an open page recover after a server restart |
| `POST /api/history/start`, `/frames`, `/abort`, `GET /api/history/status` | history download |

## Days of data each metric needs

From `internal/science/readiness.go`. A metric is available when its data exists, not because an unrelated
path was verified. HRV, resting heart rate, respiratory rate and fitness read the day's own samples, so
they work from a live stream today. A "night" is a day with at least two hours of overnight data (a
stand-in until sleep detection exists).

| Metric | Provisional | Full | Needs |
|---|---|---|---|
| Strain | day 1 | 14 days | your date of birth (for max heart rate) |
| HRV | day 1 | 14 days | RR intervals from the strap; overnight is more reliable than live |
| Resting heart rate | day 1 | 14 days | five quiet minutes with the strap on; sleep gives a truer value |
| Respiratory rate | day 1 | 14 days | two minutes of unbroken RR |
| Fitness (VO2max estimate) | day 1 | 14 days | your date of birth, and a resting heart rate |
| Recovery | 7 nights | 28 nights | an overnight recording: history sync, or a night streamed live |
| Sleep | 1 night | 14 nights | history sync and verified motion data (blocked) |
| Stress | 7 days | 28 days | continuous RR and verified motion data (blocked) |
| Fitness age | 7 days | 14 days | an age/sex reference table (blocked) |

A metric that cannot run says why; the API never hides it.

## Autopilot (clock and history)

A strap with an invalid clock records nothing: its own log says "RTC timestamp is invalid; not saving data to flash".
So while a strap is connected the page runs an autopilot (on by default, switchable, remembered in the browser): every
20 seconds it checks the strap's clock from its live packets; if the clock is before 2020 or more than 5 seconds off it
sends SET_CLOCK and **verifies** it against the strap's own next packets; once the clock is right it downloads the strap's
history, and again every 15 minutes. It only ever sends the clock and history commands. Each sync reports how many of the
packets were heart-rate records, so a sync that only returned logs is never presented as a day of data.
The strap keeps its clock while it has power; if it ever loses power the next connect fixes it.

Real strap, first history (version-24 records, 96 bytes, one per second): heart rate at byte 17 and RR from byte 19 are
confirmed; RR is present for about 48% of beats.

## Dating history from a strap whose clock was never set

A strap that has never been given the time stamps its records with its boot clock (1971-01-01 plus the
time since power-on). The page reads that clock from live packets together with the wall clock and sends
both when a sync starts; the server shifts each record by the difference. This is only done where it can be
right: not for a record later than the strap's own clock now, not across a reboot (the clock goes backwards,
so the older records are on a different boot and stay undated), and not if it would land more than 60 days
back. A stale reading (over two minutes old) is ignored. Every record is stored raw regardless, and a burst
containing a record too short to store is refused, so it is never acknowledged and the strap keeps its data.
Each burst is summarised in the server log (records, how many re-dated, the time span covered).

Because of this, **sync history first, then set the clock**: history is dated using the offset while it is
still valid.

## Wear detection

The realtime packet's last two bytes are an on-wrist flag and a worn location (the official app's own
decoder reads `[18] == 0` as off-wrist and `[19]` as the location; every capture so far read `01 01`:
on the wrist). Off-wrist samples are stored (wear time is a metric) but are kept out of every
heart-rate metric, the chart and the zones. The off-wrist side has not been seen on a real strap.

## Profile

Date of birth gives an exact age (27.2, not just 27). Saving a field merges with what is stored, so
setting a sex never erases the date of birth. The profile is also copied to `$VITALS_HOME/profile.json`
(mode 0600) and restored from it if the database is ever lost or replaced. Sex is optional: it only adds
the second (Banister) load measure.

## Tested end to end in a browser

A fake WHOOP strap is injected into the page (valid frames, command responses, realtime packets with
the wrist flag, a history stream with an end marker and token). Like the real strap it ignores writes
without response, so these tests prove the page writes the right way. Every button was exercised:
Connect, Disconnect, a link dropped by the strap, Set strap clock, Sync history, Download capture,
Start workout (including the "under a minute" message), Resting check (cancel and a full run), the
sex buttons, range links, Log and delete. Tests found and fixed: a false 503 from the integrity check
under concurrent writes, a "Saving" crash from an out-of-scope helper, the short-workout message
vanishing within a second, a single-dot chart for short recordings, and off-wrist samples drawn as a
dip to zero.

## What is verified and what is not

Tested in code on real bytes: the framing, event and command-response decoding, the
history **end marker** (a real one from the public capture: 67 records, token
`2e47010004000000`), the realtime packet, and eight real history records.

From the official app's code, **not yet run against your strap**: the history start and
abort commands (empty payload), the ack layout, `SetClock`. The first real run is
supervised: press **Set strap clock**, then **Sync history**, and download the capture.

Not built yet, on purpose: sleep (the actigraphy weights could not be confirmed from a
primary source, and the motion fields are still a guess), stress (needs motion), fitness
age (needs a norms table), calories (the Keytel coefficients are unconfirmed).

## Next: recording without a browser tab

Today the Bluetooth link lives in a Chrome tab. A native background collector (a small
Go program that connects to the strap whenever it is in range, syncs history by itself
and writes to the same database) would remove that, and would collect whenever the
computer is awake without anything open. It needs a Bluetooth library for macOS (cgo
with CoreBluetooth). The database and protocol code are already shared, so it is an
addition, not a rewrite.
