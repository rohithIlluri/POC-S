import { haversineMeters } from "./geo.js";

export const OVERPASS_ENDPOINTS = [
  "https://overpass-api.de/api/interpreter",
  "https://overpass.kumi.systems/api/interpreter",
];
export const DEFAULT_RADIUS_M = 8000;
export const MAX_RESULTS = 20;

export function buildQuery(lat, lon, radiusM = DEFAULT_RADIUS_M) {
  const around = `around:${radiusM},${lat},${lon}`;
  return (
    "[out:json][timeout:25];" +
    "(" +
    `node["shop"="car_repair"](${around});` +
    `way["shop"="car_repair"](${around});` +
    `node["shop"="tyres"](${around});` +
    `way["shop"="tyres"](${around});` +
    ");" +
    "out center tags;"
  );
}

function composeAddress(tags) {
  const line1 = [tags["addr:housenumber"], tags["addr:street"]].filter(Boolean).join(" ");
  const parts = [line1, tags["addr:city"]].filter(Boolean);
  return parts.length ? parts.join(", ") : null;
}

function elementCoords(element) {
  if (element.type === "node") {
    if (typeof element.lat === "number" && typeof element.lon === "number") {
      return { lat: element.lat, lon: element.lon };
    }
    return null;
  }
  // way/relation: Overpass "out center" attaches a computed centroid.
  if (element.center && typeof element.center.lat === "number" && typeof element.center.lon === "number") {
    return element.center;
  }
  return null;
}

// origin: { lat, lon }. Returns shops sorted nearest-first, deduped, capped at MAX_RESULTS.
export function parseShops(overpassJson, origin) {
  const elements = Array.isArray(overpassJson?.elements) ? overpassJson.elements : [];
  const seen = new Set();
  const shops = [];

  for (const element of elements) {
    const coords = elementCoords(element);
    if (!coords) continue;

    const tags = element.tags || {};
    const name = tags.name || tags.brand || "Unnamed shop";
    const dedupeKey = `${name.toLowerCase()}|${coords.lat.toFixed(4)}|${coords.lon.toFixed(4)}`;
    if (seen.has(dedupeKey)) continue;
    seen.add(dedupeKey);

    shops.push({
      id: `${element.type}/${element.id}`,
      name,
      kind: tags.shop === "tyres" ? "Tire shop" : "Auto repair",
      address: composeAddress(tags),
      distanceMi: haversineMeters(origin, coords) / 1609.344,
      lat: coords.lat,
      lon: coords.lon,
    });
  }

  shops.sort((a, b) => a.distanceMi - b.distanceMi);
  return shops.slice(0, MAX_RESULTS);
}

export function directionsUrl(shop) {
  return `https://www.google.com/maps/dir/?api=1&destination=${shop.lat},${shop.lon}`;
}
