# vitals — a private, local-first health tracker

Log how you're doing in one line from the terminal, then see trends and
plain-language insights computed from **your own data, on your own machine**.

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

- **Entirely local.** Everything lives in `~/.vitals` (or `$VITALS_HOME`): an
  append-only `log.jsonl` and a `config.json` for goals. Directory `0700`,
  files `0600`.
- **Zero network surface.** No outbound code, no accounts, no telemetry, no
  model calls. Insights are transparent rules and correlations you can read
  in [`internal/insights`](internal/insights/insights.go).
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
| Correlations | sleep↔mood, sleep↔energy, exercise↔mood, steps↔mood, water↔energy share ≥7 days in the last 30 and \|r\| ≥ 0.4 |
| Recurring symptom | Same symptom ≥3× in 14 days, plus its sleep contrast if ≥0.75 h |
| Resting heart rate | Up ≥5 bpm week over week |
| Weight | Change across the last 30 days |
| Streak | ≥3 consecutive days with any entry |

## Layout

```
cmd/vitals/          CLI: argument parsing, output formatting
internal/metrics/    metric registry: units, ranges, aggregation, direction
internal/store/      append-only JSONL log; atomic rewrite for deletes
internal/config/     goals (config.json)
internal/stats/      daily roll-ups, windows, sparklines, Pearson r, streaks
internal/insights/   the rules above
```

Everything below `cmd/` is a pure function of its inputs (including "now"
and the time zone), so it is fully tested without touching the clock.

## Development

```sh
make test    # go test ./...
make vet
make fmt
```

## Ideas / next steps

- Import from Apple Health / Google Fit / Oura exports
- Lagged correlations (does last night's sleep predict today's headache?)
- Medication schedules and adherence
- Optional encryption at rest
