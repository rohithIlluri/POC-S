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

The interface is deliberately plain-language — no aspect-ratio numbers or film-format jargon on screen.

- **Show what you'd miss** — the headline trick: switches to the IMAX screen and dims the bands a normal cinema would cut off, labelled *"a normal cinema cuts this off"*. It turns the abstract ratio argument into something you can point at.
- **Nothing to set up** — until you load anything, an animated scene plays on the screen (drifting clouds, birds, a boat crossing the sun path, shimmering water), painted on a canvas at the full IMAX shape. Every button visibly does something from the first second, with no video and no network.
- **Copy link** — produces a link like `?v=<video>&s=imax` that reopens the same trailer at the same screen size, so a share lands the way you saw it.
- **Black bars removed automatically** — dropped-in videos and pictures are sampled for letterboxing, and the bars are cropped so the picture genuinely fills the frame (`Trim black bars` still toggles it manually for trailers, which can't be sampled cross-origin).
- **Blocked trailers say so** — if an uploader disabled embedding, the app detects the player never came alive and shows a plain explanation with a link to open it on YouTube, instead of a black rectangle.
- **Drag and drop** a video or picture anywhere on the page, or just paste a YouTube link — it starts playing on paste, no button needed.

- **Paste a trailer, hit Watch it in IMAX** — drop in a YouTube link, hit **Play**, then the big blue **Watch it in IMAX** button: the trailer fills a full IMAX-shaped screen, fullscreen, right in your tab. Sound starts off (so autoplay works); toggle it with **Sound on/off**.
- **Screen size** — four friendly choices: **TV & laptop**, **Widescreen**, **Cinema**, **IMAX**. The screen stays a fixed width and grows taller as you go up, so IMAX visibly opens up the picture — with a plain caption like *"On IMAX you see about 67% more picture than at a normal movie theater."*
- **Trim black bars** — most trailers are uploaded as 16:9 with the film letterboxed inside; this (on by default) pushes those baked-in bars off-screen so the picture actually fills the frame. Turn it off to see the raw upload.
- **Famous IMAX films** — *The Odyssey*, *Oppenheimer*, *Sinners*, *Interstellar*, *The Dark Knight*, *Dune: Part Two*. Tap one and the screen replays that film's real IMAX moments, opening up for the IMAX scenes and settling back for the rest.
- **Use your own video** — load any local image or video; it's cropped to each screen size so you can frame your own footage in IMAX.

Until a video is loaded, the screen shows a demo scene painted procedurally on a canvas (stars up top, sea and rocks below — the parts only the taller IMAX frame keeps), so the app ships with zero binary assets.

## Layout of the code

| File | Role |
|---|---|
| `ratios.js` | Pure logic: screen/film data, equal-width area gain, frame sizing, cover-crop sizing, letterbox-bar math, YouTube URL parsing, share-link encoding. No DOM. |
| `app.js` | UI wiring, media handling (files, drops, trailers), bar detection, film-moment player, share links. |
| `scene.js` | The animated demo scene, drawn on a canvas with a static backdrop blitted per frame. Honours `prefers-reduced-motion`. |
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
