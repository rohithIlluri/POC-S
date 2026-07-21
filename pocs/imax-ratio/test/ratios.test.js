import test from "node:test";
import assert from "node:assert/strict";
import {
  FORMATS,
  FILMS,
  BASELINE_RATIO,
  formatById,
  fitFraction,
  moreVsBaseline,
  frameSize,
  cropLines,
  coverSize,
  parseYouTubeId,
} from "../ratios.js";

const close = (a, b, eps = 1e-9) =>
  assert.ok(Math.abs(a - b) < eps, `expected ${a} ≈ ${b}`);

test("FORMATS are well-formed, unique, and ordered tallest → widest", () => {
  const ids = new Set(FORMATS.map((f) => f.id));
  assert.equal(ids.size, FORMATS.length);
  for (const f of FORMATS) {
    assert.ok(f.ratio > 1 && f.ratio < 3, f.id);
    assert.ok(f.name && f.label && f.blurb, f.id);
  }
  for (let i = 1; i < FORMATS.length; i++) {
    assert.ok(FORMATS[i].ratio > FORMATS[i - 1].ratio, "sorted by ratio");
  }
});

test("formatById finds formats and rejects unknowns", () => {
  assert.equal(formatById("imax143").ratio, 1.43);
  assert.throws(() => formatById("betamax"));
});

test("fitFraction: perfect fit, known letterbox value, symmetry", () => {
  close(fitFraction(2.39, 2.39), 1);
  // 2.39 scope on a 16:9 display covers 16/9 ÷ 2.39 of the screen area
  close(fitFraction(2.39, 16 / 9), 16 / 9 / 2.39);
  for (const a of [1.43, 1.9, 2.39]) {
    for (const b of [1.375, 16 / 9, 2.76]) {
      close(fitFraction(a, b), fitFraction(b, a));
    }
  }
  assert.throws(() => fitFraction(0, 1), RangeError);
});

test("moreVsBaseline: IMAX film shows ~67% more than scope at equal width", () => {
  close(moreVsBaseline(1.43), 2.39 / 1.43 - 1);
  close(moreVsBaseline(BASELINE_RATIO), 0);
  // Sinners' Ultra Panavision → IMAX jump is ~93%
  close(moreVsBaseline(1.43, 2.76), 2.76 / 1.43 - 1);
  assert.throws(() => moreVsBaseline(-1), RangeError);
});

test("frameSize cinema mode locks width across formats", () => {
  const stage = { w: 1600, h: 700 };
  const scope = frameSize(stage.w, stage.h, 2.39, "cinema");
  const imax = frameSize(stage.w, stage.h, 1.43, "cinema");
  close(scope.width, imax.width); // same wall, screen opens vertically
  close(imax.width, Math.min(1600, 700 * 1.43));
  close(imax.height, imax.width / 1.43);
  assert.ok(imax.height <= stage.h + 1e-9, "tallest format still fits");
  assert.ok(imax.height > scope.height);
});

test("frameSize fit mode is a plain contain fit", () => {
  const wide = frameSize(1600, 700, 2.39, "fit");
  close(wide.width, 1600);
  close(wide.height, 1600 / 2.39);
  const tall = frameSize(500, 700, 1.43, "fit");
  close(tall.width, 500);
  close(tall.height, 500 / 1.43);
  assert.throws(() => frameSize(0, 700, 1.43), RangeError);
});

test("cropLines marks every strictly wider format with correct bar fraction", () => {
  const lines = cropLines(1.43);
  assert.equal(lines.length, FORMATS.length - 1);
  const scope = lines.find((l) => l.id === "scope");
  close(scope.frac, (1 - 1.43 / 2.39) / 2);
  // widest format has no wider siblings
  assert.equal(cropLines(2.76).length, 0);
  // total bar coverage + visible band = full height
  for (const l of lines) {
    close(2 * l.frac + 1.43 / l.ratio, 1);
  }
});

test("coverSize: native 16:9 media covers every frame shape", () => {
  const v = 16 / 9;
  // frame taller than the media (1.43 IMAX): height rules, width overflows
  const tall = coverSize(1000, 700, v);
  close(tall.height, 700);
  close(tall.width, 700 * v);
  assert.ok(tall.width >= 1000);
  // frame wider than the media (2.39 scope): width rules, height overflows
  const wide = coverSize(1000, 418, v);
  close(wide.width, 1000);
  close(wide.height, 1000 / v);
  assert.ok(wide.height >= 418);
  assert.throws(() => coverSize(0, 1, v), RangeError);
});

test("coverSize: letterboxed source oversizes so baked-in bars clip away", () => {
  const v = 16 / 9;
  // 2.39 picture inside a 16:9 embed, shown in a 2.39 frame:
  // the picture band exactly covers the frame, bars land outside it
  const fw = 956;
  const fh = fw / 2.39;
  const { width, height } = coverSize(fw, fh, 2.39, v);
  close(width, fw);
  close(height, fw / v);
  const pictureH = width / 2.39;
  close(pictureH, fh); // real picture matches the frame exactly
  assert.ok(height > fh); // iframe (with bars) sticks out and gets clipped
  // same source in a 1.43 frame: the picture, not the iframe, must cover it
  const imax = coverSize(1000, 1000 / 1.43, 2.39, v);
  assert.ok(imax.width / 2.39 >= 1000 / 1.43 - 1e-9);
});

test("parseYouTubeId handles every common URL shape and rejects junk", () => {
  const id = "dQw4w9WgXcQ";
  const good = [
    id,
    `https://www.youtube.com/watch?v=${id}`,
    `https://youtube.com/watch?v=${id}&t=42s`,
    `https://m.youtube.com/watch?v=${id}`,
    `https://youtu.be/${id}`,
    `https://youtu.be/${id}?si=abc`,
    `https://www.youtube.com/shorts/${id}`,
    `https://www.youtube.com/embed/${id}`,
    `https://www.youtube-nocookie.com/embed/${id}`,
    `https://www.youtube.com/live/${id}?feature=share`,
    `  https://youtu.be/${id}  `,
  ];
  for (const url of good) assert.equal(parseYouTubeId(url), id, url);
  const bad = [
    "",
    null,
    "not a url",
    "https://example.com/watch?v=dQw4w9WgXcQ",
    "https://www.youtube.com/watch?v=tooshort",
    "https://www.youtube.com/feed/subscriptions",
    "https://vimeo.com/12345678",
  ];
  for (const url of bad) assert.equal(parseYouTubeId(url), null, String(url));
});

test("FILMS reference sane ratios and never shrink in IMAX", () => {
  for (const film of FILMS) {
    assert.ok(film.title && film.year > 2000 && film.note, film.id);
    assert.ok(film.base >= film.imax, `${film.id}: IMAX frame is taller or equal`);
    assert.ok(film.imax >= 1.43, film.id);
  }
  const odyssey = FILMS.find((f) => f.id === "odyssey");
  assert.equal(odyssey.base, 1.43);
  assert.equal(odyssey.imax, 1.43);
});
