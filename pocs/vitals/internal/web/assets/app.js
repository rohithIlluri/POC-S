// Progressive enhancement only: every feature works without this file.
(() => {
  // Show the "name" field for event metrics (symptom, med).
  const metric = document.getElementById("metric");
  const labelField = document.getElementById("label-field");
  if (metric && labelField) {
    const sync = () => {
      labelField.hidden = metric.selectedOptions[0]?.dataset.event !== "true";
    };
    metric.addEventListener("change", sync);
    sync();
  }

  // Chart hover: crosshair + tooltip on the nearest day, with a hit area
  // that is the whole plot rather than the tiny markers.
  for (const fig of document.querySelectorAll("[data-chart]")) {
    const svg = fig.querySelector("svg");
    const tip = fig.querySelector(".tip");
    const cross = fig.querySelector(".cross");
    const marker = fig.querySelector(".marker");
    const pts = [...fig.querySelectorAll(".pt")];
    if (!svg || !tip || !marker || !pts.length) continue;
    const vbWidth = svg.viewBox.baseVal.width;
    const vbHeight = svg.viewBox.baseVal.height;
    // The SVG stretches to its box, so markers are HTML dots placed in
    // percentages — an SVG circle would be squashed into an ellipse.
    const place = (p) => {
      marker.style.left = `${(p.cx.baseVal.value / vbWidth) * 100}%`;
      marker.style.top = `${(p.cy.baseVal.value / vbHeight) * 100}%`;
      marker.hidden = false;
    };
    const rest = () => {
      if (marker.hasAttribute("data-single")) place(pts[0]);
      else marker.hidden = true;
    };
    rest();

    const show = (clientX) => {
      const box = svg.getBoundingClientRect();
      const x = ((clientX - box.left) / box.width) * vbWidth;
      let best = pts[0];
      for (const p of pts) {
        if (Math.abs(p.cx.baseVal.value - x) < Math.abs(best.cx.baseVal.value - x)) best = p;
      }
      place(best);
      const px = best.cx.baseVal.value;
      cross.setAttribute("x1", px);
      cross.setAttribute("x2", px);
      cross.hidden = false;
      tip.textContent = best.dataset.tip;
      tip.hidden = false;
      tip.style.left = `${(px / vbWidth) * 100}%`;
    };
    const hide = () => {
      tip.hidden = true;
      cross.hidden = true;
      rest();
    };
    svg.addEventListener("pointermove", (e) => show(e.clientX));
    svg.addEventListener("pointerdown", (e) => show(e.clientX));
    svg.addEventListener("pointerleave", hide);
  }

  setupStrap();
})();

// Live heart rate from a Bluetooth strap via Web Bluetooth, using only the
// standard Heart Rate service (0x180D) — the same thing fitness apps read.
// The page records raw samples; the server does the maths (internal/hrv).
function setupStrap() {
  const $ = (id) => document.getElementById(id);
  const status = $("strap-status");
  const connectBtn = $("strap-connect");
  const restBtn = $("strap-rest");
  const workoutBtn = $("strap-workout");
  const disconnectBtn = $("strap-disconnect");
  const live = $("strap-live");
  const bpmEl = $("strap-bpm");
  const trace = $("strap-trace");
  const note = $("strap-note");
  const form = $("strap-form");
  if (!status || !form) return;

  if (!navigator.bluetooth) {
    status.textContent = "Not available in this browser";
    return;
  }
  status.textContent = "Not connected";
  connectBtn.hidden = false;

  const REST_SECONDS = 120;
  let device = null;
  let recent = []; // last ~2 min of bpm for the trace
  let sawRR = false;
  let rec = null; // { kind, hr: [], rr: [], start, timer }

  const show = (connected) => {
    connectBtn.hidden = connected;
    restBtn.hidden = workoutBtn.hidden = disconnectBtn.hidden = !connected;
    live.hidden = !connected;
  };

  // Heart Rate Measurement (0x2A37): flags, bpm (uint8 or uint16), optional
  // energy expended (uint16), optional RR intervals (uint16, 1/1024 s).
  const parse = (dv) => {
    const flags = dv.getUint8(0);
    let i = 1;
    const bpm = flags & 0x01 ? dv.getUint16(i, true) : dv.getUint8(i);
    i += flags & 0x01 ? 2 : 1;
    if (flags & 0x08) i += 2;
    const rr = [];
    if (flags & 0x10) {
      for (; i + 1 < dv.byteLength; i += 2) rr.push((dv.getUint16(i, true) * 1000) / 1024);
    }
    return { bpm, rr };
  };

  const draw = () => {
    if (recent.length < 2) return;
    const lo = Math.min(...recent) - 2;
    const hi = Math.max(...recent) + 2;
    const step = 300 / (recent.length - 1);
    trace.setAttribute("d", recent.map((v, i) => `${i ? "L" : "M"}${(i * step).toFixed(1)} ${(((hi - v) / (hi - lo)) * 60).toFixed(1)}`).join(" "));
  };

  const clock = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`;
  const elapsed = () => (Date.now() - rec.start) / 1000;

  const describe = () => {
    if (!rec) {
      note.textContent = sawRR
        ? "Beat-to-beat intervals detected — a resting check will also save HRV."
        : "Waiting for beat-to-beat data… if none arrives, a resting check saves heart rate only.";
      return;
    }
    if (rec.kind === "resting") {
      note.textContent = `Sit still and breathe normally · ${clock(Math.max(0, REST_SECONDS - elapsed()))} left · ${rec.rr.length} beats captured`;
    } else {
      note.textContent = `Workout recording · ${clock(elapsed())}`;
    }
  };

  const onMeasurement = (event) => {
    const { bpm, rr } = parse(event.target.value);
    if (rr.length) sawRR = true;
    if (bpm > 0) {
      bpmEl.textContent = bpm;
      recent.push(bpm);
      if (recent.length > 120) recent.shift();
      draw();
    }
    if (rec) {
      rec.hr.push(bpm);
      rec.rr.push(...rr);
    }
    describe();
  };

  const save = () => {
    const f = form.elements;
    f.kind.value = rec.kind;
    f.hr.value = rec.hr.join(",");
    f.rr.value = rec.rr.map((v) => v.toFixed(1)).join(",");
    f.seconds.value = elapsed().toFixed(1);
    clearInterval(rec.timer);
    status.textContent = "Saving…";
    form.submit();
  };

  const stop = () => {
    if (!rec) return;
    clearInterval(rec.timer);
    rec = null;
    restBtn.textContent = "Resting check · 2 min";
    workoutBtn.textContent = "Start workout";
    restBtn.classList.remove("recording");
    workoutBtn.classList.remove("recording");
    restBtn.disabled = workoutBtn.disabled = false;
    describe();
  };

  const start = (kind) => {
    rec = { kind, hr: [], rr: [], start: Date.now() };
    rec.timer = setInterval(() => {
      describe();
      if (kind === "resting" && elapsed() >= REST_SECONDS) save();
    }, 1000);
    const btn = kind === "resting" ? restBtn : workoutBtn;
    btn.classList.add("recording");
    btn.textContent = kind === "resting" ? "Cancel check" : "Stop & save workout";
    (kind === "resting" ? workoutBtn : restBtn).disabled = true;
    describe();
  };

  restBtn.addEventListener("click", () => (rec ? stop() : start("resting")));
  workoutBtn.addEventListener("click", () => {
    if (!rec) return start("workout");
    if (elapsed() < 60) {
      note.textContent = "Workouts under a minute aren't saved — keep going or press again after 1:00.";
      return;
    }
    save();
  });

  connectBtn.addEventListener("click", async () => {
    try {
      status.textContent = "Choose your strap…";
      device = await navigator.bluetooth.requestDevice({ filters: [{ services: ["heart_rate"] }] });
      device.addEventListener("gattserverdisconnected", () => {
        stop();
        show(false);
        status.textContent = "Disconnected";
      });
      status.textContent = "Connecting…";
      const server = await device.gatt.connect();
      const service = await server.getPrimaryService("heart_rate");
      const ch = await service.getCharacteristic("heart_rate_measurement");
      ch.addEventListener("characteristicvaluechanged", onMeasurement);
      await ch.startNotifications();
      status.textContent = `Connected · ${device.name || "heart-rate strap"}`;
      recent = [];
      show(true);
      describe();
    } catch (err) {
      show(false);
      status.textContent =
        err && err.name === "NotFoundError"
          ? "No strap chosen — is heart-rate broadcast on?"
          : `Couldn't connect: ${(err && err.message) || err}`;
    }
  });

  disconnectBtn.addEventListener("click", () => {
    stop();
    if (device && device.gatt.connected) device.gatt.disconnect();
    show(false);
    status.textContent = "Not connected";
  });
}
