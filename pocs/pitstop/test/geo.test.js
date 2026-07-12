import { test } from "node:test";
import assert from "node:assert/strict";
import { haversineMeters, createTrip, addFix, tripMiles, ACCURACY_MAX_M } from "../app/js/lib/geo.js";

test("haversineMeters matches a known distance (SF to LA, ~559 km)", () => {
  const sf = { lat: 37.7749, lon: -122.4194 };
  const la = { lat: 34.0522, lon: -118.2437 };
  const meters = haversineMeters(sf, la);
  const km = meters / 1000;
  assert.ok(km > 550 && km < 570, `expected ~559km, got ${km}`);
});

test("haversineMeters is zero for identical points", () => {
  const p = { lat: 40.0, lon: -75.0 };
  assert.equal(haversineMeters(p, p), 0);
});

test("addFix: first fix anchors the trip without adding distance", () => {
  let trip = createTrip();
  trip = addFix(trip, { lat: 40.0, lon: -75.0, accuracyM: 5, timestampMs: 0 });
  assert.equal(trip.meters, 0);
  assert.equal(trip.fixesUsed, 1);
  assert.ok(trip.lastFix);
});

test("addFix: drops fixes with poor accuracy", () => {
  let trip = createTrip();
  trip = addFix(trip, { lat: 40.0, lon: -75.0, accuracyM: ACCURACY_MAX_M + 1, timestampMs: 0 });
  assert.equal(trip.lastFix, null);
  assert.equal(trip.fixesDropped, 1);
});

test("addFix: drops stationary GPS jitter", () => {
  let trip = createTrip();
  trip = addFix(trip, { lat: 40.0, lon: -75.0, accuracyM: 5, timestampMs: 0 });
  // ~1 meter north: well under the jitter threshold
  trip = addFix(trip, { lat: 40.0 + 0.000009, lon: -75.0, accuracyM: 5, timestampMs: 1000 });
  assert.equal(trip.meters, 0);
  assert.equal(trip.fixesDropped, 1);
});

test("addFix: drops an implausible teleport but re-anchors", () => {
  let trip = createTrip();
  trip = addFix(trip, { lat: 40.0, lon: -75.0, accuracyM: 5, timestampMs: 0 });
  // ~100km jump in 1 second: not physically plausible
  trip = addFix(trip, { lat: 40.9, lon: -75.0, accuracyM: 5, timestampMs: 1000 });
  assert.equal(trip.meters, 0);
  assert.equal(trip.fixesDropped, 1);
  assert.ok(Math.abs(trip.lastFix.lat - 40.9) < 1e-9, "re-anchors to the glitched fix");
});

test("addFix: drops a duplicate/out-of-order fix (zero or negative dt) without adding distance", () => {
  let trip = createTrip();
  trip = addFix(trip, { lat: 40.0, lon: -75.0, accuracyM: 5, timestampMs: 1000 });
  // Same timestamp, ~10km away: can't be speed-checked, must not be trusted.
  trip = addFix(trip, { lat: 40.09, lon: -75.0, accuracyM: 5, timestampMs: 1000 });
  assert.equal(trip.meters, 0);
  assert.equal(trip.fixesDropped, 1);
  assert.ok(Math.abs(trip.lastFix.lat - 40.09) < 1e-9, "re-anchors to the new fix");
});

test("addFix: sums a realistic synthetic route", () => {
  let trip = createTrip();
  const fixes = [
    { lat: 40.0, lon: -75.0, accuracyM: 5, timestampMs: 0 },
    { lat: 40.0009, lon: -75.0, accuracyM: 5, timestampMs: 10000 }, // ~100m north, 10s later => 10 m/s
    { lat: 40.0018, lon: -75.0, accuracyM: 5, timestampMs: 20000 },
    { lat: 40.0027, lon: -75.0, accuracyM: 5, timestampMs: 30000 },
  ];
  for (const fix of fixes) trip = addFix(trip, fix);
  assert.equal(trip.fixesUsed, 4);
  assert.equal(trip.fixesDropped, 0);
  assert.ok(trip.meters > 250 && trip.meters < 350, `expected ~300m, got ${trip.meters}`);
});

test("tripMiles converts meters to miles", () => {
  const trip = { meters: 1609.344, fixesUsed: 0, fixesDropped: 0, lastFix: null };
  assert.ok(Math.abs(tripMiles(trip) - 1) < 1e-9);
});
