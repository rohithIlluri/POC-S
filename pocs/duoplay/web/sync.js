// Pure timing maths shared by the browser and the room server. No DOM, no clocks:
// everything takes the time as an argument so it can be unit tested.

/**
 * Estimate the offset between the server clock and ours from ping samples.
 * Each sample: { t0: our clock when we sent, ts: server clock when it replied,
 * t1: our clock when the reply arrived }. The sample with the shortest round
 * trip is the least distorted by network jitter, so we take the median offset
 * of the three fastest. offset = serverTime - ourTime.
 */
export function estimateOffset(samples) {
  if (!samples.length) return null;
  const ranked = samples
    .map(({ t0, ts, t1 }) => ({ rtt: t1 - t0, offset: ts - (t0 + t1) / 2 }))
    .sort((a, b) => a.rtt - b.rtt);
  const best = ranked
    .slice(0, Math.min(3, ranked.length))
    .map((s) => s.offset)
    .sort((a, b) => a - b);
  return {
    offset: best[Math.floor(best.length / 2)],
    rtt: ranked[0].rtt,
    samples: ranked.length,
  };
}

/**
 * Where the shared track should be at `now` (same clock as state.at).
 * state: { playing, pos (seconds), at (ms). Before `at` the play has been
 * announced but not started yet, so the position is still `pos`.
 */
export function expectedPosition(state, now) {
  if (!state.playing || now < state.at) return state.pos;
  return state.pos + (now - state.at) / 1000;
}

/** Keep a position inside the track: loops wrap, one-shots stop at the end. */
export function wrapPosition(pos, duration, loop) {
  if (!duration) return 0;
  if (loop) return ((pos % duration) + duration) % duration;
  return Math.min(Math.max(pos, 0), duration);
}

/**
 * Decide when and where to start a speaker. targetPerf is when the track's
 * `position` should be heard (already including the speaker's nudge). If that
 * moment has passed (late join, slow decode) start now, further into the track.
 */
export function planStart(targetPerf, nowPerf, position) {
  if (targetPerf >= nowPerf) return { whenPerf: targetPerf, position, lateSec: 0 };
  const lateSec = (nowPerf - targetPerf) / 1000;
  return { whenPerf: nowPerf, position: position + lateSec, lateSec };
}

/** Signed difference a - b between two positions, folded to the shortest way round a loop. */
export function positionDiff(a, b, duration, loop) {
  let d = a - b;
  if (loop && duration) {
    d = ((d % duration) + duration) % duration;
    if (d > duration / 2) d -= duration;
  }
  return d;
}
