import { createTrip, addFix, tripMiles } from "../lib/geo.js";
import { showToast } from "./toast.js";

// Module-level state so a trip keeps recording across tab switches (the panel
// is only hidden, not unmounted) but is intentionally lost on page reload —
// this is a foreground-only POC, not a background tracking service.
let watchId = null;
let tripState = null;
let startTimeMs = null;
let wakeLock = null;
let tickIntervalId = null;

function formatDuration(ms) {
  const totalSeconds = Math.floor(ms / 1000);
  const m = Math.floor(totalSeconds / 60);
  const s = totalSeconds % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}

function updateStats(container) {
  const milesEl = container.querySelector("#trip-miles");
  if (!milesEl) return; // panel was re-rendered elsewhere; stale reference
  const miles = tripState ? tripMiles(tripState) : 0;
  milesEl.textContent = miles.toFixed(2);
  container.querySelector("#trip-fixes").textContent = tripState ? tripState.fixesUsed : 0;
  container.querySelector("#trip-duration").textContent = startTimeMs ? formatDuration(Date.now() - startTimeMs) : "0:00";
}

function startTicker(container) {
  if (tickIntervalId) clearInterval(tickIntervalId);
  tickIntervalId = setInterval(() => updateStats(container), 1000);
}

function stopTicker() {
  if (tickIntervalId) clearInterval(tickIntervalId);
  tickIntervalId = null;
}

function showError(container, message) {
  const el = container.querySelector("#trip-error");
  el.textContent = message;
  el.hidden = false;
}

function startTrip(app, container) {
  if (!("geolocation" in navigator)) {
    showError(container, "Geolocation isn't available on this device/browser.");
    return;
  }
  tripState = createTrip();
  startTimeMs = Date.now();
  watchId = navigator.geolocation.watchPosition(
    (pos) => {
      tripState = addFix(tripState, {
        lat: pos.coords.latitude,
        lon: pos.coords.longitude,
        accuracyM: pos.coords.accuracy ?? 999,
        timestampMs: pos.timestamp,
      });
      updateStats(container);
    },
    (err) => showError(container, `Location error: ${err.message}`),
    { enableHighAccuracy: true, maximumAge: 0, timeout: 15000 }
  );
  if ("wakeLock" in navigator) {
    navigator.wakeLock
      .request("screen")
      .then((lock) => {
        wakeLock = lock;
      })
      .catch(() => {});
  }
  startTicker(container);
  render(container, app);
}

function stopTrip(app) {
  if (watchId != null) navigator.geolocation.clearWatch(watchId);
  if (wakeLock) {
    wakeLock.release().catch(() => {});
    wakeLock = null;
  }
  stopTicker();

  const miles = tripState ? tripMiles(tripState) : 0;
  watchId = null;
  tripState = null;
  startTimeMs = null;

  if (miles > 0.05) {
    const currentOdo = app.store.currentOdometer() ?? 0;
    // round to 0.1 mi so the odometer doesn't accumulate float noise
    const newOdo = Math.round((currentOdo + miles) * 10) / 10;
    app.store.addReading({ miles: newOdo, dateISO: app.todayISO(), source: "trip" });
    showToast(`Trip logged: ${miles.toFixed(1)} mi added to odometer`);
  } else {
    showToast("Trip too short to log");
  }
  app.refresh();
}

export function render(container, app) {
  container.innerHTML = `
    <div class="section-title">GPS trip</div>
    <div class="card">
      ${watchId ? '<div class="rec-indicator">Recording</div>' : ""}
      <p class="item-detail">Track a drive to log its miles onto your odometer automatically. Keep this tab open and your device unlocked while tracking — this POC doesn't support background tracking.</p>
      <div class="trip-stats">
        <div class="trip-stat"><div class="value" id="trip-miles">0.00</div><div class="label">Miles</div></div>
        <div class="trip-stat"><div class="value" id="trip-duration">0:00</div><div class="label">Duration</div></div>
        <div class="trip-stat"><div class="value" id="trip-fixes">0</div><div class="label">GPS fixes</div></div>
      </div>
      <div id="trip-error" class="banner warn" hidden></div>
      <button class="btn ${watchId ? "danger" : ""}" id="trip-toggle" style="width:100%;">${watchId ? "Stop & log trip" : "Start trip"}</button>
    </div>
  `;

  container.querySelector("#trip-toggle").addEventListener("click", () => {
    if (watchId) stopTrip(app);
    else startTrip(app, container);
  });

  if (watchId) {
    updateStats(container);
    startTicker(container);
  }
}
