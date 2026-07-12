# pitstop

Your car's dashboard shows you the gas gauge. It doesn't show you that your
tires have ~100 miles of life left, or that your oil change was due last
week. Pitstop puts every maintenance item in one place, tells you what's
coming due — in miles *and* days — and helps you find somewhere nearby to
fix it.

## Features

- **Unified maintenance dashboard** — oil, tires, brakes, filters, coolant,
  battery, wipers, and any custom item you add, color-coded OK / due soon /
  overdue.
- **Two ways to track mileage** — log odometer readings by hand, or start a
  GPS trip and let the app measure the distance and add it to your odometer
  automatically.
- **Mileage *and* time-based alerts** — items can have a mile interval, a
  month interval, or both; the dashboard shows whichever is more urgent, plus
  a projected days-remaining estimate based on your recent driving pace.
- **Browser notifications** — opt in and get notified when something crosses
  into "due soon" or "overdue" (only while the app is open — see
  [Limitations](#limitations)).
- **Nearby shops** — on request, looks up nearby tire shops and auto repair
  shops via the free OpenStreetMap Overpass API and links out for directions.
- **Installable, offline-first PWA** — add it to your home screen; the app
  shell works offline (the Shops tab needs a connection).

## Quick start

```bash
npm install
npm start   # serves app/ at http://localhost:8080
```

Or point any static file server at `app/`. Geolocation, notifications, and
the service worker all require a **secure context** — `localhost` is fine,
`file://` is not.

### Install as an app

Open the site in a mobile browser and use "Add to Home Screen" (iOS Safari)
or the install prompt (Chrome/Edge/Android). It launches full-screen like a
native app.

## Architecture

No build step, no runtime dependencies — plain ES modules served as-is.

```
app/
├── index.html, manifest.webmanifest, sw.js   # PWA shell
├── css/app.css
└── js/
    ├── lib/   pure logic, fully unit tested, no DOM/globals
    │   ├── schedule.js   default maintenance schedule
    │   ├── store.js      localStorage-backed data model (storage injected)
    │   ├── status.js     due/overdue math + miles-per-day projection
    │   ├── geo.js        haversine + GPS trip-distance reducer
    │   ├── overpass.js   nearby-shops query builder/parser
    │   └── notify.js     which notifications should fire, and when
    └── ui/    thin DOM modules built on top of lib/
```

Data lives in `localStorage` under the key `pitstop:v1`: settings, odometer
readings, maintenance items, and a per-item "last notified status" latch so
notifications only fire on worsening, not on every page load.

The service worker (`sw.js`) precaches the app shell with a versioned cache
name (`pitstop-vN`, bump it on any shell file change) and serves cache-first
with a network fallback; cross-origin requests (like Overpass lookups) pass
straight through.

## Testing & linting

```bash
npm test   # node --test over app/js/lib/**
npm run lint
```

Only the pure `lib/` logic is unit tested (status math, GPS filtering,
Overpass parsing, store behavior, notification latching) — `ui/` is thin
DOM glue, verified manually in a browser.

## Limitations

- Geolocation, the service worker, and notifications all require HTTPS or
  `localhost`.
- Trip mode only tracks while the tab is open and the device is unlocked
  (best-effort screen wake lock); iOS in particular suspends
  `watchPosition` when the screen locks or Safari backgrounds. There's no
  background/push notification server in this POC, so alerts only fire
  while the app is open.
- GPS trip distance is an estimate — expect it to run a few percent off a
  real odometer, even with the accuracy/jitter/speed filtering in `geo.js`.
- All data is device-local; there's no sync or export yet.
