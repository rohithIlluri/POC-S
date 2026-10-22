# To-do

## Sync latency (parked, low priority)
- A 10 h backlog takes ~10 min. Measured from the 2026-10-22 capture: 88% is the strap streaming (~100 packets/s, ~10 KB/s), 12% is the ack round trip (88 ms median per burst). The limit is the strap's notification rate.
- Possible small win: shave the ack path (HTTP post + one fsync per burst). Never acknowledge before the burst is stored.
- `ENTER_HIGH_FREQ_SYNC` was NOT tried. The APK suggests a 180 s smart-alarm cadence, not a faster link. Skipped by the user on 2026-10-22.
- Real fix for the wait: sync often so the backlog stays small (autopilot every 15 min, relay every few hours).

## Always-on relay (next after decoding)
- Device not chosen yet (Mac with Chrome tab + Go uploader + launchd, Android phone, or Pi).
- Pair once, reconnect with `getDevices()` / autoConnect; set the clock on every connect; flag event 13 (RTC_LOST).
- Go uploader: strapdb -> POST https://whooped.vercel.app/api/ingest (bearer INGEST_TOKEN), resumable, retries.

## Data and metrics
- DONE 2026-10-22: decoding agent finished (Round 10 in the findings file): motion, strap sleep state, battery, 25 Hz optical bursts. Sleep totals and battery are on the dashboard (/api/sleep, /api/battery).
- Still undecoded: SpO2 value, gyroscope, temperature units, several heart-record bytes. Extra strap modes (realtime IMU, optical raw) need the user's OK first.
- Sleep stages (light/deep/REM) are NOT scored: no verified coefficients. Motion sleep (5.3 h) and the strap's label (6.5 h) disagree by 1.2 h; no polysomnography to say which is closer.
- Capacity: the flash trim pointer in the console may wrap ~21:15 on 2026-10-22; sync after that to learn the ring size.
- Same-second duplicate heart records lose one RR set (~1.04 records/s, samples keyed by second).
- Stress and fitness age are still blocked (stress needs continuous RR; fitness age needs an age/sex reference table). Recovery needs several nights of baseline.
- Measure strap capacity: log GET_DATA_RANGE every sync, add a continuity report, run 24 h / 48 h no-ack trials.
- Battery is shown on the dashboard from the console log, so it is only as fresh as the last sync. Add an alert (e.g. notify below 20%) once the relay exists.

## Housekeeping
- pocs/vitals changes are uncommitted (decoder allow-list, `strap repair`, tests). Do not push unless asked.
- UI on top of Neon once the relay is feeding it.
