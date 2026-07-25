// Pure aspect-ratio math + presentation-format data for the IMAX ratio visualizer.
// No DOM, no dependencies — everything here is unit-testable in Node.

/**
 * Theatrical presentation formats, ordered tallest → widest.
 * `ratio` is width / height.
 */
export const FORMATS = [
  {
    id: "imax143",
    name: "IMAX 70mm film",
    label: "1.43:1",
    ratio: 1.43,
    blurb: "15-perf 65mm film — the full-height frame The Odyssey is shot in",
  },
  {
    id: "tv",
    name: "16:9 TV",
    label: "1.78:1",
    ratio: 16 / 9,
    blurb: "Your living-room display",
  },
  {
    id: "flat",
    name: "Flat widescreen",
    label: "1.85:1",
    ratio: 1.85,
    blurb: "Standard theatrical flat",
  },
  {
    id: "imax190",
    name: "IMAX digital",
    label: "1.90:1",
    ratio: 1.9,
    blurb: "IMAX with Laser — Dune: Part Two plays here edge to edge",
  },
  {
    id: "seventy",
    name: "70mm (5-perf)",
    label: "2.20:1",
    ratio: 2.2,
    blurb: "Classic 70mm — Oppenheimer's base frame",
  },
  {
    id: "scope",
    name: "Anamorphic scope",
    label: "2.39:1",
    ratio: 2.39,
    blurb: "The widescreen most movies ship in",
  },
  {
    id: "ultrapan",
    name: "Ultra Panavision",
    label: "2.76:1",
    ratio: 2.76,
    blurb: "Ben-Hur wide — Sinners' second format",
  },
];

/** Films with famous IMAX presentations / mid-film ratio shifts. */
export const FILMS = [
  {
    id: "odyssey",
    title: "The Odyssey",
    year: 2026,
    base: 1.43,
    imax: 1.43,
    note: "First feature shot entirely with IMAX film cameras — every frame fills 1.43:1",
  },
  {
    id: "oppenheimer",
    title: "Oppenheimer",
    year: 2023,
    base: 2.2,
    imax: 1.43,
    note: "70mm base frame opening to full IMAX for key sequences",
  },
  {
    id: "sinners",
    title: "Sinners",
    year: 2025,
    base: 2.76,
    imax: 1.43,
    note: "Ultra Panavision 2.76 snapping to 1.43 — the wildest shift yet",
  },
  {
    id: "interstellar",
    title: "Interstellar",
    year: 2014,
    base: 2.39,
    imax: 1.43,
    note: "Over an hour of IMAX 15/70 photography",
  },
  {
    id: "darkknight",
    title: "The Dark Knight",
    year: 2008,
    base: 2.39,
    imax: 1.43,
    note: "The film that started IMAX-sequence mania",
  },
  {
    id: "dune2",
    title: "Dune: Part Two",
    year: 2024,
    base: 2.39,
    imax: 1.9,
    note: "Presented 1.90 top-to-bottom in IMAX, scope everywhere else",
  },
];

/** Baseline for "how much more picture" comparisons: anamorphic scope. */
export const BASELINE_RATIO = 2.39;

/**
 * The four screen choices the app actually shows people, in plain language.
 * Deliberately free of ratio numbers and format jargon — `ratio` is internal.
 */
export const SCREENS = [
  { key: "tv", name: "TV & laptop", ratio: 16 / 9 },
  { key: "wide", name: "Widescreen", ratio: 1.85 },
  { key: "cinema", name: "Cinema", ratio: BASELINE_RATIO },
  { key: "imax", name: "IMAX", ratio: 1.43, hero: true },
];

export function screenByKey(key) {
  return SCREENS.find((s) => s.key === key) ?? null;
}

/** Nearest friendly screen to an arbitrary frame shape. */
export function nearestScreen(ratio) {
  if (!(ratio > 0)) throw new RangeError("ratio must be positive");
  return SCREENS.reduce((best, s) =>
    Math.abs(s.ratio - ratio) < Math.abs(best.ratio - ratio) ? s : best
  );
}

export function formatById(id) {
  const f = FORMATS.find((f) => f.id === id);
  if (!f) throw new Error(`unknown format: ${id}`);
  return f;
}

/**
 * Fraction of a container's area a source fills when letterboxed/pillarboxed
 * into it ("contain" fit). 1 means a perfect fit; symmetric in its arguments.
 */
export function fitFraction(sourceRatio, containerRatio) {
  if (sourceRatio <= 0 || containerRatio <= 0) {
    throw new RangeError("ratios must be positive");
  }
  return Math.min(sourceRatio / containerRatio, containerRatio / sourceRatio);
}

/**
 * Extra picture area a format shows versus a baseline ratio when both are
 * projected at the same width — the cinema-screen "opens up" number.
 * moreVsBaseline(1.43) ≈ 0.67 → "+67% picture vs scope".
 */
export function moreVsBaseline(ratio, baseline = BASELINE_RATIO) {
  if (ratio <= 0 || baseline <= 0) throw new RangeError("ratios must be positive");
  return baseline / ratio - 1;
}

/**
 * Pixel dimensions for the on-page frame.
 *
 * mode "cinema": width is locked to what lets the tallest format (1.43) fill
 * the stage, so switching formats keeps width constant and the screen opens
 * vertically — the viral IMAX comparison.
 *
 * mode "fit": plain contain-fit, like a TV shows each ratio.
 */
export function frameSize(stageW, stageH, ratio, mode = "cinema", tallest = 1.43) {
  if (stageW <= 0 || stageH <= 0 || ratio <= 0) {
    throw new RangeError("dimensions and ratio must be positive");
  }
  const width =
    mode === "cinema"
      ? Math.min(stageW, stageH * tallest)
      : Math.min(stageW, stageH * ratio);
  return { width, height: width / ratio };
}

/**
 * Size an embedded media element (e.g. a 16:9 YouTube iframe) so the actual
 * picture inside it covers the frame — the manual version of
 * `object-fit: cover` for media we can't crop directly.
 *
 * `pictureRatio` is the real picture's ratio; when it differs from
 * `mediaRatio` (the element's own ratio) the source has baked-in letterbox or
 * pillarbox bars, and the element is oversized so those bars fall outside the
 * frame and get clipped.
 */
export function coverSize(frameW, frameH, pictureRatio, mediaRatio = pictureRatio) {
  if (frameW <= 0 || frameH <= 0 || pictureRatio <= 0 || mediaRatio <= 0) {
    throw new RangeError("dimensions and ratios must be positive");
  }
  const pictureW = Math.max(frameW, frameH * pictureRatio);
  const width =
    pictureRatio >= mediaRatio ? pictureW : (pictureW / pictureRatio) * mediaRatio;
  return { width, height: width / mediaRatio };
}

/**
 * Extract the 11-character video id from a YouTube URL (watch, youtu.be,
 * shorts, embed, live) or from a bare id. Returns null when unrecognized.
 */
export function parseYouTubeId(input) {
  const s = String(input ?? "").trim();
  const ID = /^[\w-]{11}$/;
  if (ID.test(s)) return s;
  let url;
  try {
    url = new URL(s);
  } catch {
    return null;
  }
  const host = url.hostname.replace(/^(www|m|music)\./, "");
  if (host === "youtu.be") {
    const id = url.pathname.slice(1).split("/")[0];
    return ID.test(id) ? id : null;
  }
  if (host === "youtube.com" || host === "youtube-nocookie.com") {
    const v = url.searchParams.get("v");
    if (v && ID.test(v)) return v;
    const m = url.pathname.match(/^\/(?:embed|shorts|live|v)\/([\w-]{11})(?:[/?]|$)/);
    return m ? m[1] : null;
  }
  return null;
}

/**
 * Fraction of the current frame's height that a wider screen slices off each
 * edge — i.e. how much picture a normal cinema loses versus what's on screen
 * now. Zero when the target is the same shape or taller.
 */
export function cropBandFraction(currentRatio, targetRatio = BASELINE_RATIO) {
  if (currentRatio <= 0 || targetRatio <= 0) {
    throw new RangeError("ratios must be positive");
  }
  if (targetRatio <= currentRatio) return 0;
  return (1 - currentRatio / targetRatio) / 2;
}

/**
 * How many rows of a frame are letterbox bars, given each row's average
 * brightness (0–255) from one edge inward. Stops at the first row brighter
 * than `threshold`, and refuses to call more than 40% of the frame a bar so a
 * genuinely dark shot is never mistaken for letterboxing.
 */
export function countDarkRows(rowLuma, threshold = 12) {
  if (!Array.isArray(rowLuma)) throw new TypeError("rowLuma must be an array");
  const cap = Math.floor(rowLuma.length * 0.4);
  let n = 0;
  while (n < rowLuma.length && rowLuma[n] <= threshold) n++;
  return Math.min(n, cap);
}

/**
 * Real picture ratio of a video that has black bars baked into its frame.
 * `topRows`/`bottomRows` are letterbox thicknesses in pixels; the remaining
 * band is the actual picture. Falls back to the frame's own ratio when the
 * bars are implausible.
 */
export function pictureRatioFromBars(frameW, frameH, topRows, bottomRows) {
  if (frameW <= 0 || frameH <= 0) throw new RangeError("dimensions must be positive");
  const visible = frameH - topRows - bottomRows;
  if (!(visible > 0) || visible / frameH < 0.35) return frameW / frameH;
  return frameW / visible;
}

/** Read state back out of a shared link: ?v=<youtube id>&s=<screen key>. */
export function parseShareParams(search) {
  const params = new URLSearchParams(String(search ?? "").replace(/^\?/, ""));
  const video = parseYouTubeId(params.get("v") ?? "");
  const screen = screenByKey(params.get("s") ?? "");
  return { video, screen };
}

/** Build a shareable link that reopens the same video at the same screen. */
export function buildShareUrl(baseUrl, { video = null, screenKey = null } = {}) {
  const url = new URL(String(baseUrl));
  url.search = "";
  url.hash = "";
  if (video) url.searchParams.set("v", video);
  if (screenKey) url.searchParams.set("s", screenKey);
  return url.toString();
}

/**
 * Where wider formats' crop lines fall inside the current frame (same width).
 * Returns one entry per strictly-wider format: `frac` is the distance of the
 * top/bottom crop line from each frame edge, as a fraction of frame height.
 */
export function cropLines(currentRatio, formats = FORMATS) {
  if (currentRatio <= 0) throw new RangeError("ratio must be positive");
  return formats
    .filter((f) => f.ratio > currentRatio + 1e-9)
    .map((f) => ({
      id: f.id,
      name: f.name,
      label: f.label,
      ratio: f.ratio,
      frac: (1 - currentRatio / f.ratio) / 2,
    }));
}
