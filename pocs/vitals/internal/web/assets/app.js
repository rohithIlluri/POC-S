// POST JSON to the local server. The token is read from the page once. If the
// server was restarted (it issues a new token) the refusal triggers one refresh
// and a retry, so recording never stalls silently.
let csrfToken = null;
// Tell the local server log what happened (button outcomes, script errors), so a
// problem in this browser can be read without the browser. Best effort, short text.
function report(event, message) {
  try {
    postJSON("/api/clientlog", { event, message: String(message || "") }).catch(() => {});
  } catch (e) {}
}
addEventListener("error", (e) => report("script_error", `${e.message} @ ${(e.filename || "").split("/").pop()}:${e.lineno}`));
addEventListener("unhandledrejection", (e) => report("unhandled_rejection", (e.reason && (e.reason.stack || e.reason.message)) || e.reason));
async function postJSON(path, body, opts) {
  if (csrfToken === null) csrfToken = document.querySelector('input[name="csrf"]').value;
  const send = () =>
    fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken },
      body: JSON.stringify(body),
      ...opts,
    });
  let res = await send();
  if (res.status === 403) {
    const t = await fetch("/api/csrf", { cache: "no-store" });
    if (t.ok) {
      csrfToken = (await t.json()).token;
      res = await send();
    }
  }
  return res;
}

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
  const syncBtn = $("strap-sync");
  const autoRow = $("strap-auto-row");
  const live = $("strap-live");
  const bpmEl = $("strap-bpm");
  const trace = $("strap-trace");
  const note = $("strap-note");
  // An explanation written to the note (like "workouts under a minute aren't saved")
  // is held for a few seconds; otherwise the once-a-second status text wipes it before it can be read.
  let holdNoteUntil = 0;
  const say = (text, ms = 6000) => {
    note.textContent = text;
    holdNoteUntil = Date.now() + ms;
  };
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
    clockBtn.hidden = syncBtn.hidden = !connected || !whoopCmd;
    autoRow.hidden = clockBtn.hidden;
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
    if (Date.now() < holdNoteUntil) return;
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

  // Every sample is also kept on this computer: batched every 5 s to the local
  // server, which stores it in the local database. Nothing leaves the machine.
  const outbox = [];
  let sending = false;
  const flushSamples = async () => {
    if (sending || outbox.length === 0) return;
    sending = true;
    const batch = outbox.splice(0, 2000);
    try {
      const res = await postJSON("/api/samples", { samples: batch }, { keepalive: true });
      if (!res.ok) throw new Error(String(res.status));
    } catch {
      outbox.unshift(...batch); // try again on the next tick
      if (outbox.length > 50000) outbox.splice(0, outbox.length - 50000);
    } finally {
      sending = false;
    }
  };
  setInterval(flushSamples, 5000);
  addEventListener("pagehide", flushSamples);

  const onSample = (bpm, rr, off = false) => {
    if (off) {
      // Off the wrist: record that (wear time is a metric) but never show or save a heart rate.
      outbox.push({ t: Date.now(), hr: 0, rr: [], off: true });
      bpmEl.textContent = "off";
      return;
    }
    if (rr.length) sawRR = true;
    if (bpm > 0) outbox.push({ t: Date.now(), hr: bpm, rr });
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
  // History download. The strap records on its own memory while nothing is
  // connected (a night with the computer off is still there in the morning).
  // The server runs the protocol and stores each burst BEFORE it hands back the
  // acknowledgement; this code only carries frames both ways. Acks are written
  // with response, like every command.
  let lastStrap = null; // { sec, wall }: a strap-clock reading paired with the wall clock
  const STRAP_CLOCK_VALID_FROM = 1577836800; // 2020-01-01: anything earlier is a strap that lost its time
  const clockHint = $("strap-clock-hint");
  let clockHintLocked = 0; // a verification message is shown until this time
  const updateClockHint = () => {
    if (Date.now() < clockHintLocked) return;
    if (!lastStrap) return void (clockHint.hidden = true);
    const bad = lastStrap.sec < STRAP_CLOCK_VALID_FROM;
    clockHint.hidden = !bad;
    clockHint.classList.remove("ok");
    if (bad) clockHint.textContent = "The strap's clock is not set (it reads 1971), so it is NOT saving heart-rate history: it logs \"RTC timestamp is invalid; not saving data to flash\". Click Set strap clock.";
  };
  let histActive = false;
  let histBusy = false;
  let lastHistoryAt = 0;
  const histQueue = [];
  const apiJSON = async (path, body) => {
    const res = await postJSON(path, body || {});
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || `server said ${res.status}`);
    return data;
  };
  let histDone = null; // resolves the promise of the running download
  let lastSync = null; // { at, message, ok, packets, bursts, heart, dated }
  const endHistory = (message, result) => {
    report("sync_ended", message);
    histActive = false;
    histQueue.length = 0;
    syncBtn.disabled = false;
    syncBtn.textContent = "Sync history";
    status.textContent = message;
    lastSync = { at: Date.now(), message, ok: false, ...(result || {}) };
    const done = histDone;
    histDone = null;
    if (done) done(lastSync);
    renderAutopilot();
  };
  const pumpHistory = async () => {
    if (histBusy) return;
    histBusy = true;
    try {
      while (histActive && histQueue.length) {
        const bodies = histQueue.splice(0, 500);
        const out = await apiJSON("/api/history/frames", { bodies });
        for (const reply of out.replies) await writeCmd(hexBytes(reply));
        syncBtn.textContent = `Syncing… ${out.records} packets, ${out.heart_records || 0} heart-rate records`;
        if (out.done) {
          const hr = out.heart_records || 0;
          return endHistory(
            `History synced: ${out.records} packets in ${out.bursts} batches, ${hr} heart-rate record${hr === 1 ? "" : "s"}${hr === 0 ? " (the strap had none yet)" : ""}`,
            { ok: true, packets: out.records, bursts: out.bursts, heart: hr, dated: out.dated || 0 }
          );
        }
      }
    } catch (err) {
      endHistory(`History sync stopped: ${(err && err.message) || err}. Nothing was acknowledged for the batch that failed, so the strap still has it.`, { ok: false });
    } finally {
      histBusy = false;
    }
  };
  // One download, as a promise that resolves with the outcome when it ends.
  const syncNow = (why) =>
    new Promise((resolve) => {
      if (histActive) return resolve({ ok: false, message: "already syncing" });
      report("sync_attempt", why);
      histDone = resolve;
      syncBtn.disabled = true;
      syncBtn.textContent = "Syncing…";
      (async () => {
        try {
          histQueue.length = 0;
          histActive = true;
          lastHistoryAt = Date.now();
          const fresh = lastStrap && Date.now() - lastStrap.wall < 5000 ? lastStrap : null;
          const { frame } = await apiJSON("/api/history/start", fresh ? { strap_sec: fresh.sec, wall_ms: fresh.wall } : {});
          await writeCmd(hexBytes(frame));
          status.textContent = "Asking the strap for what it recorded…";
          report("sync_started", "start command written");
          // Give up if the strap goes quiet for 30 s.
          const watchdog = setInterval(() => {
            if (!histActive) return clearInterval(watchdog);
            if (Date.now() - lastHistoryAt > 30000) {
              clearInterval(watchdog);
              apiJSON("/api/history/abort").then((a) => writeCmd(hexBytes(a.frame))).catch(() => {});
              endHistory("The strap stopped sending history. Try again; nothing was lost.", { ok: false });
            }
          }, 2000);
        } catch (err) {
          report("sync_failed", `${err && err.name}: ${err && err.message}`);
          endHistory(`Couldn't start the history sync: ${(err && err.message) || err}`, { ok: false });
        }
      })();
    });
  syncBtn.addEventListener("click", () => {
    report("click", "Sync history");
    syncNow("button");
  });

  // Only on a click: ask the server for the SetClock frame (it holds the
  // tested builder) and write it to the strap. The answer lands in the capture.
  // Set the strap's clock and prove it took, using the strap's own clock in its next packets.
  // Resolves true when the strap now reads the right time.
  const setClockNow = async (why) => {
    report("clock_attempt", why);
    clockBtn.disabled = true;
    try {
      const res = await fetch("/strap/clock-command", { cache: "no-store" });
      if (!res.ok) throw new Error(`server said ${res.status}`);
      await writeCmd(hexBytes((await res.text()).trim()));
      status.textContent = "Clock sent — checking that the strap took it…";
      // The strap's own clock rides on every realtime packet: wait for one that proves it.
      const sentAt = Date.now();
      clockHintLocked = sentAt + 15000;
      let ok = false;
      while (Date.now() - sentAt < 8000) {
        await new Promise((r) => setTimeout(r, 400));
        if (lastStrap && lastStrap.wall > sentAt && lastStrap.sec >= STRAP_CLOCK_VALID_FROM && Math.abs(lastStrap.sec - Date.now() / 1000) < 15) {
          ok = true;
          break;
        }
      }
      clockHint.hidden = false;
      clockHint.classList.toggle("ok", ok);
      if (ok) {
        status.textContent = "Strap clock set";
        clockHint.textContent = `Strap clock set: it now reads ${new Date(lastStrap.sec * 1000).toLocaleString()}. It will save heart-rate history from here on; sync it later with Sync history.`;
      } else {
        const reads = lastStrap ? new Date(lastStrap.sec * 1000).getUTCFullYear() : "nothing";
        status.textContent = "The strap did not take the clock";
        clockHint.textContent = `The strap still reads ${reads}: it did not accept the clock. Download the capture so it can be looked at.`;
      }
      clockHintLocked = Date.now() + 20000;
      report(ok ? "clock_verified" : "clock_not_taken", lastStrap ? `strap reads ${lastStrap.sec}` : "no live packet");
      return ok;
    } catch (err) {
      report("clock_failed", `${err && err.name}: ${err && err.message}`);
      status.textContent = `Couldn't set the clock: ${(err && err.message) || err}`;
      return false;
    } finally {
      clockBtn.disabled = false;
      renderAutopilot();
    }
  };
  clockBtn.addEventListener("click", () => {
    report("click", "Set strap clock");
    setClockNow("button");
  });

  // Autopilot. While the strap is connected: keep its clock right (a strap with an
  // invalid clock records NOTHING, so this is what makes recording happen at all),
  // and download what it recorded every 15 minutes. On by default, switchable, and
  // it only ever sends the clock and the history commands.
  const AUTO = Object.assign({ first: 7000, every: 20000, syncEvery: 15 * 60 * 1000, clockRetry: 10 * 60 * 1000, drift: 5 }, window.__autoConfig || {});
  const autoBox = $("strap-auto");
  const autoLine = $("strap-auto-status");
  let autopilotOn = true;
  try {
    autopilotOn = localStorage.getItem("vitals.autopilot") !== "off";
  } catch (e) {}
  autoBox.checked = autopilotOn;
  let autoBusy = false;
  let autoTimer = null;
  let autoFirst = null;
  let lastAutoSync = 0;
  let lastClockTry = 0;
  const strapDrift = () => (lastStrap ? lastStrap.sec - lastStrap.wall / 1000 : Infinity);
  const clockGood = () => !!lastStrap && lastStrap.sec >= STRAP_CLOCK_VALID_FROM && Math.abs(strapDrift()) <= AUTO.drift;
  const hhmm = (ms) => new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  function renderAutopilot() {
    if (!autoLine) return;
    if (!autopilotOn) {
      autoLine.textContent = "Autopilot is off: set the clock and sync by hand.";
      return;
    }
    const parts = [];
    parts.push(!lastStrap ? "reading the strap's clock…" : clockGood() ? "clock correct" : "clock needs setting");
    if (lastSync) parts.push(lastSync.ok ? `last sync ${hhmm(lastSync.at)}: ${lastSync.heart} heart-rate record${lastSync.heart === 1 ? "" : "s"}` : `last sync ${hhmm(lastSync.at)} did not finish`);
    else parts.push("no sync yet");
    if (autoTimer && lastAutoSync) parts.push(`next sync about ${hhmm(lastAutoSync + AUTO.syncEvery)}`);
    autoLine.textContent = `Autopilot · ${parts.join(" · ")}`;
  }
  const autopilotTick = async () => {
    if (autoBusy || !autopilotOn || !whoopCmd || histActive) return renderAutopilot();
    if (!lastStrap || Date.now() - lastStrap.wall > 10000) return renderAutopilot(); // no fresh clock reading yet
    autoBusy = true;
    try {
      if (!clockGood() && Date.now() - lastClockTry > AUTO.clockRetry) {
        lastClockTry = Date.now();
        report("autopilot", "the strap's clock is wrong: setting it");
        await setClockNow("auto");
      }
      if (clockGood() && Date.now() - lastAutoSync > AUTO.syncEvery) {
        lastAutoSync = Date.now();
        report("autopilot", "downloading the strap's history");
        await syncNow("auto");
      }
    } finally {
      autoBusy = false;
      renderAutopilot();
    }
  };
  const stopAutopilot = () => {
    clearTimeout(autoFirst);
    clearInterval(autoTimer);
    autoTimer = null;
  };
  const startAutopilot = () => {
    stopAutopilot();
    lastAutoSync = 0;
    lastClockTry = 0;
    autoFirst = setTimeout(autopilotTick, AUTO.first);
    autoTimer = setInterval(autopilotTick, AUTO.every);
    renderAutopilot();
  };
  autoBox.addEventListener("change", () => {
    autopilotOn = autoBox.checked;
    try {
      localStorage.setItem("vitals.autopilot", autopilotOn ? "on" : "off");
    } catch (e) {}
    report("autopilot_toggle", autopilotOn ? "on" : "off");
    renderAutopilot();
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
      // During a download, everything on the data channel except the live stream is part of the
      // history (heart records 0x2f, console logs 0x32, markers 0x31...): the strap's end marker
      // counts them all, so the server has to see them all.
      if (histActive && body[0] !== 0x28) {
        histQueue.push(toHex(body));
        lastHistoryAt = Date.now();
        pumpHistory();
        continue;
      }
      if (body[0] === 0x28 && body.length >= 6) {
        // The strap's own clock, read at the moment we received it: lets history stamped on an unset clock be dated.
        lastStrap = { sec: (body[2] | (body[3] << 8) | (body[4] << 16) | (body[5] << 24)) >>> 0, wall: Date.now() };
        updateClockHint();
      }
      if (body[0] !== 0x28 || body.length < 18) continue;
      const n = body[9];
      const rr = [];
      for (let i = 0; i < n && i < 4; i++) {
        const v = body[10 + 2 * i] | (body[11 + 2 * i] << 8);
        if (v >= 300 && v <= 2000) rr.push(v);
      }
      onSample(body[8], rr, body.length >= 20 && body[18] === 0);
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

  // Saving a session happens in the background, not with a form submit: a submit
  // reloads the page, and a reload closes the Bluetooth connection, which would
  // end the live recording every time a resting check or workout finished.
  const flashBanner = (text, level) => {
    const p = document.createElement("p");
    p.className = `flash flash-${level}`;
    p.textContent = text;
    p.setAttribute("role", "status");
    const main = document.querySelector("main");
    main.prepend(p);
    setTimeout(() => p.remove(), 15000);
  };
  const save = async () => {
    const f = form.elements;
    f.kind.value = rec.kind;
    f.hr.value = rec.hr.join(",");
    f.rr.value = rec.rr.map((v) => v.toFixed(1)).join(",");
    f.seconds.value = elapsed().toFixed(1);
    clearInterval(rec.timer);
    const body = new URLSearchParams(new FormData(form));
    stop();
    status.textContent = "Saving…";
    try {
      const res = await fetch(form.action, { method: "POST", body, headers: { "Content-Type": "application/x-www-form-urlencoded" } });
      if (!res.ok) throw new Error((await res.text()).trim() || `server said ${res.status}`);
      // The server answers with the dashboard and the result in its flash line; read just that text.
      const doc = new DOMParser().parseFromString(await res.text(), "text/html");
      const flash = doc.querySelector(".flash");
      const text = flash ? flash.textContent.trim() : "Saved.";
      const bad = flash && flash.classList.contains("flash-watch");
      flashBanner(text, bad ? "watch" : "good");
    } catch (err) {
      flashBanner(`Couldn't save the session: ${(err && err.message) || err}`, "watch");
    } finally {
      status.textContent = device && device.gatt && device.gatt.connected ? `Connected · ${device.name || "strap"}` : "Not connected";
    }
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
      say("Workouts under a minute aren't saved — keep going or press again after 1:00.");
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
    report("click", "Connect strap");
    try {
      status.textContent = "Choose your strap…";
      device = await navigator.bluetooth.requestDevice({
        filters: [{ services: ["heart_rate"] }, { namePrefix: "WHOOP" }],
        optionalServices: ["heart_rate", whoop.service],
      });
      device.addEventListener("gattserverdisconnected", () => {
        stopAutopilot();
        flushSamples();
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
      report("connected", `${device.name || "?"} · ${mode}`);
      if (whoopCmd) startAutopilot();
      recent = [];
      show(true);
      describe();
    } catch (err) {
      report("connect_failed", `${err && err.name}: ${err && err.message}`);
      show(false);
      status.textContent =
        err && err.name === "NotFoundError"
          ? "No strap chosen — is it charged, nearby, and the WHOOP app closed?"
          : `Couldn't connect: ${(err && err.message) || err}`;
    }
  });

  disconnectBtn.addEventListener("click", async () => {
    stopAutopilot();
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


// Today: reads what the local server has stored and shows it. Everything is
// built with textContent / createElement(NS); nothing from the data is ever
// parsed as HTML.
(() => {
  const root = document.getElementById("data");
  if (!root) return;
  const $ = (id) => document.getElementById(id);
  const NS = "http://www.w3.org/2000/svg";
  const el = (tag, cls, text) => {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  };
  const sv = (tag, attrs) => {
    const e = document.createElementNS(NS, tag);
    for (const k in attrs) e.setAttribute(k, attrs[k]);
    return e;
  };
  const api = async (path, body) => {
    const res = body === undefined ? await fetch(path) : await postJSON(path, body);
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || `server said ${res.status}`);
    return data;
  };
  const pad = (n) => String(n).padStart(2, "0");
  const localDay = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  const hhmm = (ms) => {
    const d = new Date(ms);
    return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  };
  const clamp = (v, lo = 0, hi = 1) => Math.max(lo, Math.min(hi, v));
  // The server always sends its numbers, but a missing one must never take the whole panel down.
  const n0 = (v) => (Number.isFinite(v) ? v : (v ?? 0));
  const ZONE_COLOR = ["var(--z0)", "var(--z1)", "var(--z2)", "var(--z3)", "var(--z4)", "var(--z5)"];
  const ZONE_NAME = ["Rest", "Zone 1 · warm-up", "Zone 2 · light", "Zone 3 · moderate", "Zone 4 · hard", "Zone 5 · max"];
  const zoneOf = (hr, max) => {
    if (!max) return 0;
    const x = hr / max;
    return x >= 0.9 ? 5 : x >= 0.8 ? 4 : x >= 0.7 ? 3 : x >= 0.6 ? 2 : x >= 0.5 ? 1 : 0;
  };

  // ---- rings
  const CIRC = 2 * Math.PI * 48;
  const buildRing = (id, label) => {
    const fig = el("figure", "ring");
    fig.id = id;
    const centre = el("div", "ring-centre");
    const svg = sv("svg", { viewBox: "0 0 120 120", "aria-hidden": "true" });
    svg.append(sv("circle", { class: "track", cx: 60, cy: 60, r: 48 }), sv("circle", { class: "arc", cx: 60, cy: 60, r: 48, "stroke-dasharray": CIRC, "stroke-dashoffset": CIRC }));
    const text = el("div", "ring-text");
    text.append(el("span", "ring-big", "—"), el("span", "ring-unit", ""));
    centre.append(svg, text);
    const cap = el("figcaption", null, label);
    cap.append(el("small", null, ""));
    fig.append(centre, cap);
    return fig;
  };
  const setRing = (id, frac, color, big, unit, small) => {
    const fig = $(id);
    const arc = fig.querySelector(".arc");
    arc.setAttribute("stroke", color);
    arc.setAttribute("stroke-dashoffset", String(CIRC * (1 - clamp(frac))));
    fig.querySelector(".ring-big").textContent = big;
    fig.querySelector(".ring-unit").textContent = unit;
    fig.querySelector("small").textContent = small;
  };
  const hero = $("data-hero");
  hero.append(buildRing("ring-strain", "Strain"), buildRing("ring-recovery", "Recovery"), buildRing("ring-hr", "Heart rate"));

  // ---- chart
  let chartState = null;
  const drawChart = (series, from, to, hrMax) => {
    const svg = $("data-svg");
    svg.replaceChildren();
    const cap = $("data-cap");
    cap.replaceChildren();
    chartState = null;
    $("data-chart").classList.toggle("empty", !series.t.length);
    if (!series.t.length) {
      cap.append(el("span", null, "No heart rate stored today yet. Connect the strap to start recording."));
      return;
    }
    const W = 600, H = 160, padL = 26, padB = 14;
    const lo = Math.max(30, Math.floor(Math.min(...series.hr, hrMax ? hrMax * 0.5 : 999) / 10) * 10 - 5);
    const hi = Math.max(Math.ceil(Math.max(...series.hr) / 10) * 10 + 5, hrMax ? Math.min(hrMax, Math.max(...series.hr) + 25) : 0);
    const x = (t) => padL + ((t - from) / (to - from)) * (W - padL);
    const y = (v) => H - padB - ((v - lo) / (hi - lo)) * (H - padB - 6);
    if (hrMax) {
      // zone bands behind the line
      const edges = [0.5, 0.6, 0.7, 0.8, 0.9, 1.0].map((f) => f * hrMax);
      for (let z = 1; z <= 5; z++) {
        const top = Math.min(hi, edges[z]);
        const bot = Math.max(lo, edges[z - 1]);
        if (top > bot) svg.append(sv("rect", { x: padL, y: y(top), width: W - padL, height: y(bot) - y(top), fill: ZONE_COLOR[z], opacity: 0.1 }));
      }
      edges.slice(0, 5).forEach((e, k) => {
        if (e > lo && e < hi) {
          svg.append(sv("line", { x1: padL, x2: W, y1: y(e), y2: y(e), stroke: "var(--grid)", "stroke-width": 0.5, "vector-effect": "non-scaling-stroke" }));
          svg.append(sv("text", { x: 2, y: y(e) + 3 }, ));
          svg.lastChild.textContent = String(Math.round(e));
        }
      });
    }
    // x ticks every 1-6 hours
    const hours = (to - from) / 3600000;
    const step = hours <= 3 ? 1 : hours <= 8 ? 2 : hours <= 14 ? 3 : 6;
    const d0 = new Date(from);
    const first = new Date(d0.getFullYear(), d0.getMonth(), d0.getDate(), Math.ceil((d0.getHours() + d0.getMinutes() / 60) / step) * step).getTime();
    for (let t = first; t < to; t += step * 3600000) {
      svg.append(sv("text", { x: x(t) - 8, y: H - 2 }));
      svg.lastChild.textContent = hhmm(t);
    }
    // line, split where the strap went quiet
    const gap = ((to - from) / 300) * 4;
    let seg = [];
    const segs = [];
    series.t.forEach((t, i) => {
      if (seg.length && t - series.t[i - 1] > gap) {
        segs.push(seg);
        seg = [];
      }
      seg.push([x(t), y(series.hr[i])]);
    });
    segs.push(seg);
    for (const sg of segs) {
      if (sg.length < 2) {
        svg.append(sv("circle", { cx: sg[0][0], cy: sg[0][1], r: 1.5, fill: "var(--series)" }));
        continue;
      }
      const pts = sg.map((p) => `${p[0].toFixed(1)},${p[1].toFixed(1)}`).join(" ");
      svg.append(sv("polygon", { points: `${sg[0][0].toFixed(1)},${H - padB} ${pts} ${sg[sg.length - 1][0].toFixed(1)},${H - padB}`, fill: "var(--series)", opacity: 0.1 }));
      svg.append(sv("polyline", { points: pts, fill: "none", stroke: "var(--series)", "stroke-width": 1.6, "vector-effect": "non-scaling-stroke", "stroke-linejoin": "round" }));
    }
    const cross = sv("line", { y1: 0, y2: H - padB, stroke: "var(--ink-2)", "stroke-width": 0.8, "vector-effect": "non-scaling-stroke", visibility: "hidden" });
    const dot = sv("circle", { r: 3.5, fill: "var(--series)", stroke: "var(--surface)", "stroke-width": 1.5, visibility: "hidden", "vector-effect": "non-scaling-stroke" });
    svg.append(cross, dot);
    chartState = { series, from, to, x, y, hrMax, W, cross, dot, idx: -1 };
    const lowHr = Math.round(Math.min(...series.hr)), highHr = Math.round(Math.max(...series.hr));
    cap.append(el("span", null, `${lowHr}–${highHr} bpm`), el("span", null, `${hhmm(from)} → ${hhmm(to)}`));
    svg.setAttribute("aria-label", `Heart rate today, ${lowHr} to ${highHr} beats per minute. Use the arrow keys to read values.`);
  };
  const showAt = (i) => {
    const cs = chartState;
    if (!cs || i < 0 || i >= cs.series.t.length) return;
    cs.idx = i;
    const px = cs.x(cs.series.t[i]);
    cs.cross.setAttribute("x1", px);
    cs.cross.setAttribute("x2", px);
    cs.cross.setAttribute("visibility", "visible");
    cs.dot.setAttribute("cx", px);
    cs.dot.setAttribute("cy", cs.y(cs.series.hr[i]));
    cs.dot.setAttribute("visibility", "visible");
    const tip = $("data-tip");
    const z = zoneOf(cs.series.hr[i], cs.hrMax);
    tip.textContent = `${hhmm(cs.series.t[i])} · ${Math.round(cs.series.hr[i])} bpm${cs.hrMax ? ` · ${ZONE_NAME[z]}` : ""}`;
    tip.hidden = false;
    tip.style.left = `${clamp((px / cs.W) * 100, 12, 88)}%`;
  };
  const hideTip = () => {
    if (!chartState) return;
    chartState.cross.setAttribute("visibility", "hidden");
    chartState.dot.setAttribute("visibility", "hidden");
    $("data-tip").hidden = true;
    chartState.idx = -1;
  };
  const chartSvg = $("data-svg");
  chartSvg.addEventListener("pointermove", (ev) => {
    const cs = chartState;
    if (!cs) return;
    const box = chartSvg.getBoundingClientRect();
    const vx = ((ev.clientX - box.left) / box.width) * cs.W;
    let best = 0;
    let dist = Infinity;
    cs.series.t.forEach((t, i) => {
      const dd = Math.abs(cs.x(t) - vx);
      if (dd < dist) (dist = dd), (best = i);
    });
    showAt(best);
  });
  chartSvg.addEventListener("pointerleave", hideTip);
  chartSvg.addEventListener("blur", hideTip);
  chartSvg.addEventListener("keydown", (ev) => {
    const cs = chartState;
    if (!cs) return;
    const n = cs.series.t.length;
    if (ev.key === "ArrowLeft" || ev.key === "ArrowRight" || ev.key === "Home" || ev.key === "End") {
      ev.preventDefault();
      const next = ev.key === "Home" ? 0 : ev.key === "End" ? n - 1 : clamp((cs.idx < 0 ? n - 1 : cs.idx) + (ev.key === "ArrowLeft" ? -1 : 1), 0, n - 1);
      showAt(next);
    } else if (ev.key === "Escape") hideTip();
  });

  // ---- zones bar
  const drawZones = (zones) => {
    const box = $("data-zones");
    const total = zones.reduce((a, b) => a + b, 0);
    box.hidden = !(total > 0);
    if (box.hidden) return;
    const bar = $("data-zonebar");
    bar.replaceChildren();
    zones.forEach((m, z) => {
      if (m <= 0) return;
      const seg = el("span");
      seg.style.flexGrow = String(m);
      seg.style.background = ZONE_COLOR[z];
      seg.title = `${ZONE_NAME[z]}: ${Math.round(m)} min`;
      bar.append(seg);
    });
    const legend = $("data-zone-legend");
    legend.replaceChildren();
    zones.forEach((m, z) => {
      if (m < 0.5) return;
      const li = el("li");
      const sw = el("i");
      sw.style.background = ZONE_COLOR[z];
      li.append(sw, document.createTextNode(`${ZONE_NAME[z].split(" · ")[0]} ${Math.round(m)}m`));
      legend.append(li);
    });
  };

  // ---- metric cards
  const spark = (days, key, have) => {
    const pts = days.filter((d) => d[have]).map((d) => d[key]);
    if (pts.length < 2) return null;
    const lo = Math.min(...pts), hi = Math.max(...pts);
    const svg = sv("svg", { class: "spark", viewBox: "0 0 100 28", preserveAspectRatio: "none", "aria-hidden": "true" });
    const path = pts.map((v, i) => `${(i / (pts.length - 1)) * 100},${26 - ((v - lo) / (hi - lo || 1)) * 22}`).join(" ");
    svg.append(sv("polyline", { points: path, fill: "none", stroke: "var(--series)", "stroke-width": 1.5, "vector-effect": "non-scaling-stroke", "stroke-linejoin": "round" }));
    return svg;
  };
  const card = (name, chip, value, unit, note, sparkEl) => {
    const c = el("div", "metric");
    const top = el("div", "metric-top");
    top.append(el("span", "metric-name", name));
    if (chip) top.append(el("span", `chip ${chip.cls}`, chip.text));
    c.append(top);
    const v = el("div", value == null ? "metric-value dim" : "metric-value", value == null ? "No data yet" : String(value));
    if (value != null && unit) v.append(el("small", null, unit));
    c.append(v);
    if (sparkEl) c.append(sparkEl);
    if (note) c.append(el("p", "metric-note", note));
    return c;
  };
  const CHIP = { provisional: { cls: "provisional", text: "Early" }, full: { cls: "full", text: "Ready" } };

  // Last night's sleep, from /api/sleep. Both numbers are estimates and they are shown side by side
  // on purpose: one is scored from the strap's motion with published methods, the other is the
  // strap's own label. Neither is a lab measurement.
  const EST = { cls: "provisional", text: "Estimate" };
  const sleepCards = (s) => {
    const m = s && s.motion, dv = s && s.device;
    const t = (iso) => hhmm(Date.parse(iso));
    const cards = [];
    if (m) {
      const gap = dv ? Math.abs(m.asleep_hours - dv.sleep_hours) : 0;
      cards.push(card("Sleep · from motion", EST, n0(m.asleep_hours).toFixed(1), "h",
        `in bed ${t(m.window.start)}–${t(m.window.end)} · ${Math.round(n0(m.efficiency) * 100)}% efficiency · ${Math.round(n0(m.awake_minutes))} min awake · ${m.awakenings} awakenings` +
          (gap >= 0.5 ? ` · differs from the strap's label by ${gap.toFixed(1)} h` : ""), null));
    }
    if (dv) cards.push(card("Sleep · strap's label", EST, n0(dv.sleep_hours).toFixed(1), "h", `${t(dv.first_sleep)}–${t(dv.last_sleep)} · the strap's own sleep state`, null));
    if (m && s.lowest_hr) cards.push(card("Lowest heart rate", null, Math.round(s.lowest_hr), "bpm", `lowest 5-minute median · ${Math.round(n0(s.average_hr))} bpm average while asleep`, null));
    const g = s && s.stages;
    if (g && g.totals && g.totals.tst_min > 0) {
      const tt = g.totals;
      const hm = (min) => `${Math.floor(min / 60)}:${pad(Math.round(min % 60))}`;
      cards.push(card("Deep sleep", EST, hm(n0(tt.deep_min)), "h", `${Math.round(n0(tt.deep_pct))}% of sleep · from heart rate, RR and motion`, null));
      cards.push(card("REM sleep", EST, hm(n0(tt.rem_min)), "h", `${Math.round(n0(tt.rem_pct))}% of sleep` + (tt.have_rem ? ` · first REM ${Math.round(n0(tt.rem_latency_min))} min after sleep onset` : " · none detected"), null));
      cards.push(card("Light sleep", EST, hm(n0(tt.light_min)), "h", `${Math.round(n0(tt.light_pct))}% of sleep · the default when evidence is weak`, null));
      cards.push(card("Sleep cycles", EST, n0(tt.cycles), "", `asleep ${hhmm(Date.parse(tt.sleep_onset))}–${hhmm(Date.parse(tt.final_wake))} · ${Math.round(n0(tt.waso_min))} min awake after onset`, null));
    }
    return cards;
  };

  // Hypnogram: one bar per run of equal stages, on the row of its stage. Runs below the
  // confidence flag are faded. Hover (or long-press) a bar for its times; the totals cards
  // are the table view.
  const STAGE_ROW = { wake: 0, rem: 1, light: 2, deep: 3 };
  const STAGE_NAME = { wake: "Wake", rem: "REM", light: "Light", deep: "Deep", nodata: "No data" };
  const STAGE_COLOR = { wake: "var(--st-wake)", rem: "var(--st-rem)", light: "var(--st-light)", deep: "var(--st-deep)" };
  const renderHypno = (s) => {
    const fig = $("data-hypno");
    const g = s && s.stages;
    if (!g || !g.runs || !g.runs.length) {
      fig.hidden = true;
      return;
    }
    fig.hidden = false;
    const t0 = Date.parse(g.start), t1 = Date.parse(g.end);
    const span = t1 - t0 || 1;
    const svg = $("data-hypno-svg");
    const rowH = 24;
    const kids = [];
    for (let r = 0; r < 4; r++) kids.push(sv("line", { x1: 0, x2: 600, y1: r * rowH + rowH / 2, y2: r * rowH + rowH / 2, stroke: "var(--grid)", "stroke-width": 1, "vector-effect": "non-scaling-stroke" }));
    for (const run of g.runs) {
      if (!(run.stage in STAGE_ROW)) continue;
      const a = Date.parse(run.start), b = Date.parse(run.end);
      const x = ((a - t0) / span) * 600, w = Math.max(0.6, ((b - a) / span) * 600);
      const rect = sv("rect", { x, y: STAGE_ROW[run.stage] * rowH + 4, width: w, height: rowH - 8, rx: 2, fill: STAGE_COLOR[run.stage] });
      if (run.confidence < 0.6) rect.setAttribute("class", "low");
      const tip = sv("title", {});
      tip.textContent = `${STAGE_NAME[run.stage]} ${hhmm(a)}–${hhmm(b)} · ${Math.round((b - a) / 60000)} min · confidence ${Math.round(run.confidence * 100)}%`;
      rect.append(tip);
      kids.push(rect);
    }
    svg.replaceChildren(...kids);
    const tt = g.totals || {};
    svg.setAttribute("aria-label", `Estimated sleep stages ${hhmm(t0)} to ${hhmm(t1)}: deep ${Math.round(n0(tt.deep_min))} min, REM ${Math.round(n0(tt.rem_min))} min, light ${Math.round(n0(tt.light_min))} min, awake ${Math.round(n0(tt.wake_min))} min`);
    const axis = $("data-hypno-axis");
    const ticks = [];
    for (let i = 0; i <= 4; i++) ticks.push(el("span", null, hhmm(t0 + (span * i) / 4)));
    axis.replaceChildren(...ticks);
    const legend = $("data-hypno-legend");
    legend.replaceChildren(...["wake", "rem", "light", "deep"].map((k) => {
      const li = el("li");
      const sw = el("i");
      sw.style.background = STAGE_COLOR[k];
      const mins = { wake: tt.wake_min, rem: tt.rem_min, light: tt.light_min, deep: tt.deep_min }[k];
      li.append(sw, `${STAGE_NAME[k]} ${Math.round(n0(mins))} min`);
      return li;
    }), el("li", null, "faded = low confidence"));
    const failed = (g.checks || []).filter((c) => !c.pass).map((c) => c.name);
    $("data-hypno-note").textContent =
      `Estimate, confidence ${g.confidence}. ${g.method}. Not validated against a sleep lab; published wrist trackers with trained models agree with polysomnography on about 60–70% of 30-second epochs (kappa 0.4–0.5), and hand-made rules like these should be expected to do worse.` +
      (failed.length ? ` Sanity checks not met tonight: ${failed.join("; ")}.` : "");
  };

  // Battery matters more than it looks: if the strap dies it loses its clock and stops recording.
  // The level comes from the strap's own log, so it is as fresh as the last history sync.
  const batteryCard = (b) => {
    if (!b || !b.have) return null;
    const low = !b.charging && (b.soc < 20 || (b.hours_left > 0 && b.hours_left < 12));
    const chip = b.charging ? { cls: "full", text: "Charging" } : low ? { cls: "low", text: "Charge soon" } : null;
    const bits = [`as of ${hhmm(Date.parse(b.at))}, the last sync`];
    if (b.hours_left > 0) bits.push(`about ${Math.round(b.hours_left)} h left at ${n0(b.drain_per_hour).toFixed(1)}%/h (estimate)`);
    else if (b.charging) bits.push("level was rising");
    return { low, el: card("Strap battery", chip, Math.round(b.soc), "%", bits.join(" · "), null) };
  };

  const renderMetrics = (st, d, trend, sleep, battery) => {
    const by = Object.fromEntries(st.readiness.map((m) => [m.metric, m]));
    const chip = (k) => (by[k] && by[k].state !== "blocked" && by[k].state !== "calibrating" ? CHIP[by[k].state] : null);
    const days = trend ? trend.days : [];
    const cards = sleepCards(sleep);
    renderHypno(sleep);
    const night = d.night, live = d.live;
    const sleepNote = $("data-sleep-note");
    sleepNote.hidden = !cards.length;
    if (cards.length) sleepNote.textContent = sleep.note;

    if (night.have_hrv) cards.push(card("HRV", chip("hrv"), n0(night.rmssd).toFixed(0), "ms", `overnight · RMSSD · ${n0(night.rr_pairs)} beat pairs`, spark(days, "rmssd", "have_hrv")));
    else if (live.have_hrv) cards.push(card("HRV", chip("hrv"), n0(live.rmssd).toFixed(0), "ms", `live RMSSD · from ${Math.round(n0(live.rr_coverage) * 100)}% of beats. Overnight is more reliable`, spark(days, "rmssd", "have_hrv")));
    else cards.push(card("HRV", null, null, "", live.rr_beats ? `only ${live.rr_beats} beats with RR so far (needs about 30 in a row)` : "needs RR intervals from the strap", null));

    if (night.have_resting_hr) cards.push(card("Resting heart rate", chip("resting_hr"), Math.round(n0(night.resting_hr)), "bpm", "overnight · lowest 5 minutes", spark(days, "resting_hr", "have_resting_hr")));
    else if (live.have_resting_hr) cards.push(card("Resting heart rate", chip("resting_hr"), Math.round(n0(live.resting_hr)), "bpm", "lowest 5 minutes today, awake · sleep gives a truer value", spark(days, "resting_hr", "have_resting_hr")));
    else cards.push(card("Resting heart rate", null, null, "", "needs 5 quiet minutes with the strap on", null));

    if (night.have_resp) cards.push(card("Respiratory rate", chip("respiratory_rate"), n0(night.resp_rate).toFixed(1), "/min", "overnight", null));
    else if (live.have_resp) cards.push(card("Respiratory rate", chip("respiratory_rate"), n0(live.resp_rate).toFixed(1), "/min", "live · from a clean RR run", null));
    else cards.push(card("Respiratory rate", null, null, "", "needs two minutes of unbroken RR", null));

    if (live.have_vo2) cards.push(card("Fitness (VO2max est.)", chip("fitness"), n0(live.vo2max).toFixed(0), "ml/kg/min", "estimate: max HR ÷ resting HR · rough, ±5", null));

    const worn = n0(d.minutes) / 60;
    cards.push(card("Time worn", null, worn.toFixed(1), "h", n0(d.off_wrist_minutes) >= 1 ? `${Math.round(d.off_wrist_minutes)} min off the wrist` : "no time off the wrist recorded", spark(days.map((x) => ({ ...x, h: x.worn_minutes / 60, ok: x.worn_minutes > 0 })), "h", "ok")));
    if (d.samples) cards.push(card("Heart rate", null, Math.round(n0(d.avg_hr)), "bpm avg", `${Math.round(n0(d.min_hr))}–${Math.round(n0(d.max_hr))} bpm today`, null));
    const bat = batteryCard(battery);
    if (bat) bat.low ? cards.unshift(bat.el) : cards.push(bat.el);
    $("data-metrics").replaceChildren(...cards);

    // Metrics that cannot run yet: one tidy list, not a wall of cards.
    const haveSleep = !!(sleep && sleep.motion); // the readiness list predates motion decoding; sleep totals exist now
    const locked = st.readiness.filter((m) => (m.state === "blocked" && !(m.metric === "sleep" && haveSleep)) || (m.metric === "recovery" && m.state === "calibrating"));
    const box = $("data-locked");
    box.hidden = locked.length === 0;
    if (locked.length) {
      $("data-locked-sum").textContent = `${locked.length} more metric${locked.length === 1 ? "" : "s"} unlock with more data`;
      $("data-locked-list").replaceChildren(
        ...locked.map((m) => {
          const li = el("li");
          li.append(el("b", null, m.label), document.createTextNode(` · ${m.note || `${m.have} of ${m.provisional} ${m.unit} so far`}`));
          return li;
        })
      );
    }
  };

  // ---- hero rings
  const renderHero = (d, health, rec, st) => {
    if (d.have_strain) setRing("ring-strain", n0(d.strain) / 21, "var(--accent)", n0(d.strain).toFixed(1), "of 21", `load ${Math.round(n0(d.edwards))}`);
    else setRing("ring-strain", 0, "var(--accent)", "—", "", d.samples ? "needs your date of birth" : "no data today yet");

    if (rec) {
      const color = rec.Tier === "high" ? "var(--good)" : rec.Tier === "moderate" ? "var(--warning)" : "var(--critical)";
      setRing("ring-recovery", n0(rec.Score) / 100, color, String(Math.round(n0(rec.Score))), "%", rec.Full ? rec.Tier : `${rec.Tier} · calibrating`);
    } else {
      const r = st.readiness.find((m) => m.metric === "recovery");
      setRing("ring-recovery", 0, "var(--grid)", "—", "", r && r.state === "blocked" ? "needs a night of data" : `${r ? r.have : 0} of ${r ? r.provisional : 7} nights`);
    }

    const fresh = health.latest_sample_age_seconds != null && health.latest_sample_age_seconds <= 30;
    if (fresh && health.on_wrist === false) setRing("ring-hr", 0, "var(--grid)", "off", "", "strap is off the wrist");
    else if (fresh && health.latest_hr) {
      const z = zoneOf(health.latest_hr, d.hr_max_used);
      setRing("ring-hr", d.hr_max_used ? health.latest_hr / d.hr_max_used : 0.5, ZONE_COLOR[z], String(health.latest_hr), "bpm", d.hr_max_used ? ZONE_NAME[z] : "live");
    } else setRing("ring-hr", 0, "var(--grid)", "—", "", "no live signal");
  };

  const renderStatusBar = (st, health, hist) => {
    const sub = $("data-sub");
    const midnight = new Date(new Date().getFullYear(), new Date().getMonth(), new Date().getDate()).getTime();
    if (st.day_zero) {
      const n = Math.floor((midnight - new Date(`${st.day_zero}T00:00:00`).getTime()) / 86400000) + 1;
      sub.textContent = `Day ${n} · since ${st.day_zero} · ${(st.db_bytes / 1024).toFixed(0)} KB stored`;
    } else sub.textContent = "Nothing stored yet. Connect your strap and it starts recording from today.";
    const live = $("data-live");
    const label = live.querySelector("span");
    const age = health.latest_sample_age_seconds;
    if (age == null) {
      live.className = "pill";
      label.textContent = "No data yet";
    } else if (age <= 15) {
      live.className = "pill live";
      label.textContent = "Live";
    } else {
      live.className = "pill stale";
      label.textContent = age < 90 ? `Last data ${age}s ago` : `No signal since ${hhmm(health.latest_sample * 1000)}`;
    }
    const wrist = $("data-wrist");
    wrist.hidden = !(age != null && age <= 30);
    if (!wrist.hidden) {
      wrist.className = health.on_wrist === false ? "pill off" : "pill";
      wrist.textContent = health.on_wrist === false ? "Off wrist" : "On wrist";
    }
    $("data-sync").textContent = hist.synced_at ? `History synced ${new Date(hist.synced_at * 1000).toLocaleString([], { dateStyle: "medium", timeStyle: "short" })}` : "No history downloaded yet";
  };

  // ---- profile (age is known; sex is optional)
  let profile = {};
  const renderProfile = () => {
    const line = $("data-profile");
    line.replaceChildren();
    if (profile.birth_date) {
      const b = new Date(`${profile.birth_date}T00:00:00`);
      const now = new Date();
      let age = now.getFullYear() - b.getFullYear();
      if (now < new Date(now.getFullYear(), b.getMonth(), b.getDate())) age--;
      line.append(document.createTextNode(`Age ${age}  ·  Sex`));
    } else line.append(document.createTextNode("Add your date of birth with the API (POST /api/profile)  ·  Sex"));
    for (const sex of ["male", "female"]) {
      const b = el("button", null, sex === "male" ? "Male" : "Female");
      b.type = "button";
      b.dataset.sex = sex;
      b.setAttribute("aria-pressed", String(profile.sex === sex));
      line.append(b);
    }
    line.append(el("span", null, profile.sex ? "" : "optional · adds a second load measure"));
  };
  $("data-profile").addEventListener("click", async (ev) => {
    const b = ev.target.closest("button[data-sex]");
    if (!b) return;
    try {
      profile = await api("/api/profile", { sex: b.dataset.sex });
      renderProfile();
      refresh(true);
    } catch (e) {
      showErr(`Couldn't save: ${e.message}`);
    }
  });

  const showErr = (msg) => {
    const err = $("data-err");
    err.textContent = msg;
    err.hidden = !msg;
  };

  // ---- refresh loop
  let busy = false;
  let tick = 0;
  let trend = null;
  let hist = {};
  let sleep = null;
  let battery = null;
  const refresh = async (force) => {
    if (busy) return;
    busy = true;
    try {
      const now = new Date();
      const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
      const slow = force || tick % 6 === 0;
      const [st, dayRes, health] = await Promise.all([api("/api/status"), api(`/api/day?date=${localDay(now)}`), api("/api/health")]);
      const d = dayRes.summary;
      // Chart the window the data actually covers, so a short recording is a line, not one dot.
      const start = d.first_sample_ms ? Math.max(midnight, d.first_sample_ms - 60000) : midnight;
      const needTrend = slow || !trend;
      const [series, tr, h, pr, sl, bt] = await Promise.all([
        api(`/api/samples?from=${start}&to=${now.getTime()}&points=300`),
        needTrend ? api("/api/trend?days=14") : Promise.resolve(trend),
        slow ? api("/api/history/status") : Promise.resolve(hist),
        slow || !profile.birth_date ? api("/api/profile") : Promise.resolve(profile),
        slow || !sleep ? api(`/api/sleep?date=${localDay(now)}`).catch(() => null) : Promise.resolve(sleep), // sleep is optional: never break the dashboard
        slow || !battery ? api("/api/battery").catch(() => null) : Promise.resolve(battery),
      ]);
      sleep = sl;
      battery = bt;
      trend = tr;
      hist = h;
      profile = pr;
      showErr("");
      renderStatusBar(st, health, hist);
      renderHero(d, health, dayRes.recovery, st);
      drawChart(series, start, now.getTime(), d.hr_max_used);
      drawZones(d.zones_minutes || []);
      renderMetrics(st, d, trend, sleep, battery);
      renderProfile();
      root.setAttribute("aria-busy", "false");
    } catch (e) {
      showErr(`Couldn't read stored data: ${e.message}`);
      root.setAttribute("aria-busy", "false");
    } finally {
      busy = false;
      tick++;
    }
  };

  refresh(true);
  setInterval(refresh, 5000);
})();
