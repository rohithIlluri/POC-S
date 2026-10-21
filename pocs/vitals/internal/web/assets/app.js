function hexBytes(hex) {
  const out = new Uint8Array((hex || "").length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.substr(2 * i, 2), 16);
  return out;
}

// Reassembles WHOOP frames from BLE notifications and validates both
// checksums; mirrors internal/whoopble (the Go side is the tested reference):
//   0xAA | len u16 LE | crc8(len) | body | crc32(body) u32 LE
class WhoopFrames {
  constructor() {
    this.buf = new Uint8Array(0);
  }
  static crc8(a, b) {
    let c = 0;
    for (const byte of [a, b]) {
      c ^= byte;
      for (let i = 0; i < 8; i++) c = c & 0x80 ? ((c << 1) ^ 0x07) & 0xff : (c << 1) & 0xff;
    }
    return c;
  }
  static crc32(bytes) {
    let c = ~0;
    for (const byte of bytes) {
      c ^= byte;
      for (let i = 0; i < 8; i++) c = c & 1 ? (c >>> 1) ^ 0xedb88320 : c >>> 1;
    }
    return ~c >>> 0;
  }
  push(chunk) {
    const merged = new Uint8Array(this.buf.length + chunk.length);
    merged.set(this.buf);
    merged.set(chunk, this.buf.length);
    let b = merged;
    const bodies = [];
    for (;;) {
      const start = b.indexOf(0xaa);
      if (start < 0) {
        b = new Uint8Array(0);
        break;
      }
      b = b.subarray(start);
      if (b.length < 4) break;
      const len = b[1] | (b[2] << 8);
      if (WhoopFrames.crc8(b[1], b[2]) !== b[3] || len < 5) {
        b = b.subarray(1);
        continue;
      }
      if (b.length < 4 + len) break;
      const body = b.subarray(4, len);
      const want = (b[len] | (b[len + 1] << 8) | (b[len + 2] << 16) | (b[len + 3] << 24)) >>> 0;
      if (WhoopFrames.crc32(body) !== want) {
        b = b.subarray(1);
        continue;
      }
      bodies.push(body.slice());
      b = b.subarray(4 + len);
    }
    this.buf = b.slice();
    return bodies;
  }
}

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
  const clockBtn = $("strap-clock");
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
    clockBtn.hidden = !connected || !whoopCmd;
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

  const onSample = (bpm, rr) => {
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

  const onMeasurement = (event) => {
    const { bpm, rr } = parse(event.target.value);
    onSample(bpm, rr);
  };

  // WHOOP 4.0 direct: the strap's own realtime stream (packet type 0x28)
  // carries heart rate plus up to four RR intervals per second.
  const card = $("strap");
  const whoop = {
    service: card.dataset.whoopService,
    cmd: card.dataset.whoopCmd,
    data: card.dataset.whoopData,
    cmdFrom: card.dataset.whoopCmdFrom,
    events: card.dataset.whoopEvents,
    on: hexBytes(card.dataset.whoopRtOn),
    off: hexBytes(card.dataset.whoopRtOff),
  };
  let whoopCmd = null;
  const frames = new WhoopFrames();

  // Raw capture of everything exchanged with a WHOOP, for protocol work:
  // "<ms>\t<channel>\t<hex>" lines, read by `vitals strap decode`.
  const captureBtn = $("strap-capture");
  let capture = [];
  let captureStart = 0;
  const toHex = (u8) => Array.from(u8, (x) => x.toString(16).padStart(2, "0")).join("");
  const logRaw = (channel, u8) => {
    if (capture.length < 200000) capture.push(`${Date.now() - captureStart}\t${channel}\t${toHex(u8)}`);
  };
  const bytesOf = (dv) => new Uint8Array(dv.buffer, dv.byteOffset, dv.byteLength);
  // Only on a click: ask the server for the SetClock frame (it holds the
  // tested builder) and write it to the strap. The answer lands in the capture.
  clockBtn.addEventListener("click", async () => {
    clockBtn.disabled = true;
    try {
      const res = await fetch("/strap/clock-command", { cache: "no-store" });
      if (!res.ok) throw new Error(`server said ${res.status}`);
      await writeCmd(hexBytes((await res.text()).trim()));
      status.textContent = "Clock sent to the strap — check the capture for its answer";
    } catch (err) {
      status.textContent = `Couldn't set the clock: ${(err && err.message) || err}`;
    } finally {
      clockBtn.disabled = false;
    }
  });
  captureBtn.addEventListener("click", () => {
    const header = [
      "# vitals WHOOP capture v1",
      `# device: ${(device && device.name) || "unknown"}`,
      `# started: ${new Date(captureStart).toISOString()}`,
      "# summarise with: vitals strap decode <this file>",
    ];
    const blob = new Blob([header.concat(capture).join("\n") + "\n"], { type: "text/plain" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `whoop-capture-${new Date(captureStart).toISOString().replace(/[:.]/g, "-")}.txt`;
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  });

  const onWhoopData = (event) => {
    const raw = bytesOf(event.target.value);
    logRaw("data", raw);
    for (const body of frames.push(raw)) {
      if (body[0] !== 0x28 || body.length < 18) continue;
      const n = body[9];
      const rr = [];
      for (let i = 0; i < n && i < 4; i++) {
        const v = body[10 + 2 * i] | (body[11 + 2 * i] << 8);
        if (v >= 300 && v <= 2000) rr.push(v);
      }
      onSample(body[8], rr);
    }
  };
  const writeCmd = (bytes) => {
    logRaw("write", bytes);
    // The official app writes every command with response; the strap never
    // answered our write-without-response in captures, so match it when we can.
    return whoopCmd.properties.write && whoopCmd.writeValueWithResponse
      ? whoopCmd.writeValueWithResponse(bytes)
      : whoopCmd.writeValue(bytes);
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

  // Prefer the WHOOP's own realtime stream (no app or membership needed);
  // fall back to the standard Heart Rate service for any other strap, or a
  // WHOOP with broadcast already on.
  const connectWhoop = async (server) => {
    const svc = await server.getPrimaryService(whoop.service);
    whoopCmd = await svc.getCharacteristic(whoop.cmd);
    const data = await svc.getCharacteristic(whoop.data);
    capture = [];
    captureStart = Date.now();
    data.addEventListener("characteristicvaluechanged", onWhoopData);
    await data.startNotifications();
    // Listen (only) on the strap's other two channels so captures are complete.
    for (const [name, uuid] of [["cmd", whoop.cmdFrom], ["events", whoop.events]]) {
      try {
        const ch = await svc.getCharacteristic(uuid);
        ch.addEventListener("characteristicvaluechanged", (e) => logRaw(name, bytesOf(e.target.value)));
        await ch.startNotifications();
      } catch {
        // Not every firmware exposes both; the capture just won't include it.
      }
    }
    await writeCmd(whoop.on);
    captureBtn.hidden = false;
  };
  const connectStandard = async (server) => {
    const service = await server.getPrimaryService("heart_rate");
    const ch = await service.getCharacteristic("heart_rate_measurement");
    ch.addEventListener("characteristicvaluechanged", onMeasurement);
    await ch.startNotifications();
  };

  connectBtn.addEventListener("click", async () => {
    try {
      status.textContent = "Choose your strap…";
      device = await navigator.bluetooth.requestDevice({
        filters: [{ services: ["heart_rate"] }, { namePrefix: "WHOOP" }],
        optionalServices: ["heart_rate", whoop.service],
      });
      device.addEventListener("gattserverdisconnected", () => {
        whoopCmd = null;
        captureBtn.hidden = capture.length === 0;
        stop();
        show(false);
        status.textContent = "Disconnected";
      });
      status.textContent = "Connecting…";
      const server = await device.gatt.connect();
      let mode = "heart-rate broadcast";
      try {
        await connectWhoop(server);
        mode = "WHOOP direct";
      } catch {
        whoopCmd = null;
        await connectStandard(server);
      }
      status.textContent = `Connected · ${device.name || "heart-rate strap"} · ${mode}`;
      recent = [];
      show(true);
      describe();
    } catch (err) {
      show(false);
      status.textContent =
        err && err.name === "NotFoundError"
          ? "No strap chosen — is it charged, nearby, and the WHOOP app closed?"
          : `Couldn't connect: ${(err && err.message) || err}`;
    }
  });

  disconnectBtn.addEventListener("click", async () => {
    stop();
    if (whoopCmd) {
      // Stop the realtime stream so the strap goes back to normal battery use.
      await writeCmd(whoop.off).catch(() => {});
      whoopCmd = null;
    }
    if (device && device.gatt.connected) device.gatt.disconnect();
    show(false);
    status.textContent = "Not connected";
  });
}
