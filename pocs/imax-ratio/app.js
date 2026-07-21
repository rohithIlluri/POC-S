import {
  FORMATS,
  FILMS,
  fitFraction,
  moreVsBaseline,
  frameSize,
  cropLines,
  coverSize,
  parseYouTubeId,
} from "./ratios.js";

const $ = (id) => document.getElementById(id);
const stage = $("stage");
const frame = $("frame");
const scene = $("scene");
const guides = $("guides");
const flash = $("flash");

const state = {
  ratio: 2.39, // start in scope so IMAX "opens up" from here
  mode: "cinema", // cinema | fit
  guides: false,
  film: null,
  shiftTimer: null,
  media: null, // user <img>/<video>/YouTube <iframe> replacing the painted scene
  mediaKind: null, // "file" | "yt"
  sourceRatio: 16 / 9, // ratio of the real picture inside a YouTube embed
  muted: true,
};

/* ---------- painted demo scene (no assets needed) ---------- */
// A full-height 1.43:1 Aegean scene; wider formats crop it top and bottom,
// which is exactly what open-matte IMAX comparisons show.
function paintScene() {
  const W = 1716; // 1.43:1
  const H = 1200;
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

  // stars in the upper band (the part scope never shows you)
  ctx.fillStyle = "rgba(255,255,255,0.8)";
  let seed = 42;
  const rand = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
  for (let i = 0; i < 140; i++) {
    const x = rand() * W;
    const y = rand() * H * 0.3;
    const r = rand() * 1.4 + 0.2;
    ctx.globalAlpha = 0.25 + rand() * 0.6;
    ctx.beginPath();
    ctx.arc(x, y, r, 0, Math.PI * 2);
    ctx.fill();
  }
  ctx.globalAlpha = 1;

  // sun + glow
  const sunX = W * 0.62;
  const sunY = H * 0.56;
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

  // distant islands
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

  // sea
  const sea = ctx.createLinearGradient(0, H * 0.62, 0, H);
  sea.addColorStop(0, "#b4703a");
  sea.addColorStop(0.12, "#3d4f74");
  sea.addColorStop(1, "#0a1626");
  ctx.fillStyle = sea;
  ctx.fillRect(0, H * 0.62, W, H * 0.38);

  // sun path shimmer
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

  // ship silhouette on the sun path
  const sx = sunX - 30;
  const sy = H * 0.78;
  ctx.fillStyle = "#0c1220";
  ctx.beginPath(); // hull
  ctx.moveTo(sx - 90, sy);
  ctx.quadraticCurveTo(sx, sy + 34, sx + 90, sy);
  ctx.lineTo(sx + 104, sy - 16);
  ctx.lineTo(sx - 104, sy - 16);
  ctx.closePath();
  ctx.fill();
  ctx.fillRect(sx - 3, sy - 92, 5, 78); // mast
  ctx.beginPath(); // sail
  ctx.moveTo(sx + 4, sy - 88);
  ctx.quadraticCurveTo(sx + 74, sy - 56, sx + 4, sy - 22);
  ctx.closePath();
  ctx.fill();

  // foreground rocks (bottom band — also IMAX-only real estate)
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

/* ---------- layout ---------- */
function layout(animate = true) {
  const cs = getComputedStyle(stage);
  const stageW =
    stage.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
  const stageH =
    stage.clientHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom);
  if (stageW <= 0 || stageH <= 0) return;
  const { width, height } = frameSize(stageW, stageH, state.ratio, state.mode);
  if (!animate) frame.style.transition = "none";
  frame.style.width = `${width}px`;
  frame.style.height = `${height}px`;
  if (!animate) {
    void frame.offsetWidth; // flush so the next change animates again
    frame.style.transition = "";
  }
  layoutEmbed(width, height);
  renderGuides();
}

// A YouTube iframe can't be object-fit-cropped, so it's oversized to cover
// the frame; the frame's overflow:hidden clips the rest (including any
// letterbox bars baked into the source when sourceRatio says so).
function layoutEmbed(frameW, frameH) {
  if (state.mediaKind !== "yt" || !state.media) return;
  const { width, height } = coverSize(frameW, frameH, state.sourceRatio, 16 / 9);
  state.media.style.width = `${width}px`;
  state.media.style.height = `${height}px`;
}

function renderGuides() {
  guides.innerHTML = "";
  for (const line of cropLines(state.ratio)) {
    const pct = (line.frac * 100).toFixed(2);
    const top = document.createElement("div");
    top.className = "guide";
    top.style.top = `${pct}%`;
    top.innerHTML = `<em>${line.label} ${line.name}</em>`;
    const bottom = document.createElement("div");
    bottom.className = "guide bottom";
    bottom.style.bottom = `${pct}%`;
    guides.append(top, bottom);
  }
}

/* ---------- stats + flash ---------- */
function fmtRatio(r) {
  return `${r.toFixed(2).replace(/0$/, "")}:1`;
}

function updateStats() {
  const f = FORMATS.find((f) => Math.abs(f.ratio - state.ratio) < 1e-6);
  $("statFormat").textContent = f ? `${f.label} ${f.name}` : fmtRatio(state.ratio);
  const gain = moreVsBaseline(state.ratio);
  $("statGain").textContent = `${gain >= 0 ? "+" : ""}${Math.round(gain * 100)}%`;
  const cover = fitFraction(state.ratio, window.innerWidth / window.innerHeight);
  $("statCover").textContent = `${Math.round(cover * 100)}%`;
}

let flashTimer;
function showFlash(title, sub) {
  flash.innerHTML = `<div><strong>${title}</strong><small>${sub}</small></div>`;
  flash.classList.add("show");
  clearTimeout(flashTimer);
  flashTimer = setTimeout(() => flash.classList.remove("show"), 1800);
}

/* ---------- state changes ---------- */
function setRatio(ratio, { announce = true, keepFilm = false } = {}) {
  state.ratio = ratio;
  if (!keepFilm) {
    stopShift();
    state.film = null;
  }
  layout();
  updateStats();
  syncChips();
  if (announce) {
    const f = FORMATS.find((f) => Math.abs(f.ratio - ratio) < 1e-6);
    const gain = Math.round(moreVsBaseline(ratio) * 100);
    showFlash(
      f ? f.label : fmtRatio(ratio),
      gain > 0 ? `+${gain}% picture vs scope` : f ? f.name : ""
    );
  }
}

function selectFilm(film) {
  stopShift();
  state.film = film.id;
  setRatio(film.base, { announce: false, keepFilm: true });
  syncChips();
  showFlash(`${film.title} (${film.year})`, film.note);
  if (film.base !== film.imax) startShift(film);
  else setTimeout(() => showFlash("1.43:1", "the whole film, full frame"), 2000);
}

function startShift(film) {
  stopShift();
  let atImax = false;
  $("shiftBtn").classList.add("active");
  state.shiftTimer = setInterval(() => {
    atImax = !atImax;
    setRatio(atImax ? film.imax : film.base, { announce: false, keepFilm: true });
    showFlash(
      fmtRatio(atImax ? film.imax : film.base),
      atImax ? "IMAX sequence — the frame opens" : "back to the base frame"
    );
  }, 2600);
}

function stopShift() {
  if (state.shiftTimer) clearInterval(state.shiftTimer);
  state.shiftTimer = null;
  $("shiftBtn").classList.remove("active");
}

/* ---------- chips ---------- */
function syncChips() {
  document.querySelectorAll("#formatRow .chip").forEach((el) => {
    el.classList.toggle("active", Math.abs(+el.dataset.ratio - state.ratio) < 1e-6);
  });
  document.querySelectorAll("#filmRow .chip").forEach((el) => {
    el.classList.toggle("active", el.dataset.film === state.film);
  });
  $("modeBtn").textContent =
    state.mode === "cinema" ? "Cinema wall (fixed width)" : "TV fit (contain)";
  $("guidesBtn").classList.toggle("active", state.guides);
  guides.classList.toggle("on", state.guides);
}

function buildChips() {
  const formatRow = $("formatRow");
  FORMATS.forEach((f, i) => {
    const b = document.createElement("button");
    b.className = "chip";
    b.dataset.ratio = f.ratio;
    b.innerHTML = `<b>${f.label}</b><small>${f.name}</small>`;
    b.title = `${f.blurb} — key ${i + 1}`;
    b.addEventListener("click", () => setRatio(f.ratio));
    formatRow.appendChild(b);
  });
  const filmRow = $("filmRow");
  for (const film of FILMS) {
    const b = document.createElement("button");
    b.className = "chip film";
    b.dataset.film = film.id;
    b.innerHTML = `<b>${film.title}</b><small>${film.year}</small>`;
    b.title = film.note;
    b.addEventListener("click", () => selectFilm(film));
    filmRow.appendChild(b);
  }
}

/* ---------- user media + YouTube ---------- */
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
    el.muted = true;
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
  frame.insertBefore(el, guides);
  scene.style.display = "none";
  showFlash("Your footage", "cropped live to each format");
}

function useYouTube(input) {
  const id = parseYouTubeId(input);
  if (!id) {
    showFlash("Hmm", "couldn't read that YouTube link");
    return;
  }
  clearMedia();
  const el = document.createElement("iframe");
  el.src =
    `https://www.youtube-nocookie.com/embed/${id}` +
    `?autoplay=1&mute=1&loop=1&playlist=${id}` +
    `&controls=0&rel=0&playsinline=1&enablejsapi=1&origin=${location.origin}`;
  el.allow = "autoplay; encrypted-media";
  el.title = "YouTube video cropped to the selected format";
  state.media = el;
  state.mediaKind = "yt";
  state.muted = true;
  syncSoundBtn();
  frame.insertBefore(el, guides);
  scene.style.display = "none";
  layout(false);
  showFlash("Rolling", "now pick a format — or hit Watch it in IMAX");
}

function ytCommand(func, args = "") {
  if (state.mediaKind !== "yt" || !state.media?.contentWindow) return;
  state.media.contentWindow.postMessage(
    JSON.stringify({ event: "command", func, args }),
    "*"
  );
}

function syncSoundBtn() {
  $("soundBtn").textContent = state.muted ? "🔇 Muted" : "🔊 Sound on";
  $("soundBtn").classList.toggle("active", !state.muted);
}

/* ---------- wiring ---------- */
buildChips();
paintScene();

$("modeBtn").addEventListener("click", () => {
  state.mode = state.mode === "cinema" ? "fit" : "cinema";
  layout();
  syncChips();
});
$("guidesBtn").addEventListener("click", () => {
  state.guides = !state.guides;
  syncChips();
});
$("shiftBtn").addEventListener("click", () => {
  if (state.shiftTimer) return stopShift();
  const film = FILMS.find((f) => f.id === state.film) ?? FILMS.find((f) => f.id === "oppenheimer");
  selectFilm(film);
});
$("fsBtn").addEventListener("click", () => {
  if (document.fullscreenElement) document.exitFullscreen();
  else document.documentElement.requestFullscreen?.();
});
$("mediaBtn").addEventListener("click", () => $("mediaInput").click());
$("mediaInput").addEventListener("change", (e) => {
  const file = e.target.files?.[0];
  if (file) useMedia(file);
});

$("ytBtn").addEventListener("click", () => useYouTube($("ytUrl").value));
$("ytUrl").addEventListener("keydown", (e) => {
  if (e.key === "Enter") useYouTube($("ytUrl").value);
});
$("srcRatio").addEventListener("change", (e) => {
  state.sourceRatio = Number(e.target.value);
  layout(false);
});
$("soundBtn").addEventListener("click", () => {
  if (state.mediaKind === "yt") {
    state.muted = !state.muted;
    ytCommand(state.muted ? "mute" : "unMute");
  } else if (state.media instanceof HTMLVideoElement) {
    state.muted = !state.muted;
    state.media.muted = state.muted;
  }
  syncSoundBtn();
});
$("imaxBtn").addEventListener("click", () => {
  setRatio(1.43, { announce: false });
  document.documentElement.requestFullscreen?.().catch(() => {});
  showFlash("1.43:1 IMAX", "this is the whole frame");
});

document.addEventListener("keydown", (e) => {
  if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement)
    return;
  const n = Number(e.key);
  if (n >= 1 && n <= FORMATS.length) return setRatio(FORMATS[n - 1].ratio);
  if (e.key === "f") return $("fsBtn").click();
  if (e.key === "g") return $("guidesBtn").click();
  if (e.key === "m") return $("modeBtn").click();
  if (e.key === " ") {
    e.preventDefault();
    return $("shiftBtn").click();
  }
});

new ResizeObserver(() => layout(false)).observe(stage);
window.addEventListener("resize", updateStats);

layout(false);
updateStats();
syncChips();
setTimeout(
  () => showFlash("2.39:1 scope", "press 1 to open the frame to IMAX 70mm"),
  600
);
