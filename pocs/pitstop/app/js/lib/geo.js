const EARTH_RADIUS_M = 6371000;

// GPS fixes noisier than this (meters) are discarded outright.
export const ACCURACY_MAX_M = 25;
// A moved segment shorter than this (or half the fix's accuracy, whichever is
// larger) is treated as GPS jitter from a stationary device, not real movement.
const MIN_SEGMENT_M = 3;
// A segment implying a speed above this (~123 mph) is treated as a glitch; the
// distance is dropped but the fix still re-anchors the trip.
const MAX_SPEED_MPS = 55;

export function haversineMeters(a, b) {
  const toRad = (deg) => (deg * Math.PI) / 180;
  const dLat = toRad(b.lat - a.lat);
  const dLon = toRad(b.lon - a.lon);
  const lat1 = toRad(a.lat);
  const lat2 = toRad(b.lat);
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(lat1) * Math.cos(lat2) * Math.sin(dLon / 2) ** 2;
  return 2 * EARTH_RADIUS_M * Math.asin(Math.sqrt(h));
}

export function createTrip() {
  return { lastFix: null, meters: 0, fixesUsed: 0, fixesDropped: 0 };
}

// fix: { lat, lon, accuracyM, timestampMs }. Returns a new trip state; never
// mutates the one passed in.
export function addFix(trip, fix) {
  if (fix.accuracyM > ACCURACY_MAX_M) {
    return { ...trip, fixesDropped: trip.fixesDropped + 1 };
  }
  if (!trip.lastFix) {
    return { ...trip, lastFix: fix, fixesUsed: trip.fixesUsed + 1 };
  }

  const distanceM = haversineMeters(trip.lastFix, fix);
  const dtSeconds = (fix.timestampMs - trip.lastFix.timestampMs) / 1000;
  const jitterThresholdM = Math.max(MIN_SEGMENT_M, fix.accuracyM / 2);

  if (distanceM < jitterThresholdM) {
    return { ...trip, fixesDropped: trip.fixesDropped + 1 };
  }
  // A non-positive time delta (duplicate or out-of-order fix) can't be
  // speed-checked; treat it like a teleport: drop the distance but still
  // re-anchor to the new fix so a real glitch doesn't poison later segments.
  if (dtSeconds <= 0 || distanceM / dtSeconds > MAX_SPEED_MPS) {
    return { ...trip, lastFix: fix, fixesDropped: trip.fixesDropped + 1 };
  }

  return {
    ...trip,
    lastFix: fix,
    meters: trip.meters + distanceM,
    fixesUsed: trip.fixesUsed + 1,
  };
}

export function tripMiles(trip) {
  return trip.meters / 1609.344;
}
