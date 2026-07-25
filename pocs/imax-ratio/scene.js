// The animated demo "movie" that plays until the viewer loads their own video.
// Painted procedurally so the app ships with zero binary assets and always has
// something moving on screen — no network, no uploads, nothing to block.
//
// The scene is drawn at the full IMAX shape (1.43:1). Wider screen choices crop
// it top and bottom, so the sky and foreground rocks are the parts only the
// tall frame keeps — which is exactly the point the app is making.

const W = 1144;
const H = 800; // 1.43:1

export function createScene(canvas) {
  canvas.width = W;
  canvas.height = H;
  const ctx = canvas.getContext("2d");

  // deterministic randomness so the scene looks the same every visit
  let seed = 42;
  const rand = () => (seed = (seed * 16807) % 2147483647) / 2147483647;

  const HORIZON = H * 0.62;
  const sunX = W * 0.62;
  const sunY = H * 0.55;

  const stars = Array.from({ length: 150 }, () => ({
    x: rand() * W,
    y: rand() * HORIZON * 0.62,
    r: rand() * 1.3 + 0.25,
    base: 0.25 + rand() * 0.5,
    speed: 0.6 + rand() * 2.2,
    phase: rand() * Math.PI * 2,
  }));

  const shimmer = Array.from({ length: 90 }, () => {
    const depth = rand(); // 0 at horizon, 1 at the bottom of the frame
    return {
      depth,
      x: sunX + (rand() - 0.5) * (60 + depth * 420),
      w: 12 + rand() * 70,
      base: 0.06 + rand() * 0.24,
      speed: 0.4 + rand() * 1.6,
      phase: rand() * Math.PI * 2,
    };
  });

  const clouds = Array.from({ length: 7 }, () => ({
    x: rand() * W,
    y: HORIZON * (0.32 + rand() * 0.5),
    w: 130 + rand() * 320,
    h: 10 + rand() * 20,
    alpha: 0.05 + rand() * 0.12,
    speed: 1.6 + rand() * 4.5, // px per second
  }));

  const birds = Array.from({ length: 5 }, () => ({
    x: rand() * W,
    y: HORIZON * (0.3 + rand() * 0.35),
    size: 5 + rand() * 6,
    speed: 12 + rand() * 22,
    phase: rand() * Math.PI * 2,
  }));

  // Static parts are painted once into an offscreen canvas and blitted each
  // frame — only the moving pieces are redrawn.
  const bg = document.createElement("canvas");
  bg.width = W;
  bg.height = H;
  paintBackdrop(bg.getContext("2d"));

  function paintBackdrop(c) {
    const sky = c.createLinearGradient(0, 0, 0, HORIZON);
    sky.addColorStop(0, "#08172e");
    sky.addColorStop(0.5, "#27467a");
    sky.addColorStop(0.84, "#c8783c");
    sky.addColorStop(1, "#f2b65e");
    c.fillStyle = sky;
    c.fillRect(0, 0, W, HORIZON);

    const glow = c.createRadialGradient(sunX, sunY, 8, sunX, sunY, 260);
    glow.addColorStop(0, "rgba(255,214,140,0.95)");
    glow.addColorStop(0.25, "rgba(255,180,90,0.45)");
    glow.addColorStop(1, "rgba(255,180,90,0)");
    c.fillStyle = glow;
    c.fillRect(sunX - 260, sunY - 260, 520, 520);
    c.fillStyle = "#ffe4ad";
    c.beginPath();
    c.arc(sunX, sunY, 40, 0, Math.PI * 2);
    c.fill();

    // islands
    c.fillStyle = "#182741";
    c.beginPath();
    c.moveTo(0, HORIZON);
    c.lineTo(W * 0.15, HORIZON - 52);
    c.lineTo(W * 0.33, HORIZON);
    c.closePath();
    c.fill();
    c.beginPath();
    c.moveTo(W * 0.71, HORIZON);
    c.lineTo(W * 0.87, HORIZON - 40);
    c.lineTo(W, HORIZON - 4);
    c.lineTo(W, HORIZON);
    c.closePath();
    c.fill();

    const sea = c.createLinearGradient(0, HORIZON, 0, H);
    sea.addColorStop(0, "#b06f3c");
    sea.addColorStop(0.1, "#3a4b70");
    sea.addColorStop(1, "#081222");
    c.fillStyle = sea;
    c.fillRect(0, HORIZON, W, H - HORIZON);

    // foreground rocks — the bottom band only the tall frame keeps
    c.fillStyle = "#05090f";
    c.beginPath();
    c.moveTo(0, H);
    c.lineTo(0, H * 0.88);
    c.quadraticCurveTo(W * 0.13, H * 0.84, W * 0.23, H);
    c.closePath();
    c.fill();
    c.beginPath();
    c.moveTo(W, H);
    c.lineTo(W, H * 0.92);
    c.quadraticCurveTo(W * 0.87, H * 0.88, W * 0.79, H);
    c.closePath();
    c.fill();
  }

  function drawShip(t) {
    // drifts slowly across the sun path and bobs on the swell
    const x = sunX - 120 + ((t * 7) % (W * 0.5));
    const y = H * 0.775 + Math.sin(t * 0.9) * 3;
    const s = 0.62;
    ctx.save();
    ctx.translate(x, y);
    ctx.rotate(Math.sin(t * 0.7) * 0.02);
    ctx.scale(s, s);
    ctx.fillStyle = "#080d18";
    ctx.beginPath();
    ctx.moveTo(-90, 0);
    ctx.quadraticCurveTo(0, 34, 90, 0);
    ctx.lineTo(104, -16);
    ctx.lineTo(-104, -16);
    ctx.closePath();
    ctx.fill();
    ctx.fillRect(-3, -92, 5, 78);
    ctx.beginPath();
    ctx.moveTo(4, -88);
    ctx.quadraticCurveTo(74, -56, 4, -22);
    ctx.closePath();
    ctx.fill();
    ctx.restore();
  }

  function drawBird(b, t) {
    const x = (b.x + t * b.speed) % (W + 60) - 30;
    const flap = Math.sin(t * 6 + b.phase) * 0.4;
    ctx.strokeStyle = "rgba(10,16,28,0.75)";
    ctx.lineWidth = 1.6;
    ctx.beginPath();
    ctx.moveTo(x - b.size, b.y + flap * b.size);
    ctx.quadraticCurveTo(x, b.y - b.size * 0.5, x + b.size, b.y + flap * b.size);
    ctx.stroke();
  }

  function frame(t) {
    ctx.drawImage(bg, 0, 0);

    for (const s of stars) {
      ctx.globalAlpha = Math.max(0, s.base + Math.sin(t * s.speed + s.phase) * 0.28);
      ctx.fillStyle = "#ffffff";
      ctx.beginPath();
      ctx.arc(s.x, s.y, s.r, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.globalAlpha = 1;

    for (const c of clouds) {
      const x = (c.x + t * c.speed) % (W + c.w) - c.w;
      ctx.globalAlpha = c.alpha;
      ctx.fillStyle = "#dfe8ff";
      ctx.beginPath();
      ctx.ellipse(x + c.w / 2, c.y, c.w / 2, c.h, 0, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.globalAlpha = 1;

    for (const b of birds) drawBird(b, t);

    drawShip(t);

    // the sun path on the water, shimmering
    ctx.fillStyle = "#ffd08a";
    for (const s of shimmer) {
      const y = HORIZON + 6 + s.depth * (H - HORIZON - 10);
      const drift = Math.sin(t * s.speed + s.phase);
      ctx.globalAlpha = Math.max(0, s.base + drift * 0.12);
      ctx.fillRect(s.x + drift * 9 - s.w / 2, y, s.w, 2.2);
    }
    ctx.globalAlpha = 1;
  }

  let raf = null;
  let last = 0;
  const reduced =
    typeof matchMedia === "function" &&
    matchMedia("(prefers-reduced-motion: reduce)").matches;

  function loop(now) {
    // ~30fps is plenty for this and keeps laptops cool
    if (now - last > 33) {
      last = now;
      frame(now / 1000);
    }
    raf = requestAnimationFrame(loop);
  }

  return {
    start() {
      if (reduced) return frame(0); // one still frame, no motion
      if (raf == null) raf = requestAnimationFrame(loop);
    },
    stop() {
      if (raf != null) cancelAnimationFrame(raf);
      raf = null;
    },
  };
}
