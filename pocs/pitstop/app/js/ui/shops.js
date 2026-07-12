import { buildQuery, parseShops, directionsUrl, OVERPASS_ENDPOINTS } from "../lib/overpass.js";

function escapeHtml(s) {
  return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function setStatus(container, message, kind = "info") {
  const el = container.querySelector("#shops-status");
  if (!message) {
    el.hidden = true;
    return;
  }
  el.hidden = false;
  el.className = `banner ${kind}`;
  el.textContent = message;
}

function renderShops(container, shops) {
  const list = container.querySelector("#shops-list");
  list.innerHTML = shops
    .map(
      (s, i) => `
    <div class="card shop-card" style="--i:${i}">
      <div class="shop-info">
        <div class="shop-name">${escapeHtml(s.name)}</div>
        <div class="shop-meta">${s.kind} · ${s.distanceMi.toFixed(1)} mi${s.address ? ` · ${escapeHtml(s.address)}` : ""}</div>
      </div>
      <a class="btn-small" href="${directionsUrl(s)}" target="_blank" rel="noopener noreferrer">Directions</a>
    </div>
  `
    )
    .join("");
}

async function findShops(container) {
  if (!navigator.onLine) {
    setStatus(container, "You're offline — nearby shops needs an internet connection.", "warn");
    return;
  }
  if (!("geolocation" in navigator)) {
    setStatus(container, "Geolocation isn't available on this device/browser.", "warn");
    return;
  }

  container.querySelector("#shops-list").innerHTML =
    '<div class="skeleton"></div><div class="skeleton"></div><div class="skeleton"></div>';
  setStatus(container, "Getting your location…");

  let position;
  try {
    position = await new Promise((resolve, reject) =>
      navigator.geolocation.getCurrentPosition(resolve, reject, { enableHighAccuracy: true, timeout: 15000 })
    );
  } catch (err) {
    container.querySelector("#shops-list").innerHTML = "";
    setStatus(container, `Couldn't get your location: ${err.message}`, "warn");
    return;
  }

  const origin = { lat: position.coords.latitude, lon: position.coords.longitude };
  const query = buildQuery(origin.lat, origin.lon);
  setStatus(container, "Searching nearby shops…");

  for (const endpoint of OVERPASS_ENDPOINTS) {
    try {
      const res = await fetch(endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: `data=${encodeURIComponent(query)}`,
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const json = await res.json();
      const shops = parseShops(json, origin);
      renderShops(container, shops);
      setStatus(container, shops.length ? "" : "No shops found nearby.", shops.length ? "info" : "warn");
      return;
    } catch {
      // fall through to the next mirror
    }
  }
  container.querySelector("#shops-list").innerHTML = "";
  setStatus(container, "Couldn't reach the shop lookup service. Try again in a moment.", "warn");
}

export function render(container) {
  container.innerHTML = `
    <div class="section-title">Nearby shops</div>
    <div class="card">
      <p class="item-detail">Find tire shops and auto repair near your current location, via OpenStreetMap.</p>
      <button class="btn" id="find-shops-btn" style="width:100%;">Find nearby shops</button>
      <div id="shops-status" class="banner info" hidden></div>
    </div>
    <div id="shops-list"></div>
  `;
  container.querySelector("#find-shops-btn").addEventListener("click", () => findShops(container));
}
