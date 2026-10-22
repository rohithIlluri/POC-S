#!/usr/bin/env python3
"""Continuity report for the strap's stored heart-rate samples (read-only, loopback API).

usage: check-night.py [--port 8739] [--since "2026-10-21 21:16:37"] [--until now]
Times are local. Reports first/last sample, coverage, and every gap longer than --gap seconds.
"""
import argparse, datetime as dt, json, urllib.request

ap = argparse.ArgumentParser()
ap.add_argument("--port", type=int, default=8739)
ap.add_argument("--since", default="2026-10-21 21:16:37")
ap.add_argument("--until", default="now")
ap.add_argument("--gap", type=int, default=5)
a = ap.parse_args()

fmt = "%Y-%m-%d %H:%M:%S"
since = dt.datetime.strptime(a.since, fmt).astimezone()
until = dt.datetime.now().astimezone() if a.until == "now" else dt.datetime.strptime(a.until, fmt).astimezone()
span = int((until - since).total_seconds())
ms = lambda d: int(d.timestamp() * 1000)
# one bucket per second; the API caps points per request, so ask in one-hour chunks
t = []
for c0 in range(0, span, 3600):
    c1 = min(c0 + 3600, span)
    lo, hi = since + dt.timedelta(seconds=c0), since + dt.timedelta(seconds=c1)
    url = f"http://localhost:{a.port}/api/samples?from={ms(lo)}&to={ms(hi)}&points={c1 - c0}"
    t += [x // 1000 for x in json.load(urllib.request.urlopen(url))["t"]]
t = sorted(set(t))
if not t:
    raise SystemExit("no samples in that window")
loc = lambda s: dt.datetime.fromtimestamp(s).strftime("%H:%M:%S")
gaps = [(t[i], t[i + 1]) for i in range(len(t) - 1) if t[i + 1] - t[i] > a.gap]
print(f"window   {since:%H:%M:%S} -> {until:%H:%M:%S}  ({span} s)")
print(f"samples  {len(t)}   first {loc(t[0])}   last {loc(t[-1])}")
print(f"coverage {100 * len(t) / span:.1f}% of the window")
print(f"gaps > {a.gap}s: {len(gaps)}")
for g0, g1 in gaps[:40]:
    print(f"  {loc(g0)} -> {loc(g1)}  ({g1 - g0} s missing)")
