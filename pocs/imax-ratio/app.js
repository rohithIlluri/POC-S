import { FILMS, moreVsBaseline, frameSize, coverSize, parseYouTubeId } from "./ratios.js";

const $ = (id) => document.getElementById(id);
const stage = $("stage");
const frame = $("frame");
const scene = $("scene");
const flash = $("flash");

// Friendly screen sizes — no ratios or jargon shown to the viewer.
// Ordered widest → tallest so "IMAX" reads as the biggest, fullest one.
const SIZES = [
  { key: "tv", name: "TV & laptop", ratio: 16 / 9 },
  { key: "wide", name: "Widescreen", ratio: 1.85 },
  { key: "cinema", name: "Cinema", ratio: 2.39 },
  { key: "imax", name: "IMAX", ratio: 1.43, hero: true },
];
const IMAX = SIZES.find((s) => s.key === "imax");

const state = {
  size: SIZES.find((s) => s.key === "cinema"), // start on a normal movie screen
  film: null,
  shiftTimer: null,
  media: null, // <img>/<video>/YouTube <iframe> shown inside the frame
  mediaKind: null, // "file" | "yt"
  trim: true, // trim baked-in black bars on trailers by default
  muted: true,
};

/* ---------- painted demo scene (shown until a video is loaded) ---------- */
function paintScene() {
  const W = 1716, H = 1200; // 1.43:1, the full IMAX shape
  scene.width = W;
  scene.height = H;
  const ctx = scene.getContext("2d");

  const sky = ctx.createLinearGradient(0, 0, 0, H * 0.62);
  sky.addColorStop(0, "#0b1d3a");
  sky.addColorStop(0.55, "#2a4a7b");
  sky.addColorStop(0.85, "#c97b3d");
  sky.addColorStop(1, "#f0b45c");
  ctx.fillStyle = sky;
  ctx.fillRect(0, 0, W, H * 0.62);

  ctx.fillStyle = "rgba(255,255,255,0.8)";
  let seed = 42;
  const rand = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
  for (let i = 0; i < 140; i++) {
    const x = rand() * W, y = rand() * H * 0.3, r = rand() * 1.4 + 0.2;
    ctx.globalAlpha = 0.25 + rand() * 0.6;
    ctx.beginPath();
    ctx.arc(x, y, r, 0, Math.PI * 2);
    ctx.fill();
  }
  ctx.globalAlpha = 1;

  const sunX = W * 0.62, sunY = H * 0.56;
  const glow = ctx.createRadialGradient(sunX, sunY, 10, sunX, sunY, 320);
  glow.addColorStop(0, "rgba(255,214,140,0.95)");
  glow.addColorStop(0.25, "rgba(255,180,90,0.5)");
  glow.addColorStop(1, "rgba(255,180,90,0)");
  ctx.fillStyle = glow;
  ctx.fillRect(sunX - 320, sunY - 320, 640, 640);
  ctx.fillStyle = "#ffe4ad";
  ctx.beginPath();
  ctx.arc(sunX, sunY, 58, 0, Math.PI * 2);
  ctx.fill();

  ctx.fillStyle = "#1d2c47";
  ctx.beginPath();
  ctx.moveTo(0, H * 0.62);
  ctx.lineTo(W * 0.16, H * 0.545);
  ctx.lineTo(W * 0.34, H * 0.62);
  ctx.closePath();
  ctx.fill();
  ctx.beginPath();
  ctx.moveTo(W * 0.72, H * 0.62);
  ctx.lineTo(W * 0.88, H * 0.56);
  ctx.lineTo(W, H * 0.615);
  ctx.lineTo(W, H * 0.62);
  ctx.closePath();
  ctx.fill();

  const sea = ctx.createLinearGradient(0, H * 0.62, 0, H);
  sea.addColorStop(0, "#b4703a");
  sea.addColorStop(0.12, "#3d4f74");
  sea.addColorStop(1, "#0a1626");
  ctx.fillStyle = sea;
  ctx.fillRect(0, H * 0.62, W, H * 0.38);

  for (let i = 0; i < 60; i++) {
    const y = H * 0.63 + rand() * H * 0.33;
    const spread = 40 + (y - H * 0.62) * 0.55;
    const x = sunX + (rand() - 0.5) * spread * 2;
    const w = 14 + rand() * 60;
    ctx.globalAlpha = 0.05 + rand() * 0.22;
    ctx.fillStyle = "#ffd08a";
    ctx.fillRect(x - w / 2, y, w, 2.4);
  }
  ctx.globalAlpha = 1;

  const sx = sunX - 30, sy = H * 0.78;
  ctx.fillStyle = "#0c1220";
  ctx.beginPath();
  ctx.moveTo(sx - 90, sy);
  ctx.quadraticCurveTo(sx, sy + 34, sx + 90, sy);
  ctx.lineTo(sx + 104, sy - 16);
  ctx.lineTo(sx - 104, sy - 16);
  ctx.closePath();
  ctx.fill();
  ctx.fillRect(sx - 3, sy - 92, 5, 78);
  ctx.beginPath();
  ctx.moveTo(sx + 4, sy - 88);
  ctx.quadraticCurveTo(sx + 74, sy - 56, sx + 4, sy - 22);
  ctx.closePath();
  ctx.fill();

  ctx.fillStyle = "#060b13";
  ctx.beginPath();
  ctx.moveTo(0, H);
  ctx.lineTo(0, H * 0.9);
  ctx.quadraticCurveTo(W * 0.14, H * 0.86, W * 0.24, H);
  ctx.closePath();
  ctx.fill();
  ctx.beginPath();
  ctx.moveTo(W, H);
  ctx.lineTo(W, H * 0.93);
  ctx.quadraticCurveTo(W * 0.86, H * 0.9, W * 0.78, H);
  ctx.closePath();
  ctx.fill();
}

/* ---------- layout (always "cinema wall": fixed width, opens vertically) ---------- */
function layout(animate = true) {
  const cs = getComputedStyle(stage);
  const stageW = stage.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
  const stageH = stage.clientHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom);
  if (stageW <= 0 || stageH <= 0) return;
  const { width, height } = frameSize(stageW, stageH, state.size.ratio, "cinema");
  if (!animate) frame.style.transition = "none";
  frame.style.width = `${width}px`;
  frame.style.height = `${height}px`;
  if (!animate) {
    void frame.offsetWidth;
    frame.style.transition = "";
  }
  layoutEmbed(width, height);
}

// A YouTube iframe can't be cropped by CSS, so oversize it to cover the frame;
// the frame's overflow:hidden clips the rest. When "trim black bars" is on we
// treat the trailer as a wide cinematic picture so its baked-in bars fall
// outside the frame instead of showing inside it.
function layoutEmbed(frameW, frameH) {
  if (state.mediaKind !== "yt" || !state.media) return;
  const sourceRatio = state.trim ? 2.39 : 16 / 9;
  const { width, height } = coverSize(frameW, frameH, sourceRatio, 16 / 9);
  state.media.style.width = `${width}px`;
  state.media.style.height = `${height}px`;
}

/* ---------- caption + flash ---------- */
function updateCaption() {
  const gain = Math.round(moreVsBaseline(state.size.ratio) * 100);
  const el = $("caption");
  if (state.size.key === "imax") {
    el.innerHTML = `On IMAX you see about <b>${gain}% more picture</b> than at a normal movie theater.`;
  } else if (state.size.key === "tv") {
    el.innerHTML = `This is how much a normal <b>TV or laptop</b> shows you.`;
  } else if (state.size.key === "cinema") {
    el.innerHTML = `A normal wide <b>movie theater</b> screen.`;
  } else {
    el.innerHTML = `A standard <b>widescreen</b> picture.`;
  }
}

let flashTimer;
function showFlash(title, sub) {
  flash.innerHTML = `<div><strong>${title}</strong><small>${sub}</small></div>`;
  flash.classList.add("show");
  clearTimeout(flashTimer);
  flashTimer = setTimeout(() => flash.classList.remove("show"), 1900);
}

/* ---------- change size ---------- */
function setSize(size, { announce = true, keepFilm = false } = {}) {
  state.size = size;
  if (!keepFilm) {
    stopShift();
    state.film = null;
  }
  layout();
  updateCaption();
  syncButtons();
  if (announce) {
    const gain = Math.round(moreVsBaseline(size.ratio) * 100);
    const sub =
      size.key === "imax"
        ? `about ${gain}% more picture than a normal cinema`
        : size.key === "tv"
          ? "what your screen normally shows"
          : size.key === "cinema"
            ? "a normal wide movie screen"
            : "standard widescreen";
    showFlash(size.name, sub);
  }
}

/* ---------- famous films (replay their real IMAX moments) ---------- */
function ratioToSize(ratio) {
  // nearest friendly size for a film's frame shape
  return SIZES.reduce((best, s) =>
    Math.abs(s.ratio - ratio) < Math.abs(best.ratio - ratio) ? s : best
  );
}

function selectFilm(film) {
  stopShift();
  state.film = film.id;
  setSize(ratioToSize(film.base), { announce: false, keepFilm: true });
  syncButtons();
  showFlash(film.title, `${film.year}`);
  if (film.base !== film.imax) startShift(film);
  else setTimeout(() => showFlash(film.title, "shot entirely in full IMAX"), 2100);
}

function startShift(film) {
  stopShift();
  let big = false;
  state.shiftTimer = setInterval(() => {
    big = !big;
    setSize(ratioToSize(big ? film.imax : film.base), { announce: false, keepFilm: true });
    showFlash(
      film.title,
      big ? "the screen opens up for the IMAX scenes" : "back to the normal screen"
    );
  }, 2700);
  syncButtons();
}

function stopShift() {
  if (state.shiftTimer) clearInterval(state.shiftTimer);
  state.shiftTimer = null;
}

/* ---------- buttons ---------- */
function syncButtons() {
  document.querySelectorAll("#sizeRow .btn").forEach((el) => {
    el.classList.toggle("on", el.dataset.key === state.size.key && !state.film);
  });
  document.querySelectorAll("#filmRow .btn").forEach((el) => {
    el.classList.toggle("on", el.dataset.film === state.film);
  });
  $("trimBtn").classList.toggle("on", state.trim);
  $("soundBtn").textContent = state.muted ? "Sound off" : "Sound on";
  $("soundBtn").classList.toggle("on", !state.muted);
}

function buildButtons() {
  const sizeRow = $("sizeRow");
  for (const s of SIZES) {
    const b = document.createElement("button");
    b.className = "btn" + (s.hero ? " on" : "");
    b.dataset.key = s.key;
    b.innerHTML = `<b>${s.name}</b>`;
    b.addEventListener("click", () => setSize(s));
    sizeRow.appendChild(b);
  }
  const filmRow = $("filmRow");
  for (const film of FILMS) {
    const b = document.createElement("button");
    b.className = "btn film";
    b.dataset.film = film.id;
    b.textContent = film.title;
    b.title = `${film.title} (${film.year})`;
    b.addEventListener("click", () => selectFilm(film));
    filmRow.appendChild(b);
  }
}

/* ---------- media ---------- */
function clearMedia() {
  if (state.media) {
    state.media.remove();
    if (state.media.src?.startsWith("blob:")) URL.revokeObjectURL(state.media.src);
    state.media = null;
    state.mediaKind = null;
  }
}

function useMedia(file) {
  clearMedia();
  const url = URL.createObjectURL(file);
  let el;
  if (file.type.startsWith("video/")) {
    el = document.createElement("video");
    el.src = url;
    el.muted = state.muted;
    el.loop = true;
    el.autoplay = true;
    el.playsInline = true;
  } else {
    el = document.createElement("img");
    el.src = url;
    el.alt = "";
  }
  state.media = el;
  state.mediaKind = "file";
  frame.insertBefore(el, flash);
  scene.style.display = "none";
  showFlash("Playing your video", "now try Watch it in IMAX");
}

function useYouTube(input) {
  const id = parseYouTubeId(input);
  if (!id) {
    showFlash("Hmm", "that doesn't look like a YouTube link");
    return;
  }
  clearMedia();
  const el = document.createElement("iframe");
  el.src =
    `https://www.youtube-nocookie.com/embed/${id}` +
    `?autoplay=1&mute=1&loop=1&playlist=${id}` +
    `&controls=0&rel=0&playsinline=1&modestbranding=1&enablejsapi=1&origin=${location.origin}`;
  el.allow = "autoplay; encrypted-media";
  el.title = "Trailer";
  state.media = el;
  state.mediaKind = "yt";
  state.muted = true;
  syncButtons();
  frame.insertBefore(el, flash);
  scene.style.display = "none";
  layout(false);
  showFlash("Rolling", "now hit Watch it in IMAX");
}

function ytCommand(func) {
  if (state.mediaKind !== "yt" || !state.media?.contentWindow) return;
  state.media.contentWindow.postMessage(
    JSON.stringify({ event: "command", func, args: "" }),
    "*"
  );
}

/* ---------- wiring ---------- */
buildButtons();
paintScene();

$("ytBtn").addEventListener("click", () => useYouTube($("ytUrl").value));
$("ytUrl").addEventListener("keydown", (e) => {
  if (e.key === "Enter") useYouTube($("ytUrl").value);
});
$("mediaBtn").addEventListener("click", () => $("mediaInput").click());
$("mediaInput").addEventListener("change", (e) => {
  const file = e.target.files?.[0];
  if (file) useMedia(file);
});

$("imaxBtn").addEventListener("click", () => {
  setSize(IMAX, { announce: false });
  document.documentElement.requestFullscreen?.().catch(() => {});
  const gain = Math.round(moreVsBaseline(IMAX.ratio) * 100);
  showFlash("IMAX", `about ${gain}% more picture than a normal cinema`);
});

$("fsBtn").addEventListener("click", () => {
  if (document.fullscreenElement) document.exitFullscreen();
  else document.documentElement.requestFullscreen?.().catch(() => {});
});

$("soundBtn").addEventListener("click", () => {
  state.muted = !state.muted;
  if (state.mediaKind === "yt") ytCommand(state.muted ? "mute" : "unMute");
  else if (state.media instanceof HTMLVideoElement) state.media.muted = state.muted;
  syncButtons();
});

$("trimBtn").addEventListener("click", () => {
  state.trim = !state.trim;
  layout(false);
  syncButtons();
  showFlash(state.trim ? "Black bars trimmed" : "Showing full video", "");
});

new ResizeObserver(() => layout(false)).observe(stage);

layout(false);
updateCaption();
syncButtons();
setTimeout(() => showFlash("Watch it in IMAX", "paste a trailer, then tap the blue button"), 700);
