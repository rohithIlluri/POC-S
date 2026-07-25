import {
  FILMS,
  SCREENS,
  screenByKey,
  nearestScreen,
  moreVsBaseline,
  frameSize,
  coverSize,
  cropBandFraction,
  countDarkRows,
  pictureRatioFromBars,
  parseYouTubeId,
  parseShareParams,
  buildShareUrl,
} from "./ratios.js";
import { createScene } from "./scene.js";

const $ = (id) => document.getElementById(id);
const stage = $("stage");
const frame = $("frame");
const sceneCanvas = $("scene");
const flash = $("flash");
const notice = $("notice");

const IMAX = screenByKey("imax");
const CINEMA = screenByKey("cinema");
const YT_RATIO = 16 / 9;

const state = {
  screen: CINEMA, // start on a normal movie screen so IMAX opens up from here
  film: null,
  shiftTimer: null,
  media: null, // { el, kind, mediaRatio, pictureRatio }
  videoId: null,
  trim: true, // trim black bars baked into trailers
  showMiss: false,
  muted: true,
};

const scene = createScene(sceneCanvas);
scene.start();

/* ---------- layout: fixed width, screen opens vertically ---------- */
function layout(animate = true) {
  const cs = getComputedStyle(stage);
  const stageW = stage.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
  const stageH = stage.clientHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom);
  if (stageW <= 0 || stageH <= 0) return;
  const { width, height } = frameSize(stageW, stageH, state.screen.ratio, "cinema");
  if (!animate) frame.style.transition = "none";
  frame.style.width = `${width}px`;
  frame.style.height = `${height}px`;
  if (!animate) {
    void frame.offsetWidth;
    frame.style.transition = "";
  }
  layoutMedia(width, height);
  layoutMissBands();
}

// Media is centred and oversized so it covers the frame; anything outside
// (including bars baked into the source) is clipped by the frame.
function layoutMedia(frameW, frameH) {
  const m = state.media;
  if (!m) return;
  const picture = m.kind === "yt" ? (state.trim ? 2.39 : YT_RATIO) : m.pictureRatio;
  const { width, height } = coverSize(frameW, frameH, picture, m.mediaRatio);
  m.el.style.width = `${width}px`;
  m.el.style.height = `${height}px`;
}

function layoutMissBands() {
  const frac = cropBandFraction(state.screen.ratio);
  const pct = `${(frac * 100).toFixed(2)}%`;
  for (const el of [$("missTop"), $("missBottom")]) {
    el.style.height = pct;
    el.classList.toggle("on", state.showMiss && frac > 0.001);
  }
}

/* ---------- words on screen ---------- */
function gainPercent(ratio) {
  return Math.round(moreVsBaseline(ratio) * 100);
}

function updateCaption() {
  const el = $("caption");
  if (state.showMiss && cropBandFraction(state.screen.ratio) > 0.001) {
    el.innerHTML = `Those dark bands are picture that a normal cinema <b>never shows you</b>.`;
    return;
  }
  switch (state.screen.key) {
    case "imax":
      el.innerHTML = `On IMAX you see about <b>${gainPercent(IMAX.ratio)}% more picture</b> than at a normal movie theater.`;
      break;
    case "tv":
      el.innerHTML = `This is how much a normal <b>TV or laptop</b> shows you.`;
      break;
    case "cinema":
      el.innerHTML = `A normal wide <b>movie theater</b> screen.`;
      break;
    default:
      el.innerHTML = `A standard <b>widescreen</b> picture.`;
  }
}

let flashTimer;
function showFlash(title, sub = "") {
  flash.innerHTML = `<div><strong>${title}</strong><small>${sub}</small></div>`;
  flash.classList.add("show");
  clearTimeout(flashTimer);
  flashTimer = setTimeout(() => flash.classList.remove("show"), 1900);
}

function showNotice(text, link) {
  $("noticeText").textContent = text;
  const a = $("noticeLink");
  if (link) {
    a.href = link;
    a.style.display = "";
  } else {
    a.style.display = "none";
  }
  notice.classList.add("show");
}

function hideNotice() {
  notice.classList.remove("show");
}

/* ---------- changing screen size ---------- */
function setScreen(screen, { announce = true, keepFilm = false } = {}) {
  state.screen = screen;
  if (!keepFilm) {
    stopShift();
    state.film = null;
  }
  layout();
  updateCaption();
  syncButtons();
  if (announce) {
    const sub =
      screen.key === "imax"
        ? `about ${gainPercent(screen.ratio)}% more picture than a normal cinema`
        : screen.key === "tv"
          ? "what your screen normally shows"
          : screen.key === "cinema"
            ? "a normal wide movie screen"
            : "standard widescreen";
    showFlash(screen.name, sub);
  }
}

/* ---------- famous films replay their real IMAX moments ---------- */
function selectFilm(film) {
  stopShift();
  state.film = film.id;
  setScreen(nearestScreen(film.base), { announce: false, keepFilm: true });
  syncButtons();
  showFlash(film.title, String(film.year));
  if (film.base !== film.imax) startShift(film);
  else setTimeout(() => showFlash(film.title, "shot entirely in full IMAX"), 2100);
}

function startShift(film) {
  stopShift();
  let big = false;
  state.shiftTimer = setInterval(() => {
    big = !big;
    setScreen(nearestScreen(big ? film.imax : film.base), {
      announce: false,
      keepFilm: true,
    });
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
    el.classList.toggle("on", el.dataset.key === state.screen.key && !state.film);
  });
  document.querySelectorAll("#filmRow .btn").forEach((el) => {
    el.classList.toggle("on", el.dataset.film === state.film);
  });
  $("trimBtn").classList.toggle("on", state.trim);
  $("missBtn").classList.toggle("on", state.showMiss);
  $("soundBtn").textContent = state.muted ? "Sound off" : "Sound on";
  $("soundBtn").classList.toggle("on", !state.muted);
}

function buildButtons() {
  const sizeRow = $("sizeRow");
  for (const s of SCREENS) {
    const b = document.createElement("button");
    b.className = "btn";
    b.dataset.key = s.key;
    b.innerHTML = `<b>${s.name}</b>`;
    b.addEventListener("click", () => setScreen(s));
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
  try {
    player?.destroy?.(); // takes the iframe with it
  } catch {
    /* already gone */
  }
  player = null;
  playing = false;
  clearTimeout(tapTimer);
  showTap(false);
  if (state.media) {
    const { el } = state.media;
    if (el.src?.startsWith("blob:")) URL.revokeObjectURL(el.src);
    el.remove();
    state.media = null;
  }
  state.videoId = null;
  hideNotice();
}

function attachMedia(el, kind, mediaRatio, pictureRatio = mediaRatio) {
  state.media = { el, kind, mediaRatio, pictureRatio };
  frame.insertBefore(el, $("missTop"));
  sceneCanvas.style.display = "none";
  scene.stop();
  layout(false);
}

/**
 * Look for black bars baked into a picture or video frame and, if found,
 * report the real picture shape so those bars can be cropped away.
 * Returns the element's own ratio when nothing conclusive is found.
 */
function detectPictureRatio(source, naturalW, naturalH) {
  const w = 160;
  const h = Math.max(2, Math.round((w * naturalH) / naturalW));
  try {
    const c = document.createElement("canvas");
    c.width = w;
    c.height = h;
    const ctx = c.getContext("2d", { willReadFrequently: true });
    ctx.drawImage(source, 0, 0, w, h);
    const { data } = ctx.getImageData(0, 0, w, h);
    const rowLuma = [];
    for (let y = 0; y < h; y++) {
      let sum = 0;
      for (let x = 0; x < w; x++) {
        const i = (y * w + x) * 4;
        sum += 0.299 * data[i] + 0.587 * data[i + 1] + 0.114 * data[i + 2];
      }
      rowLuma.push(sum / w);
    }
    const top = countDarkRows(rowLuma);
    const bottom = countDarkRows([...rowLuma].reverse());
    if (top + bottom < 2) return naturalW / naturalH; // no bars worth trimming
    // Downsampling blends the last bar row into the first picture row, so each
    // bar reads a touch thin. Trimming one sampled row extra loses a sliver of
    // picture but guarantees no black edge survives, which reads far better.
    const scale = naturalH / h;
    return pictureRatioFromBars(
      naturalW,
      naturalH,
      (top + 1) * scale,
      (bottom + 1) * scale
    );
  } catch {
    return naturalW / naturalH; // tainted canvas or odd source: leave it alone
  }
}

function useFile(file) {
  clearMedia();
  const url = URL.createObjectURL(file);
  if (file.type.startsWith("video/")) {
    const el = document.createElement("video");
    el.src = url;
    el.muted = state.muted;
    el.loop = true;
    el.autoplay = true;
    el.playsInline = true;
    el.addEventListener(
      "loadeddata",
      () => {
        const mediaRatio = el.videoWidth / el.videoHeight;
        const picture = detectPictureRatio(el, el.videoWidth, el.videoHeight);
        if (state.media?.el === el) {
          state.media.mediaRatio = mediaRatio;
          state.media.pictureRatio = picture;
          layout(false);
          if (picture > mediaRatio * 1.05) showFlash("Black bars trimmed", "");
        }
      },
      { once: true }
    );
    attachMedia(el, "file", 16 / 9);
    showFlash("Playing your video", "now try Watch it in IMAX");
  } else {
    const el = document.createElement("img");
    el.alt = "";
    el.addEventListener(
      "load",
      () => {
        const mediaRatio = el.naturalWidth / el.naturalHeight;
        const picture = detectPictureRatio(el, el.naturalWidth, el.naturalHeight);
        if (state.media?.el === el) {
          state.media.mediaRatio = mediaRatio;
          state.media.pictureRatio = picture;
          layout(false);
        }
      },
      { once: true }
    );
    el.src = url;
    attachMedia(el, "file", 16 / 9);
    showFlash("Your picture", "now try Watch it in IMAX");
  }
}

/* ---------- YouTube ---------- */
// Browsers routinely refuse muted autoplay, and uploaders can switch embedding
// off entirely. Those are different problems with different answers, so we ask
// the real player which one we're looking at rather than guessing.

let player = null; // YT.Player once the API is up
let playing = false;
let tapTimer = null;
let apiPromise = null;

function loadYouTubeApi() {
  if (window.YT?.Player) return Promise.resolve(window.YT);
  if (apiPromise) return apiPromise;
  apiPromise = new Promise((resolve, reject) => {
    const previous = window.onYouTubeIframeAPIReady;
    window.onYouTubeIframeAPIReady = () => {
      previous?.();
      resolve(window.YT);
    };
    const s = document.createElement("script");
    s.src = "https://www.youtube.com/iframe_api";
    s.onerror = () => reject(new Error("player API blocked"));
    document.head.appendChild(s);
    setTimeout(() => reject(new Error("player API timed out")), 8000);
  });
  return apiPromise;
}

function showTap(on) {
  $("tap").classList.toggle("show", on);
}

// Called from a click on our own page, so the browser counts it as the user
// asking for playback — which is exactly what a blocked autoplay was missing.
function startPlayback() {
  showTap(false);
  if (player?.playVideo) player.playVideo();
  else ytCommand("playVideo");
}

function watchUrl(id) {
  return `https://www.youtube.com/watch?v=${id}`;
}

function useYouTube(input) {
  const id = parseYouTubeId(input);
  if (!id) {
    showFlash("Hmm", "that doesn't look like a YouTube link");
    return;
  }
  clearMedia();
  const el = document.createElement("iframe");
  el.id = "ytplayer";
  el.src =
    `https://www.youtube-nocookie.com/embed/${id}` +
    `?autoplay=1&mute=1&loop=1&playlist=${id}` +
    `&controls=0&rel=0&playsinline=1&modestbranding=1&enablejsapi=1&origin=${location.origin}`;
  el.allow = "autoplay; encrypted-media; fullscreen";
  el.title = "Trailer";
  state.muted = true;
  state.videoId = id;
  playing = false;
  player = null;
  attachMedia(el, "yt", YT_RATIO);
  syncButtons();
  showFlash("Rolling", "now hit Watch it in IMAX");

  // If it hasn't started shortly, offer a button that starts it for real.
  clearTimeout(tapTimer);
  tapTimer = setTimeout(() => {
    if (!playing && state.media?.el === el) showTap(true);
  }, 2600);

  loadYouTubeApi()
    .then((YT) => {
      if (state.media?.el !== el) return; // something else got loaded meanwhile
      player = new YT.Player(el, {
        events: {
          onReady: () => player.playVideo?.(),
          onStateChange: (e) => {
            playing = e.data === YT.PlayerState.PLAYING;
            if (playing) {
              showTap(false);
              hideNotice();
            } else if (e.data === YT.PlayerState.UNSTARTED) {
              showTap(true);
            }
          },
          onError: (e) => {
            // 101/150: embedding disabled by the uploader. 2/100: bad or gone.
            const blocked = e.data === 101 || e.data === 150;
            showTap(false);
            showNotice(
              blocked
                ? "The uploader doesn't allow this video to play outside YouTube. Try a different trailer — most official ones work."
                : "That video couldn't be loaded. Check the link, or try a different trailer.",
              watchUrl(id)
            );
          },
        },
      });
    })
    .catch(() => {
      // API blocked (an extension, a strict network). The embed itself may well
      // be fine, so just make sure the viewer has a way to start it.
      if (!playing && state.media?.el === el) showTap(true);
    });
}

function ytCommand(func) {
  if (state.media?.kind !== "yt") return;
  state.media.el.contentWindow?.postMessage(
    JSON.stringify({ event: "command", func, args: "" }),
    "*"
  );
}

/* ---------- wiring ---------- */
buildButtons();

$("ytBtn").addEventListener("click", () => useYouTube($("ytUrl").value));
$("ytUrl").addEventListener("keydown", (e) => {
  if (e.key === "Enter") useYouTube($("ytUrl").value);
});
// pasting a link is the whole point — don't make people hunt for Play too
$("ytUrl").addEventListener("paste", (e) => {
  const text = e.clipboardData?.getData("text");
  if (parseYouTubeId(text)) setTimeout(() => useYouTube(text), 120);
});

$("mediaBtn").addEventListener("click", () => $("mediaInput").click());
$("mediaInput").addEventListener("change", (e) => {
  const file = e.target.files?.[0];
  if (file) useFile(file);
});

// drop a video or picture anywhere on the page
document.addEventListener("dragover", (e) => e.preventDefault());
document.addEventListener("drop", (e) => {
  e.preventDefault();
  const file = [...(e.dataTransfer?.files ?? [])].find((f) =>
    /^(video|image)\//.test(f.type)
  );
  if (file) return useFile(file);
  const text = e.dataTransfer?.getData("text");
  if (text && parseYouTubeId(text)) useYouTube(text);
});

$("imaxBtn").addEventListener("click", () => {
  setScreen(IMAX, { announce: false });
  document.documentElement.requestFullscreen?.().catch(() => {});
  showFlash("IMAX", `about ${gainPercent(IMAX.ratio)}% more picture than a normal cinema`);
});

$("fsBtn").addEventListener("click", () => {
  if (document.fullscreenElement) document.exitFullscreen();
  else document.documentElement.requestFullscreen?.().catch(() => {});
});

$("tapBtn").addEventListener("click", startPlayback);
$("noticeClose").addEventListener("click", hideNotice);

$("soundBtn").addEventListener("click", () => {
  state.muted = !state.muted;
  if (state.media?.kind === "yt") {
    if (player?.unMute) state.muted ? player.mute() : player.unMute();
    else ytCommand(state.muted ? "mute" : "unMute");
  } else if (state.media?.el instanceof HTMLVideoElement) {
    state.media.el.muted = state.muted;
  }
  syncButtons();
});

$("missBtn").addEventListener("click", () => {
  state.showMiss = !state.showMiss;
  if (state.showMiss && state.screen.key !== "imax") {
    setScreen(IMAX, { announce: false }); // the point only lands on the tall frame
  }
  layoutMissBands();
  updateCaption();
  syncButtons();
  if (state.showMiss) showFlash("Look at the edges", "that's what a normal cinema cuts");
});

$("trimBtn").addEventListener("click", () => {
  state.trim = !state.trim;
  layout(false);
  syncButtons();
  showFlash(state.trim ? "Black bars trimmed" : "Showing the full video");
});

$("shareBtn").addEventListener("click", async () => {
  const url = buildShareUrl(location.href, {
    video: state.videoId,
    screenKey: state.screen.key,
  });
  try {
    await navigator.clipboard.writeText(url);
    showFlash("Link copied", "share it and it opens exactly like this");
  } catch {
    showFlash("Copy this link", url);
  }
});

new ResizeObserver(() => layout(false)).observe(stage);

/* ---------- open in whatever state the link asked for ---------- */
const shared = parseShareParams(location.search);
if (shared.screen) state.screen = shared.screen;
layout(false);
updateCaption();
syncButtons();

if (shared.video) {
  useYouTube(shared.video);
  setScreen(shared.screen ?? IMAX, { announce: false });
} else {
  setTimeout(
    () => showFlash("Watch it in IMAX", "paste a trailer, then tap the blue button"),
    700
  );
}
