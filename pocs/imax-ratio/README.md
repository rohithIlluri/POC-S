# imax-ratio — IMAX Frame

An immersive, zero-dependency web visualizer for theatrical aspect ratios — the thing behind every viral *"The Odyssey is shot 100% in IMAX film"* thread. A simulated cinema screen **opens up** as you switch formats, so you can *feel* the difference between 2.39:1 scope, 1.90:1 IMAX digital, and full 1.43:1 IMAX 70mm film instead of reading numbers about it.

## Why

Most movies ship in 2.39:1 anamorphic scope. True IMAX 15-perf 70mm film is 1.43:1 — nearly square — and at the same screen width it shows **+67% more picture**. Films like *Oppenheimer* and *Sinners* switch ratios mid-film, and *The Odyssey* (2026) is the first feature shot entirely with IMAX film cameras. On a 16:9 TV all of that collapses into letterboxes (see: every photo of a TV with black bars). This POC makes the geometry visceral.

## Run

```sh
cd pocs/imax-ratio
npm start          # → http://localhost:4173
```

No dependencies, no build step. Any static server works too (`python3 -m http.server`). The page needs to be served over HTTP (not `file://`) because it uses ES modules.

## What you can do

- **Watch YouTube in IMAX** — paste any YouTube link (trailers work best), hit **Load**, then **▶ Watch it in IMAX**: the video plays cropped live to the selected format, fullscreen, right in your laptop tab. Most trailers bake their letterbox bars into the 16:9 upload; the **Source** selector (16:9 native / 1.85 / 1.90 / 2.20 / 2.39 / 2.76 letterboxed) tells the app the real picture ratio so it oversizes the embed and clips the baked-in bars away. Sound is muted for autoplay; toggle it with the sound chip.
- **Format chips / keys 1–7** — snap between 1.43:1 IMAX film, 16:9, 1.85 flat, 1.90 IMAX digital, 2.20 70mm, 2.39 scope, and 2.76 Ultra Panavision. In the default *cinema wall* mode the width stays fixed and the screen grows vertically, exactly like the viral side-by-side clips.
- **Film presets** — *The Odyssey*, *Oppenheimer*, *Sinners*, *Interstellar*, *The Dark Knight*, *Dune: Part Two*. Selecting one plays its signature **ratio shift**: the frame alternates between the film's base ratio and its IMAX ratio, mimicking scene transitions (`space` toggles the shift loop).
- **Crop guides** (`g`) — dashed overlays showing where every wider format would slice the current frame, i.e. what scope viewers never see.
- **View mode** (`m`) — *cinema wall* (fixed width, screen opens up) vs *TV fit* (contain fit with letterboxing, like your display at home).
- **Your own footage** — load any image or video; it's cropped live to each format with `object-fit: cover`, so you can preview your own shots in IMAX framing.
- **Fullscreen** (`f`) + live stats: current format, **% more picture vs scope** at equal width, and **% of your actual display** each format fills.

The built-in demo scene is painted procedurally on a canvas at 1.43:1 (stars up top, sea and rocks below — the bands only IMAX keeps), so the POC ships with zero binary assets.

## Layout of the code

| File | Role |
|---|---|
| `ratios.js` | Pure logic: format/film data, letterbox coverage, equal-width area gain, frame sizing for both view modes, crop-line math, YouTube URL parsing, embed cover-crop sizing. No DOM. |
| `app.js` | UI wiring, canvas demo scene, ratio-shift player, media loading, YouTube embed player. |
| `index.html` | Markup + styles. |
| `serve.js` | 40-line static server for `npm start`. |
| `test/ratios.test.js` | `node:test` suite over everything in `ratios.js`. |

## Test

```sh
npm test
```

## The numbers (equal screen width)

| Format | Ratio | Picture vs 2.39 scope |
|---|---|---|
| IMAX 70mm film (15/70) | 1.43:1 | **+67%** |
| 16:9 | 1.78:1 | +34% |
| Flat | 1.85:1 | +29% |
| IMAX digital (laser) | 1.90:1 | +26% |
| 70mm (5-perf) | 2.20:1 | +9% |
| Anamorphic scope | 2.39:1 | baseline |
| Ultra Panavision | 2.76:1 | −13% |
