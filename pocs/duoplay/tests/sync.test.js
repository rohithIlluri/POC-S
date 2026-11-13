import test from 'node:test';
import assert from 'node:assert/strict';
import { estimateOffset, expectedPosition, wrapPosition, planStart, positionDiff } from '../web/sync.js';

test('estimateOffset recovers a known clock offset from symmetric pings', () => {
  // Server clock is 5000 ms ahead of ours; 20 ms each way.
  const samples = [0, 100, 200].map((t0) => ({ t0, ts: t0 + 20 + 5000, t1: t0 + 40 }));
  const { offset, rtt } = estimateOffset(samples);
  assert.equal(offset, 5000);
  assert.equal(rtt, 40);
});

test('estimateOffset prefers fast round trips over a jittery outlier', () => {
  const good = [0, 100, 200].map((t0) => ({ t0, ts: t0 + 10 + 1000, t1: t0 + 20 }));
  // One reply stuck in the network for 400 ms one way: its offset is badly wrong.
  const bad = { t0: 300, ts: 300 + 400 + 1000, t1: 300 + 410 };
  const { offset } = estimateOffset([bad, ...good]);
  assert.equal(offset, 1000);
});

test('estimateOffset with no samples returns null', () => {
  assert.equal(estimateOffset([]), null);
});

test('expectedPosition holds until the scheduled start, then advances', () => {
  const s = { playing: true, pos: 10, at: 1000 };
  assert.equal(expectedPosition(s, 500), 10); // announced, not started
  assert.equal(expectedPosition(s, 1000), 10);
  assert.equal(expectedPosition(s, 3500), 12.5);
  assert.equal(expectedPosition({ ...s, playing: false }, 9999), 10);
});

test('wrapPosition loops, clamps one-shots, and handles an empty track', () => {
  assert.equal(wrapPosition(2.5, 1, true), 0.5);
  assert.equal(wrapPosition(-0.25, 1, true), 0.75);
  assert.equal(wrapPosition(99, 10, false), 10);
  assert.equal(wrapPosition(5, 0, true), 0);
});

test('planStart keeps a future start and catches up a late one', () => {
  assert.deepEqual(planStart(2000, 1000, 3), { whenPerf: 2000, position: 3, lateSec: 0 });
  const late = planStart(1000, 1500, 3);
  assert.equal(late.whenPerf, 1500);
  assert.equal(late.position, 3.5);
  assert.equal(late.lateSec, 0.5);
});

test('positionDiff folds to the shortest way round a loop', () => {
  assert.ok(Math.abs(positionDiff(0.05, 0.95, 1, true) - 0.1) < 1e-9);
  assert.ok(Math.abs(positionDiff(0.95, 0.05, 1, true) + 0.1) < 1e-9);
  assert.equal(positionDiff(5, 3, 10, false), 2);
});
