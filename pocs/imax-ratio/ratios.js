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
